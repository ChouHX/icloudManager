package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
)

// fakeBackend 是测试用内存 Backend,记录调用,不访问网络。
type fakeBackend struct {
	// accounts 可能被并发读取(后台任务循环会调 ListAccounts),必须加锁访问
	accountsMu sync.RWMutex
	accounts   []account.Summary
	aliases    []hme.Alias
	inbox      InboxResult
	created    *hme.CreateResult

	addedInput   account.AddAccountInput
	updatedID    string
	updatedInput account.UpdateAccountInput
	proxyID      string
	proxyValue   string
	cookiesID    string
	cookiesValue string
	appPwdID     string
	appPwdEmail  string
	loginID      string
	loginErr     error
	removedID    string
	removedOK    bool

	aliasActID     string
	aliasActActive bool
	aliasActErr    error
	aliasDeleteID  string
	aliasDeleteErr error
	listInboxQuery InboxQuery
	reloadCount    int

	// 多账号任务会并发调用,计数器必须是原子的
	createAliasCalls atomic.Int64
	// 记录最近一次 GetMessage 的选项,用于断言查询参数的传递
	lastMessageOpts mail.MessageOptions
	// fullMessage 非空时由 GetMessage 返回,便于构造越权等场景
	fullMessage *mail.FullMessage

	// 可选的行为注入:设置后优先于字段,便于测试精确控制每一轮结果
	createAliasFn func(accountID, label string) (*hme.CreateResult, error)
	listAliasesFn func(accountID string) ([]hme.Alias, error)
}

func (f *fakeBackend) ListAccounts() []account.Summary {
	f.accountsMu.RLock()
	defer f.accountsMu.RUnlock()
	return f.accounts
}

// setAccounts 并发安全地替换账号列表(测试中改账号时使用)。
func (f *fakeBackend) setAccounts(list []account.Summary) {
	f.accountsMu.Lock()
	defer f.accountsMu.Unlock()
	f.accounts = list
}

func (f *fakeBackend) AddAccount(in account.AddAccountInput) (account.Summary, error) {
	f.addedInput = in
	return account.Summary{ID: "acc_new", Name: in.Name, Status: "pending"}, nil
}

func (f *fakeBackend) UpdateAccount(id string, in account.UpdateAccountInput) (account.Summary, error) {
	f.updatedID, f.updatedInput = id, in
	if len(f.accounts) == 0 {
		return account.Summary{}, fmt.Errorf("fake: 更新失败")
	}
	return f.accounts[0], nil
}

func (f *fakeBackend) UpdateProxy(id, proxy string) (account.Summary, error) {
	f.proxyID, f.proxyValue = id, proxy
	if len(f.accounts) == 0 {
		return account.Summary{}, fmt.Errorf("fake: 代理更新失败")
	}
	return f.accounts[0], nil
}

func (f *fakeBackend) UpdateCookies(id, cookies string) (account.Summary, error) {
	f.cookiesID, f.cookiesValue = id, cookies
	if len(f.accounts) == 0 {
		return account.Summary{}, fmt.Errorf("fake: cookie 更新失败")
	}
	return f.accounts[0], nil
}

func (f *fakeBackend) SetAppPassword(id, email, appPassword string) (account.Summary, error) {
	f.appPwdID, f.appPwdEmail = id, email
	if len(f.accounts) == 0 {
		return account.Summary{}, fmt.Errorf("fake: 密码设置失败")
	}
	return f.accounts[0], nil
}

func (f *fakeBackend) SetMailbox(id string, config account.MailboxConfig) (account.Summary, error) {
	if len(f.accounts) == 0 {
		return account.Summary{}, fmt.Errorf("fake: 收件邮箱设置失败")
	}
	return f.accounts[0], nil
}

func (f *fakeBackend) LoginAccount(id, password, otp string) (account.Summary, error) {
	f.loginID = id
	if f.loginErr != nil {
		return account.Summary{}, f.loginErr
	}
	if len(f.accounts) == 0 {
		return account.Summary{}, fmt.Errorf("fake: 登录失败")
	}
	return f.accounts[0], nil
}

func (f *fakeBackend) RemoveAccount(id string) bool {
	f.removedID = id
	return f.removedOK
}

func (f *fakeBackend) CreateAlias(accountID, label string) (*hme.CreateResult, error) {
	f.createAliasCalls.Add(1)
	if f.createAliasFn != nil {
		return f.createAliasFn(accountID, label)
	}
	if f.created == nil {
		// 默认给一个合成结果:不让替身返回 (nil, nil) 这种真实实现不会出现的组合
		return &hme.CreateResult{Email: "auto@icloud.com", Label: label}, nil
	}
	return f.created, nil
}

func (f *fakeBackend) ListAliases(accountID string) ([]hme.Alias, error) {
	if f.listAliasesFn != nil {
		return f.listAliasesFn(accountID)
	}
	return f.aliases, nil
}

func (f *fakeBackend) SetAliasActive(accountID, anonymousID string, active bool) (bool, error) {
	f.aliasActID, f.aliasActActive = anonymousID, active
	return true, f.aliasActErr
}

func (f *fakeBackend) DeleteAlias(accountID, anonymousID string) error {
	f.aliasDeleteID = anonymousID
	return f.aliasDeleteErr
}

func (f *fakeBackend) ListInbox(q InboxQuery) (InboxResult, error) {
	f.listInboxQuery = q
	return f.inbox, nil
}

func (f *fakeBackend) GetMessage(accountID string, uid uint32, opts mail.MessageOptions) (*mail.FullMessage, error) {
	f.lastMessageOpts = opts
	if f.fullMessage != nil {
		return f.fullMessage, nil
	}
	return &mail.FullMessage{Message: mail.Message{ID: fmt.Sprint(uid)}}, nil
}

func (f *fakeBackend) DeleteMessage(accountID string, uid uint32) error { return nil }

func (f *fakeBackend) Reload() error {
	f.reloadCount++
	return nil
}

// newTestServer 构造带固定密码与 fake backend 的测试 Server。
func newTestServer(t *testing.T, f *fakeBackend) (*Server, *httptest.Server) {
	cfg := Config{
		Debug:         false,
		AdminPassword: "admin-pass-2026-strong",
		SessionTTL:    12 * time.Hour,
	}
	s := mustServer(t, f, cfg)
	ts := httptest.NewServer(s.Handler())
	return s, ts
}

// login 登录测试服务并返回 session Cookie 与 CSRF。
func login(t *testing.T, ts *httptest.Server, password string) (sessionCookie, csrf string) {
	t.Helper()
	body := fmt.Sprintf(`{"password":%q}`, password)
	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			CSRFToken string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "hme_session" {
			return c.Value, out.Data.CSRFToken
		}
	}
	t.Fatalf("响应未设置 hme_session Cookie (status=%d)", resp.StatusCode)
	return "", ""
}

// authedReq 构造带会话 Cookie 与 CSRF 头的请求。
func authedReq(t *testing.T, ts *httptest.Server, method, path, body string) *http.Request {
	t.Helper()
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func do(t *testing.T, req *http.Request) (int, string, []*http.Cookie) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw), resp.Cookies()
}
