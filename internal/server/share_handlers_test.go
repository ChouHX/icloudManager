package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
)

// newShareTestServer 构造一个带两个别名的测试服务。
func newShareTestServer(t *testing.T) (*Server, *fakeBackend, *httptest.Server) {
	t.Helper()
	backend := &fakeBackend{
		accounts: []account.Summary{{ID: "acc_1", Name: "主号", Status: "active"}},
		aliases: []hme.Alias{
			{Email: "alpha@icloud.com", AnonymousID: "anon_1", Label: "购物", Active: true},
			{Email: "beta@icloud.com", AnonymousID: "anon_2", Label: "订阅", Active: true},
		},
	}
	s := mustServer(t, backend, Config{
		AdminPassword: "admin-pass-2026-strong",
		SessionTTL:    time.Hour,
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, backend, ts
}

// serveRequest 直接打到 gin handler(服务端侧),用于公开接口的测试。
func serveRequest(t *testing.T, s *Server, method, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// createShareLink 生成取件链接并返回 token 与 url。
func createShareLink(t *testing.T, ts *httptest.Server, session, csrf, anonymousID string) (token, url string) {
	t.Helper()
	req := authedReq(t, ts, "POST", "/api/aliases/"+anonymousID+"/share-link", `{"account_id":"acc_1"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: session})
	req.Header.Set("X-CSRF-Token", csrf)
	status, body, _ := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("生成取件链接失败: %d %s", status, body)
	}
	var payload struct {
		Data struct {
			Token     string `json:"token"`
			URL       string `json:"url"`
			Alias     string `json:"alias"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if payload.Data.Token == "" || payload.Data.URL == "" {
		t.Fatalf("响应缺少 token 或 url: %s", body)
	}
	if !strings.Contains(payload.Data.URL, "/?token="+payload.Data.Token) {
		t.Fatalf("链接格式应为 host:port/?token=xxx,实际 %s", payload.Data.URL)
	}
	return payload.Data.Token, payload.Data.URL
}

func TestShareLinkRequiresSessionAndCSRF(t *testing.T) {
	s, _, ts := newShareTestServer(t)

	// 无会话
	if status, _ := serveRequest(t, s, "POST", "/api/aliases/anon_1/share-link"); status != http.StatusUnauthorized {
		t.Fatalf("无会话应返回 401,实际 %d", status)
	}

	// 有会话但缺 CSRF
	session, _ := login(t, ts, "admin-pass-2026-strong")
	csrfReq := authedReq(t, ts, "POST", "/api/aliases/anon_1/share-link", `{"account_id":"acc_1"}`)
	csrfReq.AddCookie(&http.Cookie{Name: "hme_session", Value: session})
	if status, _, _ := do(t, csrfReq); status != http.StatusForbidden {
		t.Fatalf("缺 CSRF 应返回 403,实际 %d", status)
	}
}

func TestShareLinkIsIdempotentAndListed(t *testing.T) {
	_, _, ts := newShareTestServer(t)
	session, csrf := login(t, ts, "admin-pass-2026-strong")

	first, _ := createShareLink(t, ts, session, csrf, "anon_1")
	second, _ := createShareLink(t, ts, session, csrf, "anon_1")
	if first != second {
		t.Fatalf("重复生成应复用同一 token: %q vs %q", first, second)
	}

	// 别名列表应带出 shareToken,便于界面显示"已生成"
	req := authedReq(t, ts, "GET", "/api/aliases?account_id=acc_1", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: session})
	_, body, _ := do(t, req)
	var payload struct {
		Data struct {
			Aliases []struct {
				Email      string `json:"email"`
				ShareToken string `json:"shareToken"`
			} `json:"aliases"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("解析别名列表失败: %v", err)
	}
	found := false
	for _, alias := range payload.Data.Aliases {
		if alias.Email == "alpha@icloud.com" {
			if alias.ShareToken != first {
				t.Fatalf("别名应带出 shareToken,实际 %q", alias.ShareToken)
			}
			found = true
		}
		if alias.Email == "beta@icloud.com" && alias.ShareToken != "" {
			t.Fatalf("未生成链接的别名不应带 shareToken: %q", alias.ShareToken)
		}
	}
	if !found {
		t.Fatal("列表里没有找到目标别名")
	}
}

func TestShareInboxIsPublicAndScoped(t *testing.T) {
	s, backend, ts := newShareTestServer(t)
	session, csrf := login(t, ts, "admin-pass-2026-strong")
	token, _ := createShareLink(t, ts, session, csrf, "anon_1")

	// 公开访问:不带任何 Cookie
	status, body := serveRequest(t, s, "GET", "/api/share/"+token+"/inbox")
	if status != http.StatusOK {
		t.Fatalf("公开取件应返回 200,实际 %d: %s", status, body)
	}

	// 查询被强制限定在该 token 的别名上
	if backend.listInboxQuery.AccountID != "acc_1" || backend.listInboxQuery.Alias != "alpha@icloud.com" {
		t.Fatalf("取件查询未被限定到 token 对应别名: %+v", backend.listInboxQuery)
	}
	// 响应不应暴露账号内部标识
	if strings.Contains(body, "acc_1") {
		t.Fatalf("公开响应不应包含 account_id: %s", body)
	}
	if !strings.Contains(body, "alpha@icloud.com") {
		t.Fatalf("响应应带出别名: %s", body)
	}
}

func TestShareRejectsUnknownToken(t *testing.T) {
	s, _, _ := newShareTestServer(t)

	for _, path := range []string{"/api/share/nope/inbox", "/api/share/nope/inbox/1042"} {
		status, body := serveRequest(t, s, "GET", path)
		if status != http.StatusNotFound {
			t.Fatalf("%s 期望 404,实际 %d: %s", path, status, body)
		}
		if !strings.Contains(body, codeShareInvalid) {
			t.Fatalf("%s 应返回 %s 错误码: %s", path, codeShareInvalid, body)
		}
	}
}

// TestShareCannotReadOtherAliasMessages 是越权防护的关键用例:
// IMAP UID 是账号级全局的,拿到某个别名的链接不能读走同账号其它别名的邮件。
func TestShareCannotReadOtherAliasMessages(t *testing.T) {
	s, backend, ts := newShareTestServer(t)
	session, csrf := login(t, ts, "admin-pass-2026-strong")
	token, _ := createShareLink(t, ts, session, csrf, "anon_1")

	// 同一账号下发给其它别名的邮件
	backend.fullMessage = &mail.FullMessage{
		Message:     mail.Message{ID: "1042", To: "beta@icloud.com", Subject: "别人的邮件"},
		Body:        "这是发给 beta 的正文",
		ContentType: "text/plain",
	}
	status, body := serveRequest(t, s, "GET", "/api/share/"+token+"/inbox/1042")
	if status != http.StatusNotFound {
		t.Fatalf("越权读取应返回 404,实际 %d: %s", status, body)
	}
	if strings.Contains(body, "别人的邮件") || strings.Contains(body, "beta") {
		t.Fatalf("越权响应泄露了其它别名的内容: %s", body)
	}

	// 发给本别名的邮件可以正常读取
	backend.fullMessage = &mail.FullMessage{
		Message:     mail.Message{ID: "1042", To: "alpha@icloud.com", Subject: "自己的邮件"},
		Body:        "验证码：123456",
		ContentType: "text/plain",
	}
	status, body = serveRequest(t, s, "GET", "/api/share/"+token+"/inbox/1042")
	if status != http.StatusOK {
		t.Fatalf("读取本别名邮件应成功,实际 %d: %s", status, body)
	}
	if !strings.Contains(body, "验证码：123456") {
		t.Fatalf("响应应包含正文: %s", body)
	}
	// 详情也应走清理版(sanitize)
	if !backend.lastMessageOpts.Sanitize {
		t.Fatal("公开详情应请求清理后的 HTML")
	}
}

func TestShareRevokeInvalidatesLink(t *testing.T) {
	s, _, ts := newShareTestServer(t)
	session, csrf := login(t, ts, "admin-pass-2026-strong")
	token, _ := createShareLink(t, ts, session, csrf, "anon_1")

	// 撤销
	req := authedReq(t, ts, "DELETE", "/api/aliases/anon_1/share-link", `{"account_id":"acc_1"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: session})
	req.Header.Set("X-CSRF-Token", csrf)
	status, body, _ := do(t, req)
	if status != http.StatusOK {
		t.Fatalf("撤销失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"removed":true`) {
		t.Fatalf("撤销响应应表明删除成功: %s", body)
	}

	// 原 token 立即失效
	if status, _ := serveRequest(t, s, "GET", "/api/share/"+token+"/inbox"); status != http.StatusNotFound {
		t.Fatalf("撤销后应 404,实际 %d", status)
	}

	// 重新生成得到新 token
	regenerated, _ := createShareLink(t, ts, session, csrf, "anon_1")
	if regenerated == token {
		t.Fatal("重新生成必须轮换 token")
	}
}

func TestShareLinksAreIsolatedPerAlias(t *testing.T) {
	s, backend, ts := newShareTestServer(t)
	session, csrf := login(t, ts, "admin-pass-2026-strong")

	alphaToken, _ := createShareLink(t, ts, session, csrf, "anon_1")
	betaToken, _ := createShareLink(t, ts, session, csrf, "anon_2")
	if alphaToken == betaToken {
		t.Fatal("不同别名必须持有不同的 token")
	}

	// 用 beta 的 token 取件,查询应限定在 beta
	if status, body := serveRequest(t, s, "GET", "/api/share/"+betaToken+"/inbox"); status != http.StatusOK {
		t.Fatalf("beta 链接取件失败: %d %s", status, body)
	}
	if backend.listInboxQuery.Alias != "beta@icloud.com" {
		t.Fatalf("查询应限定在 beta,实际 %+v", backend.listInboxQuery)
	}
}

func TestShareInboxValidatesPagingParams(t *testing.T) {
	s, _, ts := newShareTestServer(t)
	session, csrf := login(t, ts, "admin-pass-2026-strong")
	token, _ := createShareLink(t, ts, session, csrf, "anon_1")

	for _, query := range []string{"?limit=0", "?limit=101", "?days=0", "?days=999", "?limit=abc"} {
		if status, body := serveRequest(t, s, "GET", "/api/share/"+token+"/inbox"+query); status != http.StatusBadRequest {
			t.Fatalf("%s 期望 400,实际 %d: %s", query, status, body)
		}
	}
}
