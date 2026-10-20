package tagstore

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RenameTag 把所有记录中的 from 标签重命名为 to(忽略大小写匹配),
// 返回受影响的记录数。目标标签已存在时会自动去重。
func (s *Store) RenameTag(from, to string) (int, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" {
		return 0, fmt.Errorf("原标签不能为空")
	}
	if to == "" {
		return 0, fmt.Errorf("新标签不能为空")
	}
	if len([]rune(to)) > maxTagLen {
		return 0, fmt.Errorf("单个标签不能超过 %d 个字符", maxTagLen)
	}
	if strings.EqualFold(from, to) {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	fromLower := strings.ToLower(from)
	toLower := strings.ToLower(to)

	fromIDs, err := entryIDsWithTag(tx, fromLower)
	if err != nil {
		return 0, err
	}
	if len(fromIDs) == 0 {
		return 0, nil
	}
	toIDs, err := entryIDsWithTag(tx, toLower)
	if err != nil {
		return 0, err
	}

	// 已有目标标签的记录:直接删除旧标签行即可完成去重。
	if overlap := intersect(fromIDs, toIDs); len(overlap) > 0 {
		if err := deleteTagRows(tx, fromLower, overlap); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(`UPDATE entry_tags SET tag = ?, tag_lower = ? WHERE tag_lower = ?`,
		to, toLower, fromLower); err != nil {
		return 0, err
	}
	if err := touchEntries(tx, fromIDs, time.Now().Format(time.RFC3339)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.hardenFiles()
	return len(fromIDs), nil
}

// DeleteTag 从所有记录中移除该标签,返回受影响的记录数。
func (s *Store) DeleteTag(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("标签不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	lower := strings.ToLower(name)
	ids, err := entryIDsWithTag(tx, lower)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(`DELETE FROM entry_tags WHERE tag_lower = ?`, lower); err != nil {
		return 0, err
	}
	if err := touchEntries(tx, ids, time.Now().Format(time.RFC3339)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.hardenFiles()
	return len(ids), nil
}

func entryIDsWithTag(tx *sql.Tx, lower string) ([]string, error) {
	rows, err := tx.Query(`SELECT entry_id FROM entry_tags WHERE tag_lower = ?`, lower)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func intersect(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, id := range b {
		set[id] = struct{}{}
	}
	out := make([]string, 0)
	for _, id := range a {
		if _, ok := set[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// deleteTagRows 删除指定记录上的某个标签(用于重命名时的去重)。
func deleteTagRows(tx *sql.Tx, lower string, ids []string) error {
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk)+1)
		args = append(args, lower)
		for _, id := range chunk {
			args = append(args, id)
		}
		if _, err := tx.Exec(`DELETE FROM entry_tags WHERE tag_lower = ? AND entry_id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
	}
	return nil
}

// touchEntries 批量刷新记录的更新时间。
func touchEntries(tx *sql.Tx, ids []string, updatedAt string) error {
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk)+1)
		args = append(args, updatedAt)
		for _, id := range chunk {
			args = append(args, id)
		}
		if _, err := tx.Exec(`UPDATE entries SET updated_at = ? WHERE id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
	}
	return nil
}

func containsTag(tags []string, name string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, name) {
			return true
		}
	}
	return false
}
