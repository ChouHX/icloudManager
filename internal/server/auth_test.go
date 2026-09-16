package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAuthRequiresSession 验证不带 Cookie 的 API 返回 401/AUTH_REQUIRED。
func TestAuthRequiresSession(t *testing.T) {
	f := &fakeBackend{}
	s, ts := newTestServer(t, f)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/accounts", nil)
	status, body, _ := do(t, req)
	if status != http.StatusUnauthorized {
		t.Fatalf("期望 401,得到 %d", status)
	}
	var out struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != "AUTH_REQUIRED" {
		t.Fatalf("期望 code=AUTH_REQUIRED,得到 %q", out.Code)
	}
	_ = s
}

// TestAuthWrongPassword 验证错误密码返回 401/INVALID_CREDENTIALS 且不设置 Cookie。
func TestAuthWrongPassword(t *testing.T) {
	f := &fakeBackend{}
	_, ts := newTestServer(t, f)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/login", strings.NewReader(`{"password":"wrong-password"}`))
	req.Header.Set("Content-Type", "application/json")
	status, body, cookies := do(t, req)
	if status != http.StatusUnauthorized {
		t.Fatalf("期望 401,得到 %d", status)
	}
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if out.Code != "INVALID_CREDENTIALS" {
		t.Fatalf("期望 code=INVALID_CREDENTIALS,得到 %q", out.Code)
	}
	for _, c := range cookies {
		if c.Name == "hme_session" {
			t.Fatal("错误密码不应设置 hme_session Cookie")
		}
	}
}

// TestAuthLoginSuccess 验证正确登录设置 HttpOnly/SameSite=Strict Cookie。
func TestAuthLoginSuccess(t *testing.T) {
	f := &fakeBackend{}
	_, ts := newTestServer(t, f)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/login", strings.NewReader(`{"password":"admin-pass-2026-strong"}`))
	req.Header.Set("Content-Type", "application/json")
	status, body, cookies := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("期望 200,得到 %d: %s", status, body)
	}
	var out struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.CSRFToken == "" {
		t.Fatal("响应应包含 csrf_token")
	}
	if _, err := time.Parse(time.RFC3339, out.Data.ExpiresAt); err != nil {
		t.Fatalf("expires_at 应为 RFC3339: %v", err)
	}
	var sess *http.Cookie
	for _, c := range cookies {
		if c.Name == "hme_session" {
			sess = c
		}
	}
	if sess == nil {
		t.Fatal("登录成功应设置 hme_session Cookie")
	}
	if !sess.HttpOnly {
		t.Fatal("hme_session 应设置 HttpOnly")
	}
	if sess.SameSite != http.SameSiteStrictMode {
		t.Fatalf("hme_session SameSite 应为 Strict,得到 %v", sess.SameSite)
	}
	if sess.Path != "/" {
		t.Fatalf("hme_session Path 应为 /,得到 %q", sess.Path)
	}
}

// TestAuthSessionFlow 验证 Cookie 有效时 GET 成功;POST 需要 CSRF。
func TestAuthSessionFlow(t *testing.T) {
	f := &fakeBackend{}
	_, ts := newTestServer(t, f)
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	// 带 Cookie 的 GET 成功
	req := authedReq(t, ts, "GET", "/api/accounts", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	status, body, _ := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("带 Cookie 的 GET 期望 200,得到 %d: %s", status, body)
	}

	// 无 CSRF 的 POST 返回 403/CSRF_INVALID
	req = authedReq(t, ts, "POST", "/api/accounts", `{"name":"x","icloud_email":"a@icloud.com"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	status, body, _ = do(t, req)
	if status != http.StatusForbidden {
		t.Fatalf("无 CSRF 的 POST 期望 403,得到 %d: %s", status, body)
	}
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if out.Code != "CSRF_INVALID" {
		t.Fatalf("期望 code=CSRF_INVALID,得到 %q", out.Code)
	}

	// 带正确 CSRF 头成功
	req = authedReq(t, ts, "POST", "/api/accounts", `{"name":"x","icloud_email":"a@icloud.com"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)
	status, _, _ = do(t, req)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("带 CSRF 的 POST 期望 2xx,得到 %d", status)
	}
}

// TestAuthLogout 验证退出后同一 Cookie 立即失效。
func TestAuthLogout(t *testing.T) {
	f := &fakeBackend{}
	_, ts := newTestServer(t, f)
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	req := authedReq(t, ts, "POST", "/api/auth/logout", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)
	status, _, _ := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("退出期望 200,得到 %d", status)
	}

	// 同一 Cookie 立即失效
	req = authedReq(t, ts, "GET", "/api/accounts", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	status, _, _ = do(t, req)
	if status != http.StatusUnauthorized {
		t.Fatalf("退出后 GET 期望 401,得到 %d", status)
	}
}

// TestAuthRateLimit 验证同 IP 连续 6 次失败后第 6 次返回 429/RATE_LIMITED。
func TestAuthRateLimit(t *testing.T) {
	f := &fakeBackend{}
	_, ts := newTestServer(t, f)
	defer ts.Close()

	var lastStatus int
	var lastBody string
	for i := 0; i < 6; i++ {
		req, _ := http.NewRequest("POST", ts.URL+"/api/auth/login", strings.NewReader(`{"password":"wrong-password"}`))
		req.Header.Set("Content-Type", "application/json")
		lastStatus, lastBody, _ = do(t, req)
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("第 6 次失败期望 429,得到 %d: %s", lastStatus, lastBody)
	}
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal([]byte(lastBody), &out)
	if out.Code != "RATE_LIMITED" {
		t.Fatalf("期望 code=RATE_LIMITED,得到 %q", out.Code)
	}
}

// TestAuthSessionEndpoint 验证 GET /api/auth/session 返回 CSRF 与过期时间。
func TestAuthSessionEndpoint(t *testing.T) {
	f := &fakeBackend{}
	_, ts := newTestServer(t, f)
	defer ts.Close()

	sess, _ := login(t, ts, "admin-pass-2026-strong")
	req := authedReq(t, ts, "GET", "/api/auth/session", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	status, body, _ := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("期望 200,得到 %d", status)
	}
	var out struct {
		Data struct {
			CSRFToken string `json:"csrf_token"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.CSRFToken == "" {
		t.Fatal("session 端点应返回 csrf_token")
	}
}

// TestAuthSessionEndpointInvalid 验证无效会话的 session 端点返回 401。
func TestAuthSessionEndpointInvalid(t *testing.T) {
	f := &fakeBackend{}
	_, ts := newTestServer(t, f)
	defer ts.Close()

	req := authedReq(t, ts, "GET", "/api/auth/session", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: "invalid-session"})
	status, _, _ := do(t, req)
	if status != http.StatusUnauthorized {
		t.Fatalf("无效会话期望 401,得到 %d", status)
	}
}
