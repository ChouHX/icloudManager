package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"testing/fstest"
)

// mustServer 构造测试用 Server,初始化失败直接终止测试。
func mustServer(t *testing.T, be Backend, cfg Config) *Server {
	t.Helper()
	// 每个测试用独立的数据目录,避免取件链接等状态相互串扰
	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}
	s, err := newWithBackend(be, cfg)
	if err != nil {
		t.Fatalf("初始化测试服务失败: %v", err)
	}
	return s
}

// TestSPAAPI404JSON 验证 /api/not-found 返回 JSON 404 而非 HTML。
func TestSPAAPI404JSON(t *testing.T) {
	f := &fakeBackend{}
	s := mustServer(t, f, Config{
		Debug:         false,
		AdminPassword: "admin-pass-2026-strong",
	})
	// 替换 webui 为内存 FS,避免依赖构建产物
	webuiFS = fstest.MapFS{
		"index.html": {Data: []byte("<html>管理界面</html>")},
	}
	defer func() { webuiFS = nil }()

	req := httptest.NewRequest("GET", "/api/not-found", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("期望 404,得到 %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Body.String(), `{"success":false`) {
		t.Fatalf("API 404 应返回 JSON: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("API 404 不应返回 HTML: %s", rec.Body.String())
	}
}

// TestSPAIndexServed 验证根路径由 webui 服务(返回 index 而非 404)。
func TestSPAIndexServed(t *testing.T) {
	f := &fakeBackend{}
	s := mustServer(t, f, Config{
		Debug:         false,
		AdminPassword: "admin-pass-2026-strong",
	})
	webuiFS = fstest.MapFS{
		"index.html": {Data: []byte("<html>管理界面</html>")},
	}
	defer func() { webuiFS = nil }()

	req := httptest.NewRequest("GET", "/accounts?account_id=acc_test", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("SPA 路径期望 200,得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "管理界面") {
		t.Fatalf("SPA 路径应返回 index 内容: %s", rec.Body.String())
	}
}
