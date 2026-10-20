package tagstore

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestCreateAndPersist(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	entry, err := s.Create(Input{
		Email:    "user@example.com",
		Password: "p@ss",
		Site:     "GitHub",
		Tags:     []string{"项目A", "项目A", " 开发 "},
		Note:     "备注",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if entry.ID == "" {
		t.Fatal("期望生成 ID")
	}
	if len(entry.Tags) != 2 {
		t.Fatalf("标签应去重并去空白,得到 %v", entry.Tags)
	}

	// 重新打开应能读回
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatalf("重新打开: %v", err)
	}
	got, ok := reopened.Get(entry.ID)
	if !ok {
		t.Fatal("重新打开后记录丢失")
	}
	if got.Email != "user@example.com" || got.Password != "p@ss" || got.Site != "GitHub" {
		t.Fatalf("记录内容不一致: %+v", got)
	}

	// 文件权限应为 0600
	info, err := os.Stat(filepath.Join(dir, storeFileName))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("期望文件权限 0600,得到 %o", perm)
	}
}

func TestCreateValidation(t *testing.T) {
	s := newTestStore(t)
	cases := []Input{
		{Email: ""},
		{Email: "not-an-email"},
		{Email: "user@example.com", Tags: make([]string, 0)},
	}
	// 超出标签数量上限
	many := make([]string, 0, maxTags+1)
	for i := 0; i <= maxTags; i++ {
		many = append(many, string(rune('a'+i)))
	}
	cases[2].Tags = many

	for i, in := range cases {
		if _, err := s.Create(in); err == nil {
			t.Fatalf("用例 %d 期望校验失败: %+v", i, in)
		}
	}
}

func TestUpdateAndDelete(t *testing.T) {
	s := newTestStore(t)
	entry, err := s.Create(Input{Email: "a@example.com", Tags: []string{"one"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := s.Update(entry.ID, Input{
		Email:    "b@example.com",
		Password: "new",
		Tags:     []string{"two", "three"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Email != "b@example.com" || len(updated.Tags) != 2 {
		t.Fatalf("更新结果不符: %+v", updated)
	}
	if updated.CreatedAt != entry.CreatedAt {
		t.Fatal("CreatedAt 不应被修改")
	}

	if _, err := s.Update("missing", Input{Email: "x@example.com"}); err == nil {
		t.Fatal("更新不存在的记录应报错")
	}

	removed, err := s.Delete(entry.ID)
	if err != nil || !removed {
		t.Fatalf("Delete: removed=%v err=%v", removed, err)
	}
	if removed, _ := s.Delete(entry.ID); removed {
		t.Fatal("重复删除应返回 false")
	}
}

func TestTagsAggregation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "a@example.com", Tags: []string{"Beta", "alpha"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Input{Email: "b@example.com", Tags: []string{"ALPHA", "gamma"}}); err != nil {
		t.Fatal(err)
	}
	tags := s.Tags()
	want := []string{"Beta", "alpha", "gamma"} // 忽略大小写去重,保留首次写法,按字母序
	if len(tags) != len(want) {
		t.Fatalf("标签聚合错误: %v", tags)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("标签聚合错误: got %v want %v", tags, want)
		}
	}
}
