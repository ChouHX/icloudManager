package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTagsRequireAuth 验证标签接口需要管理员会话与 CSRF。
func TestTagsRequireAuth(t *testing.T) {
	f := &fakeBackend{}
	_, ts := newTestServer(t, f)
	defer ts.Close()

	// 未登录
	req := authedReq(t, ts, "GET", "/api/tags", "")
	if status, _, _ := do(t, req); status != http.StatusUnauthorized {
		t.Fatalf("未登录应 401,得到 %d", status)
	}

	sess, _ := login(t, ts, "admin-pass-2026-strong")

	// 已登录但缺少 CSRF 的写操作应 403
	req = authedReq(t, ts, "POST", "/api/tags", `{"email":"a@example.com"}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	if status, _, _ := do(t, req); status != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 应 403,得到 %d", status)
	}

	// 已登录读取应 200
	req = authedReq(t, ts, "GET", "/api/tags", "")
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	if status, _, _ := do(t, req); status != http.StatusOK {
		t.Fatalf("已登录读取应 200,得到 %d", status)
	}
}

// TestTagsCRUD 覆盖标签凭据的增删改查与持久化。
func TestTagsCRUD(t *testing.T) {
	dir := t.TempDir()
	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       dir,
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	authed := func(method, path, body string) *http.Request {
		req := authedReq(t, ts, method, path, body)
		req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
		if method != "GET" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		return req
	}

	// 创建
	status, body, _ := do(t, authed("POST", "/api/tags", `{"email":"user@example.com","password":"p@ss","site":"GitHub","tags":["项目A","项目A"]}`))
	if status != http.StatusOK || !strings.Contains(body, `"email":"user@example.com"`) {
		t.Fatalf("创建失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"tags":["项目A"]`) {
		t.Fatalf("标签应去重: %s", body)
	}

	// 列表
	status, body, _ = do(t, authed("GET", "/api/tags", ""))
	if status != http.StatusOK || !strings.Contains(body, `"count":1`) || !strings.Contains(body, `"项目A"`) {
		t.Fatalf("列表失败: %d %s", status, body)
	}

	id := extractEntryID(t, body)
	if id == "" {
		t.Fatalf("未找到记录 ID: %s", body)
	}

	// 更新
	status, body, _ = do(t, authed("PUT", "/api/tags/"+id, `{"email":"new@example.com","password":"new","tags":["项目B"]}`))
	if status != http.StatusOK || !strings.Contains(body, `"email":"new@example.com"`) || !strings.Contains(body, `"项目B"`) {
		t.Fatalf("更新失败: %d %s", status, body)
	}

	// 无效邮箱
	status, _, _ = do(t, authed("POST", "/api/tags", `{"email":"not-an-email"}`))
	if status != http.StatusBadRequest {
		t.Fatalf("无效邮箱应 400,得到 %d", status)
	}

	// 删除不存在的记录
	status, _, _ = do(t, authed("DELETE", "/api/tags/missing", ""))
	if status != http.StatusNotFound {
		t.Fatalf("删除不存在记录应 404,得到 %d", status)
	}

	// 删除
	status, _, _ = do(t, authed("DELETE", "/api/tags/"+id, ""))
	if status != http.StatusOK {
		t.Fatalf("删除失败: %d", status)
	}
	status, body, _ = do(t, authed("GET", "/api/tags", ""))
	if status != http.StatusOK || !strings.Contains(body, `"count":0`) {
		t.Fatalf("删除后列表应为空: %d %s", status, body)
	}

	// 持久化到独立的 tags.db,不触碰 accounts.json
	info, err := os.Stat(filepath.Join(dir, "tags.db"))
	if err != nil {
		t.Fatalf("tags.db 未生成: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("tags.db 权限应为 0600,得到 %o", perm)
	}
	if _, err := os.Stat(filepath.Join(dir, "accounts.json")); !os.IsNotExist(err) {
		t.Fatalf("标签功能不应创建 accounts.json (err=%v)", err)
	}
}

// TestTagsIsolatedFromAccounts 验证标签写入不影响已有 iCloud 账号。
func TestTagsIsolatedFromAccounts(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.json")
	original := []byte(`{"accounts":{"acc_1":{"id":"acc_1","name":"主号","real_email":"owner@icloud.com"}}}`)
	if err := os.WriteFile(accountsPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       dir,
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	req := authedReq(t, ts, "POST", "/api/tags", `{"email":"user@example.com","tags":["项目A"]}`)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	req.Header.Set("X-CSRF-Token", csrf)
	if status, body, _ := do(t, req); status != http.StatusOK {
		t.Fatalf("创建标签失败: %d %s", status, body)
	}

	after, err := os.ReadFile(accountsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("accounts.json 被修改:\n%s", after)
	}
}

// extractEntryID 从列表响应中取出第一条记录的 id。
func extractEntryID(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		Data struct {
			Entries []struct {
				ID string `json:"id"`
			} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("解析响应失败: %v (%s)", err, body)
	}
	if len(out.Data.Entries) == 0 {
		return ""
	}
	return out.Data.Entries[0].ID
}

func tagAuthed(t *testing.T, ts *httptest.Server, sess, csrf, method, path, body string) *http.Request {
	t.Helper()
	req := authedReq(t, ts, method, path, body)
	req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
	if method != "GET" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	return req
}

// TestTagsImport 验证批量导入会统一打标签,并处理重复与非法行。
func TestTagsImport(t *testing.T) {
	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	// 缺少 CSRF 应 403
	req := tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/import", `{"text":"a@example.com"}`)
	req.Header.Del("X-CSRF-Token")
	if status, _, _ := do(t, req); status != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 应 403,得到 %d", status)
	}

	payload := `{"text":"a@example.com----p1----GitHub\nb@example.com----p2\nnot-an-email\n","tags":["项目A"],"site":"Steam"}`
	status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/import", payload))
	if status != http.StatusOK {
		t.Fatalf("导入失败: %d %s", status, body)
	}
	for _, want := range []string{`"imported":2`, `"invalid":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("导入结果缺少 %s: %s", want, body)
		}
	}
	// 逐条记录不再随响应回传,避免万级导入响应过大
	if strings.Contains(body, `"entries"`) {
		t.Fatalf("导入响应不应包含 entries: %s", body)
	}

	// 通过列表确认标签与默认网站已落地
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "GET", "/api/tags", ""))
	if status != http.StatusOK {
		t.Fatalf("列表失败: %d %s", status, body)
	}
	for _, want := range []string{`"项目A"`, `"Steam"`, `a@example.com`, `b@example.com`} {
		if !strings.Contains(body, want) {
			t.Fatalf("导入后列表缺少 %s: %s", want, body)
		}
	}

	// 再次导入相同数据默认跳过
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/import", payload))
	if status != http.StatusOK || !strings.Contains(body, `"skipped":2`) {
		t.Fatalf("重复导入应跳过: %d %s", status, body)
	}

	// 空内容应报错
	status, _, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/import", `{"text":"  \n"}`))
	if status != http.StatusBadRequest {
		t.Fatalf("空内容应 400,得到 %d", status)
	}
}

// TestTagLabelsRenameAndDelete 验证标签的全局重命名与删除。
func TestTagLabelsRenameAndDelete(t *testing.T) {
	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	if status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags", `{"email":"a@example.com","tags":["开发"]}`)); status != http.StatusOK {
		t.Fatalf("创建失败: %d %s", status, body)
	}
	if status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags", `{"email":"b@example.com","tags":["开发"]}`)); status != http.StatusOK {
		t.Fatalf("创建失败: %d %s", status, body)
	}

	// 重命名
	status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "PUT", "/api/tag-labels/rename", `{"from":"开发","to":"前端"}`))
	if status != http.StatusOK || !strings.Contains(body, `"changed":2`) {
		t.Fatalf("重命名失败: %d %s", status, body)
	}
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "GET", "/api/tags", ""))
	if status != http.StatusOK || !strings.Contains(body, `"前端"`) || strings.Contains(body, `"开发"`) {
		t.Fatalf("重命名未生效: %d %s", status, body)
	}

	// 删除标签
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "DELETE", "/api/tag-labels/"+url.PathEscape("前端"), ""))
	if status != http.StatusOK || !strings.Contains(body, `"changed":2`) {
		t.Fatalf("删除标签失败: %d %s", status, body)
	}
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "GET", "/api/tags", ""))
	if status != http.StatusOK || !strings.Contains(body, `"tags":[]`) {
		t.Fatalf("标签应被清空: %d %s", status, body)
	}

	// 重命名缺少参数应 400
	status, _, _ = do(t, tagAuthed(t, ts, sess, csrf, "PUT", "/api/tag-labels/rename", `{"from":"","to":"x"}`))
	if status != http.StatusBadRequest {
		t.Fatalf("空标签应 400,得到 %d", status)
	}
}

// TestTagsBackupExportRestore 验证备份导出与导入(含 CSRF)。
func TestTagsBackupExportRestore(t *testing.T) {
	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	if status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags", `{"email":"a@example.com","password":"p1","site":"GitHub","tags":["项目A"]}`)); status != http.StatusOK {
		t.Fatalf("创建失败: %d %s", status, body)
	}

	// 导出
	status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "GET", "/api/tags/export", ""))
	if status != http.StatusOK {
		t.Fatalf("导出失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"type":"icloud-hme-tags"`) || !strings.Contains(body, `"version":1`) || !strings.Contains(body, `"count":1`) {
		t.Fatalf("备份元信息缺失: %s", body)
	}

	// 提取备份 data 对象
	var exported struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &exported); err != nil {
		t.Fatalf("解析导出响应失败: %v", err)
	}

	// 缺少 CSRF 应 403
	req := tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/restore", string(exported.Data))
	req.Header.Del("X-CSRF-Token")
	if status, _, _ := do(t, req); status != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 应 403,得到 %d", status)
	}

	// merge:已存在则跳过
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/restore", string(exported.Data)))
	if status != http.StatusOK || !strings.Contains(body, `"skipped":1`) {
		t.Fatalf("merge 导入应跳过: %d %s", status, body)
	}

	// replace:清空重建
	replaceBody := strings.TrimSuffix(string(exported.Data), "}") + `,"mode":"replace"}`
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/restore", replaceBody))
	if status != http.StatusOK || !strings.Contains(body, `"imported":1`) || !strings.Contains(body, `"total":1`) {
		t.Fatalf("replace 导入失败: %d %s", status, body)
	}

	// 兼容纯数组格式
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/restore", `[{"email":"arr@example.com"}]`))
	if status != http.StatusOK || !strings.Contains(body, `"imported":1`) {
		t.Fatalf("数组格式导入失败: %d %s", status, body)
	}

	// 非法内容应 400
	status, _, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/restore", `{}`))
	if status != http.StatusBadRequest {
		t.Fatalf("空备份应 400,得到 %d", status)
	}
}

// TestTagsPaginationAndStats 验证分页、搜索、标签筛选与统计卡片数据。
func TestTagsPaginationAndStats(t *testing.T) {
	dir := t.TempDir()
	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       dir,
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	authed := func(method, path, body string) *http.Request {
		req := authedReq(t, ts, method, path, body)
		req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
		if method != "GET" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		return req
	}

	for _, body := range []string{
		`{"email":"a@example.com","site":"GitHub","tags":["项目A","开发"]}`,
		`{"email":"b@example.com","site":"Steam","tags":["游戏"]}`,
		`{"email":"c@example.com"}`,
	} {
		if status, resp, _ := do(t, authed("POST", "/api/tags", body)); status != http.StatusOK {
			t.Fatalf("创建失败: %d %s", status, resp)
		}
	}

	// 分页:每页 1 条,共 3 页
	status, body, _ := do(t, authed("GET", "/api/tags?page=1&page_size=1", ""))
	if status != http.StatusOK {
		t.Fatalf("列表失败: %d %s", status, body)
	}
	for _, want := range []string{`"count":3`, `"page":1`, `"page_size":1`, `"total_pages":3`} {
		if !strings.Contains(body, want) {
			t.Fatalf("分页响应缺少 %s: %s", want, body)
		}
	}

	// 统计卡片:3 条记录、3 个标签、2 个网站、1 条未打标签
	for _, want := range []string{`"total":3`, `"tags":3`, `"sites":2`, `"untagged":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("统计响应缺少 %s: %s", want, body)
		}
	}

	// 搜索匹配网站
	status, body, _ = do(t, authed("GET", "/api/tags?search=github", ""))
	if status != http.StatusOK || !strings.Contains(body, `"count":1`) || !strings.Contains(body, "a@example.com") {
		t.Fatalf("搜索失败: %d %s", status, body)
	}

	// 标签筛选(OR,忽略大小写)
	status, body, _ = do(t, authed("GET", "/api/tags?tags="+url.QueryEscape("游戏"), ""))
	if status != http.StatusOK || !strings.Contains(body, `"count":1`) || !strings.Contains(body, "b@example.com") {
		t.Fatalf("标签筛选失败: %d %s", status, body)
	}

	// 页码越界回退到最后一页
	status, body, _ = do(t, authed("GET", "/api/tags?page=99&page_size=2", ""))
	if status != http.StatusOK || !strings.Contains(body, `"page":2`) {
		t.Fatalf("越界页码应回退: %d %s", status, body)
	}
}

// TestTagsBatchByFilterAndIDs 验证按筛选条件/ID 批量修改标签。
func TestTagsBatchByFilterAndIDs(t *testing.T) {
	dir := t.TempDir()
	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       dir,
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	authed := func(method, path, body string) *http.Request {
		req := authedReq(t, ts, method, path, body)
		req.AddCookie(&http.Cookie{Name: "hme_session", Value: sess})
		if method != "GET" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		return req
	}

	ids := make([]string, 0, 3)
	for _, body := range []string{
		`{"email":"a@example.com","site":"GitHub"}`,
		`{"email":"b@example.com","site":"GitHub"}`,
		`{"email":"c@example.com","site":"Steam"}`,
	} {
		status, resp, _ := do(t, authed("POST", "/api/tags", body))
		if status != http.StatusOK {
			t.Fatalf("创建失败: %d %s", status, resp)
		}
		var created struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(resp), &created); err != nil {
			t.Fatalf("解析创建响应: %v", err)
		}
		ids = append(ids, created.Data.ID)
	}

	// 没有 ID 也没有筛选条件时应拒绝,避免误改全部记录
	status, resp, _ := do(t, authed("POST", "/api/tags/batch", `{"action":"add","tags":["x"]}`))
	if status != http.StatusBadRequest || !strings.Contains(resp, "请先筛选或勾选") {
		t.Fatalf("空条件应 400: %d %s", status, resp)
	}

	// 按网站批量追加
	status, resp, _ = do(t, authed("POST", "/api/tags/batch",
		`{"filter":{"site":"github"},"action":"add","tags":["批量"]}`))
	if status != http.StatusOK || !strings.Contains(resp, `"affected":2`) {
		t.Fatalf("按网站批量追加失败: %d %s", status, resp)
	}

	// 按 ID 覆盖
	status, resp, _ = do(t, authed("POST", "/api/tags/batch",
		`{"ids":["`+ids[2]+`"],"action":"replace","tags":["覆盖"]}`))
	if status != http.StatusOK || !strings.Contains(resp, `"affected":1`) {
		t.Fatalf("按 ID 覆盖失败: %d %s", status, resp)
	}

	// 列表校验结果与 sites 字段
	status, resp, _ = do(t, authed("GET", "/api/tags?page_size=10", ""))
	if status != http.StatusOK {
		t.Fatalf("列表失败: %d %s", status, resp)
	}
	var list struct {
		Data struct {
			Entries []struct {
				ID   string   `json:"id"`
				Tags []string `json:"tags"`
			} `json:"entries"`
			Sites []string `json:"sites"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &list); err != nil {
		t.Fatalf("解析列表: %v", err)
	}
	if len(list.Data.Sites) != 2 || list.Data.Sites[0] != "GitHub" || list.Data.Sites[1] != "Steam" {
		t.Fatalf("sites 字段不符: %v", list.Data.Sites)
	}
	byID := make(map[string][]string, len(list.Data.Entries))
	for _, entry := range list.Data.Entries {
		byID[entry.ID] = entry.Tags
	}
	for _, id := range ids[:2] {
		if len(byID[id]) != 1 || byID[id][0] != "批量" {
			t.Fatalf("GitHub 记录应带「批量」: id=%s tags=%v", id, byID[id])
		}
	}
	if got := byID[ids[2]]; len(got) != 1 || got[0] != "覆盖" {
		t.Fatalf("Steam 记录应被覆盖: %v", got)
	}

	// 非法时间参数返回 400
	if status, _, _ := do(t, authed("GET", "/api/tags?created_from=not-a-time", "")); status != http.StatusBadRequest {
		t.Fatalf("非法时间参数应 400,得到 %d", status)
	}
}

// TestTagsBatchDelete 验证按勾选 ID、筛选条件和全部记录批量删除。
func TestTagsBatchDelete(t *testing.T) {
	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	ids := make([]string, 0, 3)
	for _, payload := range []string{
		`{"email":"a@example.com","site":"GitHub"}`,
		`{"email":"b@example.com","site":"GitHub"}`,
		`{"email":"c@example.com","site":"Steam"}`,
	} {
		status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags", payload))
		if status != http.StatusOK {
			t.Fatalf("创建失败: %d %s", status, body)
		}
		var created struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &created); err != nil {
			t.Fatalf("解析创建响应: %v", err)
		}
		ids = append(ids, created.Data.ID)
	}

	// 缺少 CSRF 应 403。
	req := tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/batch-delete", `{"ids":["`+ids[0]+`"]}`)
	req.Header.Del("X-CSRF-Token")
	if status, _, _ := do(t, req); status != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 应 403,得到 %d", status)
	}

	// 空条件应拒绝,避免误删全部。
	status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/batch-delete", `{}`))
	if status != http.StatusBadRequest || !strings.Contains(body, "请先筛选或勾选") {
		t.Fatalf("空条件应 400: %d %s", status, body)
	}

	// 按勾选 ID 删除。
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/batch-delete", `{"ids":["`+ids[0]+`"]}`))
	if status != http.StatusOK || !strings.Contains(body, `"affected":1`) {
		t.Fatalf("按 ID 删除失败: %d %s", status, body)
	}

	// 按筛选条件删除。
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/batch-delete", `{"filter":{"site":"github"}}`))
	if status != http.StatusOK || !strings.Contains(body, `"affected":1`) {
		t.Fatalf("按筛选删除失败: %d %s", status, body)
	}

	// 显式 all 删除全部。
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/batch-delete", `{"all":true}`))
	if status != http.StatusOK || !strings.Contains(body, `"affected":1`) {
		t.Fatalf("全部删除失败: %d %s", status, body)
	}

	// 删除后列表应为空。
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "GET", "/api/tags", ""))
	if status != http.StatusOK || !strings.Contains(body, `"count":0`) {
		t.Fatalf("全部删除后列表应为空: %d %s", status, body)
	}

	// 非法时间参数应 400。
	status, _, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/batch-delete",
		`{"filter":{"created_from":"2026-08-02T00:00:00+08:00","created_to":"2026-08-01T00:00:00+08:00"}}`))
	if status != http.StatusBadRequest {
		t.Fatalf("非法时间参数应 400,得到 %d", status)
	}
}

// TestTagsExportSelection 验证按勾选 ID、筛选条件和全部记录导出。
func TestTagsExportSelection(t *testing.T) {
	s := mustServer(t, &fakeBackend{}, Config{
		AdminPassword: "admin-pass-2026-strong",
		DataDir:       t.TempDir(),
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	sess, csrf := login(t, ts, "admin-pass-2026-strong")

	ids := make([]string, 0, 3)
	for _, payload := range []string{
		`{"email":"a@example.com","password":"p1","site":"GitHub","tags":["项目A"]}`,
		`{"email":"b@example.com","site":"GitHub"}`,
		`{"email":"c@example.com","site":"Steam"}`,
	} {
		status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags", payload))
		if status != http.StatusOK {
			t.Fatalf("创建失败: %d %s", status, body)
		}
		var created struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &created); err != nil {
			t.Fatalf("解析创建响应: %v", err)
		}
		ids = append(ids, created.Data.ID)
	}

	// 按勾选 ID 导出。
	status, body, _ := do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/export", `{"ids":["`+ids[0]+`"]}`))
	if status != http.StatusOK || !strings.Contains(body, `"count":1`) {
		t.Fatalf("按 ID 导出失败: %d %s", status, body)
	}
	if !strings.Contains(body, "a@example.com") || strings.Contains(body, "b@example.com") || strings.Contains(body, "c@example.com") {
		t.Fatalf("按 ID 导出内容不符: %s", body)
	}

	// 按筛选条件导出。
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/export", `{"filter":{"site":"GitHub"}}`))
	if status != http.StatusOK || !strings.Contains(body, `"count":2`) {
		t.Fatalf("按筛选导出失败: %d %s", status, body)
	}
	if !strings.Contains(body, "a@example.com") || !strings.Contains(body, "b@example.com") || strings.Contains(body, "c@example.com") {
		t.Fatalf("按筛选导出内容不符: %s", body)
	}

	// 无条件导出全部。
	status, body, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/export", `{}`))
	if status != http.StatusOK || !strings.Contains(body, `"count":3`) {
		t.Fatalf("全部导出失败: %d %s", status, body)
	}

	// 非法时间参数应 400。
	status, _, _ = do(t, tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/export",
		`{"filter":{"created_from":"2026-08-02T00:00:00+08:00","created_to":"2026-08-01T00:00:00+08:00"}}`))
	if status != http.StatusBadRequest {
		t.Fatalf("非法时间参数应 400,得到 %d", status)
	}

	// 缺少 CSRF 应 403。
	req := tagAuthed(t, ts, sess, csrf, "POST", "/api/tags/export", `{}`)
	req.Header.Del("X-CSRF-Token")
	if status, _, _ := do(t, req); status != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 应 403,得到 %d", status)
	}
}
