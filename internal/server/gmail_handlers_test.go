package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGmailImportAndList 验证 Gmail 账号导入、列表脱敏与取件链接生成。
func TestGmailImportAndList(t *testing.T) {
	f := &fakeBackend{}
	s := mustServer(t, f, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	// 未登录不可访问
	if status, _, _ := do(t, authedReq(t, ts, "GET", "/api/gmail", "")); status != http.StatusUnauthorized {
		t.Fatalf("未登录应 401,得到 %d", status)
	}

	body := `{"text":"a@gmail.com----login-pass----JBSWY3DPEHPK3PXP----app-pass\nbad-line"}`
	status, resp, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/gmail/import", body))
	if status != http.StatusOK {
		t.Fatalf("导入失败: %d %s", status, resp)
	}
	if !strings.Contains(resp, `"imported":1`) || !strings.Contains(resp, `"invalid":1`) {
		t.Fatalf("导入统计不符: %s", resp)
	}

	status, resp, _ = do(t, tagAuthed(t, ts, sess, csrf, "GET", "/api/gmail", ""))
	if status != http.StatusOK {
		t.Fatalf("列表失败: %d %s", status, resp)
	}
	// 密码类字段绝不能出现在响应里
	for _, secret := range []string{"login-pass", "JBSWY3DPEHPK3PXP", "app-pass"} {
		if strings.Contains(resp, secret) {
			t.Fatalf("响应泄露了密码字段 %q: %s", secret, resp)
		}
	}
	if !strings.Contains(resp, `"has_app_password":true`) {
		t.Fatalf("应标记已配置应用专用密码: %s", resp)
	}

	var list struct {
		Data struct {
			Accounts []struct {
				ID string `json:"id"`
			} `json:"accounts"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &list); err != nil {
		t.Fatalf("解析列表: %v", err)
	}
	if len(list.Data.Accounts) != 1 {
		t.Fatalf("应有 1 个账号: %s", resp)
	}
	id := list.Data.Accounts[0].ID

	// 生成取件链接
	status, resp, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/gmail/"+id+"/share-link", ""))
	if status != http.StatusOK {
		t.Fatalf("生成取件链接失败: %d %s", status, resp)
	}
	if !strings.Contains(resp, "gtoken=") {
		t.Fatalf("取件链接应带 gtoken: %s", resp)
	}

	// 公开接口:无效 token 返回 404
	if status, _, _ = do(t, authedReq(t, ts, "GET", "/api/gmail-share/nope/inbox", "")); status != http.StatusNotFound {
		t.Fatalf("无效 token 应 404,得到 %d", status)
	}

	// 过滤条件可单独更新
	status, resp, _ = do(t, tagAuthed(t, ts, sess, csrf, "PUT", "/api/gmail/"+id+"/filter",
		`{"include":["验证码"],"exclude":["广告"],"days":500}`))
	if status != http.StatusOK {
		t.Fatalf("更新过滤失败: %d %s", status, resp)
	}
	if !strings.Contains(resp, `"days":90`) {
		t.Fatalf("天数应被限制为 90: %s", resp)
	}

	// 删除账号会一并撤销取件链接
	if status, resp, _ = do(t, tagAuthed(t, ts, sess, csrf, "DELETE", "/api/gmail/"+id, "")); status != http.StatusOK {
		t.Fatalf("删除失败: %d %s", status, resp)
	}
	if status, resp, _ = do(t, tagAuthed(t, ts, sess, csrf, "GET", "/api/gmail", "")); status != http.StatusOK {
		t.Fatalf("列表失败: %d %s", status, resp)
	}
}

// TestGmailShareRejectsAliasToken 确认 iCloud 别名的 token 不能用于 Gmail 公开接口。
func TestGmailShareRejectsAliasToken(t *testing.T) {
	f := &fakeBackend{}
	f.setAccounts(nil)
	s := mustServer(t, f, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	link, err := s.share.Ensure("acc_x", "someone@icloud.com")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	status, resp, _ := do(t, authedReq(t, ts, "GET", "/api/gmail-share/"+link.Token+"/inbox", ""))
	if status != http.StatusNotFound {
		t.Fatalf("别名 token 不应可用于 Gmail 取件,得到 %d %s", status, resp)
	}
}
