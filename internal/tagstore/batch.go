package tagstore

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// BatchInput 是批量修改标签的输入。
//
// IDs 非空时只处理这些记录;否则按 Filter 筛选目标记录。
// Action 为 add(追加)、remove(移除)或 replace(覆盖)。
type BatchInput struct {
	IDs    []string
	Filter ListQuery
	Action string
	Tags   []string
}

// BatchResult 是批量修改标签的结果。
type BatchResult struct {
	Affected int      `json:"affected"`
	Action   string   `json:"action"`
	Tags     []string `json:"tags"`
}

// DeleteQuery 定义批量删除范围。
//
// IDs 非空时按记录 ID 删除;否则 Filter 有筛选条件时按筛选删除;
// 只有显式 All=true 时才会删除全部记录,避免空请求误删。
type DeleteQuery struct {
	IDs    []string
	Filter ListQuery
	All    bool
}

// BatchValidationError 表示批量操作参数不合法,handler 应返回 400。
type BatchValidationError struct {
	Message string
}

func (e *BatchValidationError) Error() string { return e.Message }

func batchInvalid(format string, args ...any) error {
	return &BatchValidationError{Message: fmt.Sprintf(format, args...)}
}

// BatchTag 按 ID 或筛选条件批量添加/移除/覆盖标签。
//
// 返回值 Affected 是命中的记录数(不一定是实际发生变化的记录数)。
// 所有写入在同一个事务内完成,任一步失败都会整体回滚。
func (s *Store) BatchTag(in BatchInput) (BatchResult, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action != "add" && action != "remove" && action != "replace" {
		return BatchResult{}, batchInvalid("操作类型只能是 add、remove 或 replace")
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return BatchResult{}, batchInvalid("%v", err)
	}
	if len(tags) == 0 {
		return BatchResult{}, batchInvalid("请至少填写一个标签")
	}

	ids := dedupeIDs(in.IDs)
	if len(ids) == 0 && !in.Filter.hasFilter() {
		return BatchResult{}, batchInvalid("请先筛选或勾选要修改的记录")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return BatchResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if len(ids) > 0 {
		ids, err = existingEntryIDs(tx, ids)
		if err != nil {
			return BatchResult{}, err
		}
	} else {
		where, args := buildWhere(in.Filter)
		rows, err := tx.Query(`SELECT e.id FROM entries e `+where, args...)
		if err != nil {
			return BatchResult{}, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return BatchResult{}, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return BatchResult{}, err
		}
		_ = rows.Close()
	}

	result := BatchResult{Affected: len(ids), Action: action, Tags: cloneTags(tags)}
	if len(ids) == 0 {
		return result, nil
	}

	switch action {
	case "add":
		if err := addTagsToEntries(tx, ids, tags); err != nil {
			return BatchResult{}, err
		}
	case "remove":
		if err := removeTagsFromEntries(tx, ids, tags); err != nil {
			return BatchResult{}, err
		}
	case "replace":
		if err := replaceTagsOnEntries(tx, ids, tags); err != nil {
			return BatchResult{}, err
		}
	}

	if err := touchEntries(tx, ids, time.Now().Format(time.RFC3339)); err != nil {
		return BatchResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return BatchResult{}, err
	}
	s.hardenFiles()
	return result, nil
}

// BatchDelete 按 ID、筛选条件或全部记录批量删除,返回实际删除的记录数。
//
// 删除记录时 entry_tags 会随外键级联删除,整个操作在同一事务内完成。
func (s *Store) BatchDelete(in DeleteQuery) (int, error) {
	ids := dedupeIDs(in.IDs)
	if in.All && (len(ids) > 0 || in.Filter.hasFilter()) {
		return 0, batchInvalid("all 不能与 ids 或 filter 同时使用")
	}
	if len(ids) == 0 && !in.Filter.hasFilter() && !in.All {
		return 0, batchInvalid("请先筛选或勾选要删除的记录")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	affected := int64(0)
	switch {
	case len(ids) > 0:
		ids, err = existingEntryIDs(tx, ids)
		if err != nil {
			return 0, err
		}
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
			res, err := tx.Exec(`DELETE FROM entries WHERE id IN (`+placeholders+`)`, args...)
			if err != nil {
				return 0, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return 0, err
			}
			affected += n
		}
	case in.Filter.hasFilter():
		where, args := buildWhere(in.Filter)
		res, err := tx.Exec(`DELETE FROM entries WHERE id IN (SELECT e.id FROM entries e `+where+`)`, args...)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		affected = n
	default:
		res, err := tx.Exec(`DELETE FROM entries`)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		affected = n
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.hardenFiles()
	return int(affected), nil
}

// dedupeIDs 去空白、去重并保持原顺序。
func dedupeIDs(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// existingEntryIDs 过滤掉不存在的 ID,保证 affected 反映真实命中数。
func existingEntryIDs(tx *sql.Tx, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
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
		rows, err := tx.Query(`SELECT id FROM entries WHERE id IN (`+placeholders+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// addTagsToEntries 为每条记录追加标签,已存在的标签自动跳过。
func addTagsToEntries(tx *sql.Tx, ids []string, tags []string) error {
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO entry_tags(entry_id, tag, tag_lower, ord)
		VALUES(?, ?, ?, (SELECT COALESCE(MAX(ord) + 1, 0) FROM entry_tags WHERE entry_id = ?))`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, id := range ids {
		for _, tag := range tags {
			if _, err := stmt.Exec(id, tag, strings.ToLower(tag), id); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeTagsFromEntries 从每条记录中移除指定标签。
func removeTagsFromEntries(tx *sql.Tx, ids []string, tags []string) error {
	lowers := make([]string, 0, len(tags))
	for _, tag := range tags {
		lowers = append(lowers, strings.ToLower(tag))
	}
	tagPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(lowers)), ",")

	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		idPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk)+len(lowers))
		for _, id := range chunk {
			args = append(args, id)
		}
		for _, lower := range lowers {
			args = append(args, lower)
		}
		if _, err := tx.Exec(`DELETE FROM entry_tags WHERE entry_id IN (`+idPlaceholders+`)
			AND tag_lower IN (`+tagPlaceholders+`)`, args...); err != nil {
			return err
		}
	}
	return nil
}

// replaceTagsOnEntries 清空每条记录的标签后写入新标签。
func replaceTagsOnEntries(tx *sql.Tx, ids []string, tags []string) error {
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
		if _, err := tx.Exec(`DELETE FROM entry_tags WHERE entry_id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
	}

	stmt, err := tx.Prepare(`INSERT INTO entry_tags(entry_id, tag, tag_lower, ord) VALUES(?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, id := range ids {
		for i, tag := range tags {
			if _, err := stmt.Exec(id, tag, strings.ToLower(tag), i); err != nil {
				return err
			}
		}
	}
	return nil
}
