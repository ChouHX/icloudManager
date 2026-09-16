// Package share 提供隐私邮箱的取件链接。
//
// 每个别名可以生成一个 token,持有该 token 的人可以**只读**查看这个别名收到的
// 邮件,不需要管理员会话 —— 方便把某个地址的收件情况交给别人(或别的程序)看。
//
// 设计要点:
//   - token 为 256 位随机值的 base64url 编码,不携带任何账号信息
//   - 一个别名对应一个 token,可随时撤销;重新生成会轮换 token
//   - 记录命中次数与最近使用时间,便于发现异常访问
//   - 持久化到 data/share_links.json(0600),与 accounts.json 分离
package share

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// tokenBytes 是 token 的随机字节数(256 位熵)。
	tokenBytes = 32
	// storeFileName 是持久化文件名。
	storeFileName = "share_links.json"
	// persistInterval 是命中统计的落盘节流间隔(访问频繁时不必每次都写盘)。
	persistInterval = 30 * time.Second
)

// Link 是一条取件链接。
type Link struct {
	Token      string `json:"token"`
	AccountID  string `json:"account_id"`
	Alias      string `json:"alias"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	Hits       int    `json:"hits"`
}

// Store 管理取件链接(内存索引 + JSON 持久化)。
type Store struct {
	mu      sync.RWMutex
	path    string
	links   map[string]Link   // token → link
	byAlias map[string]string // account|alias(小写) → token

	lastPersist time.Time
}

type fileFormat struct {
	Links     map[string]Link `json:"links"`
	UpdatedAt string          `json:"updated_at,omitempty"`
}

// NewStore 打开(或创建)dataDir 下的取件链接存储。
func NewStore(dataDir string) (*Store, error) {
	store := &Store{
		path:    filepath.Join(dataDir, storeFileName),
		links:   make(map[string]Link),
		byAlias: make(map[string]string),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", storeFileName, err)
	}

	var payload fileFormat
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", storeFileName, err)
	}
	for token, link := range payload.Links {
		if token == "" || link.Alias == "" || link.AccountID == "" {
			continue
		}
		link.Token = token
		s.links[token] = link
		s.byAlias[aliasKey(link.AccountID, link.Alias)] = token
	}
	return nil
}

// saveLocked 落盘(调用方需持有写锁)。使用临时文件 + rename,避免写一半损坏。
func (s *Store) saveLocked() error {
	payload := fileFormat{Links: s.links, UpdatedAt: time.Now().Format(time.RFC3339)}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Ensure 返回该别名的取件链接;不存在时生成一个。
func (s *Store) Ensure(accountID, alias string) (Link, error) {
	accountID = strings.TrimSpace(accountID)
	alias = strings.TrimSpace(alias)
	if accountID == "" || alias == "" {
		return Link{}, fmt.Errorf("账号与别名不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if token, ok := s.byAlias[aliasKey(accountID, alias)]; ok {
		if link, exists := s.links[token]; exists {
			return link, nil
		}
	}

	token, err := newToken()
	if err != nil {
		return Link{}, err
	}
	link := Link{
		Token:     token,
		AccountID: accountID,
		Alias:     strings.ToLower(alias),
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	s.links[token] = link
	s.byAlias[aliasKey(accountID, alias)] = token

	if err := s.saveLocked(); err != nil {
		// 落盘失败则回滚内存状态,避免出现"重启后链接消失"的错觉
		delete(s.links, token)
		delete(s.byAlias, aliasKey(accountID, alias))
		return Link{}, err
	}
	s.lastPersist = time.Now()
	return link, nil
}

// Get 按 token 查询链接。
//
// 采用 map 查找而非逐条常量时间比较:token 有 256 位熵,时序侧信道没有实用价值。
func (s *Store) Get(token string) (Link, bool) {
	if token == "" {
		return Link{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	link, ok := s.links[token]
	return link, ok
}

// GetByAlias 查询某个别名当前的取件链接。
func (s *Store) GetByAlias(accountID, alias string) (Link, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.byAlias[aliasKey(accountID, alias)]
	if !ok {
		return Link{}, false
	}
	link, exists := s.links[token]
	return link, exists
}

// Revoke 撤销某个别名的取件链接,返回是否确实删除了一条。
func (s *Store) Revoke(accountID, alias string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := aliasKey(accountID, alias)
	token, ok := s.byAlias[key]
	if !ok {
		return false, nil
	}
	delete(s.byAlias, key)
	delete(s.links, token)

	if err := s.saveLocked(); err != nil {
		return false, err
	}
	s.lastPersist = time.Now()
	return true, nil
}

// List 返回某个账号下的全部取件链接,按创建时间升序。
func (s *Store) List(accountID string) []Link {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Link, 0, len(s.links))
	for _, link := range s.links {
		if link.AccountID == accountID {
			out = append(out, link)
		}
	}
	// 量级很小(n 通常 < 1000),直接排序即可
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt < out[j-1].CreatedAt; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Touch 记录一次使用(命中次数与最近使用时间)。
//
// 命中统计按 persistInterval 节流落盘:取件轮询可能很频繁,不必每次都写文件。
func (s *Store) Touch(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	link, ok := s.links[token]
	if !ok {
		return
	}
	link.Hits++
	link.LastUsedAt = time.Now().Format(time.RFC3339)
	s.links[token] = link

	if time.Since(s.lastPersist) > persistInterval {
		if err := s.saveLocked(); err == nil {
			s.lastPersist = time.Now()
		}
	}
}

func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 token 失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func aliasKey(accountID, alias string) string {
	return strings.ToLower(strings.TrimSpace(accountID)) + "|" + strings.ToLower(strings.TrimSpace(alias))
}
