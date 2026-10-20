package tagstore

import (
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

	backup := cloneEntryMap(s.entries)
	now := time.Now().Format(time.RFC3339)
	changed := 0
	for _, entry := range s.entries {
		if !containsTag(entry.Tags, from) {
			continue
		}
		tags := make([]string, 0, len(entry.Tags))
		seen := make(map[string]struct{}, len(entry.Tags))
		for _, tag := range entry.Tags {
			next := tag
			if strings.EqualFold(tag, from) {
				next = to
			}
			key := strings.ToLower(next)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			tags = append(tags, next)
		}
		entry.Tags = tags
		entry.UpdatedAt = now
		changed++
	}
	if changed == 0 {
		return 0, nil
	}
	if err := s.saveLocked(); err != nil {
		s.entries = backup
		return 0, err
	}
	return changed, nil
}

// DeleteTag 从所有记录中移除该标签,返回受影响的记录数。
func (s *Store) DeleteTag(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("标签不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	backup := cloneEntryMap(s.entries)
	now := time.Now().Format(time.RFC3339)
	changed := 0
	for _, entry := range s.entries {
		if !containsTag(entry.Tags, name) {
			continue
		}
		tags := make([]string, 0, len(entry.Tags))
		for _, tag := range entry.Tags {
			if strings.EqualFold(tag, name) {
				continue
			}
			tags = append(tags, tag)
		}
		entry.Tags = tags
		entry.UpdatedAt = now
		changed++
	}
	if changed == 0 {
		return 0, nil
	}
	if err := s.saveLocked(); err != nil {
		s.entries = backup
		return 0, err
	}
	return changed, nil
}

func containsTag(tags []string, name string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, name) {
			return true
		}
	}
	return false
}
