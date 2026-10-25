// Package tagstore 提供独立的「邮箱/密码 + 项目标签」凭据管理。
//
// 它与 iCloud 账号体系完全解耦:不读取、不修改 accounts.json,也不参与
// Hide My Email 的任何流程。用途是把「某个邮箱在哪个网站/项目用过、密码是什么」
// 集中记录下来,并用项目标签归类,方便区分每个项目下使用的账号。
//
// 持久化到 dataDir/tags.db(纯 Go SQLite,文件权限 0600),与 accounts.json 分离。
// 旧版本的 dataDir/tags.json 会在首次启动时自动迁移到 SQLite,并重命名为
// tags.json.bak,避免重复导入。
package tagstore

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
	storeFileName = "tags.db"
	// legacyStoreFileName 是旧版 JSON 存储文件名,仅用于自动迁移。
	legacyStoreFileName = "tags.json"

	// defaultPageSize / maxPageSize 限制分页大小。
	defaultPageSize = 20
	maxPageSize     = 200

	maxEmailLen    = 254
	maxPasswordLen = 200
	maxSiteLen     = 100
	maxNoteLen     = 500
	maxTags        = 20
	maxTagLen      = 32
	// maxSites 限制单个邮箱可关联的网站数量。
	maxSites = 50
)

// Entry 是一条凭据记录:邮箱 + 网站密码 + 项目标签。
//
// 一个邮箱对应一条记录,它可能在多个网站用过,因此 Sites 是列表。
// Site 保留为「主网站」(Sites[0]),用于兼容只认单网站的旧客户端与旧备份。
type Entry struct {
	ID        string   `json:"id"`
	Email     string   `json:"email"`
	Password  string   `json:"password,omitempty"`
	Site      string   `json:"site,omitempty"`
	Sites     []string `json:"sites"`
	Tags      []string `json:"tags"`
	Note      string   `json:"note,omitempty"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// Input 是创建/更新一条记录的输入。
//
// Sites 优先;只传 Site 时按单网站处理(兼容旧调用方)。
type Input struct {
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Site     string   `json:"site"`
	Sites    []string `json:"sites"`
	Tags     []string `json:"tags"`
	Note     string   `json:"note"`
}

// Stats 是标签账号的汇总统计,供页面顶部的紧凑统计卡片使用。
type Stats struct {
	Total    int `json:"total"`
	Tags     int `json:"tags"`
	Sites    int `json:"sites"`
	Untagged int `json:"untagged"`
}

// TagUsage 是一个标签及其使用记录数。
type TagUsage struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// SiteUsage 是一个网站及其使用记录数。
type SiteUsage struct {
	Site  string `json:"site"`
	Count int    `json:"count"`
}

// SiteTagUsage 是单个网站下的标签分布。
//
// Total 是该网站的记录数(含未打标签的记录),作为各标签占比的分母;
// Tags 按使用记录数降序排列,各 Count 之和可能大于 Total(一条记录可带多个标签)。
type SiteTagUsage struct {
	Site  string     `json:"site"`
	Total int        `json:"total"`
	Tags  []TagUsage `json:"tags"`
}

// ListQuery 是分页/批量操作共用的筛选条件。
type ListQuery struct {
	Page     int
	PageSize int
	Search   string
	// Tags 按「任一命中」筛选(OR),忽略大小写。
	Tags []string
	// Site 按网站精确筛选(忽略大小写)。
	Site string
	// CreatedFrom / CreatedTo 按创建时间(RFC3339)闭区间筛选。
	// 批量导入的新记录,其创建时间即导入时间。
	CreatedFrom string
	CreatedTo   string
}

// hasFilter 判断是否包含任何有效筛选条件。
//
// 只有空白的标签/关键字不算筛选条件,避免批量操作误命中全部记录。
func (q ListQuery) hasFilter() bool {
	if strings.TrimSpace(q.Search) != "" ||
		strings.TrimSpace(q.Site) != "" ||
		strings.TrimSpace(q.CreatedFrom) != "" ||
		strings.TrimSpace(q.CreatedTo) != "" {
		return true
	}
	for _, tag := range q.Tags {
		if strings.TrimSpace(tag) != "" {
			return true
		}
	}
	return false
}

// ListResult 是分页查询结果。
type ListResult struct {
	Entries  []Entry
	Total    int
	Page     int
	PageSize int
}

// Store 管理标签凭据(SQLite 持久化),线程安全。
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

// legacyFileFormat 是旧版 tags.json 的结构。
type legacyFileFormat struct {
	Entries   map[string]*Entry `json:"entries"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// NewStore 打开(或创建)dataDir 下的标签凭据数据库。
func NewStore(dataDir string) (*Store, error) {
	// 统一转成绝对路径:SQLite 的 file: URI 不接受相对路径产生的
	// "file://./data/..." 形式(authority 为 ".")。
	dbPath, err := filepath.Abs(filepath.Join(dataDir, storeFileName))
	if err != nil {
		return nil, fmt.Errorf("解析标签数据库路径失败: %w", err)
	}
	// 预创建 WAL/SHM 文件并收紧权限,避免 SQLite 以默认 umask 创建可读文件。
	precreateSecure(dbPath)

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("打开标签数据库失败: %w", err)
	}
	// SQLite 写入串行化,避免多连接竞争导致的 database is locked。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, path: dbPath}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrateLegacyJSON(filepath.Join(dataDir, legacyStoreFileName)); err != nil {
		_ = db.Close()
		return nil, err
	}
	// 修复旧版导入遗留的 "-Lord" 一类网站名,避免同一网站被拆成多行统计。
	// 失败不影响启动,仅表示本次没有完成清理。
	_, _ = s.repairSiteNames()
	// 把旧版单值 site 回填到 entry_sites,再把「一个邮箱 + 一个网站一行」的
	// 历史数据合并成「一个邮箱一行」。
	_, _ = s.migrateSitesFromColumn()
	_, _ = s.mergeDuplicateEmails()
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

// sqliteDSN 构造带 pragma 的 SQLite DSN。
//
// 使用 file: URI 以便正确转义数据目录中的特殊字符。
func sqliteDSN(path string) string {
	// 手动拼接以避免 url.URL.String() 把相对路径渲染成 file://./...(非法 authority)。
	// 绝对路径在 Linux 下形如 file:///abs/tags.db,Windows 下形如 file:///C:/abs/tags.db。
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	return "file://" + (&url.URL{Path: slash}).EscapedPath() + "?" + q.Encode()
}

// precreateSecure 预先创建数据库相关文件并设置 0600 权限。
func precreateSecure(dbPath string) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		f, err := os.OpenFile(dbPath+suffix, os.O_CREATE|os.O_RDWR, 0o600)
		if err == nil {
			_ = f.Close()
		}
		_ = os.Chmod(dbPath+suffix, 0o600)
	}
}

// hardenFiles 在写入后收紧 SQLite 文件权限(忽略不存在的文件)。
func (s *Store) hardenFiles() {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Chmod(s.path+suffix, 0o600)
	}
}

func (s *Store) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS entries (
			id         TEXT PRIMARY KEY,
			email      TEXT NOT NULL,
			password   TEXT NOT NULL DEFAULT '',
			site       TEXT NOT NULL DEFAULT '',
			note       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS entry_tags (
			entry_id  TEXT NOT NULL,
			tag       TEXT NOT NULL,
			tag_lower TEXT NOT NULL,
			ord       INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (entry_id, tag_lower),
			FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS entry_sites (
			entry_id   TEXT NOT NULL,
			site       TEXT NOT NULL,
			site_lower TEXT NOT NULL,
			ord        INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (entry_id, site_lower),
			FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_updated ON entries(updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_email_lower ON entries(lower(email))`,
		`CREATE INDEX IF NOT EXISTS idx_entry_tags_lower ON entry_tags(tag_lower)`,
		`CREATE INDEX IF NOT EXISTS idx_entry_sites_lower ON entry_sites(site_lower)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("初始化标签数据库失败: %w", err)
		}
	}
	return nil
}

// migrateLegacyJSON 把旧版 tags.json 导入 SQLite(仅在数据库为空时)。
func (s *Store) migrateLegacyJSON(path string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&count); err != nil {
		return fmt.Errorf("检查标签数据库失败: %w", err)
	}
	if count > 0 {
		// 数据库已有数据,视为已完成迁移,保留 JSON 原文件,也不解析它。
		return nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取旧版标签数据失败: %w", err)
	}
	var payload legacyFileFormat
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("解析旧版标签数据失败: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().Format(time.RFC3339)
	for id, entry := range payload.Entries {
		if entry == nil || strings.TrimSpace(entry.Email) == "" {
			continue
		}
		if entry.ID == "" {
			entry.ID = id
		}
		if entry.ID == "" {
			entry.ID = newEntryID()
		}
		if entry.CreatedAt == "" {
			entry.CreatedAt = now
		}
		if entry.UpdatedAt == "" {
			entry.UpdatedAt = entry.CreatedAt
		}
		entry.Tags = cloneTags(entry.Tags)
		if err := insertEntryTx(tx, entry); err != nil {
			return fmt.Errorf("迁移旧版标签数据失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("迁移旧版标签数据失败: %w", err)
	}
	// 迁移成功后重命名,防止下次启动重复导入。
	_ = os.Rename(path, path+".bak")
	return nil
}

// List 返回全部记录,按更新时间倒序(最新在前)。
func (s *Store) List() []Entry {
	entries, err := s.listAll()
	if err != nil {
		return []Entry{}
	}
	return entries
}

func (s *Store) listAll() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id, email, password, site, note, created_at, updated_at
		FROM entries ORDER BY updated_at DESC, email ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	entries := make([]Entry, 0)
	ids := make([]string, 0)
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Email, &e.Password, &e.Site, &e.Note, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Tags = []string{}
		entries = append(entries, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	tags, err := s.tagsForIDs(ids)
	if err != nil {
		return nil, err
	}
	sites, err := s.sitesForIDs(ids)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if list, ok := tags[entries[i].ID]; ok {
			entries[i].Tags = list
		}
		entries[i].Sites = cloneSites(sites[entries[i].ID])
		entries[i].Site = primarySite(entries[i].Sites)
	}
	return entries, nil
}

// Get 按 ID 返回记录副本。
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := getEntry(s.db, id)
	if err != nil {
		return Entry{}, false
	}
	return entry, true
}

// ListPage 按条件分页查询记录。
func (s *Store) ListPage(q ListQuery) (ListResult, error) {
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	where, args := buildWhere(q)

	s.mu.Lock()
	defer s.mu.Unlock()

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM entries e `+where, args...).Scan(&total); err != nil {
		return ListResult{}, err
	}
	res := ListResult{Total: total, Page: page, PageSize: size, Entries: []Entry{}}
	if total == 0 {
		return res, nil
	}

	offset := (page - 1) * size
	if offset >= total {
		// 页码越界(例如删除最后一页的最后一条)时回退到最后一页。
		page = (total + size - 1) / size
		offset = (page - 1) * size
		res.Page = page
	}

	query := `SELECT id, email, password, site, note, created_at, updated_at
		FROM entries e ` + where + ` ORDER BY updated_at DESC, email ASC LIMIT ? OFFSET ?`
	rows, err := s.db.Query(query, append(cloneArgs(args), size, offset)...)
	if err != nil {
		return ListResult{}, err
	}
	defer func() { _ = rows.Close() }()

	entries := make([]Entry, 0, size)
	ids := make([]string, 0, size)
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Email, &e.Password, &e.Site, &e.Note, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return ListResult{}, err
		}
		e.Tags = []string{}
		entries = append(entries, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	_ = rows.Close()
	tags, err := s.tagsForIDs(ids)
	if err != nil {
		return ListResult{}, err
	}
	sites, err := s.sitesForIDs(ids)
	if err != nil {
		return ListResult{}, err
	}
	for i := range entries {
		if list, ok := tags[entries[i].ID]; ok {
			entries[i].Tags = list
		}
		entries[i].Sites = cloneSites(sites[entries[i].ID])
		entries[i].Site = primarySite(entries[i].Sites)
	}
	res.Entries = entries
	return res, nil
}

// Tags 返回所有记录中出现过的去重标签,按字母序排列。
//
// 同名标签出现不同大小写写法时,以创建时间最早(其次邮箱、ID)的记录为准,
// 保证结果可复现。
func (s *Store) Tags() []string {
	usages, err := s.TagCounts()
	if err != nil {
		return []string{}
	}
	out := make([]string, len(usages))
	for i, usage := range usages {
		out[i] = usage.Tag
	}
	return out
}

// TagCounts 返回每个标签及其使用记录数,按标签字母序排列。
func (s *Store) TagCounts() ([]TagUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT tag_lower, tag, usage FROM (
			SELECT t.tag_lower AS tag_lower,
			       t.tag AS tag,
			       COUNT(*) OVER (PARTITION BY t.tag_lower) AS usage,
			       ROW_NUMBER() OVER (
			           PARTITION BY t.tag_lower
			           ORDER BY e.created_at, e.email, e.id
			       ) AS rn
			FROM entry_tags t
			JOIN entries e ON e.id = t.entry_id
		) WHERE rn = 1`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]TagUsage, 0)
	for rows.Next() {
		var lower, tag string
		var usage int
		if err := rows.Scan(&lower, &tag, &usage); err != nil {
			return nil, err
		}
		out = append(out, TagUsage{Tag: tag, Count: usage})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out, nil
}

// SiteCounts 返回每个网站及其使用记录数,按记录数降序、网站名升序排列。
//
// 一个邮箱可关联多个网站,因此按 entry_sites 统计、按 entry_id 去重。
func (s *Store) SiteCounts() ([]SiteUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT MIN(site) AS site, COUNT(DISTINCT entry_id) AS usage
		FROM entry_sites
		GROUP BY site_lower
		ORDER BY usage DESC, lower(site) ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]SiteUsage, 0)
	for rows.Next() {
		var usage SiteUsage
		if err := rows.Scan(&usage.Site, &usage.Count); err != nil {
			return nil, err
		}
		out = append(out, usage)
	}
	return out, rows.Err()
}

// Stats 返回标签账号的汇总统计。
func (s *Store) Stats() (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var st Stats
	err := s.db.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM entries),
			(SELECT COUNT(DISTINCT tag_lower) FROM entry_tags),
			(SELECT COUNT(DISTINCT site_lower) FROM entry_sites),
			(SELECT COUNT(*) FROM entries e
				WHERE NOT EXISTS (SELECT 1 FROM entry_tags t WHERE t.entry_id = e.id))`).
		Scan(&st.Total, &st.Tags, &st.Sites, &st.Untagged)
	if err != nil {
		return Stats{}, err
	}
	return st, nil
}

// dashOnlyPrefix 匹配以连字符开头、后面还有内容的网站名。
//
// 这类值是旧版导入按字面量 "----" 拆分时的分隔符残留(例如 5 个连字符被切成
// "----" + "-",网站名就成了 "-Lord")。它会把同一个网站拆成多行统计。
var dashOnlyPrefix = regexp.MustCompile(`^-+[ \t]*(?P<rest>\S.*)$`)

// repairSiteNames 清理历史数据里由导入分隔符残留产生的网站名(如 "-Lord" → "Lord")。
//
// 幂等:没有残留数据时不产生任何写入;重复启动也不会再次变更。
func (s *Store) repairSiteNames() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT DISTINCT site FROM entries WHERE site LIKE '-%'`)
	if err != nil {
		return 0, err
	}
	names := make([]string, 0)
	for rows.Next() {
		var site string
		if err := rows.Scan(&site); err != nil {
			_ = rows.Close()
			return 0, err
		}
		names = append(names, site)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	var affected int64
	for _, site := range names {
		match := dashOnlyPrefix.FindStringSubmatch(site)
		if match == nil {
			continue
		}
		cleaned := strings.TrimSpace(match[1])
		if cleaned == "" || cleaned == site {
			continue
		}
		res, err := s.db.Exec(`UPDATE entries SET site = ? WHERE site = ?`, cleaned, site)
		if err != nil {
			return affected, err
		}
		n, _ := res.RowsAffected()
		affected += n

		// entry_sites 里可能已有同名行(同一邮箱的另一个网站),冲突时直接删掉残留。
		siteRows, err := s.db.Query(`SELECT entry_id, site FROM entry_sites WHERE site = ?`, site)
		if err != nil {
			return affected, err
		}
		type siteRow struct{ entryID, site string }
		pending := make([]siteRow, 0)
		for siteRows.Next() {
			var row siteRow
			if err := siteRows.Scan(&row.entryID, &row.site); err != nil {
				_ = siteRows.Close()
				return affected, err
			}
			pending = append(pending, row)
		}
		if err := siteRows.Err(); err != nil {
			_ = siteRows.Close()
			return affected, err
		}
		_ = siteRows.Close()
		for _, row := range pending {
			res, err := s.db.Exec(`UPDATE OR IGNORE entry_sites SET site = ?, site_lower = ?
				WHERE entry_id = ? AND site = ?`, cleaned, strings.ToLower(cleaned), row.entryID, site)
			if err != nil {
				return affected, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				if _, err := s.db.Exec(`DELETE FROM entry_sites WHERE entry_id = ? AND site = ?`,
					row.entryID, site); err != nil {
					return affected, err
				}
			}
			affected++
		}
	}
	if affected > 0 {
		s.hardenFiles()
	}
	return affected, nil
}

// SiteTagCounts 返回每个网站下的标签分布,按网站记录数降序、网站名升序排列。
//
// 用于回答「同一个网站里,各标签各占多少」:例如网站1 有 100 条记录,
// 其中标签1 40 条、标签2 60 条,则占比为 40% : 60%。
// 网站与标签均忽略大小写归并,展示名取字典序最小的写法。
func (s *Store) SiteTagCounts() ([]SiteTagUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	totals := make(map[string]int)
	totalRows, err := s.db.Query(`SELECT site_lower, COUNT(DISTINCT entry_id)
		FROM entry_sites GROUP BY site_lower`)
	if err != nil {
		return nil, err
	}
	for totalRows.Next() {
		var key string
		var count int
		if err := totalRows.Scan(&key, &count); err != nil {
			_ = totalRows.Close()
			return nil, err
		}
		totals[key] = count
	}
	if err := totalRows.Err(); err != nil {
		_ = totalRows.Close()
		return nil, err
	}
	_ = totalRows.Close()

	rows, err := s.db.Query(`
		SELECT s.site_lower AS site_key, MIN(s.site) AS site,
		       t.tag_lower AS tag_key, MIN(t.tag) AS tag, COUNT(DISTINCT s.entry_id) AS usage
		FROM entry_sites s
		JOIN entry_tags t ON t.entry_id = s.entry_id
		GROUP BY site_key, tag_key
		ORDER BY site_key, usage DESC, tag_key`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]SiteTagUsage, 0)
	index := make(map[string]int)
	for rows.Next() {
		var siteKey, site, tagKey, tag string
		var usage int
		if err := rows.Scan(&siteKey, &site, &tagKey, &tag, &usage); err != nil {
			return nil, err
		}
		pos, ok := index[siteKey]
		if !ok {
			pos = len(out)
			index[siteKey] = pos
			out = append(out, SiteTagUsage{Site: site, Total: totals[siteKey], Tags: []TagUsage{}})
		}
		out[pos].Tags = append(out[pos].Tags, TagUsage{Tag: tag, Count: usage})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 网站顺序与 SiteCounts 保持一致:记录数多的在前,同数量按名称。
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return strings.ToLower(out[i].Site) < strings.ToLower(out[j].Site)
	})
	return out, nil
}

// Sites 返回所有记录中出现过的去重网站名,按字母序排列。
//
// 同名网站存在不同大小写写法时,取字典序最小的写法,保证结果可复现。
func (s *Store) Sites() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT MIN(site) FROM entry_sites
		GROUP BY site_lower ORDER BY lower(site)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]string, 0)
	for rows.Next() {
		var site string
		if err := rows.Scan(&site); err != nil {
			return nil, err
		}
		out = append(out, site)
	}
	return out, rows.Err()
}

// Create 新建一条记录。
func (s *Store) Create(in Input) (Entry, error) {
	email, password, sites, tags, note, err := normalize(in)
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().Format(time.RFC3339)
	entry := &Entry{
		ID:        newEntryID(),
		Email:     email,
		Password:  password,
		Site:      primarySite(sites),
		Sites:     sites,
		Tags:      tags,
		Note:      note,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := insertEntry(s.db, entry); err != nil {
		return Entry{}, err
	}
	s.hardenFiles()
	return cloneEntry(entry), nil
}

// Update 覆盖更新一条记录。
func (s *Store) Update(id string, in Input) (Entry, error) {
	email, password, sites, tags, note, err := normalize(in)
	if err != nil {
		return Entry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var createdAt string
	if err := tx.QueryRow(`SELECT created_at FROM entries WHERE id = ?`, id).Scan(&createdAt); err != nil {
		if err == sql.ErrNoRows {
			return Entry{}, fmt.Errorf("记录不存在: %s", id)
		}
		return Entry{}, err
	}
	now := time.Now().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE entries SET email = ?, password = ?, site = ?, note = ?, updated_at = ? WHERE id = ?`,
		email, password, primarySite(sites), note, now, id); err != nil {
		return Entry{}, err
	}
	if err := replaceSitesTx(tx, id, sites); err != nil {
		return Entry{}, err
	}
	if err := replaceTagsTx(tx, id, tags); err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, err
	}
	s.hardenFiles()
	return Entry{
		ID:        id,
		Email:     email,
		Password:  password,
		Site:      primarySite(sites),
		Sites:     cloneSites(sites),
		Tags:      cloneTags(tags),
		Note:      note,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}, nil
}

// Delete 删除一条记录,返回是否存在。
func (s *Store) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM entries WHERE id = ?`, id)
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

// ===== 内部辅助函数 =====

func newEntryID() string {
	return "tag_" + uuid.New().String()[:8]
}

func cloneEntry(entry *Entry) Entry {
	cp := *entry
	cp.Tags = cloneTags(entry.Tags)
	cp.Sites = cloneSites(entry.Sites)
	return cp
}

// cloneTags 返回非 nil 的标签副本,保证序列化为 [] 而不是 null。
func cloneTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	return append([]string(nil), tags...)
}

func cloneArgs(args []any) []any {
	out := make([]any, len(args), len(args)+2)
	copy(out, args)
	return out
}

// buildWhere 生成分页/批量操作共用的 WHERE 子句(表别名 e)。
func buildWhere(q ListQuery) (string, []any) {
	clauses := make([]string, 0, 5)
	args := make([]any, 0, 6+len(q.Tags))

	search := strings.TrimSpace(q.Search)
	if search != "" {
		like := "%" + strings.ToLower(search) + "%"
		clauses = append(clauses, `(lower(e.email) LIKE ? OR lower(e.note) LIKE ?
			OR EXISTS (SELECT 1 FROM entry_sites s WHERE s.entry_id = e.id AND lower(s.site) LIKE ?)
			OR EXISTS (SELECT 1 FROM entry_tags t WHERE t.entry_id = e.id AND lower(t.tag) LIKE ?))`)
		args = append(args, like, like, like, like)
	}

	if site := strings.TrimSpace(q.Site); site != "" {
		clauses = append(clauses, `EXISTS (SELECT 1 FROM entry_sites s
			WHERE s.entry_id = e.id AND s.site_lower = lower(?))`)
		args = append(args, site)
	}
	if from := strings.TrimSpace(q.CreatedFrom); from != "" {
		clauses = append(clauses, `datetime(e.created_at) >= datetime(?)`)
		args = append(args, from)
	}
	if to := strings.TrimSpace(q.CreatedTo); to != "" {
		clauses = append(clauses, `datetime(e.created_at) <= datetime(?)`)
		args = append(args, to)
	}

	if tags := q.Tags; len(tags) > 0 {
		seen := make(map[string]struct{}, len(tags))
		lowers := make([]string, 0, len(tags))
		for _, tag := range tags {
			key := strings.ToLower(strings.TrimSpace(tag))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			lowers = append(lowers, key)
		}
		if len(lowers) > 0 {
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(lowers)), ",")
			clauses = append(clauses, `EXISTS (SELECT 1 FROM entry_tags t
				WHERE t.entry_id = e.id AND t.tag_lower IN (`+placeholders+`))`)
			for _, lower := range lowers {
				args = append(args, lower)
			}
		}
	}

	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

// tagsForIDs 批量读取若干记录的标签,按 ord 保持原始顺序。
func (s *Store) tagsForIDs(ids []string) (map[string][]string, error) {
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
		rows, err := s.db.Query(`SELECT entry_id, tag FROM entry_tags
			WHERE entry_id IN (`+placeholders+`) ORDER BY entry_id, ord`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, tag string
			if err := rows.Scan(&id, &tag); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[id] = append(out[id], tag)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// ==== SQL 执行器抽象,便于在 db / tx 之间复用 ====

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Prepare(query string) (*sql.Stmt, error)
}

func insertEntry(db execer, entry *Entry) error {
	sites := entry.Sites
	if len(sites) == 0 && entry.Site != "" {
		sites = []string{entry.Site}
	}
	_, err := db.Exec(`INSERT INTO entries(id, email, password, site, note, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Email, entry.Password, primarySite(sites), entry.Note, entry.CreatedAt, entry.UpdatedAt)
	if err != nil {
		return err
	}
	if err := insertSites(db, entry.ID, sites); err != nil {
		return err
	}
	return insertTags(db, entry.ID, entry.Tags)
}

func insertEntryTx(tx *sql.Tx, entry *Entry) error {
	return insertEntry(tx, entry)
}

func insertTags(db execer, entryID string, tags []string) error {
	stmt, err := db.Prepare(`INSERT INTO entry_tags(entry_id, tag, tag_lower, ord) VALUES(?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, tag := range tags {
		if _, err := stmt.Exec(entryID, tag, strings.ToLower(tag), i); err != nil {
			return err
		}
	}
	return nil
}

// replaceTagsTx 先清空记录的所有标签再写入。
func replaceTagsTx(tx *sql.Tx, entryID string, tags []string) error {
	if _, err := tx.Exec(`DELETE FROM entry_tags WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	return insertTags(tx, entryID, tags)
}

func loadTags(db execer, entryID string) ([]string, error) {
	rows, err := db.Query(`SELECT tag FROM entry_tags WHERE entry_id = ? ORDER BY ord`, entryID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tags := make([]string, 0)
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func getEntry(db execer, id string) (Entry, error) {
	var e Entry
	err := db.QueryRow(`SELECT id, email, password, site, note, created_at, updated_at
		FROM entries WHERE id = ?`, id).
		Scan(&e.ID, &e.Email, &e.Password, &e.Site, &e.Note, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return Entry{}, err
	}
	tags, err := loadTags(db, id)
	if err != nil {
		return Entry{}, err
	}
	sites, err := loadSites(db, id)
	if err != nil {
		return Entry{}, err
	}
	if len(sites) == 0 && e.Site != "" {
		sites = []string{e.Site}
	}
	e.Sites = cloneSites(sites)
	e.Site = primarySite(e.Sites)
	e.Tags = cloneTags(tags)
	return e, nil
}

// normalize 校验并规整输入字段。
//
// 网站支持多值:优先取 Sites,为空时回退到单值的 Site(兼容旧调用方)。
func normalize(in Input) (email, password string, sites, tags []string, note string, err error) {
	email = strings.TrimSpace(in.Email)
	if email == "" {
		return "", "", nil, nil, "", fmt.Errorf("邮箱不能为空")
	}
	if len([]rune(email)) > maxEmailLen {
		return "", "", nil, nil, "", fmt.Errorf("邮箱长度超出限制")
	}
	if addr, perr := mail.ParseAddress(email); perr != nil || addr.Address != email {
		return "", "", nil, nil, "", fmt.Errorf("邮箱地址格式无效")
	}

	password = strings.TrimSpace(in.Password)
	if len([]rune(password)) > maxPasswordLen {
		return "", "", nil, nil, "", fmt.Errorf("密码长度不能超过 %d 个字符", maxPasswordLen)
	}

	rawSites := in.Sites
	if len(rawSites) == 0 && strings.TrimSpace(in.Site) != "" {
		rawSites = []string{in.Site}
	}
	sites, err = normalizeSites(rawSites)
	if err != nil {
		return "", "", nil, nil, "", err
	}

	note = strings.TrimSpace(in.Note)
	if len([]rune(note)) > maxNoteLen {
		return "", "", nil, nil, "", fmt.Errorf("备注不能超过 %d 个字符", maxNoteLen)
	}

	tags, err = normalizeTags(in.Tags)
	if err != nil {
		return "", "", nil, nil, "", err
	}
	return email, password, sites, tags, note, nil
}

// normalizeSites 去空白、去重(忽略大小写,保留首次出现的写法)并限制数量/长度。
func normalizeSites(raw []string) ([]string, error) {
	sites := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		site := strings.TrimSpace(item)
		if site == "" {
			continue
		}
		if len([]rune(site)) > maxSiteLen {
			return nil, fmt.Errorf("网站名称不能超过 %d 个字符", maxSiteLen)
		}
		key := strings.ToLower(site)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		sites = append(sites, site)
	}
	if len(sites) > maxSites {
		return nil, fmt.Errorf("单个邮箱的网站数量不能超过 %d 个", maxSites)
	}
	return sites, nil
}

// normalizeTags 去空白、去重(忽略大小写,保留首次出现的写法)并限制数量/长度。
func normalizeTags(raw []string) ([]string, error) {
	tags := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		tag := strings.TrimSpace(item)
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > maxTagLen {
			return nil, fmt.Errorf("单个标签不能超过 %d 个字符", maxTagLen)
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
	}
	if len(tags) > maxTags {
		return nil, fmt.Errorf("标签数量不能超过 %d 个", maxTags)
	}
	return tags, nil
}
