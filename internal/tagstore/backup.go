package tagstore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
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

// Export 返回全部记录的备份快照,按创建时间排序,便于跨实例迁移。
func (s *Store) Export() Backup {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, cloneEntry(entry))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt != entries[j].CreatedAt {
			return entries[i].CreatedAt < entries[j].CreatedAt
		}
		return entries[i].ID < entries[j].ID
	})

	return Backup{
		Version:    backupVersion,
		Type:       backupType,
		ExportedAt: time.Now().Format(time.RFC3339),
		Count:      len(entries),
		Entries:    entries,
	}
}

// Restore 从备份导入记录。
//
// mode 为 merge(默认,跳过已存在的「邮箱+网站」)或 replace(清空后重建);
// 备份内重复的「邮箱+网站」会被去重。整体只落盘一次,保存失败会回滚。
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

	backup := cloneEntryMap(s.entries)
	if mode == "replace" {
		s.entries = make(map[string]*Entry)
	}

	index := make(map[string]string, len(s.entries)) // email|site → id
	for id, entry := range s.entries {
		index[entryKey(entry.Email, entry.Site)] = id
	}

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
		key := entryKey(email, site)
		if _, ok := index[key]; ok {
			result.Skipped++
			continue
		}

		id := strings.TrimSpace(raw.ID)
		if id == "" || s.entries[id] != nil {
			id = "tag_" + uuid.New().String()[:8]
		}
		createdAt := normalizeTimestamp(raw.CreatedAt, now)
		updatedAt := normalizeTimestamp(raw.UpdatedAt, createdAt)
		entry := &Entry{
			ID:        id,
			Email:     email,
			Password:  password,
			Site:      site,
			Tags:      tags,
			Note:      note,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}
		s.entries[id] = entry
		index[key] = id
		result.Imported++
	}

	result.Total = len(s.entries)
	if err := s.saveLocked(); err != nil {
		s.entries = backup
		return RestoreResult{}, err
	}
	return result, nil
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
