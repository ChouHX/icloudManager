package tagstore

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// backupVersion 是备份格式版本,便于未来兼容。
	backupVersion = 1
	// backupType 用于识别本项目的备份文件。
	backupType = "icloud-hme-tags"
)

// Backup 是可导出的备份文档,可直接用于其他实例的导入。
type Backup struct {
	Version    int     `json:"version"`
	Type       string  `json:"type"`
	ExportedAt string  `json:"exported_at"`
	Count      int     `json:"count"`
	Entries    []Entry `json:"entries"`
}

// RestoreInput 是导入备份的输入。
type RestoreInput struct {
	Version int     `json:"version"`
	Type    string  `json:"type"`
	Entries []Entry `json:"entries"`
	// Mode 为 merge(默认,跳过已存在的「邮箱+网站」)或 replace(清空后重建)。
	Mode string `json:"mode"`
}

// RestoreResult 是导入备份的结果。
type RestoreResult struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Invalid  int      `json:"invalid"`
	Total    int      `json:"total"`
	Errors   []string `json:"errors"`
}

// ExportQuery 定义导出范围:IDs 非空时按记录 ID 导出,否则按 Filter 筛选;
// 两者都为空时导出全部记录。
type ExportQuery struct {
	IDs    []string
	Filter ListQuery
}

// Export 返回全部记录的备份快照,按创建时间排序,便于跨实例迁移。
func (s *Store) Export() (Backup, error) {
	return s.ExportQuery(ExportQuery{})
}

// ExportQuery 返回指定范围的备份快照,按创建时间排序,便于跨实例迁移或批量导出。
func (s *Store) ExportQuery(in ExportQuery) (Backup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := dedupeIDs(in.IDs)
	entries := make([]Entry, 0)
	if len(ids) > 0 {
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
			rows, err := s.db.Query(`SELECT id, email, password, site, note, created_at, updated_at
				FROM entries WHERE id IN (`+placeholders+`) ORDER BY created_at ASC, id ASC`, args...)
			if err != nil {
				return Backup{}, err
			}
			chunkEntries, err := scanExportRows(rows)
			_ = rows.Close()
			if err != nil {
				return Backup{}, err
			}
			entries = append(entries, chunkEntries...)
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].CreatedAt != entries[j].CreatedAt {
				return entries[i].CreatedAt < entries[j].CreatedAt
			}
			return entries[i].ID < entries[j].ID
		})
	} else {
		where, args := buildWhere(in.Filter)
		rows, err := s.db.Query(`SELECT e.id, e.email, e.password, e.site, e.note, e.created_at, e.updated_at
			FROM entries e `+where+` ORDER BY e.created_at ASC, e.id ASC`, args...)
		if err != nil {
			return Backup{}, err
		}
		entries, err = scanExportRows(rows)
		_ = rows.Close()
		if err != nil {
			return Backup{}, err
		}
	}

	entryIDs := make([]string, len(entries))
	for i := range entries {
		entryIDs[i] = entries[i].ID
	}
	tags, err := s.tagsForIDs(entryIDs)
	if err != nil {
		return Backup{}, err
	}
	for i := range entries {
		if list, ok := tags[entries[i].ID]; ok {
			entries[i].Tags = list
		}
	}
	return Backup{
		Version:    backupVersion,
		Type:       backupType,
		ExportedAt: time.Now().Format(time.RFC3339),
		Count:      len(entries),
		Entries:    entries,
	}, nil
}

func scanExportRows(rows *sql.Rows) ([]Entry, error) {
	entries := make([]Entry, 0)
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Email, &e.Password, &e.Site, &e.Note, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Tags = []string{}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Restore 从备份导入记录。
//
// mode 为 merge(默认,跳过已存在的「邮箱+网站」)或 replace(清空后重建);
// 备份内重复的「邮箱+网站」会被去重。所有写入在同一事务内完成。
func (s *Store) Restore(in RestoreInput) (RestoreResult, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" {
		return RestoreResult{}, fmt.Errorf("导入模式只能是 merge 或 replace")
	}
	if in.Type != "" && in.Type != backupType {
		return RestoreResult{}, fmt.Errorf("备份文件类型不匹配")
	}
	if in.Version != 0 && in.Version != backupVersion {
		return RestoreResult{}, fmt.Errorf("不支持的备份版本: %d", in.Version)
	}
	if len(in.Entries) == 0 {
		return RestoreResult{}, fmt.Errorf("备份文件没有可导入的记录")
	}

	result := RestoreResult{Errors: make([]string, 0)}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return RestoreResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if mode == "replace" {
		if _, err := tx.Exec(`DELETE FROM entries`); err != nil {
			return RestoreResult{}, err
		}
	}

	find, err := tx.Prepare(`SELECT 1 FROM entries WHERE lower(email) = ? AND lower(site) = ? LIMIT 1`)
	if err != nil {
		return RestoreResult{}, err
	}
	defer func() { _ = find.Close() }()
	findID, err := tx.Prepare(`SELECT 1 FROM entries WHERE id = ? LIMIT 1`)
	if err != nil {
		return RestoreResult{}, err
	}
	defer func() { _ = findID.Close() }()
	insert, err := tx.Prepare(`INSERT INTO entries(id, email, password, site, note, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return RestoreResult{}, err
	}
	defer func() { _ = insert.Close() }()

	now := time.Now().Format(time.RFC3339)
	for i, raw := range in.Entries {
		email, password, site, tags, note, err := normalize(Input{
			Email:    raw.Email,
			Password: raw.Password,
			Site:     raw.Site,
			Tags:     raw.Tags,
			Note:     raw.Note,
		})
		if err != nil {
			result.Invalid++
			appendCappedError(&result.Errors, fmt.Sprintf("第 %d 条: %v", i+1, err))
			continue
		}

		var exists int
		err = find.QueryRow(strings.ToLower(email), strings.ToLower(site)).Scan(&exists)
		if err == nil {
			result.Skipped++
			continue
		}
		if err != sql.ErrNoRows {
			return RestoreResult{}, err
		}

		id := strings.TrimSpace(raw.ID)
		if id == "" || idTaken(findID, id) {
			id = newEntryID()
		}
		createdAt := normalizeTimestamp(raw.CreatedAt, now)
		updatedAt := normalizeTimestamp(raw.UpdatedAt, createdAt)
		entry := &Entry{
			ID:        id,
			Email:     email,
			Password:  password,
			Site:      site,
			Tags:      cloneTags(tags),
			Note:      note,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}
		if _, err := insert.Exec(entry.ID, entry.Email, entry.Password, entry.Site, entry.Note, entry.CreatedAt, entry.UpdatedAt); err != nil {
			return RestoreResult{}, err
		}
		if err := insertTags(tx, entry.ID, entry.Tags); err != nil {
			return RestoreResult{}, err
		}
		result.Imported++
	}

	var total int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&total); err != nil {
		return RestoreResult{}, err
	}
	result.Total = total

	if err := tx.Commit(); err != nil {
		return RestoreResult{}, err
	}
	s.hardenFiles()
	return result, nil
}

// idTaken 判断 ID 是否已存在。
func idTaken(stmt *sql.Stmt, id string) bool {
	var exists int
	return stmt.QueryRow(id).Scan(&exists) == nil
}

// normalizeTimestamp 校验 RFC3339 时间戳,非法或缺失时回退。
func normalizeTimestamp(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return fallback
	}
	return value
}
