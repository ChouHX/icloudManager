package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestCreateRejectsOversizedBody 验证 1 MiB 请求体上限真正生效。
// 回归点:maxBodyBytes 曾被声明却从未挂到中间件上。
func TestCreateRejectsOversizedBody(t *testing.T) {
	f := &fakeBackend{}
	s := mustServer(t, f, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")
	oversized := strings.Repeat("a", 1<<20+1024)
	req := authedReq(t, ts, "POST", "/api/create", `{"account_id":"acc_1","label":"`+oversized+`"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)

	status, body, _ := do(t, req)
	if status != http.StatusBadRequest {
		t.Fatalf("超过 1 MiB 的请求体应被拒绝,得到 %d: %s", status, body)
	}
	if f.createAliasCalls.Load() != 0 {
		t.Fatalf("超限请求不应触达 backend,实际调用 %d 次", f.createAliasCalls.Load())
	}
}

// TestInboxMessageIDZeroRejected 验证 uid=0 在入参层被拦下,不会打到上游变成 502。
func TestInboxMessageIDZeroRejected(t *testing.T) {
	f := &fakeBackend{}
	s := mustServer(t, f, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, _ := login(t, ts, "admin-pass-2026-strong")
	for _, path := range []string{"/api/inbox/0?account_id=acc_1", "/api/inbox/abc?account_id=acc_1"} {
		req := authedReq(t, ts, "GET", path, "")
		req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
		status, body, _ := do(t, req)
		if status != http.StatusBadRequest {
			t.Fatalf("%s 期望 400,得到 %d: %s", path, status, body)
		}
	}
}

// TestAPINotFoundIsJSONAndNoStore 验证 /api 未知路径(含无尾斜杠)返回 JSON 且不可缓存。
func TestAPINotFoundIsJSONAndNoStore(t *testing.T) {
	s := mustServer(t, &fakeBackend{}, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})

	for _, path := range []string{"/api", "/api/not-found"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s 期望 404,得到 %d", path, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s 期望 Cache-Control: no-store,得到 %q", path, got)
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s 应返回 JSON: %v", path, err)
		}
		if payload["success"] != false {
			t.Fatalf("%s 失败响应 success 应为 false: %v", path, payload)
		}
	}
}

// TestMailboxPortRangeRejected 验证端口越界在入参层返回 400,而不是被当成上游失败。
func TestMailboxPortRangeRejected(t *testing.T) {
	f := &fakeBackend{}
	s := mustServer(t, f, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")
	req := authedReq(t, ts, "PUT", "/api/accounts/acc_1/mailbox",
		`{"provider":"qq","email":"me@qq.com","imap_host":"imap.qq.com","imap_port":70000,"authorization_code":"code"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)

	status, body, _ := do(t, req)
	if status != http.StatusBadRequest {
		t.Fatalf("端口越界期望 400,得到 %d: %s", status, body)
	}
}

// TestGetMessageOptionsArePassedThrough 验证 sanitize / raw 查询参数被传到邮件层。
func TestGetMessageOptionsArePassedThrough(t *testing.T) {
	f := &fakeBackend{}
	s := mustServer(t, f, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	session, _ := login(t, ts, "admin-pass-2026-strong")

	cases := []struct {
		path       string
		sanitize   bool
		includeRaw bool
	}{
		{"/api/inbox/1042?account_id=acc_1", false, false},
		{"/api/inbox/1042?account_id=acc_1&sanitize=1", true, false},
		{"/api/inbox/1042?account_id=acc_1&raw=true", false, true},
		{"/api/inbox/1042?account_id=acc_1&sanitize=yes&raw=1", true, true},
		{"/api/inbox/1042?account_id=acc_1&sanitize=0&raw=no", false, false},
	}

	for _, tc := range cases {
		req := authedReq(t, ts, "GET", tc.path, "")
		req.AddCookie(&http.Cookie{Name: "hme_session", Value: session})
		status, body, _ := do(t, req)
		if status != http.StatusOK {
			t.Fatalf("%s 期望 200,得到 %d: %s", tc.path, status, body)
		}
		if f.lastMessageOpts.Sanitize != tc.sanitize || f.lastMessageOpts.IncludeRaw != tc.includeRaw {
			t.Fatalf("%s 选项传递错误: 期望 sanitize=%v raw=%v,实际 %+v",
				tc.path, tc.sanitize, tc.includeRaw, f.lastMessageOpts)
		}
	}
}
