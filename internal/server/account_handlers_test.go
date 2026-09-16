package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"icloud-hme/internal/account"
)

// writeSecretAccounts 把含秘密的账号写入测试数据目录。
func writeSecretAccounts(t *testing.T, dir string) {
	t.Helper()
	data := `{
  "accounts": {
    "acc_secret": {
      "id": "acc_secret",
      "name": "秘密账号",
      "real_email": "owner@example.com",
      "icloud_email": "owner@icloud.com",
      "cookies": {"X-APPLE-WEBAUTH-USER": "cookie-secret", "dsid": "dsid-secret"},
      "host": "icloud.com",
      "proxy": "http://user:proxy-secret@example.com:8080",
      "app_password": "app-secret",
      "status": "active",
      "alias_total": 3,
      "alias_active": 2,
      "last_validated": "2026-08-04T09:00:00+08:00",
      "created_at": "2026-08-01T09:00:00+08:00"
    }
  },
  "updated_at": "2026-08-04T09:00:00+08:00"
}`
	if err := os.WriteFile(filepath.Join(dir, "accounts.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestAccountResponseNoSecrets 验证账号列表响应不包含任何秘密。
func TestAccountResponseNoSecrets(t *testing.T) {
	dir := t.TempDir()
	writeSecretAccounts(t, dir)
	mgr, err := account.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := mustServer(t, &managerBackend{mgr: mgr}, Config{
		Debug:         false,
		AdminPassword: "admin-pass-2026-strong",
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")
	req := authedReq(t, ts, "GET", "/api/accounts", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	status, body, _ := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("期望 200,得到 %d: %s", status, body)
	}

	// 字节级断言:不含秘密子串
	for _, secret := range []string{"cookie-secret", "app-secret", "proxy-secret", "dsid-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("响应泄露秘密 %q: %s", secret, body)
		}
	}

	// 精确键断言:不存在 cookies / app_password / proxy 键
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 1 {
		t.Fatalf("期望 1 个账号,得到 %d", len(out.Data))
	}
	acc := out.Data[0]
	for _, forbidden := range []string{"cookies", "app_password", "proxy"} {
		if _, exists := acc[forbidden]; exists {
			t.Fatalf("响应包含禁止键 %q", forbidden)
		}
	}
	// 合法键存在
	for _, required := range []string{"has_cookies", "has_app_password", "has_proxy", "id", "name"} {
		if _, exists := acc[required]; !exists {
			t.Fatalf("响应缺少键 %q", required)
		}
	}
	if acc["has_cookies"] != true || acc["has_app_password"] != true || acc["has_proxy"] != true {
		t.Fatalf("凭据状态错误: %v", acc)
	}
	_ = csrf
}

// TestAccountLoginResponseNoSecrets 验证 iCloud 登录成功响应只含 Summary 字段。
func TestAccountLoginResponseNoSecrets(t *testing.T) {
	f := &fakeBackend{
		accounts: []account.Summary{{
			ID: "acc_secret", Name: "秘密账号", Status: "active",
			HasCookies: true, HasAppPassword: true, HasProxy: true,
		}},
	}
	s := mustServer(t, f, Config{
		Debug:         false,
		AdminPassword: "admin-pass-2026-strong",
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")
	req := authedReq(t, ts, "POST", "/api/accounts/acc_secret/login", `{"password":"p@ssw0rd-2026"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)
	status, body, _ := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("期望 200,得到 %d: %s", status, body)
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if _, exists := out.Data["cookies"]; exists {
		t.Fatalf("登录响应不应包含 cookies 键")
	}
	// 只允许 Summary 字段
	for key := range out.Data {
		switch key {
		case "id", "name", "real_email", "icloud_email", "host", "status", "alias_total",
			"alias_active", "has_cookies", "has_app_password", "has_proxy",
			"last_validated", "status_message", "created_at":
		default:
			t.Fatalf("登录响应包含意外字段 %q", key)
		}
	}
}

// TestAccountHandlerValidation 验证账号端点的参数校验(使用真实 manager 适配器)。
func TestAccountHandlerValidation(t *testing.T) {
	dir := t.TempDir()
	mgr, err := account.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := mustServer(t, &managerBackend{mgr: mgr}, Config{
		Debug:         false,
		AdminPassword: "admin-pass-2026-strong",
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		code   string
	}{
		{"空名称", "POST", "/api/accounts", `{"name":"","icloud_email":"a@icloud.com"}`, 400, "VALIDATION_ERROR"},
		{"空邮箱", "POST", "/api/accounts", `{"name":"主号","icloud_email":""}`, 400, "VALIDATION_ERROR"},
		{"非法主机", "POST", "/api/accounts", `{"name":"主号","icloud_email":"a@icloud.com","host":"evil.com"}`, 400, "VALIDATION_ERROR"},
		{"非法代理", "POST", "/api/accounts", `{"name":"主号","icloud_email":"a@icloud.com","proxy":"ftp://x"}`, 400, "VALIDATION_ERROR"},
		{"PATCH 空更新", "PATCH", "/api/accounts/acc_1", `{}`, 400, "VALIDATION_ERROR"},
		{"PUT 非法代理", "PUT", "/api/accounts/acc_1/proxy", `{"proxy":"ftp://x"}`, 400, "VALIDATION_ERROR"},
	}
	for _, tc := range cases {
		req := authedReq(t, ts, tc.method, tc.path, tc.body)
		req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
		req.Header.Set("X-CSRF-Token", csrf)
		status, body, _ := do(t, req)
		if status != tc.status {
			t.Fatalf("%s: 期望 %d,得到 %d: %s", tc.name, tc.status, status, body)
		}
		var out struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal([]byte(body), &out)
		if out.Code != tc.code {
			t.Fatalf("%s: 期望 code=%s,得到 %q", tc.name, tc.code, out.Code)
		}
	}
}

// TestAccountUpdateCookiesAcceptString 验证 PUT cookies 同时接受字符串和对象。
func TestAccountUpdateCookiesAcceptString(t *testing.T) {
	f := &fakeBackend{accounts: []account.Summary{{ID: "acc_1", Name: "主号"}}}
	s := mustServer(t, f, Config{
		Debug:         false,
		AdminPassword: "admin-pass-2026-strong",
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	// 字符串输入
	req := authedReq(t, ts, "PUT", "/api/accounts/acc_1/cookies", `{"cookies":"a=1; b=2"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)
	status, _, _ := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("字符串 cookies 期望 200,得到 %d", status)
	}

	// 对象输入
	req = authedReq(t, ts, "PUT", "/api/accounts/acc_1/cookies", `{"cookies":{"a":"1","b":"2"}}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)
	status, _, _ = do(t, req)
	if status != http.StatusOK {
		t.Fatalf("对象 cookies 期望 200,得到 %d", status)
	}
}

// TestAccountDeleteNotFound 验证删除不存在的账号返回 404/ACCOUNT_NOT_FOUND。
func TestAccountDeleteNotFound(t *testing.T) {
	f := &fakeBackend{removedOK: false}
	s := mustServer(t, f, Config{
		Debug:         false,
		AdminPassword: "admin-pass-2026-strong",
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")
	req := authedReq(t, ts, "DELETE", "/api/accounts/acc_missing", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)
	status, body, _ := do(t, req)
	if status != http.StatusNotFound {
		t.Fatalf("期望 404,得到 %d", status)
	}
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if out.Code != "ACCOUNT_NOT_FOUND" {
		t.Fatalf("期望 code=ACCOUNT_NOT_FOUND,得到 %q", out.Code)
	}
}

var _ = io.Discard
