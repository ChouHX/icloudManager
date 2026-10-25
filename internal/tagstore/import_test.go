package tagstore

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImportParsesCommonFormats(t *testing.T) {
	s := newTestStore(t)
	res, err := s.Import(ImportInput{
		Text: "# 注释行\n" +
			"a@example.com----pass-a----GitHub\n" +
			"b@example.com\tpass-b\n" +
			"c@example.com,pass-c,Steam\n" +
			"d@example.com:pass-d\n" +
			"e@example.com\n" +
			"\n",
		Tags: []string{"项目A", "项目A", " 开发 "},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Imported != 5 || res.Invalid != 0 || res.Skipped != 0 {
		t.Fatalf("导入统计不符: %+v", res)
	}

	byEmail := map[string]Entry{}
	for _, entry := range res.Entries {
		byEmail[entry.Email] = entry
	}
	if got := byEmail["a@example.com"]; got.Password != "pass-a" || got.Site != "GitHub" {
		t.Fatalf("a 行解析错误: %+v", got)
	}
	if got := byEmail["b@example.com"]; got.Password != "pass-b" {
		t.Fatalf("b 行解析错误: %+v", got)
	}
	if got := byEmail["c@example.com"]; got.Site != "Steam" {
		t.Fatalf("c 行解析错误: %+v", got)
	}
	if got := byEmail["d@example.com"]; got.Password != "pass-d" {
		t.Fatalf("d 行解析错误: %+v", got)
	}
	if got := byEmail["e@example.com"]; got.Password != "" {
		t.Fatalf("e 行应无密码: %+v", got)
	}
	// 所有记录都应带上统一标签(去重后 2 个)
	for _, entry := range res.Entries {
		if len(entry.Tags) != 2 {
			t.Fatalf("统一标签未正确应用: %+v", entry.Tags)
		}
	}
}

func TestImportLongDashSeparatorKeepsSiteClean(t *testing.T) {
	s := newTestStore(t)
	// 5/6 个连字符曾被按字面量 "----" 切分,残留的 "-" 粘到网站名上,
	// 导致同一个网站被拆成 Lord / -Lord / --Lord 多行统计。
	res, err := s.Import(ImportInput{
		Text: "a@example.com----p1-----Lord\n" +
			"b@example.com----p2------Lord\n" +
			"c@example.com-----p3----Lord\n",
		Tags: []string{"Lord"},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Imported != 3 || res.Invalid != 0 {
		t.Fatalf("导入统计不符: %+v", res)
	}
	for _, entry := range res.Entries {
		if entry.Site != "Lord" {
			t.Fatalf("网站名被分隔符污染: %q", entry.Site)
		}
	}
	counts, err := s.SiteCounts()
	if err != nil {
		t.Fatalf("SiteCounts: %v", err)
	}
	if len(counts) != 1 || counts[0].Site != "Lord" || counts[0].Count != 3 {
		t.Fatalf("网站统计未合并: %+v", counts)
	}
}

func TestRepairSiteNamesMergesDashResidue(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// 直接写入带残留连字符的历史数据,绕过 normalize 的输入校验。
	// 同时写入 entry_sites(统计已改为读这张表)。
	for _, site := range []string{"Lord", "-Lord", "--Lord", "---Lord", "Wimbledon"} {
		id := newEntryID()
		if _, err := s.db.Exec(`INSERT INTO entries(id, email, password, site, note, created_at, updated_at)
			VALUES(?, ?, '', ?, '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			id, site+"@example.com", site); err != nil {
			t.Fatalf("插入测试数据: %v", err)
		}
		if _, err := s.db.Exec(`INSERT INTO entry_sites(entry_id, site, site_lower, ord)
			VALUES(?, ?, lower(?), 0)`, id, site, site); err != nil {
			t.Fatalf("插入测试网站: %v", err)
		}
	}
	if _, err := s.repairSiteNames(); err != nil {
		t.Fatalf("repairSiteNames: %v", err)
	}
	counts, err := s.SiteCounts()
	if err != nil {
		t.Fatalf("SiteCounts: %v", err)
	}
	if len(counts) != 2 || counts[0].Site != "Lord" || counts[0].Count != 4 {
		t.Fatalf("残留连字符未合并: %+v", counts)
	}
	// 幂等:再次执行不应再产生变更。
	affected, err := s.repairSiteNames()
	if err != nil {
		t.Fatalf("repairSiteNames 第二次: %v", err)
	}
	if affected != 0 {
		t.Fatalf("repairSiteNames 不幂等,影响了 %d 行", affected)
	}
}

func TestImportCustomSeparator(t *testing.T) {
	s := newTestStore(t)
	// 自定义分隔符优先于自动识别:密码里的逗号不会被误切。
	res, err := s.Import(ImportInput{
		Text:      "a@example.com|p,1|GitHub\nb@example.com|p|2|Steam",
		Separator: "|",
		Tags:      []string{"项目A"},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Imported != 2 || res.Invalid != 0 {
		t.Fatalf("导入统计不符: %+v", res)
	}
	byEmail := map[string]Entry{}
	for _, entry := range res.Entries {
		byEmail[entry.Email] = entry
	}
	if got := byEmail["a@example.com"]; got.Password != "p,1" || got.Site != "GitHub" {
		t.Fatalf("自定义分隔符未生效: %+v", got)
	}
	// 第 4 段之后的内容会被忽略,只取邮箱/密码/网站三列。
	if got := byEmail["b@example.com"]; got.Password != "p" || got.Site != "2" {
		t.Fatalf("多列行解析错误: %+v", got)
	}
}

func TestImportSeparatorEscapes(t *testing.T) {
	s := newTestStore(t)
	// "\t" 两个字面量字符应被识别为真正的制表符。
	res, err := s.Import(ImportInput{
		Text:      "a@example.com\tpass-a\tGitHub",
		Separator: `\t`,
		Tags:      []string{"项目A"},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("导入统计不符: %+v", res)
	}
	if got := res.Entries[0]; got.Password != "pass-a" || got.Site != "GitHub" {
		t.Fatalf("\\t 转义未生效: %+v", got)
	}
}

func TestImportSeparatorValidation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Import(ImportInput{Text: "a@example.com", Separator: "0123456789"}); err == nil {
		t.Fatalf("超长分隔符应报错")
	}
}

func TestImportDefaultSiteAndValidation(t *testing.T) {
	s := newTestStore(t)
	res, err := s.Import(ImportInput{
		Text: "a@example.com----p1\nnot-an-email----p2\nb@example.com----p3----Steam",
		Site: "GitHub",
		Tags: []string{"项目A"},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Imported != 2 || res.Invalid != 1 || len(res.Errors) != 1 {
		t.Fatalf("统计不符: %+v", res)
	}
	for _, entry := range res.Entries {
		if entry.Email == "a@example.com" && entry.Site != "GitHub" {
			t.Fatalf("默认网站未生效: %+v", entry)
		}
		if entry.Email == "b@example.com" && entry.Site != "Steam" {
			t.Fatalf("行内网站应覆盖默认值: %+v", entry)
		}
	}
}

func TestImportSkipAndUpdateDuplicates(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{
		Email:    "a@example.com",
		Password: "old",
		Site:     "GitHub",
		Tags:     []string{"旧标签"},
	}); err != nil {
		t.Fatal(err)
	}

	// 完全重复(邮箱 + 网站 + 标签都相同):默认 skip 模式跳过,已有记录不变
	skipped, err := s.Import(ImportInput{
		Text: "a@example.com----new----GitHub",
		Tags: []string{"旧标签"},
	})
	if err != nil {
		t.Fatalf("Import skip: %v", err)
	}
	if skipped.Skipped != 1 || skipped.Imported != 0 || skipped.Updated != 0 {
		t.Fatalf("skip 统计不符: %+v", skipped)
	}
	if entry := s.List()[0]; entry.Password != "old" || len(entry.Tags) != 1 || entry.Tags[0] != "旧标签" {
		t.Fatalf("skip 不应修改已有记录: %+v", entry)
	}

	// 同邮箱 + 新网站:把网站追加到该邮箱,并合并标签;skip 模式不覆盖密码
	appended, err := s.Import(ImportInput{
		Text: "a@example.com----newer----Steam",
		Tags: []string{"新标签"},
	})
	if err != nil {
		t.Fatalf("Import 追加网站: %v", err)
	}
	if appended.Updated != 1 || appended.Imported != 0 || appended.SitesAppended != 1 {
		t.Fatalf("追加网站统计不符: %+v", appended)
	}
	entry := s.List()[0]
	if len(entry.Sites) != 2 || entry.Sites[0] != "GitHub" || entry.Sites[1] != "Steam" {
		t.Fatalf("网站应追加到同一邮箱: %+v", entry.Sites)
	}
	if entry.Password != "old" {
		t.Fatalf("skip 模式不应覆盖密码: %+v", entry)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "旧标签" || entry.Tags[1] != "新标签" {
		t.Fatalf("标签应合并: %+v", entry.Tags)
	}

	// update:覆盖密码
	updated, err := s.Import(ImportInput{
		Text: "a@example.com----new----GitHub",
		Tags: []string{"新标签"},
		Mode: "update",
	})
	if err != nil {
		t.Fatalf("Import update: %v", err)
	}
	if updated.Updated != 1 || updated.Imported != 0 || updated.SitesAppended != 0 {
		t.Fatalf("update 统计不符: %+v", updated)
	}
	if got := s.List()[0]; got.Password != "new" || len(got.Sites) != 2 {
		t.Fatalf("update 应覆盖密码且保留网站: %+v", got)
	}
}

func TestImportSameEmailDifferentSitesMergesIntoOneRecord(t *testing.T) {
	s := newTestStore(t)
	// 分两次、按单个网站导入:第二次应追加到同一邮箱而不是新建记录。
	if _, err := s.Import(ImportInput{
		Text: "a@example.com----p1",
		Site: "网站1",
		Tags: []string{"标签1"},
	}); err != nil {
		t.Fatalf("第一次导入: %v", err)
	}
	res, err := s.Import(ImportInput{
		Text: "a@example.com----p1",
		Site: "网站2",
		Tags: []string{"标签2"},
	})
	if err != nil {
		t.Fatalf("第二次导入: %v", err)
	}
	if res.Imported != 0 || res.Updated != 1 || res.SitesAppended != 1 {
		t.Fatalf("第二次导入应追加网站: %+v", res)
	}

	entries := s.List()
	if len(entries) != 1 {
		t.Fatalf("同一邮箱应只有一条记录,得到 %d 条", len(entries))
	}
	if got := entries[0].Sites; len(got) != 2 || got[0] != "网站1" || got[1] != "网站2" {
		t.Fatalf("网站列表不符: %+v", got)
	}
	if len(entries[0].Tags) != 2 {
		t.Fatalf("标签应合并: %+v", entries[0].Tags)
	}
}

func TestImportEmptyText(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Import(ImportInput{Text: "   \n# 只有注释\n"}); err == nil {
		t.Fatal("空内容应返回错误")
	}
}

func TestRenameAndDeleteTag(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "a@example.com", Tags: []string{"开发", "项目A"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Input{Email: "b@example.com", Tags: []string{"开发"}}); err != nil {
		t.Fatal(err)
	}

	changed, err := s.RenameTag("开发", "前端")
	if err != nil {
		t.Fatalf("RenameTag: %v", err)
	}
	if changed != 2 {
		t.Fatalf("期望影响 2 条记录,得到 %d", changed)
	}
	if containsTag(s.List()[0].Tags, "开发") {
		t.Fatal("旧标签应被替换")
	}

	// 重命名到已存在的标签时应去重
	if _, err := s.RenameTag("前端", "项目A"); err != nil {
		t.Fatalf("RenameTag 去重: %v", err)
	}
	for _, entry := range s.List() {
		if len(entry.Tags) != 1 || entry.Tags[0] != "项目A" {
			t.Fatalf("重命名去重失败: %+v", entry.Tags)
		}
	}

	removed, err := s.DeleteTag("项目A")
	if err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if removed != 2 {
		t.Fatalf("期望影响 2 条记录,得到 %d", removed)
	}
	for _, entry := range s.List() {
		if len(entry.Tags) != 0 {
			t.Fatalf("标签应被清空: %+v", entry.Tags)
		}
	}
	if n, err := s.DeleteTag("不存在"); err != nil || n != 0 {
		t.Fatalf("删除不存在的标签应无影响: n=%d err=%v", n, err)
	}
}

func TestImportRollbackOnWriteFailure(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "a@example.com"}); err != nil {
		t.Fatal(err)
	}
	before := len(s.List())

	// 只读模式让导入事务内的写入失败,验证整体回滚且不留下半成品。
	if _, err := s.db.Exec(`PRAGMA query_only = ON`); err != nil {
		t.Fatalf("设置只读模式: %v", err)
	}
	t.Cleanup(func() { _, _ = s.db.Exec(`PRAGMA query_only = OFF`) })

	if _, err := s.Import(ImportInput{Text: "b@example.com----p"}); err == nil {
		t.Fatal("写入失败时 Import 应返回错误")
	}
	if after := len(s.List()); after != before {
		t.Fatalf("写入失败后应回滚,记录数 %d → %d", before, after)
	}
}

// TestImportWithoutTagsKeepsTagsNonNull 回归:未选标签导入时,tags 必须是
// 空数组而非 null,否则前端 `for...of entry.tags` 会抛 "not iterable"。
func TestImportWithoutTagsKeepsTagsNonNull(t *testing.T) {
	s := newTestStore(t)
	res, err := s.Import(ImportInput{Text: "a@example.com----pass-a\nb@example.com"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Imported != 2 {
		t.Fatalf("应导入 2 条,实际 %+v", res)
	}
	for _, entry := range res.Entries {
		if entry.Tags == nil {
			t.Fatalf("导入结果的 tags 不应为 nil: %+v", entry)
		}
	}
	for _, entry := range s.List() {
		if entry.Tags == nil {
			t.Fatalf("List 的 tags 不应为 nil: %+v", entry)
		}
	}

	raw, err := json.Marshal(s.List())
	if err != nil {
		t.Fatalf("序列化记录: %v", err)
	}
	if strings.Contains(string(raw), `"tags":null`) {
		t.Fatalf("序列化结果不应出现 tags:null:\n%s", raw)
	}
}
