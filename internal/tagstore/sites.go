package tagstore

import (
	"database/sql"
	"sort"
	"strings"
)

// 本文件负责「一个邮箱对应多个网站」的存储与迁移。
//
// 网站和标签一样是多值属性,存放在 entry_sites 表(与 entry_tags 同构)。
// entries.site 仍保留,写入时同步为「主网站」(Sites[0]),用于兼容旧备份与旧客户端。

// cloneSites 返回非 nil 的网站副本,保证序列化为 [] 而不是 null。
func cloneSites(sites []string) []string {
	if len(sites) == 0 {
		return []string{}
	}
	return append([]string(nil), sites...)
}

// primarySite 返回主网站(第一个),空列表返回空串。
func primarySite(sites []string) string {
	if len(sites) == 0 {
		return ""
	}
	return sites[0]
}

// mergeSites 把 add 合并进 existing,忽略大小写重复并保持顺序。
func mergeSites(existing, add []string) []string {
	out := cloneSites(existing)
	seen := make(map[string]struct{}, len(out)+len(add))
	for _, site := range out {
		seen[strings.ToLower(site)] = struct{}{}
	}
	for _, site := range add {
		key := strings.ToLower(strings.TrimSpace(site))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		if len(out) >= maxSites {
			break
		}
		seen[key] = struct{}{}
		out = append(out, strings.TrimSpace(site))
	}
	return out
}

// insertSites 写入一条记录的全部网站。
func insertSites(db execer, entryID string, sites []string) error {
	if len(sites) == 0 {
		return nil
	}
	stmt, err := db.Prepare(`INSERT INTO entry_sites(entry_id, site, site_lower, ord) VALUES(?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, site := range sites {
		if _, err := stmt.Exec(entryID, site, strings.ToLower(site), i); err != nil {
			return err
		}
	}
	return nil
}

// replaceSitesTx 先清空记录的所有网站再写入。
func replaceSitesTx(tx *sql.Tx, entryID string, sites []string) error {
	if _, err := tx.Exec(`DELETE FROM entry_sites WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	return insertSites(tx, entryID, sites)
}

// loadSites 按 ord 读取一条记录的网站。
func loadSites(db execer, entryID string) ([]string, error) {
	rows, err := db.Query(`SELECT site FROM entry_sites WHERE entry_id = ? ORDER BY ord`, entryID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	sites := make([]string, 0)
	for rows.Next() {
		var site string
		if err := rows.Scan(&site); err != nil {
			return nil, err
		}
		sites = append(sites, site)
	}
	return sites, rows.Err()
}

// sitesForIDs 批量读取若干记录的网站,按 ord 保持原始顺序。
func (s *Store) sitesForIDs(ids []string) (map[string][]string, error) {
	out := make(map[string][]string, len(ids))
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.Query(`SELECT entry_id, site FROM entry_sites
			WHERE entry_id IN (`+placeholders+`) ORDER BY entry_id, ord`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, site string
			if err := rows.Scan(&id, &site); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[id] = append(out[id], site)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// migrateSitesFromColumn 把旧版单值的 entries.site 回填到 entry_sites(幂等)。
func (s *Store) migrateSitesFromColumn() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`INSERT OR IGNORE INTO entry_sites(entry_id, site, site_lower, ord)
		SELECT id, site, lower(site), 0 FROM entries WHERE site <> ''`)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if affected > 0 {
		s.hardenFiles()
	}
	return affected, nil
}

// mergeDuplicateEmails 把同一邮箱的多条记录合并为一条(幂等)。
//
// 合并规则:保留创建时间最早的一条作为主记录,网站与标签取并集,
// 密码与备注沿用主记录,更新时间取各条最大值。用于把旧版
// 「一个邮箱 + 一个网站一行」的数据升级为「一个邮箱一行」。
func (s *Store) mergeDuplicateEmails() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT lower(email) FROM entries GROUP BY lower(email) HAVING COUNT(*) > 1`)
	if err != nil {
		return 0, err
	}
	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			_ = rows.Close()
			return 0, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	if len(keys) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	type record struct {
		id, site, createdAt, updatedAt string
	}

	merged := 0
	for _, key := range keys {
		recRows, err := tx.Query(`SELECT id, site, created_at, updated_at FROM entries
			WHERE lower(email) = ? ORDER BY created_at ASC, id ASC`, key)
		if err != nil {
			return 0, err
		}
		recs := make([]record, 0)
		for recRows.Next() {
			var r record
			if err := recRows.Scan(&r.id, &r.site, &r.createdAt, &r.updatedAt); err != nil {
				_ = recRows.Close()
				return 0, err
			}
			recs = append(recs, r)
		}
		if err := recRows.Err(); err != nil {
			_ = recRows.Close()
			return 0, err
		}
		_ = recRows.Close()
		if len(recs) < 2 {
			continue
		}

		keeper := recs[0]
		sites := []string{}
		tags := []string{}
		latest := keeper.updatedAt
		for i, r := range recs {
			entrySites, err := loadSites(tx, r.id)
			if err != nil {
				return 0, err
			}
			if len(entrySites) == 0 && r.site != "" {
				entrySites = []string{r.site}
			}
			sites = mergeSites(sites, entrySites)

			entryTags, err := loadTags(tx, r.id)
			if err != nil {
				return 0, err
			}
			tags = mergeTags(tags, entryTags)
			if r.updatedAt > latest {
				latest = r.updatedAt
			}
			if i > 0 {
				if _, err := tx.Exec(`DELETE FROM entries WHERE id = ?`, r.id); err != nil {
					return 0, err
				}
			}
		}

		if _, err := tx.Exec(`UPDATE entries SET site = ?, updated_at = ? WHERE id = ?`,
			primarySite(sites), latest, keeper.id); err != nil {
			return 0, err
		}
		if err := replaceSitesTx(tx, keeper.id, sites); err != nil {
			return 0, err
		}
		if err := replaceTagsTx(tx, keeper.id, tags); err != nil {
			return 0, err
		}
		merged++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.hardenFiles()
	return merged, nil
}

// sortedSites 返回排序后的副本,仅用于测试与调试输出。
func sortedSites(sites []string) []string {
	out := cloneSites(sites)
	sort.Strings(out)
	return out
}
