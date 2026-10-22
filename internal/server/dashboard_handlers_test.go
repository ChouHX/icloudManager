package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"icloud-hme/internal/account"
)

// TestDashboardStats 验证看板接口聚合账号别名与标签分布,并要求登录。
func TestDashboardStats(t *testing.T) {
	f := &fakeBackend{}
	f.setAccounts([]account.Summary{
		{
			ID: "acc_1", Name: "主号", ICloudEmail: "owner@icloud.com",
			Status: "active", AliasTotal: 10, AliasActive: 7,
		},
		{
			ID: "acc_2", Name: "备用号", ICloudEmail: "backup@icloud.com",
			Status: "pending", AliasTotal: 3, AliasActive: 0,
		},
	})

	s := mustServer(t, f, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// 未登录应 401。
	req := authedReq(t, ts, "GET", "/api/dashboard", "")
	if status, _, _ := do(t, req); status != http.StatusUnauthorized {
		t.Fatalf("未登录应 401,得到 %d", status)
	}

	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	for _, payload := range []string{
		`{"email":"a@example.com","site":"GitHub","tags":["项目A","开发"]}`,
		`{"email":"b@example.com","site":"github","tags":["开发"]}`,
		`{"email":"c@example.com"}`,
	} {
		status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags", payload))
		if status != http.StatusOK {
			t.Fatalf("创建标签失败: %d %s", status, body)
		}
	}

	status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "GET", "/api/dashboard", ""))
	if status != http.StatusOK {
		t.Fatalf("看板接口失败: %d %s", status, body)
	}

	var resp struct {
		Data dashboardResponse `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("解析看板响应: %v", err)
	}

	acc := resp.Data.Accounts
	if acc.Total != 2 || acc.AliasTotal != 13 || acc.AliasActive != 7 || len(acc.Items) != 2 {
		t.Fatalf("账号统计不符: %+v", acc)
	}
	if acc.Items[0].Name != "主号" || acc.Items[0].AliasTotal != 10 {
		t.Fatalf("账号明细不符: %+v", acc.Items[0])
	}

	tags := resp.Data.Tags
	if tags.Total != 3 || tags.Tagged != 2 || tags.Untagged != 1 {
		t.Fatalf("标签汇总不符: %+v", tags)
	}
	if tags.TagKinds != 2 || tags.SiteKinds != 1 {
		t.Fatalf("标签/网站种类不符: %+v", tags)
	}
	if len(tags.BySite) != 1 || tags.BySite[0].Site != "GitHub" || tags.BySite[0].Count != 2 {
		t.Fatalf("网站分布不符: %+v", tags.BySite)
	}
	if len(tags.ByTag) != 2 || tags.ByTag[0].Tag != "开发" || tags.ByTag[0].Count != 2 {
		t.Fatalf("标签分布不符: %+v", tags.ByTag)
	}
}
