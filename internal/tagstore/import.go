package tagstore

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ImportInput 是批量导入的输入。
type ImportInput struct {
	// Text 每行一条记录,支持 `邮箱----密码----网站`、制表符、`|`、逗号、
	// `邮箱:密码` 或空格分隔;以 # 开头的行视为注释。
	Text string `json:"text"`
	// Tags 会应用到本次导入的所有记录。
	Tags []string `json:"tags"`
	// Site 是行内未指定网站时使用的默认网站。
	Site string `json:"site"`
	// Mode 为 skip(默认,跳过已存在的邮箱+网站)或 update(覆盖更新并合并标签)。
	Mode string `json:"mode"`
}

// ImportResult 是批量导入的结果。
//
// Entries 仅供内部/测试使用,HTTP 响应会置空以避免万级导入时响应过大。
type ImportResult struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Skipped  int      `json:"skipped"`
	Invalid  int      `json:"invalid"`
	Entries  []Entry  `json:"entries,omitempty"`
	Errors   []string `json:"errors"`
}

// maxImportErrors 限制返回的行级错误数量,避免响应过大。
const maxImportErrors = 20

type importLine struct {
	line     int
	email    string
	password string
	site     string
}

// Import 批量导入记录,并把 in.Tags 应用到每一条记录。
//
// 解析与校验失败的行走 Invalid 计数;邮箱+网站已存在时按 Mode 跳过或更新。
// 整个过程只落盘一次,保存失败会回滚内存状态。
func (s *Store) Import(in ImportInput) (ImportResult, error) {
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return ImportResult{}, err
	}
	defaultSite := strings.TrimSpace(in.Site)
	if len([]rune(defaultSite)) > maxSiteLen {
		return ImportResult{}, fmt.Errorf("网站名称不能超过 %d 个字符", maxSiteLen)
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != "" && mode != "skip" && mode != "update" {
		return ImportResult{}, fmt.Errorf("导入模式只能是 skip 或 update")
	}

	lines := parseImportText(in.Text)
	if len(lines) == 0 {
		return ImportResult{}, fmt.Errorf("没有可导入的内容")
	}

	result := ImportResult{Entries: make([]Entry, 0), Errors: make([]string, 0)}

	s.mu.Lock()
	defer s.mu.Unlock()

	backup := cloneEntryMap(s.entries)
	index := make(map[string]string, len(s.entries)) // email|site → id
	for id, entry := range s.entries {
		index[entryKey(entry.Email, entry.Site)] = id
	}

	now := time.Now().Format(time.RFC3339)
	for _, line := range lines {
		site := line.site
		if site == "" {
			site = defaultSite
		}
		email, password, site, _, _, err := normalize(Input{
			Email:    line.email,
			Password: line.password,
			Site:     site,
			Tags:     tags,
		})
		if err != nil {
			result.Invalid++
			appendCappedError(&result.Errors, fmt.Sprintf("第 %d 行: %v", line.line, err))
			continue
		}

		key := entryKey(email, site)
		if id, ok := index[key]; ok {
			existing := s.entries[id]
			if mode != "update" {
				result.Skipped++
				continue
			}
			existing.Password = password
			existing.Site = site
			existing.Tags = mergeTags(existing.Tags, tags)
			existing.UpdatedAt = now
			result.Updated++
			result.Entries = append(result.Entries, cloneEntry(existing))
			continue
		}

		entry := &Entry{
			ID:        "tag_" + uuid.New().String()[:8],
			Email:     email,
			Password:  password,
			Site:      site,
			Tags:      cloneTags(tags),
			CreatedAt: now,
			UpdatedAt: now,
		}
		s.entries[entry.ID] = entry
		index[key] = entry.ID
		result.Imported++
		result.Entries = append(result.Entries, cloneEntry(entry))
	}

	if err := s.saveLocked(); err != nil {
		s.entries = backup
		return ImportResult{}, err
	}
	return result, nil
}

// parseImportText 把多行文本解析为待导入行,自动跳过空行与 # 注释。
func parseImportText(text string) []importLine {
	out := make([]importLine, 0)
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitImportLine(line)
		item := importLine{line: i + 1}
		if len(fields) > 0 {
			item.email = cleanField(fields[0])
		}
		if len(fields) > 1 {
			item.password = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			item.site = cleanField(fields[2])
		}
		out = append(out, item)
	}
	return out
}

// splitImportLine 按优先级尝试常见分隔符拆分一行。
func splitImportLine(line string) []string {
	for _, sep := range []string{"----", "\t", "|"} {
		if strings.Contains(line, sep) {
			return strings.Split(line, sep)
		}
	}
	if strings.Contains(line, ",") {
		return strings.Split(line, ",")
	}
	// email:password 只按第一个冒号拆分,避免密码里的冒号被误切
	if idx := strings.Index(line, ":"); idx > 0 {
		return []string{line[:idx], line[idx+1:]}
	}
	return strings.Fields(line)
}

// cleanField 去掉字段两端空白、成对引号以及 <...> 包裹。
func cleanField(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && len(s) > 2 {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// mergeTags 把 add 合并进 existing,忽略大小写重复并限制总量。
func mergeTags(existing, add []string) []string {
	out := cloneTags(existing)
	seen := make(map[string]struct{}, len(out))
	for _, tag := range out {
		seen[strings.ToLower(tag)] = struct{}{}
	}
	for _, tag := range add {
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		if len(out) >= maxTags {
			break
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	return out
}

// entryKey 生成「邮箱+网站」的去重键(忽略大小写)。
func entryKey(email, site string) string {
	return strings.ToLower(strings.TrimSpace(email)) + "|" + strings.ToLower(strings.TrimSpace(site))
}

// cloneEntryMap 深拷贝记录表,用于保存失败时回滚。
func cloneEntryMap(src map[string]*Entry) map[string]*Entry {
	dst := make(map[string]*Entry, len(src))
	for id, entry := range src {
		cp := cloneEntry(entry)
		dst[id] = &cp
	}
	return dst
}

func appendCappedError(errors *[]string, msg string) {
	if len(*errors) >= maxImportErrors {
		return
	}
	*errors = append(*errors, msg)
}
