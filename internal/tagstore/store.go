// Package tagstore 提供独立的「邮箱/密码 + 项目标签」凭据管理。
//
// 它与 iCloud 账号体系完全解耦:不读取、不修改 accounts.json,也不参与
// Hide My Email 的任何流程。用途是把「某个邮箱在哪个网站/项目用过、密码是什么」
// 集中记录下来,并用项目标签归类,方便区分每个项目下使用的账号。
//
// 持久化到 dataDir/tags.json(0600),与 accounts.json 分离。
package tagstore

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// storeFileName 是持久化文件名。
	storeFileName = "tags.json"

	maxEmailLen    = 254
	maxPasswordLen = 200
	maxSiteLen     = 100
	maxNoteLen     = 500
	maxTags        = 20
	maxTagLen      = 32
)

// Entry 是一条凭据记录:邮箱 + 网站密码 + 项目标签。
type Entry struct {
	ID        string   `json:"id"`
	Email     string   `json:"email"`
	Password  string   `json:"password,omitempty"`
	Site      string   `json:"site,omitempty"`
	Tags      []string `json:"tags"`
	Note      string   `json:"note,omitempty"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// Input 是创建/更新一条记录的输入。
type Input struct {
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Site     string   `json:"site"`
	Tags     []string `json:"tags"`
	Note     string   `json:"note"`
}

// Store 管理标签凭据(内存索引 + JSON 持久化),线程安全。
type Store struct {
	mu      sync.RWMutex
	path    string
	entries map[string]*Entry
}

type fileFormat struct {
	Entries   map[string]*Entry `json:"entries"`
	UpdatedAt string            `json:"updated_at,omitempty"`
}

// NewStore 打开(或创建)dataDir 下的标签凭据存储。
func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		path:    filepath.Join(dataDir, storeFileName),
		entries: make(map[string]*Entry),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", storeFileName, err)
	}
	var payload fileFormat
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", storeFileName, err)
	}
	for id, entry := range payload.Entries {
		if entry == nil || entry.Email == "" {
			continue
		}
		entry.ID = id
		if entry.Tags == nil {
			entry.Tags = []string{}
		}
		s.entries[id] = entry
	}
	return nil
}

// saveLocked 落盘(调用方需持有写锁)。使用临时文件 + rename,避免写一半损坏。
func (s *Store) saveLocked() error {
	payload := fileFormat{Entries: s.entries, UpdatedAt: time.Now().Format(time.RFC3339)}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List 返回全部记录,按更新时间倒序(最新在前)。
func (s *Store) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		out = append(out, cloneEntry(entry))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].Email < out[j].Email
	})
	return out
}

// Tags 返回所有记录中出现过的去重标签,按字母序排列。
//
// 同名标签出现不同大小写写法时,以创建时间最早(其次邮箱、ID)的记录为准,
// 保证结果可复现,不随 map 迭代顺序变化。
func (s *Store) Tags() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]*Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt < b.CreatedAt
		}
		if a.Email != b.Email {
			return a.Email < b.Email
		}
		return a.ID < b.ID
	})

	seen := make(map[string]string) // lower → 原始写法
	for _, entry := range entries {
		for _, tag := range entry.Tags {
			key := strings.ToLower(tag)
			if _, ok := seen[key]; !ok {
				seen[key] = tag
			}
		}
	}
	out := make([]string, 0, len(seen))
	for _, tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

// Get 按 ID 返回记录副本。
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[id]
	if !ok {
		return Entry{}, false
	}
	return cloneEntry(entry), true
}

// Create 新建一条记录。
func (s *Store) Create(in Input) (Entry, error) {
	email, password, site, tags, note, err := normalize(in)
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().Format(time.RFC3339)
	entry := &Entry{
		ID:        "tag_" + uuid.New().String()[:8],
		Email:     email,
		Password:  password,
		Site:      site,
		Tags:      tags,
		Note:      note,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entry.ID] = entry
	if err := s.saveLocked(); err != nil {
		delete(s.entries, entry.ID)
		return Entry{}, err
	}
	return cloneEntry(entry), nil
}

// Update 覆盖更新一条记录。
func (s *Store) Update(id string, in Input) (Entry, error) {
	email, password, site, tags, note, err := normalize(in)
	if err != nil {
		return Entry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return Entry{}, fmt.Errorf("记录不存在: %s", id)
	}
	prev := cloneEntry(entry)
	entry.Email = email
	entry.Password = password
	entry.Site = site
	entry.Tags = tags
	entry.Note = note
	entry.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := s.saveLocked(); err != nil {
		*entry = prev
		return Entry{}, err
	}
	return cloneEntry(entry), nil
}

// Delete 删除一条记录,返回是否存在。
func (s *Store) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return false, nil
	}
	delete(s.entries, id)
	if err := s.saveLocked(); err != nil {
		s.entries[id] = entry
		return false, err
	}
	return true, nil
}

func cloneEntry(entry *Entry) Entry {
	cp := *entry
	cp.Tags = cloneTags(entry.Tags)
	return cp
}

// cloneTags 返回非 nil 的标签副本,保证序列化为 [] 而不是 null。
func cloneTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	return append([]string(nil), tags...)
}

// normalize 校验并规整输入字段。
func normalize(in Input) (email, password, site string, tags []string, note string, err error) {
	email = strings.TrimSpace(in.Email)
	if email == "" {
		return "", "", "", nil, "", fmt.Errorf("邮箱不能为空")
	}
	if len([]rune(email)) > maxEmailLen {
		return "", "", "", nil, "", fmt.Errorf("邮箱长度超出限制")
	}
	if addr, perr := mail.ParseAddress(email); perr != nil || addr.Address != email {
		return "", "", "", nil, "", fmt.Errorf("邮箱地址格式无效")
	}

	password = strings.TrimSpace(in.Password)
	if len([]rune(password)) > maxPasswordLen {
		return "", "", "", nil, "", fmt.Errorf("密码长度不能超过 %d 个字符", maxPasswordLen)
	}

	site = strings.TrimSpace(in.Site)
	if len([]rune(site)) > maxSiteLen {
		return "", "", "", nil, "", fmt.Errorf("网站名称不能超过 %d 个字符", maxSiteLen)
	}

	note = strings.TrimSpace(in.Note)
	if len([]rune(note)) > maxNoteLen {
		return "", "", "", nil, "", fmt.Errorf("备注不能超过 %d 个字符", maxNoteLen)
	}

	tags, err = normalizeTags(in.Tags)
	if err != nil {
		return "", "", "", nil, "", err
	}
	return email, password, site, tags, note, nil
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
