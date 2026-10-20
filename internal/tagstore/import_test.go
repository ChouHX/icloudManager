package tagstore

import (
	"os"
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

	// 默认 skip:重复项被跳过,已有记录不变
	skipped, err := s.Import(ImportInput{
		Text: "a@example.com----new----GitHub",
		Tags: []string{"新标签"},
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

	// update:覆盖密码并合并标签
	updated, err := s.Import(ImportInput{
		Text: "a@example.com----new----GitHub",
		Tags: []string{"新标签"},
		Mode: "update",
	})
	if err != nil {
		t.Fatalf("Import update: %v", err)
	}
	if updated.Updated != 1 || updated.Imported != 0 {
		t.Fatalf("update 统计不符: %+v", updated)
	}
	entry := s.List()[0]
	if entry.Password != "new" {
		t.Fatalf("update 应覆盖密码: %+v", entry)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "旧标签" || entry.Tags[1] != "新标签" {
		t.Fatalf("update 应合并标签: %+v", entry.Tags)
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

func TestImportRollbackOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Input{Email: "a@example.com"}); err != nil {
		t.Fatal(err)
	}
	before := len(s.List())

	// 破坏数据文件所在目录的写入能力:改为只读
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限限制")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("无法设置只读目录: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()

	if _, err := s.Import(ImportInput{Text: "b@example.com----p"}); err == nil {
		t.Fatal("保存失败时 Import 应返回错误")
	}
	if after := len(s.List()); after != before {
		t.Fatalf("保存失败后应回滚,记录数 %d → %d", before, after)
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

	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatalf("读取存储文件: %v", err)
	}
	if strings.Contains(string(raw), `"tags": null`) {
		t.Fatalf("持久化文件不应出现 tags:null:\n%s", raw)
	}
}
