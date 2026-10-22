package tagstore

import (
	"errors"
	"testing"
	"time"
)

func TestBatchTagBySiteAddRemoveReplace(t *testing.T) {
	s := newTestStore(t)

	githubA, err := s.Create(Input{Email: "a@example.com", Site: "GitHub", Tags: []string{"旧"}})
	if err != nil {
		t.Fatal(err)
	}
	githubB, err := s.Create(Input{Email: "b@example.com", Site: "GitHub"})
	if err != nil {
		t.Fatal(err)
	}
	steam, err := s.Create(Input{Email: "c@example.com", Site: "Steam", Tags: []string{"旧", "游戏"}})
	if err != nil {
		t.Fatal(err)
	}

	// 按网站批量追加标签(已存在的「旧」不应重复)
	res, err := s.BatchTag(BatchInput{
		Filter: ListQuery{Site: "github"},
		Action: "add",
		Tags:   []string{"新项目", "旧"},
	})
	if err != nil {
		t.Fatalf("BatchTag add: %v", err)
	}
	if res.Affected != 2 || res.Action != "add" || len(res.Tags) != 2 {
		t.Fatalf("批量追加结果不符: %+v", res)
	}
	got, _ := s.Get(githubA.ID)
	if len(got.Tags) != 2 || got.Tags[0] != "旧" || got.Tags[1] != "新项目" {
		t.Fatalf("追加后标签不符: %+v", got.Tags)
	}
	got, _ = s.Get(githubB.ID)
	if len(got.Tags) != 2 || got.Tags[0] != "新项目" || got.Tags[1] != "旧" {
		t.Fatalf("追加后标签不符: %+v", got.Tags)
	}
	got, _ = s.Get(steam.ID)
	if len(got.Tags) != 2 {
		t.Fatalf("未命中的网站不应被修改: %+v", got.Tags)
	}

	// 按网站批量移除标签
	res, err = s.BatchTag(BatchInput{
		Filter: ListQuery{Site: "GITHUB"},
		Action: "remove",
		Tags:   []string{"旧"},
	})
	if err != nil || res.Affected != 2 {
		t.Fatalf("BatchTag remove: res=%+v err=%v", res, err)
	}
	got, _ = s.Get(githubA.ID)
	if containsTag(got.Tags, "旧") {
		t.Fatalf("「旧」应被移除: %+v", got.Tags)
	}

	// 按 ID 覆盖标签
	res, err = s.BatchTag(BatchInput{
		IDs:    []string{steam.ID},
		Action: "replace",
		Tags:   []string{"仅此", "两个"},
	})
	if err != nil || res.Affected != 1 {
		t.Fatalf("BatchTag replace: res=%+v err=%v", res, err)
	}
	got, _ = s.Get(steam.ID)
	if len(got.Tags) != 2 || got.Tags[0] != "仅此" || got.Tags[1] != "两个" {
		t.Fatalf("覆盖后标签不符: %+v", got.Tags)
	}

	// ID 去重后只处理一次
	res, err = s.BatchTag(BatchInput{
		IDs:    []string{steam.ID, steam.ID, ""},
		Action: "add",
		Tags:   []string{"再次"},
	})
	if err != nil || res.Affected != 1 {
		t.Fatalf("ID 去重失败: res=%+v err=%v", res, err)
	}

	// 不存在的 ID 不应计入 affected
	res, err = s.BatchTag(BatchInput{
		IDs:    []string{"tag_missing"},
		Action: "add",
		Tags:   []string{"无效"},
	})
	if err != nil || res.Affected != 0 {
		t.Fatalf("不存在的 ID 应返回 0: res=%+v err=%v", res, err)
	}
}

func TestBatchTagByCreatedTime(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "a@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Input{Email: "b@example.com"}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	res, err := s.BatchTag(BatchInput{
		Filter: ListQuery{
			CreatedFrom: now.Add(-time.Hour).Format(time.RFC3339),
			CreatedTo:   now.Add(time.Hour).Format(time.RFC3339),
		},
		Action: "add",
		Tags:   []string{"今日"},
	})
	if err != nil || res.Affected != 2 {
		t.Fatalf("按时间批量追加失败: res=%+v err=%v", res, err)
	}

	// 未来时间段不应命中任何记录
	res, err = s.BatchTag(BatchInput{
		Filter: ListQuery{CreatedFrom: now.Add(24 * time.Hour).Format(time.RFC3339)},
		Action: "add",
		Tags:   []string{"未来"},
	})
	if err != nil || res.Affected != 0 {
		t.Fatalf("未来时间段应无命中: res=%+v err=%v", res, err)
	}
}

func TestBatchDeleteByIDsFilterAndAll(t *testing.T) {
	s := newTestStore(t)

	githubA, err := s.Create(Input{Email: "a@example.com", Site: "GitHub", Tags: []string{"项目A"}})
	if err != nil {
		t.Fatal(err)
	}
	githubB, err := s.Create(Input{Email: "b@example.com", Site: "GitHub"})
	if err != nil {
		t.Fatal(err)
	}
	steam, err := s.Create(Input{Email: "c@example.com", Site: "Steam", Tags: []string{"游戏"}})
	if err != nil {
		t.Fatal(err)
	}

	// 空条件且未显式 all 时应拒绝,避免误删全部
	if _, err := s.BatchDelete(DeleteQuery{}); err == nil {
		t.Fatal("空条件应返回参数错误")
	} else {
		var invalid *BatchValidationError
		if !errors.As(err, &invalid) {
			t.Fatalf("应返回 BatchValidationError,得到 %v", err)
		}
	}
	if entries := s.List(); len(entries) != 3 {
		t.Fatalf("校验失败时不应删除数据: %+v", entries)
	}

	// all 不能与筛选条件同时使用
	if _, err := s.BatchDelete(DeleteQuery{All: true, Filter: ListQuery{Site: "GitHub"}}); err == nil {
		t.Fatal("all 与 filter 同时使用应返回参数错误")
	}

	// 按 ID 删除,不存在的 ID 忽略
	affected, err := s.BatchDelete(DeleteQuery{IDs: []string{githubA.ID, "tag_missing", githubA.ID, ""}})
	if err != nil || affected != 1 {
		t.Fatalf("按 ID 删除失败: affected=%d err=%v", affected, err)
	}
	if _, ok := s.Get(githubA.ID); ok {
		t.Fatal("记录应已删除")
	}

	// 按筛选删除
	affected, err = s.BatchDelete(DeleteQuery{Filter: ListQuery{Site: "github"}})
	if err != nil || affected != 1 {
		t.Fatalf("按筛选删除失败: affected=%d err=%v", affected, err)
	}
	if _, ok := s.Get(githubB.ID); ok {
		t.Fatal("GitHub 记录应已删除")
	}
	if _, ok := s.Get(steam.ID); !ok {
		t.Fatal("未命中的记录不应被删除")
	}

	// 显式 all 删除全部
	affected, err = s.BatchDelete(DeleteQuery{All: true})
	if err != nil || affected != 1 {
		t.Fatalf("全部删除失败: affected=%d err=%v", affected, err)
	}
	if entries := s.List(); len(entries) != 0 {
		t.Fatalf("全部删除后应无记录: %+v", entries)
	}
}

func TestBatchTagValidation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "a@example.com", Site: "GitHub"}); err != nil {
		t.Fatal(err)
	}

	cases := []BatchInput{
		{Action: "unknown", Tags: []string{"x"}, Filter: ListQuery{Site: "GitHub"}},
		{Action: "add", Tags: nil, Filter: ListQuery{Site: "GitHub"}},
		{Action: "add", Tags: []string{"x"}},
		{Action: "add", Tags: []string{"x"}, Filter: ListQuery{Tags: []string{"   "}}},
		{Action: "add", Tags: []string{"x"}, Filter: ListQuery{Search: "   "}},
	}
	for _, in := range cases {
		_, err := s.BatchTag(in)
		var invalid *BatchValidationError
		if !errors.As(err, &invalid) {
			t.Fatalf("应返回参数错误: in=%+v err=%v", in, err)
		}
	}

	// 空筛选且无 ID 时不应误改全部记录
	entries := s.List()
	if len(entries) != 1 || containsTag(entries[0].Tags, "x") {
		t.Fatalf("校验失败时不应修改数据: %+v", entries)
	}
}
