package tagstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListPagePagination(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 25; i++ {
		if _, err := s.Create(Input{
			Email: fmt.Sprintf("user%02d@example.com", i),
			Site:  "GitHub",
			Tags:  []string{"批量"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	first, err := s.ListPage(ListQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if first.Total != 25 || first.Page != 1 || first.PageSize != 10 || len(first.Entries) != 10 {
		t.Fatalf("第一页结果不符: %+v", first)
	}
	if first.Entries[0].Tags == nil {
		t.Fatal("分页结果的 tags 不应为 nil")
	}

	last, err := s.ListPage(ListQuery{Page: 3, PageSize: 10})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if last.Page != 3 || len(last.Entries) != 5 {
		t.Fatalf("最后一页结果不符: %+v", last)
	}

	// 页码越界时回退到最后一页,避免删除后出现空白页。
	overflow, err := s.ListPage(ListQuery{Page: 99, PageSize: 10})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if overflow.Page != 3 || len(overflow.Entries) != 5 {
		t.Fatalf("越界页码应回退到最后一页: %+v", overflow)
	}

	defaulted, err := s.ListPage(ListQuery{})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if defaulted.Page != 1 || defaulted.PageSize != defaultPageSize || len(defaulted.Entries) != defaultPageSize {
		t.Fatalf("默认分页参数不符: %+v", defaulted)
	}
}

func TestListPageSearchAndTagFilter(t *testing.T) {
	s := newTestStore(t)
	mustCreate := func(email, site string, tags ...string) {
		t.Helper()
		if _, err := s.Create(Input{Email: email, Site: site, Tags: tags}); err != nil {
			t.Fatal(err)
		}
	}
	mustCreate("a@example.com", "GitHub", "项目A", "开发")
	mustCreate("b@example.com", "Steam", "游戏")
	mustCreate("c@example.com", "GitHub", "项目B")

	search, err := s.ListPage(ListQuery{Search: "github"})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if search.Total != 2 {
		t.Fatalf("按网站搜索应命中 2 条: %+v", search)
	}

	byTag, err := s.ListPage(ListQuery{Search: "项目A"})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if byTag.Total != 1 || byTag.Entries[0].Email != "a@example.com" {
		t.Fatalf("按标签搜索不符: %+v", byTag)
	}

	// 多个标签是 OR 关系
	orTags, err := s.ListPage(ListQuery{Tags: []string{"游戏", "项目B"}})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if orTags.Total != 2 {
		t.Fatalf("多标签 OR 筛选应命中 2 条: %+v", orTags)
	}

	// 大小写不敏感
	lower, err := s.ListPage(ListQuery{Tags: []string{"项目a"}})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if lower.Total != 1 {
		t.Fatalf("标签筛选应忽略大小写: %+v", lower)
	}
}

func TestStatsAndTagCounts(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "a@example.com", Site: "GitHub", Tags: []string{"项目A", "开发"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Input{Email: "b@example.com", Site: "Steam", Tags: []string{"游戏", "开发"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Input{Email: "c@example.com"}); err != nil {
		t.Fatal(err)
	}

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	want := Stats{Total: 3, Tags: 3, Sites: 2, Untagged: 1}
	if stats != want {
		t.Fatalf("统计不符: got %+v want %+v", stats, want)
	}

	counts, err := s.TagCounts()
	if err != nil {
		t.Fatalf("TagCounts: %v", err)
	}
	byTag := make(map[string]int, len(counts))
	for _, usage := range counts {
		byTag[usage.Tag] = usage.Count
	}
	if byTag["开发"] != 2 || byTag["项目A"] != 1 || byTag["游戏"] != 1 {
		t.Fatalf("标签使用数不符: %+v", counts)
	}
}

func TestSiteCounts(t *testing.T) {
	s := newTestStore(t)
	for _, in := range []Input{
		{Email: "a@example.com", Site: "GitHub"},
		{Email: "b@example.com", Site: "github"},
		{Email: "c@example.com", Site: "Steam"},
		{Email: "d@example.com"},
	} {
		if _, err := s.Create(in); err != nil {
			t.Fatal(err)
		}
	}

	counts, err := s.SiteCounts()
	if err != nil {
		t.Fatalf("SiteCounts: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("网站应去重,得到 %+v", counts)
	}
	if counts[0].Site != "GitHub" || counts[0].Count != 2 {
		t.Fatalf("GitHub 统计不符: %+v", counts[0])
	}
	if counts[1].Site != "Steam" || counts[1].Count != 1 {
		t.Fatalf("Steam 统计不符: %+v", counts[1])
	}
}

func TestSiteTagCounts(t *testing.T) {
	s := newTestStore(t)
	for _, in := range []Input{
		// 网站1 共 5 条:标签1 2 条、标签2 3 条 → 40% : 60%
		{Email: "a1@example.com", Site: "网站1", Tags: []string{"标签1"}},
		{Email: "a2@example.com", Site: "网站1", Tags: []string{"标签1"}},
		{Email: "a3@example.com", Site: "网站1", Tags: []string{"标签2"}},
		{Email: "a4@example.com", Site: "网站1", Tags: []string{"标签2"}},
		{Email: "a5@example.com", Site: "网站1", Tags: []string{"标签2"}},
		// 大小写不同的网站应归并到同一行
		{Email: "b1@example.com", Site: "SiteOne", Tags: []string{"标签1"}},
		// 未打标签的记录仍计入该网站总数(分母)
		{Email: "b2@example.com", Site: "SITEONE"},
		// 一条记录带多个标签时,两个标签都会计数
		{Email: "c1@example.com", Site: "网站2", Tags: []string{"标签1", "标签2"}},
	} {
		if _, err := s.Create(in); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.SiteTagCounts()
	if err != nil {
		t.Fatalf("SiteTagCounts: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("网站应去重,得到 %+v", got)
	}

	bySite := map[string]SiteTagUsage{}
	for _, item := range got {
		bySite[strings.ToLower(item.Site)] = item
	}

	// 网站1 共 5 条:标签1 2 条、标签2 3 条 → 40% : 60%。
	one, ok := bySite["网站1"]
	if !ok {
		t.Fatalf("缺少网站1: %+v", got)
	}
	if one.Total != 5 || len(one.Tags) != 2 {
		t.Fatalf("网站1 统计不符: %+v", one)
	}
	if one.Tags[0].Tag != "标签2" || one.Tags[0].Count != 3 {
		t.Fatalf("标签应按数量降序: %+v", one.Tags)
	}
	if one.Tags[1].Tag != "标签1" || one.Tags[1].Count != 2 {
		t.Fatalf("标签1 统计不符: %+v", one.Tags)
	}

	// SiteOne / SITEONE 归并为 2 条,其中 1 条带标签1。
	merged, ok := bySite["siteone"]
	if !ok || merged.Total != 2 || len(merged.Tags) != 1 || merged.Tags[0].Count != 1 {
		t.Fatalf("大小写网站未归并: %+v", got)
	}

	// 网站2 只有 1 条记录,两个标签各计 1 次,合计会大于 Total。
	two := bySite["网站2"]
	if two.Total != 1 || len(two.Tags) != 2 {
		t.Fatalf("网站2 统计不符: %+v", two)
	}
}

func TestMigratesLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"entries": map[string]any{
			"tag_old1": map[string]any{
				"email":      "old@example.com",
				"password":   "p1",
				"site":       "GitHub",
				"tags":       []string{"旧项目", "开发"},
				"note":       "旧备注",
				"created_at": "2026-01-01T00:00:00+08:00",
				"updated_at": "2026-01-02T00:00:00+08:00",
			},
			"tag_old2": map[string]any{
				"email":      "nulltag@example.com",
				"tags":       nil,
				"created_at": "2026-01-03T00:00:00+08:00",
				"updated_at": "2026-01-03T00:00:00+08:00",
			},
		},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dir, legacyStoreFileName)
	if err := os.WriteFile(jsonPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	entries := s.List()
	if len(entries) != 2 {
		t.Fatalf("应迁移 2 条记录,得到 %+v", entries)
	}
	byEmail := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		byEmail[entry.Email] = entry
		if entry.Tags == nil {
			t.Fatalf("迁移后 tags 不应为 nil: %+v", entry)
		}
	}
	if got := byEmail["old@example.com"]; got.Password != "p1" || got.Site != "GitHub" || len(got.Tags) != 2 || got.Note != "旧备注" {
		t.Fatalf("迁移内容不符: %+v", got)
	}
	if got := byEmail["nulltag@example.com"]; len(got.Tags) != 0 {
		t.Fatalf("null 标签应迁移为空数组: %+v", got)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("迁移后旧 JSON 应被重命名,stat err=%v", err)
	}
	if _, err := os.Stat(jsonPath + ".bak"); err != nil {
		t.Fatalf("应保留 tags.json.bak: %v", err)
	}

	// 再次打开不应重复导入
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatalf("重新打开: %v", err)
	}
	if got := len(reopened.List()); got != 2 {
		t.Fatalf("重复打开不应重复导入,得到 %d 条", got)
	}
}

// TestNewStoreRelativeDataDir 回归测试:默认 -data ./data 是相对路径,
// 必须能正常打开 SQLite,而不是报 "invalid uri authority"。
func TestNewStoreRelativeDataDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	s, err := NewStore("./data")
	if err != nil {
		t.Fatalf("NewStore 相对路径失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.Create(Input{Email: "rel@example.com", Tags: []string{"相对路径"}}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, err := s.ListPage(ListQuery{Page: 1, PageSize: 10})
	if err != nil || res.Total != 1 {
		t.Fatalf("相对路径下读写失败: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "tags.db")); err != nil {
		t.Fatalf("tags.db 未创建: %v", err)
	}
}

func TestListPageSiteAndTimeFilter(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "a@example.com", Site: "GitHub", Tags: []string{"开发"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Input{Email: "b@example.com", Site: "Steam", Tags: []string{"游戏"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Input{Email: "c@example.com"}); err != nil {
		t.Fatal(err)
	}

	bySite, err := s.ListPage(ListQuery{Site: "github"})
	if err != nil || bySite.Total != 1 || bySite.Entries[0].Email != "a@example.com" {
		t.Fatalf("网站筛选失败: %+v %v", bySite, err)
	}

	now := time.Now()
	byTime, err := s.ListPage(ListQuery{
		CreatedFrom: now.Add(-time.Hour).Format(time.RFC3339),
		CreatedTo:   now.Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil || byTime.Total != 3 {
		t.Fatalf("时间筛选失败: %+v %v", byTime, err)
	}
	future, err := s.ListPage(ListQuery{CreatedFrom: now.Add(24 * time.Hour).Format(time.RFC3339)})
	if err != nil || future.Total != 0 {
		t.Fatalf("未来时间段应无记录: %+v %v", future, err)
	}

	sites, err := s.Sites()
	if err != nil {
		t.Fatalf("Sites: %v", err)
	}
	if len(sites) != 2 || sites[0] != "GitHub" || sites[1] != "Steam" {
		t.Fatalf("网站列表不符: %v", sites)
	}
}

func TestListPageTimeFilterTimezone(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Restore(RestoreInput{Entries: []Entry{
		{Email: "a@example.com", CreatedAt: "2026-08-01T00:30:00+08:00"},
		{Email: "b@example.com", CreatedAt: "2026-08-31T23:30:00+08:00"},
	}})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// 东八区 8 月 1 日 00:00 ~ 23:59:59 对应 UTC 7 月 31 日 16:00 ~ 8 月 1 日 15:59:59
	aug1, err := s.ListPage(ListQuery{
		CreatedFrom: "2026-07-31T16:00:00Z",
		CreatedTo:   "2026-08-01T15:59:59Z",
	})
	if err != nil || aug1.Total != 1 || aug1.Entries[0].Email != "a@example.com" {
		t.Fatalf("东八区 8 月 1 日筛选失败: %+v %v", aug1, err)
	}

	aug31, err := s.ListPage(ListQuery{
		CreatedFrom: "2026-08-30T16:00:00Z",
		CreatedTo:   "2026-08-31T15:59:59Z",
	})
	if err != nil || aug31.Total != 1 || aug31.Entries[0].Email != "b@example.com" {
		t.Fatalf("东八区 8 月 31 日筛选失败: %+v %v", aug31, err)
	}
}
