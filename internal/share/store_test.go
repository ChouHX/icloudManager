package share

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	return store, dir
}

func TestEnsureIsIdempotentPerAlias(t *testing.T) {
	store, _ := newTestStore(t)

	first, err := store.Ensure("acc_1", "alpha@icloud.com")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(first.Token) < 40 {
		t.Fatalf("token 过短(%d 字符),难以抵抗猜测", len(first.Token))
	}

	second, err := store.Ensure("acc_1", "alpha@icloud.com")
	if err != nil {
		t.Fatalf("二次获取失败: %v", err)
	}
	if first.Token != second.Token {
		t.Fatalf("同一别名应复用同一 token: %q vs %q", first.Token, second.Token)
	}

	// 大小写不同的同一地址也应命中同一条
	third, err := store.Ensure("acc_1", "Alpha@iCloud.com")
	if err != nil || third.Token != first.Token {
		t.Fatalf("地址大小写不应产生新 token: %v %q", err, third.Token)
	}
}

func TestEnsureIsolatesAliasesAndAccounts(t *testing.T) {
	store, _ := newTestStore(t)

	a, _ := store.Ensure("acc_1", "alpha@icloud.com")
	b, _ := store.Ensure("acc_1", "beta@icloud.com")
	c, _ := store.Ensure("acc_2", "alpha@icloud.com")

	tokens := map[string]bool{a.Token: true, b.Token: true, c.Token: true}
	if len(tokens) != 3 {
		t.Fatalf("不同别名/账号应各自持有独立 token,实际 %d 个", len(tokens))
	}
	for _, link := range []Link{a, b, c} {
		if strings.Contains(link.Token, "acc_") || strings.Contains(link.Token, "@") {
			t.Fatalf("token 不应携带账号或地址信息: %q", link.Token)
		}
	}
}

func TestGetAndGetByAlias(t *testing.T) {
	store, _ := newTestStore(t)
	created, _ := store.Ensure("acc_1", "alpha@icloud.com")

	if link, ok := store.Get(created.Token); !ok || link.Alias != "alpha@icloud.com" {
		t.Fatalf("按 token 查询失败: %+v %v", link, ok)
	}
	if _, ok := store.Get("not-a-real-token"); ok {
		t.Fatal("无效 token 不应命中")
	}
	if _, ok := store.Get(""); ok {
		t.Fatal("空 token 不应命中")
	}

	link, ok := store.GetByAlias("acc_1", "alpha@icloud.com")
	if !ok || link.Token != created.Token {
		t.Fatalf("按别名查询失败: %+v %v", link, ok)
	}
	if _, ok := store.GetByAlias("acc_1", "missing@icloud.com"); ok {
		t.Fatal("不存在的别名不应命中")
	}
}

func TestRevokeInvalidatesToken(t *testing.T) {
	store, _ := newTestStore(t)
	link, _ := store.Ensure("acc_1", "alpha@icloud.com")

	removed, err := store.Revoke("acc_1", "alpha@icloud.com")
	if err != nil || !removed {
		t.Fatalf("撤销失败: %v %v", removed, err)
	}
	if _, ok := store.Get(link.Token); ok {
		t.Fatal("撤销后 token 应立即失效")
	}

	// 再次撤销返回 false 而不是报错(幂等)
	again, err := store.Revoke("acc_1", "alpha@icloud.com")
	if err != nil || again {
		t.Fatalf("重复撤销应为 false: %v %v", again, err)
	}

	// 撤销后可以重新生成,且 token 与旧的完全不同
	regenerated, err := store.Ensure("acc_1", "alpha@icloud.com")
	if err != nil {
		t.Fatalf("重新生成失败: %v", err)
	}
	if regenerated.Token == link.Token {
		t.Fatal("重新生成必须轮换 token")
	}
}

func TestTouchTracksUsage(t *testing.T) {
	store, _ := newTestStore(t)
	link, _ := store.Ensure("acc_1", "alpha@icloud.com")

	for i := 0; i < 3; i++ {
		store.Touch(link.Token)
	}
	store.Touch("unknown-token") // 不应 panic 也不应影响统计

	updated, _ := store.Get(link.Token)
	if updated.Hits != 3 {
		t.Fatalf("命中次数应为 3,实际 %d", updated.Hits)
	}
	if updated.LastUsedAt == "" {
		t.Fatal("应记录最近使用时间")
	}
}

func TestPersistsAcrossRestart(t *testing.T) {
	store, dir := newTestStore(t)
	link, _ := store.Ensure("acc_1", "alpha@icloud.com")

	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	loaded, ok := reopened.Get(link.Token)
	if !ok {
		t.Fatal("重启后应仍能按 token 找到链接")
	}
	if loaded.Alias != "alpha@icloud.com" || loaded.AccountID != "acc_1" {
		t.Fatalf("持久化内容异常: %+v", loaded)
	}
	// 重新打开后按别名也能命中,且不会生成第二个 token
	same, _ := reopened.Ensure("acc_1", "alpha@icloud.com")
	if same.Token != link.Token {
		t.Fatalf("重启后应按别名复用已有 token: %q vs %q", same.Token, link.Token)
	}
}

func TestStoreFilePermissions(t *testing.T) {
	store, dir := newTestStore(t)
	if _, err := store.Ensure("acc_1", "alpha@icloud.com"); err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, storeFileName))
	if err != nil {
		t.Fatalf("存储文件不存在: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("存储文件权限应为 0600(含访问凭据),实际 %o", perm)
	}

	data, _ := os.ReadFile(filepath.Join(dir, storeFileName))
	var payload fileFormat
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("落盘内容不是合法 JSON: %v", err)
	}
	if len(payload.Links) != 1 {
		t.Fatalf("应落盘 1 条链接,实际 %d", len(payload.Links))
	}
}

func TestListFiltersByAccount(t *testing.T) {
	store, _ := newTestStore(t)
	store.Ensure("acc_1", "alpha@icloud.com")
	store.Ensure("acc_1", "beta@icloud.com")
	store.Ensure("acc_2", "gamma@icloud.com")

	list := store.List("acc_1")
	if len(list) != 2 {
		t.Fatalf("acc_1 应有 2 条链接,实际 %d", len(list))
	}
	if len(store.List("acc_missing")) != 0 {
		t.Fatal("不存在的账号应返回空列表")
	}
}

func TestEnsureRejectsEmptyInputs(t *testing.T) {
	store, _ := newTestStore(t)
	if _, err := store.Ensure("", "alpha@icloud.com"); err == nil {
		t.Fatal("空账号应报错")
	}
	if _, err := store.Ensure("acc_1", "  "); err == nil {
		t.Fatal("空别名应报错")
	}
}

// TestConcurrentEnsureIsSafe 并发为同一别名取链接时不应产生多个 token。
func TestConcurrentEnsureIsSafe(t *testing.T) {
	store, _ := newTestStore(t)

	var wg sync.WaitGroup
	tokens := make([]string, 16)
	for i := range tokens {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			link, err := store.Ensure("acc_1", "alpha@icloud.com")
			if err != nil {
				t.Errorf("并发生成失败: %v", err)
				return
			}
			tokens[index] = link.Token
		}(i)
	}
	wg.Wait()

	for _, token := range tokens {
		if token != tokens[0] {
			t.Fatalf("并发下产生了多个 token: %q vs %q", token, tokens[0])
		}
	}
	if list := store.List("acc_1"); len(list) != 1 {
		t.Fatalf("并发后应只有 1 条链接,实际 %d", len(list))
	}
}
