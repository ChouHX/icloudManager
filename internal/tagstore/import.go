package tagstore

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ImportInput 是批量导入的输入。
type ImportInput struct {
	// Text 每行一条记录,支持 `邮箱----密码----网站`、制表符、`|`、逗号、
	// `邮箱:密码` 或空格分隔;以 # 开头的行视为注释。
	Text string `json:"text"`
	// Separator 是自定义分隔符;留空则按 Text 自动识别。
	// 想用制表符可直接写 "\t"(两个字面量字符),也可传真正的制表符。
	Separator string `json:"separator"`
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
// 所有写入在同一个事务内完成,任一步失败都会整体回滚。
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
	separator, err := normalizeSeparator(in.Separator)
	if err != nil {
		return ImportResult{}, err
	}

	lines := parseImportText(in.Text, separator)
	if len(lines) == 0 {
		return ImportResult{}, fmt.Errorf("没有可导入的内容")
	}

	result := ImportResult{Entries: make([]Entry, 0), Errors: make([]string, 0)}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	find, err := tx.Prepare(`SELECT id, created_at FROM entries
		WHERE lower(email) = ? AND lower(site) = ? LIMIT 1`)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = find.Close() }()
	insert, err := tx.Prepare(`INSERT INTO entries(id, email, password, site, note, created_at, updated_at)
		VALUES(?, ?, ?, ?, '', ?, ?)`)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = insert.Close() }()
	update, err := tx.Prepare(`UPDATE entries SET password = ?, site = ?, updated_at = ? WHERE id = ?`)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = update.Close() }()

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
		})
		if err != nil {
			result.Invalid++
			appendCappedError(&result.Errors, fmt.Sprintf("第 %d 行: %v", line.line, err))
			continue
		}

		var existingID, createdAt string
		err = find.QueryRow(strings.ToLower(email), strings.ToLower(site)).Scan(&existingID, &createdAt)
		switch {
		case err == nil:
			if mode != "update" {
				result.Skipped++
				continue
			}
			existingTags, err := loadTags(tx, existingID)
			if err != nil {
				return ImportResult{}, err
			}
			merged := mergeTags(existingTags, tags)
			if _, err := update.Exec(password, site, now, existingID); err != nil {
				return ImportResult{}, err
			}
			if err := replaceTagsTx(tx, existingID, merged); err != nil {
				return ImportResult{}, err
			}
			result.Updated++
			result.Entries = append(result.Entries, Entry{
				ID:        existingID,
				Email:     email,
				Password:  password,
				Site:      site,
				Tags:      merged,
				CreatedAt: createdAt,
				UpdatedAt: now,
			})
			continue
		case err != sql.ErrNoRows:
			return ImportResult{}, err
		}

		entry := &Entry{
			ID:        newEntryID(),
			Email:     email,
			Password:  password,
			Site:      site,
			Tags:      cloneTags(tags),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := insert.Exec(entry.ID, entry.Email, entry.Password, entry.Site, entry.CreatedAt, entry.UpdatedAt); err != nil {
			return ImportResult{}, err
		}
		if err := insertTags(tx, entry.ID, entry.Tags); err != nil {
			return ImportResult{}, err
		}
		result.Imported++
		result.Entries = append(result.Entries, cloneEntry(entry))
	}

	if err := tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	s.hardenFiles()
	return result, nil
}

// maxSeparatorLen 限制自定义分隔符的字符数。
const maxSeparatorLen = 8

// normalizeSeparator 校验并规整自定义分隔符,返回空串表示按内容自动识别。
//
// 支持把制表符/换行写成 `\t`、`\n` 两个字面量字符,方便从 JSON 或命令行传入。
func normalizeSeparator(raw string) (string, error) {
	sep := strings.TrimSpace(raw)
	switch sep {
	case "":
		return "", nil
	case `\t`:
		return "\t", nil
	case `\n`:
		return "\n", nil
	}
	if len([]rune(sep)) > maxSeparatorLen {
		return "", fmt.Errorf("分隔符不能超过 %d 个字符", maxSeparatorLen)
	}
	if strings.ContainsAny(sep, "\r\n") {
		return "", fmt.Errorf("分隔符不能包含换行符")
	}
	return sep, nil
}

// parseImportText 把多行文本解析为待导入行,自动跳过空行与 # 注释。
//
// separator 非空时按它精确拆分;为空时按内容自动识别常见分隔符。
func parseImportText(text, separator string) []importLine {
	out := make([]importLine, 0)
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitImportLine(line, separator)
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

// dashSeparator 匹配 4 个及以上的连续连字符。
//
// 必须按「连字符段」而不是固定字面量 "----" 拆分:否则 5 个连字符会被切成
// "----" + "-",残留的 "-" 会粘到下一个字段上,把网站名污染成 "-Lord"。
var dashSeparator = regexp.MustCompile(`-{4,}`)

// splitImportLine 拆分一行:优先使用自定义分隔符,否则按优先级自动识别。
func splitImportLine(line, separator string) []string {
	if separator != "" {
		return strings.Split(line, separator)
	}
	if dashSeparator.MatchString(line) {
		return dashSeparator.Split(line, -1)
	}
	for _, sep := range []string{"\t", "|"} {
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

func appendCappedError(errors *[]string, msg string) {
	if len(*errors) >= maxImportErrors {
		return
	}
	*errors = append(*errors, msg)
}
