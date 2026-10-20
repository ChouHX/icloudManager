package tagstore

import "testing"

func TestExportRestoreRoundTrip(t *testing.T) {
	src := newTestStore(t)
	if _, err := src.Create(Input{
		Email:    "a@example.com",
		Password: "p1",
		Site:     "GitHub",
		Tags:     []string{"项目A", "开发"},
		Note:     "备注",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Create(Input{Email: "b@example.com", Site: "Steam", Tags: []string{"游戏"}}); err != nil {
		t.Fatal(err)
	}

	backup, err := src.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if backup.Version != backupVersion || backup.Type != backupType || backup.Count != 2 {
		t.Fatalf("备份元信息不符: %+v", backup)
	}

	dst := newTestStore(t)
	res, err := dst.Restore(RestoreInput{
		Version: backup.Version,
		Type:    backup.Type,
		Entries: backup.Entries,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.Imported != 2 || res.Skipped != 0 || res.Invalid != 0 || res.Total != 2 {
		t.Fatalf("导入统计不符: %+v", res)
	}

	got := dst.List()
	if len(got) != 2 {
		t.Fatalf("记录数不符: %+v", got)
	}
	// 迁移后内容与标签应完整保留
	byEmail := map[string]Entry{}
	for _, entry := range got {
		byEmail[entry.Email] = entry
	}
	if e := byEmail["a@example.com"]; e.Password != "p1" || e.Site != "GitHub" || len(e.Tags) != 2 || e.Note != "备注" {
		t.Fatalf("迁移内容不一致: %+v", e)
	}
	if e := byEmail["b@example.com"]; e.Site != "Steam" || len(e.Tags) != 1 {
		t.Fatalf("迁移内容不一致: %+v", e)
	}
}

func TestRestoreMergeAndReplace(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "keep@example.com", Site: "GitHub"}); err != nil {
		t.Fatal(err)
	}

	entries := []Entry{
		{Email: "keep@example.com", Site: "GitHub", Password: "should-skip"},
		{Email: "new@example.com", Site: "Steam", Tags: []string{"游戏"}},
	}

	// merge:重复的「邮箱+网站」跳过,新记录导入
	merged, err := s.Restore(RestoreInput{Entries: entries, Mode: "merge"})
	if err != nil {
		t.Fatalf("Restore merge: %v", err)
	}
	if merged.Imported != 1 || merged.Skipped != 1 || merged.Total != 2 {
		t.Fatalf("merge 统计不符: %+v", merged)
	}

	// replace:清空后仅保留备份内容
	replaced, err := s.Restore(RestoreInput{Entries: entries, Mode: "replace"})
	if err != nil {
		t.Fatalf("Restore replace: %v", err)
	}
	if replaced.Imported != 2 || replaced.Skipped != 0 || replaced.Total != 2 {
		t.Fatalf("replace 统计不符: %+v", replaced)
	}
	for _, entry := range s.List() {
		if entry.Email == "keep@example.com" && entry.Password != "should-skip" {
			t.Fatalf("replace 应使用备份内容: %+v", entry)
		}
	}
}

func TestRestoreValidationAndInvalidRows(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Restore(RestoreInput{}); err == nil {
		t.Fatal("空备份应报错")
	}
	if _, err := s.Restore(RestoreInput{Type: "other", Entries: []Entry{{Email: "a@example.com"}}}); err == nil {
		t.Fatal("类型不匹配应报错")
	}
	if _, err := s.Restore(RestoreInput{Version: 99, Entries: []Entry{{Email: "a@example.com"}}}); err == nil {
		t.Fatal("版本不支持应报错")
	}
	if _, err := s.Restore(RestoreInput{Mode: "bogus", Entries: []Entry{{Email: "a@example.com"}}}); err == nil {
		t.Fatal("非法模式应报错")
	}

	// 非法行计入 invalid,合法行正常导入
	res, err := s.Restore(RestoreInput{Entries: []Entry{
		{Email: "ok@example.com"},
		{Email: "not-an-email"},
	}})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.Imported != 1 || res.Invalid != 1 || len(res.Errors) != 1 {
		t.Fatalf("统计不符: %+v", res)
	}
}

func TestRestoreRegeneratesConflictingID(t *testing.T) {
	s := newTestStore(t)
	existing, err := s.Create(Input{Email: "existing@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.Restore(RestoreInput{Entries: []Entry{
		{ID: existing.ID, Email: "imported@example.com"},
	}})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("应导入 1 条: %+v", res)
	}
	var imported *Entry
	for _, entry := range s.List() {
		if entry.Email == "imported@example.com" {
			cp := entry
			imported = &cp
		}
	}
	if imported == nil {
		t.Fatal("未找到导入的记录")
	}
	if imported.ID == existing.ID {
		t.Fatal("ID 冲突时应重新生成")
	}
}
