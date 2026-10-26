// Package gmail 管理管理员导入的 Gmail 账号，并基于 IMAP 提供只读取件。
//
// 与 iCloud 账号体系完全独立：账号由管理员批量导入
// （`账号----密码----2FA 密钥----应用专用密码`），凭应用专用密码通过
// imap.gmail.com 读取收件箱，再按管理员配置的过滤条件决定对外可见的邮件。
//
// 持久化到 dataDir/gmail.db（纯 Go SQLite，文件权限 0600），
// 密码类字段只写不读：对外一律通过 Summary 暴露布尔标记。
package gmail

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const (
	// storeFileName 是 SQLite 数据库文件名。
	storeFileName = "gmail.db"

	maxEmailLen    = 254
	maxSecretLen   = 200
	maxNoteLen     = 500
	maxFilterItems = 50
	maxFilterLen   = 100
	// maxFilterDays 限制过滤窗口天数。
	maxFilterDays = 90
)

// Filter 是管理员配置的**全局**取件过滤条件，对所有 Gmail 邮箱生效。
//
// 关键词按「主题或发件人包含任一关键词」命中（忽略大小写）：
//   - Include 为空表示不限制；否则只有命中的邮件可见
//   - Exclude 命中的邮件一律隐藏（优先级高于 Include）
//   - Days 只显示最近 N 天（0 表示不限制，最大 90）
type Filter struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
	Days    int      `json:"days"`
}

// Account 是一条 Gmail 账号记录（含密码类字段，仅内部使用）。
type Account struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Password    string `json:"password,omitempty"`
	TOTPSecret  string `json:"totp_secret,omitempty"`
	AppPassword string `json:"app_password,omitempty"`
	Note        string `json:"note,omitempty"`
	Status      string `json:"status"`
	LastChecked string `json:"last_checked,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Summary 是对外暴露的账号信息，不含任何密码字段。
type Summary struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Note           string `json:"note,omitempty"`
	Status         string `json:"status"`
	HasPassword    bool   `json:"has_password"`
	HasTOTP        bool   `json:"has_totp"`
	HasAppPassword bool   `json:"has_app_password"`
	LastChecked    string `json:"last_checked,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// Summary 去掉密码类字段。
func (a Account) Summary() Summary {
	return Summary{
		ID:             a.ID,
		Email:          a.Email,
		Note:           a.Note,
		Status:         a.Status,
		HasPassword:    a.Password != "",
		HasTOTP:        a.TOTPSecret != "",
		HasAppPassword: a.AppPassword != "",
		LastChecked:    a.LastChecked,
		LastError:      a.LastError,
		CreatedAt:      a.CreatedAt,
		UpdatedAt:      a.UpdatedAt,
	}
}

// Input 是创建/更新账号的输入。密码类字段留空表示「不修改」。
type Input struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	TOTPSecret  string `json:"totp_secret"`
	AppPassword string `json:"app_password"`
	Note        string `json:"note"`
}

// ImportResult 是批量导入的结果。
type ImportResult struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Invalid  int      `json:"invalid"`
	Errors   []string `json:"errors"`
}

// Store 管理 Gmail 账号（SQLite 持久化），线程安全。
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

// NewStore 打开（或创建）dataDir 下的 Gmail 账号数据库。
func NewStore(dataDir string) (*Store, error) {
	dbPath, err := filepath.Abs(filepath.Join(dataDir, storeFileName))
	if err != nil {
		return nil, fmt.Errorf("解析 Gmail 数据库路径失败: %w", err)
	}
	precreateSecure(dbPath)

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("打开 Gmail 数据库失败: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, path: dbPath}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s.hardenFiles()
	return s, nil
}

// Close 关闭底层数据库连接。
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func sqliteDSN(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	return "file://" + (&url.URL{Path: slash}).EscapedPath() + "?" + q.Encode()
}

func precreateSecure(dbPath string) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		f, err := os.OpenFile(dbPath+suffix, os.O_CREATE|os.O_RDWR, 0o600)
		if err == nil {
			_ = f.Close()
		}
		_ = os.Chmod(dbPath+suffix, 0o600)
	}
}

func (s *Store) hardenFiles() {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Chmod(s.path+suffix, 0o600)
	}
}

func (s *Store) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS gmail_accounts (
			id           TEXT PRIMARY KEY,
			email        TEXT NOT NULL,
			password     TEXT NOT NULL DEFAULT '',
			totp_secret  TEXT NOT NULL DEFAULT '',
			app_password TEXT NOT NULL DEFAULT '',
			note         TEXT NOT NULL DEFAULT '',
			filter       TEXT NOT NULL DEFAULT '{}',
			status       TEXT NOT NULL DEFAULT 'active',
			last_checked TEXT NOT NULL DEFAULT '',
			last_error   TEXT NOT NULL DEFAULT '',
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gmail_email_lower ON gmail_accounts(lower(email))`,
		// 取件过滤是全局设置，只保留一行。
		`CREATE TABLE IF NOT EXISTS gmail_settings (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			filter     TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("初始化 Gmail 数据库失败: %w", err)
		}
	}
	return s.migrateLegacyFilter()
}

// migrateLegacyFilter 把旧版「按账号配置」的过滤条件迁移成全局设置。
//
// 只在全局设置为空、且存在带条件的旧账号时执行一次，避免覆盖管理员已保存的设置。
func (s *Store) migrateLegacyFilter() error {
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM gmail_settings WHERE id = 1`).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	var legacy string
	_ = s.db.QueryRow(`SELECT filter FROM gmail_accounts
		WHERE filter NOT IN ('', '{}') ORDER BY updated_at DESC LIMIT 1`).Scan(&legacy)
	filter := decodeFilter(legacy)
	filterJSON, err := json.Marshal(filter)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO gmail_settings(id, filter, updated_at) VALUES(1, ?, ?)`,
		string(filterJSON), time.Now().Format(time.RFC3339))
	return err
}

// List 返回全部账号（不含密码字段），按创建时间升序。
func (s *Store) List() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, email, note, status, password, totp_secret,
		app_password, last_checked, last_error, created_at, updated_at
		FROM gmail_accounts ORDER BY created_at ASC, email ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]Summary, 0)
	for rows.Next() {
		acc, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, acc.Summary())
	}
	return out, rows.Err()
}

// Get 按 ID 返回完整账号（含密码字段，仅内部使用）。
func (s *Store) Get(id string) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, err := s.getLocked(id)
	if err != nil {
		return Account{}, false
	}
	return acc, true
}

func (s *Store) getLocked(id string) (Account, error) {
	row := s.db.QueryRow(`SELECT id, email, note, status, password, totp_secret,
		app_password, last_checked, last_error, created_at, updated_at
		FROM gmail_accounts WHERE id = ?`, id)
	return scanAccountRow(row)
}

// Create 新建一条账号。
func (s *Store) Create(in Input) (Summary, error) {
	email, password, totp, appPassword, note, err := normalize(in)
	if err != nil {
		return Summary{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM gmail_accounts WHERE lower(email) = lower(?)`, email).
		Scan(&exists); err != nil {
		return Summary{}, err
	}
	if exists > 0 {
		return Summary{}, fmt.Errorf("该邮箱已存在")
	}

	now := time.Now().Format(time.RFC3339)
	acc := Account{
		ID:          newAccountID(),
		Email:       email,
		Password:    password,
		TOTPSecret:  totp,
		AppPassword: appPassword,
		Note:        note,
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.insertLocked(&acc); err != nil {
		return Summary{}, err
	}
	s.hardenFiles()
	return acc.Summary(), nil
}

func (s *Store) insertLocked(acc *Account) error {
	_, err := s.db.Exec(`INSERT INTO gmail_accounts(id, email, password, totp_secret, app_password,
		note, status, last_checked, last_error, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		acc.ID, acc.Email, acc.Password, acc.TOTPSecret, acc.AppPassword, acc.Note,
		acc.Status, acc.LastChecked, acc.LastError, acc.CreatedAt, acc.UpdatedAt)
	return err
}

// Update 更新一条账号；密码类字段留空表示不修改。
func (s *Store) Update(id string, in Input) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.getLocked(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return Summary{}, fmt.Errorf("账号不存在")
		}
		return Summary{}, err
	}

	email := strings.TrimSpace(in.Email)
	if email == "" {
		email = current.Email
	}
	if addr, perr := mail.ParseAddress(email); perr != nil || addr.Address != email {
		return Summary{}, fmt.Errorf("邮箱地址格式无效")
	}
	if !strings.EqualFold(email, current.Email) {
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM gmail_accounts WHERE lower(email) = lower(?) AND id <> ?`,
			email, id).Scan(&exists); err != nil {
			return Summary{}, err
		}
		if exists > 0 {
			return Summary{}, fmt.Errorf("该邮箱已存在")
		}
	}

	if v := strings.TrimSpace(in.Password); v != "" {
		current.Password = v
	}
	if v := strings.TrimSpace(in.TOTPSecret); v != "" {
		current.TOTPSecret = v
	}
	if v := strings.TrimSpace(in.AppPassword); v != "" {
		current.AppPassword = v
	}
	current.Note = truncate(strings.TrimSpace(in.Note), maxNoteLen)
	current.Email = email
	current.UpdatedAt = time.Now().Format(time.RFC3339)

	if _, err := s.db.Exec(`UPDATE gmail_accounts SET email = ?, password = ?, totp_secret = ?,
		app_password = ?, note = ?, updated_at = ? WHERE id = ?`,
		current.Email, current.Password, current.TOTPSecret, current.AppPassword,
		current.Note, current.UpdatedAt, id); err != nil {
		return Summary{}, err
	}
	s.hardenFiles()
	return current.Summary(), nil
}

// Filter 返回全局取件过滤条件。
func (s *Store) Filter() Filter {
	s.mu.Lock()
	defer s.mu.Unlock()

	var raw string
	if err := s.db.QueryRow(`SELECT filter FROM gmail_settings WHERE id = 1`).Scan(&raw); err != nil {
		return Filter{Include: []string{}, Exclude: []string{}}
	}
	return decodeFilter(raw)
}

// SetFilter 保存全局取件过滤条件（对所有 Gmail 邮箱生效）。
func (s *Store) SetFilter(filter Filter) (Filter, error) {
	normalized, err := normalizeFilter(filter)
	if err != nil {
		return Filter{}, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return Filter{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(`INSERT INTO gmail_settings(id, filter, updated_at) VALUES(1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET filter = excluded.filter, updated_at = excluded.updated_at`,
		string(payload), time.Now().Format(time.RFC3339)); err != nil {
		return Filter{}, err
	}
	s.hardenFiles()
	return normalized, nil
}

// Delete 删除一条账号，返回是否存在。
func (s *Store) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM gmail_accounts WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 0 {
		s.hardenFiles()
	}
	return affected > 0, nil
}

// MarkChecked 记录一次取件结果（成功时 lastError 传空）。
func (s *Store) MarkChecked(id, lastError string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	status := "active"
	if lastError != "" {
		status = "error"
	}
	_, _ = s.db.Exec(`UPDATE gmail_accounts SET last_checked = ?, last_error = ?, status = ? WHERE id = ?`,
		now, truncate(lastError, 300), status, id)
}

// Import 批量导入账号，格式：账号----密码----2FA密钥----应用专用密码。
//
// 邮箱为唯一键：已存在则更新（只覆盖非空字段），不存在则新建。
func (s *Store) Import(text, separator string) (ImportResult, error) {
	sep, err := normalizeSeparator(separator)
	if err != nil {
		return ImportResult{}, err
	}
	lines := parseImportText(text, sep)
	if len(lines) == 0 {
		return ImportResult{}, fmt.Errorf("没有可导入的内容")
	}

	result := ImportResult{Errors: make([]string, 0)}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, line := range lines {
		email, password, totp, appPassword, note, err := normalize(Input{
			Email:       line.fields[0],
			Password:    fieldAt(line.fields, 1),
			TOTPSecret:  fieldAt(line.fields, 2),
			AppPassword: fieldAt(line.fields, 3),
			Note:        fieldAt(line.fields, 4),
		})
		if err != nil {
			result.Invalid++
			appendCappedError(&result.Errors, fmt.Sprintf("第 %d 行: %v", line.no, err))
			continue
		}

		current, err := s.getLockedByEmail(email)
		switch {
		case err == nil:
			if password != "" {
				current.Password = password
			}
			if totp != "" {
				current.TOTPSecret = totp
			}
			if appPassword != "" {
				current.AppPassword = appPassword
			}
			if note != "" {
				current.Note = note
			}
			current.UpdatedAt = time.Now().Format(time.RFC3339)
			if _, err := s.db.Exec(`UPDATE gmail_accounts SET password = ?, totp_secret = ?,
				app_password = ?, note = ?, updated_at = ? WHERE id = ?`,
				current.Password, current.TOTPSecret, current.AppPassword, current.Note,
				current.UpdatedAt, current.ID); err != nil {
				return ImportResult{}, err
			}
			result.Updated++
			continue
		case err != sql.ErrNoRows:
			return ImportResult{}, err
		}

		now := time.Now().Format(time.RFC3339)
		acc := Account{
			ID:          newAccountID(),
			Email:       email,
			Password:    password,
			TOTPSecret:  totp,
			AppPassword: appPassword,
			Note:        note,
			Status:      "active",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.insertLocked(&acc); err != nil {
			return ImportResult{}, err
		}
		result.Imported++
	}

	s.hardenFiles()
	return result, nil
}

func (s *Store) getLockedByEmail(email string) (Account, error) {
	row := s.db.QueryRow(`SELECT id, email, note, status, password, totp_secret,
		app_password, last_checked, last_error, created_at, updated_at
		FROM gmail_accounts WHERE lower(email) = lower(?)`, email)
	return scanAccountRow(row)
}

// ===== 内部辅助 =====

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccountRow(row rowScanner) (Account, error) {
	var acc Account
	if err := row.Scan(&acc.ID, &acc.Email, &acc.Note, &acc.Status, &acc.Password,
		&acc.TOTPSecret, &acc.AppPassword, &acc.LastChecked, &acc.LastError,
		&acc.CreatedAt, &acc.UpdatedAt); err != nil {
		return Account{}, err
	}
	return acc, nil
}

func scanAccount(rows *sql.Rows) (Account, error) {
	return scanAccountRow(rows)
}

func decodeFilter(raw string) Filter {
	var f Filter
	if strings.TrimSpace(raw) == "" {
		return Filter{Include: []string{}, Exclude: []string{}}
	}
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return Filter{Include: []string{}, Exclude: []string{}}
	}
	f.Include = cloneStrings(f.Include)
	f.Exclude = cloneStrings(f.Exclude)
	return f
}

func normalize(in Input) (email, password, totp, appPassword, note string, err error) {
	email = strings.TrimSpace(in.Email)
	if email == "" {
		return "", "", "", "", "", fmt.Errorf("邮箱不能为空")
	}
	if len([]rune(email)) > maxEmailLen {
		return "", "", "", "", "", fmt.Errorf("邮箱长度超出限制")
	}
	if addr, perr := mail.ParseAddress(email); perr != nil || addr.Address != email {
		return "", "", "", "", "", fmt.Errorf("邮箱地址格式无效")
	}

	password = strings.TrimSpace(in.Password)
	totp = strings.TrimSpace(in.TOTPSecret)
	appPassword = strings.TrimSpace(in.AppPassword)
	for _, secret := range []struct {
		name  string
		value string
	}{
		{"密码", password},
		{"2FA 密钥", totp},
		{"应用专用密码", appPassword},
	} {
		if len([]rune(secret.value)) > maxSecretLen {
			return "", "", "", "", "", fmt.Errorf("%s不能超过 %d 个字符", secret.name, maxSecretLen)
		}
	}
	// Gmail 应用专用密码通常是 16 位，去掉用户粘贴时带的空格便于直接登录。
	appPassword = strings.ReplaceAll(appPassword, " ", "")

	note = truncate(strings.TrimSpace(in.Note), maxNoteLen)
	return email, password, totp, appPassword, note, nil
}

// normalizeFilter 规整过滤条件：去空白、去重、限制数量与天数。
func normalizeFilter(f Filter) (Filter, error) {
	include, err := normalizeKeywords(f.Include)
	if err != nil {
		return Filter{}, err
	}
	exclude, err := normalizeKeywords(f.Exclude)
	if err != nil {
		return Filter{}, err
	}
	days := f.Days
	if days < 0 {
		days = 0
	}
	if days > maxFilterDays {
		days = maxFilterDays
	}
	return Filter{Include: include, Exclude: exclude, Days: days}, nil
}

func normalizeKeywords(items []string) ([]string, error) {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if len([]rune(value)) > maxFilterLen {
			return nil, fmt.Errorf("过滤关键词不能超过 %d 个字符", maxFilterLen)
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) > maxFilterItems {
		return nil, fmt.Errorf("过滤关键词不能超过 %d 个", maxFilterItems)
	}
	return out, nil
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	return append([]string(nil), items...)
}

func newAccountID() string {
	return "gm_" + uuid.New().String()[:8]
}

// ===== 导入文本解析 =====

// dashSeparator 匹配 4 个及以上的连续连字符（与标签导入保持一致）。
var dashSeparator = regexp.MustCompile(`-{4,}`)

// maxSeparatorLen 限制自定义分隔符长度。
const maxSeparatorLen = 8

func normalizeSeparator(raw string) (string, error) {
	sep := strings.TrimSpace(raw)
	switch sep {
	case "":
		return "", nil
	case `\t`:
		return "\t", nil
	}
	if len([]rune(sep)) > maxSeparatorLen {
		return "", fmt.Errorf("分隔符不能超过 %d 个字符", maxSeparatorLen)
	}
	return sep, nil
}

type importLine struct {
	no     int
	fields []string
}

func parseImportText(text, separator string) []importLine {
	out := make([]importLine, 0)
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitImportLine(line, separator)
		for j := range fields {
			fields[j] = cleanField(fields[j])
			// 分隔符连字符多于 4 个时(如 -----),按 "----" 拆分会在下一个字段
			// 留下多余的 "-";这里统一剥掉字段开头的连字符。
			if j > 0 {
				fields[j] = strings.TrimLeft(fields[j], "-")
			}
		}
		out = append(out, importLine{no: i + 1, fields: fields})
	}
	return out
}

// splitImportLine 拆分一行。
//
// 默认按字面量 "----" 拆分,这样空字段(例如没有 2FA 密钥)能被正确保留:
// `账号----密码--------应用专用密码` 会解析出空的第三列。
func splitImportLine(line, separator string) []string {
	if separator != "" {
		return strings.Split(line, separator)
	}
	if strings.Contains(line, "----") {
		return strings.Split(line, "----")
	}
	for _, sep := range []string{"\t", "|"} {
		if strings.Contains(line, sep) {
			return strings.Split(line, sep)
		}
	}
	if strings.Contains(line, ",") {
		return strings.Split(line, ",")
	}
	return strings.Fields(line)
}

func cleanField(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") && len(s) > 2 {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func fieldAt(fields []string, index int) string {
	if index < 0 || index >= len(fields) {
		return ""
	}
	return fields[index]
}

// appendCappedError 限制错误条数，避免响应过大。
func appendCappedError(list *[]string, message string) {
	const maxErrors = 20
	if len(*list) < maxErrors {
		*list = append(*list, message)
	}
}

// SortSummaries 按邮箱排序（供调用方需要稳定输出时使用）。
func SortSummaries(items []Summary) {
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Email) < strings.ToLower(items[j].Email)
	})
}
