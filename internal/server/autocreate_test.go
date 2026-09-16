package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
)

// newAutoTestServer 构造一个等待被立即跳过的测试服务,便于在毫秒级验证长周期任务。
func newAutoTestServer(t *testing.T, f *fakeBackend) *Server {
	t.Helper()
	s := mustServer(t, f, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	s.autoSleep = func(ctx context.Context, _ time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	return s
}

func autoAccount() []account.Summary {
	return []account.Summary{{ID: "acc_1", Name: "主号", Status: "active"}}
}

// waitForPhase 轮询任务状态直到进入期望阶段。
func waitForPhase(t *testing.T, s *Server, phase string) AutoCreateStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := s.autoStatusSnapshot()
		if status.Phase == phase && !status.Running {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待阶段 %q 超时,当前状态: %+v", phase, s.autoStatusSnapshot())
	return AutoCreateStatus{}
}

func TestAutoCreateStopsAtTarget(t *testing.T) {
	var mu sync.Mutex
	created := 0
	var s *Server
	attemptingSeen := false
	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, label string) (*hme.CreateResult, error) {
		// 单次尝试期间必须能被外部观察到
		snapshot := s.autoStatusSnapshot()
		if snapshot.Attempting && snapshot.LastAttemptAt != "" {
			attemptingSeen = true
		}
		mu.Lock()
		defer mu.Unlock()
		created++
		return &hme.CreateResult{Email: fmt.Sprintf("auto%d@icloud.com", created), Label: label}, nil
	}
	// 账号已有 3 个别名,目标 5 → 只应再创建 2 个
	f.listAliasesFn = func(string) ([]hme.Alias, error) {
		mu.Lock()
		defer mu.Unlock()
		return make([]hme.Alias, 3+created), nil
	}

	s = newAutoTestServer(t, f)
	target := 5
	status, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target})
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if status.Total != 3 {
		t.Fatalf("启动时应校准为已有 3 个,得到 %d", status.Total)
	}

	final := waitForPhase(t, s, phaseCompleted)
	if final.Created != 2 {
		t.Fatalf("应创建 2 个,实际 %d", final.Created)
	}
	if final.Total != 5 || final.Target != 5 {
		t.Fatalf("结束时总数/目标异常: %d/%d", final.Total, final.Target)
	}
	if final.LastEmail != "auto2@icloud.com" {
		t.Fatalf("最后创建的别名异常: %q", final.LastEmail)
	}
	if final.Reason == "" {
		t.Fatal("完成时应给出结束原因")
	}
	if !attemptingSeen {
		t.Fatal("创建尝试期间应暴露 attempting 状态,便于前端提示进度")
	}
	if final.Attempting {
		t.Fatal("任务结束后不应保留 attempting 标记")
	}
}

func TestAutoCreateAlreadyAtTarget(t *testing.T) {
	f := &fakeBackend{
		accounts: autoAccount(),
		aliases:  make([]hme.Alias, 10),
	}
	s := newAutoTestServer(t, f)
	target := 10

	status, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target})
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	// 启动时同步校准,发现已达目标就直接结束,不留后台空转
	if status.Phase != phaseCompleted || status.Running {
		t.Fatalf("启动响应应直接为 completed,得到 %+v", status)
	}
	if status.Total != 10 {
		t.Fatalf("启动响应应带出校准后的总数 10,得到 %d", status.Total)
	}

	final := waitForPhase(t, s, phaseCompleted)
	if final.Created != 0 {
		t.Fatalf("已达目标不应创建,实际创建 %d", final.Created)
	}
	if f.createAliasCalls.Load() != 0 {
		t.Fatalf("不应调用创建,实际 %d 次", f.createAliasCalls.Load())
	}
}

func TestAutoCreateCoolsDownOnThrottleThenContinues(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, _ string) (*hme.CreateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			return nil, fmt.Errorf("创建别名失败: HTTP 429: too many requests")
		}
		return &hme.CreateResult{Email: fmt.Sprintf("auto%d@icloud.com", attempts)}, nil
	}
	f.listAliasesFn = func(string) ([]hme.Alias, error) {
		mu.Lock()
		defer mu.Unlock()
		if attempts == 0 {
			return nil, nil
		}
		return make([]hme.Alias, 1), nil
	}

	s := newAutoTestServer(t, f)
	// 冷却也被立即跳过,用于验证"撞限流后自动恢复"
	coolingSeen := false
	s.autoSleep = func(ctx context.Context, d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		if d == 30*time.Minute {
			// 冷却期间顶层聚合状态应反映 cooling
			if s.autoStatusSnapshot().Phase == phaseCooling {
				coolingSeen = true
			}
		}
		return true
	}
	target := 2

	if _, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target}); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	final := waitForPhase(t, s, phaseCompleted)
	if !coolingSeen {
		t.Fatal("命中限流后应进入冷却阶段")
	}
	if final.Created != 1 || final.Failed != 1 {
		t.Fatalf("应 1 成功 1 失败,实际 created=%d failed=%d", final.Created, final.Failed)
	}
	logs := fmt.Sprint(final.Logs)
	if !contains(logs, "命中速率限制") {
		t.Fatalf("日志应记录限流,实际: %s", logs)
	}
}

func TestAutoCreateCompletesOnQuotaExhausted(t *testing.T) {
	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, _ string) (*hme.CreateResult, error) {
		return nil, fmt.Errorf("保留失败: You have exceeded the maximum number of aliases allowed")
	}

	s := newAutoTestServer(t, f)
	target := 700
	if _, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target}); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	final := waitForPhase(t, s, phaseCompleted)
	if final.Reason == "" || !contains(final.Reason, "上限") {
		t.Fatalf("配额耗尽的结束原因异常: %q", final.Reason)
	}
	if f.createAliasCalls.Load() != 1 {
		t.Fatalf("配额耗尽后不应继续重试,实际调用 %d 次", f.createAliasCalls.Load())
	}
}

func TestAutoCreateStopsAfterConsecutiveFailures(t *testing.T) {
	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, _ string) (*hme.CreateResult, error) {
		return nil, fmt.Errorf("连接失败: dial tcp: i/o timeout")
	}

	s := newAutoTestServer(t, f)
	target, maxFailures := 700, 3
	limit := maxFailures
	if _, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target, MaxFailures: &limit}); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	final := waitForPhase(t, s, phaseStopped)
	if final.Failed != 3 {
		t.Fatalf("应失败 3 次后停止,实际 %d", final.Failed)
	}
	if !contains(final.Reason, "连续失败") {
		t.Fatalf("停止原因异常: %q", final.Reason)
	}
}

func TestAutoCreateRejectsSecondTask(t *testing.T) {
	var mu sync.Mutex
	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, _ string) (*hme.CreateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		return &hme.CreateResult{Email: "auto@icloud.com"}, nil
	}

	// 用真实等待,保证任务处于运行中
	s := mustServer(t, f, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	target, interval := 700, 3600
	if _, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target, IntervalSeconds: &interval}); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer s.stopAutoCreate()

	_, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target})
	if err == nil {
		t.Fatal("重复启动应被拒绝")
	}
	if err != ErrAutoCreateRunning {
		t.Fatalf("应返回 ErrAutoCreateRunning,得到 %v", err)
	}
}

func TestAutoCreateStopIsIdempotent(t *testing.T) {
	var mu sync.Mutex
	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, _ string) (*hme.CreateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		return &hme.CreateResult{Email: "auto@icloud.com"}, nil
	}

	s := mustServer(t, f, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	target, interval := 700, 3600
	if _, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target, IntervalSeconds: &interval}); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	stopped := s.stopAutoCreate()
	if stopped.Running || stopped.Phase != phaseStopped {
		t.Fatalf("停止后状态异常: %+v", stopped)
	}
	// 再次停止不应 panic,且保持 idle/stopped 语义
	again := s.stopAutoCreate()
	if again.Running {
		t.Fatalf("重复停止后不应在运行: %+v", again)
	}
}

func TestAutoCreateValidatesRequest(t *testing.T) {
	f := &fakeBackend{accounts: autoAccount()}
	s := newAutoTestServer(t, f)

	zero, huge, badInterval, badCooldown, badPrefix, badFailures := 0, 99999, 1, 10, "", 0
	cases := []struct {
		name string
		req  autoCreateStartRequest
	}{
		{"缺少账号", autoCreateStartRequest{}},
		{"账号不存在", autoCreateStartRequest{AccountID: "acc_missing"}},
		{"目标过小", autoCreateStartRequest{AccountID: "acc_1", Target: &zero}},
		{"目标过大", autoCreateStartRequest{AccountID: "acc_1", Target: &huge}},
		{"间隔过短", autoCreateStartRequest{AccountID: "acc_1", IntervalSeconds: &badInterval}},
		{"冷却过短", autoCreateStartRequest{AccountID: "acc_1", CooldownSeconds: &badCooldown}},
		{"连续失败上限非法", autoCreateStartRequest{AccountID: "acc_1", MaxFailures: &badFailures}},
	}
	for _, tc := range cases {
		if _, err := s.startAutoCreate(tc.req); err == nil {
			t.Fatalf("%s:应当报错", tc.name)
		}
	}

	_ = badPrefix
	// 空标签前缀回落默认值而不是报错
	target := 10
	status, err := s.startAutoCreate(autoCreateStartRequest{AccountID: "acc_1", Target: &target, LabelPrefix: &badPrefix})
	if err != nil {
		t.Fatalf("空标签前缀应回落默认值: %v", err)
	}
	if status.LabelPrefix != defaultAutoLabelPrefix {
		t.Fatalf("标签前缀应为默认值,得到 %q", status.LabelPrefix)
	}
	s.stopAutoCreate()
}

func TestAutoCreateStatusSnapshotBeforeStart(t *testing.T) {
	s := newAutoTestServer(t, &fakeBackend{accounts: autoAccount()})
	status := s.autoStatusSnapshot()
	if status.Phase != phaseIdle || status.Running {
		t.Fatalf("未启动时状态异常: %+v", status)
	}
	if status.Logs == nil {
		t.Fatal("日志字段应为空数组而不是 null")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// 多账号并行
// ---------------------------------------------------------------------------

func multiAccounts(ids ...string) []account.Summary {
	summaries := make([]account.Summary, 0, len(ids))
	for _, id := range ids {
		summaries = append(summaries, account.Summary{ID: id, Name: "账号-" + id, Status: "active"})
	}
	return summaries
}

func TestAutoCreateRunsAccountsInParallel(t *testing.T) {
	var mu sync.Mutex
	created := map[string]int{}
	f := &fakeBackend{accounts: multiAccounts("acc_1", "acc_2", "acc_3")}
	f.createAliasFn = func(accountID, _ string) (*hme.CreateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		created[accountID]++
		return &hme.CreateResult{Email: fmt.Sprintf("%s-auto%d@icloud.com", accountID, created[accountID])}, nil
	}
	f.listAliasesFn = func(accountID string) ([]hme.Alias, error) {
		mu.Lock()
		defer mu.Unlock()
		return make([]hme.Alias, created[accountID]), nil
	}

	s := newAutoTestServer(t, f)
	target := 2
	status, err := s.startAutoCreate(autoCreateStartRequest{
		AccountIDs: []string{"acc_1", "acc_2", "acc_3"},
		Target:     &target,
	})
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	// 启动响应应带出每个账号的配额检查结果
	if len(status.Accounts) != 3 {
		t.Fatalf("应有 3 个账号条目,得到 %d", len(status.Accounts))
	}
	for _, item := range status.Accounts {
		// 任务在启动响应返回前可能已推进首轮创建,因此这里只校验配额检查本身已执行
		if !item.CapacityChecked {
			t.Fatalf("账号 %s 未完成配额检查: %+v", item.AccountID, item)
		}
		if item.Name == "" || item.Target != target {
			t.Fatalf("账号 %s 的条目信息异常: %+v", item.AccountID, item)
		}
		if item.Remaining > target || item.Total > target {
			t.Fatalf("账号 %s 的进度越界: %+v", item.AccountID, item)
		}
	}

	final := waitForPhase(t, s, phaseCompleted)
	if final.Created != 6 {
		t.Fatalf("3 个账号各建 2 个应为 6,实际 %d", final.Created)
	}
	for _, id := range []string{"acc_1", "acc_2", "acc_3"} {
		if created[id] != 2 {
			t.Fatalf("账号 %s 应创建 2 个,实际 %d", id, created[id])
		}
	}
	if final.Total != 6 {
		t.Fatalf("汇总总数应为 6,实际 %d", final.Total)
	}
}

func TestAutoCreateSkipsAccountsAlreadyAtTarget(t *testing.T) {
	var mu sync.Mutex
	created := map[string]int{}
	f := &fakeBackend{accounts: multiAccounts("acc_full", "acc_empty")}
	f.createAliasFn = func(accountID, _ string) (*hme.CreateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		created[accountID]++
		return &hme.CreateResult{Email: accountID + "@icloud.com"}, nil
	}
	f.listAliasesFn = func(accountID string) ([]hme.Alias, error) {
		mu.Lock()
		defer mu.Unlock()
		if accountID == "acc_full" {
			return make([]hme.Alias, 5), nil // 已达目标
		}
		return make([]hme.Alias, created[accountID]), nil
	}

	s := newAutoTestServer(t, f)
	target := 5
	status, err := s.startAutoCreate(autoCreateStartRequest{
		AccountIDs: []string{"acc_full", "acc_empty"},
		Target:     &target,
	})
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	for _, item := range status.Accounts {
		if item.AccountID == "acc_full" {
			if item.Phase != phaseCompleted || item.Remaining != 0 {
				t.Fatalf("已达目标的账号应直接完成: %+v", item)
			}
		}
	}
	if created["acc_full"] != 0 {
		t.Fatalf("已达目标的账号不应创建,实际 %d", created["acc_full"])
	}

	final := waitForPhase(t, s, phaseCompleted)
	if created["acc_empty"] != 5 {
		t.Fatalf("未达标账号应建满 5 个,实际 %d", created["acc_empty"])
	}
	if final.Created != 5 {
		t.Fatalf("汇总创建数应为 5,实际 %d", final.Created)
	}
}

func TestAutoCreateOneAccountFailureDoesNotBlockOthers(t *testing.T) {
	var mu sync.Mutex
	created := map[string]int{}
	f := &fakeBackend{accounts: multiAccounts("acc_good", "acc_bad")}
	f.createAliasFn = func(accountID, _ string) (*hme.CreateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if accountID == "acc_bad" {
			return nil, fmt.Errorf("连接失败: dial tcp: i/o timeout")
		}
		created[accountID]++
		return &hme.CreateResult{Email: accountID + "@icloud.com"}, nil
	}
	f.listAliasesFn = func(accountID string) ([]hme.Alias, error) {
		mu.Lock()
		defer mu.Unlock()
		return make([]hme.Alias, created[accountID]), nil
	}

	s := newAutoTestServer(t, f)
	target, maxFailures := 2, 2
	if _, err := s.startAutoCreate(autoCreateStartRequest{
		AccountIDs:  []string{"acc_good", "acc_bad"},
		Target:      &target,
		MaxFailures: &maxFailures,
	}); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	// 坏账号连续失败达到上限后停止;好账号仍应建满
	final := waitForPhase(t, s, phaseStopped)
	byID := map[string]AutoCreateAccountStatus{}
	for _, item := range final.Accounts {
		byID[item.AccountID] = item
	}
	if byID["acc_bad"].Phase != phaseStopped {
		t.Fatalf("坏账号应停止,实际 %+v", byID["acc_bad"])
	}
	if byID["acc_good"].Phase != phaseCompleted || byID["acc_good"].Created != 2 {
		t.Fatalf("好账号不应受影响,实际 %+v", byID["acc_good"])
	}
	if !strings.Contains(final.Reason, "已完成") || !strings.Contains(final.Reason, "已停止") {
		t.Fatalf("汇总原因应同时反映完成与停止: %q", final.Reason)
	}
}

func TestAutoCreateAccountValidation(t *testing.T) {
	f := &fakeBackend{accounts: multiAccounts("acc_1", "acc_2")}
	s := newAutoTestServer(t, f)

	// 一个都不选
	if _, err := s.startAutoCreate(autoCreateStartRequest{}); err == nil {
		t.Fatal("未选择账号应报错")
	}
	// 含不存在的账号
	if _, err := s.startAutoCreate(autoCreateStartRequest{AccountIDs: []string{"acc_1", "acc_missing"}}); err == nil {
		t.Fatal("不存在的账号应报错")
	}
	// 重复账号会被去重
	target := 3
	status, err := s.startAutoCreate(autoCreateStartRequest{AccountIDs: []string{"acc_1", "acc_1", " acc_1 "}, Target: &target})
	if err != nil {
		t.Fatalf("重复账号应被去重而不是报错: %v", err)
	}
	if len(status.Accounts) != 1 {
		t.Fatalf("去重后应只有 1 个账号,实际 %d", len(status.Accounts))
	}
	s.stopAutoCreate()

	// 账号数与并发上限都受上限约束
	many := make([]string, 0, maxAutoAccounts+1)
	summaries := make([]account.Summary, 0, maxAutoAccounts+1)
	for i := 0; i <= maxAutoAccounts; i++ {
		id := fmt.Sprintf("acc_many_%d", i)
		many = append(many, id)
		summaries = append(summaries, account.Summary{ID: id, Name: id, Status: "active"})
	}
	f.setAccounts(summaries)
	if _, err := s.startAutoCreate(autoCreateStartRequest{AccountIDs: many}); err == nil {
		t.Fatalf("超过 %d 个账号应报错", maxAutoAccounts)
	}
	bad := maxAutoAccounts + 1
	if _, err := s.startAutoCreate(autoCreateStartRequest{AccountIDs: many[:1], MaxParallel: &bad}); err == nil {
		t.Fatal("并发上限越界应报错")
	}
}

// ---------------------------------------------------------------------------
// 创建间隔(随机休眠)
// ---------------------------------------------------------------------------

// TestAutoCreateWaitsRandomlyAfterEachAttempt 覆盖本次需求:
// 每创建完一个就随机休眠,且首个尝试立即执行(不会先等一轮)。
func TestAutoCreateWaitsRandomlyAfterEachAttempt(t *testing.T) {
	var mu sync.Mutex
	created := 0
	var waits []time.Duration

	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, _ string) (*hme.CreateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		created++
		return &hme.CreateResult{Email: fmt.Sprintf("auto%d@icloud.com", created)}, nil
	}
	f.listAliasesFn = func(string) ([]hme.Alias, error) { return nil, nil }

	s := mustServer(t, f, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	s.autoSleep = func(ctx context.Context, d time.Duration) bool {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}

	target, minSeconds, maxSeconds := 3, 20, 40
	if _, err := s.startAutoCreate(autoCreateStartRequest{
		AccountIDs:         []string{"acc_1"},
		Target:             &target,
		IntervalSeconds:    &minSeconds,
		IntervalMaxSeconds: &maxSeconds,
	}); err != nil {
		t.Fatalf("启动失败: %v", err)
	}

	final := waitForPhase(t, s, phaseCompleted)
	if final.Created != target {
		t.Fatalf("应创建 %d 个,实际 %d", target, final.Created)
	}

	// 3 次创建 → 只在第 1、2 次之后等待;最后一次达标即结束
	mu.Lock()
	defer mu.Unlock()
	if len(waits) != target-1 {
		t.Fatalf("应在每次创建后等待(最后一次除外),期望 %d 次,实际 %d 次: %v", target-1, len(waits), waits)
	}
	for index, wait := range waits {
		if wait < 20*time.Second || wait > 40*time.Second {
			t.Fatalf("第 %d 次等待 %s 超出 20-40s 区间", index+1, wait)
		}
		if wait%time.Second != 0 {
			t.Fatalf("等待时长为 %s,应为整秒", wait)
		}
	}

	logs := fmt.Sprint(final.Logs)
	if !strings.Contains(logs, "等待 2") && !strings.Contains(logs, "等待 3") && !strings.Contains(logs, "等待 4") {
		t.Fatalf("日志应记录实际等待时长: %s", logs)
	}
}

func TestAutoCreateDefaultIntervalRange(t *testing.T) {
	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, _ string) (*hme.CreateResult, error) {
		return &hme.CreateResult{Email: "auto@icloud.com"}, nil
	}
	s := newAutoTestServer(t, f)

	target := 5
	status, err := s.startAutoCreate(autoCreateStartRequest{AccountIDs: []string{"acc_1"}, Target: &target})
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if status.IntervalSeconds != 20 || status.IntervalMaxSeconds != 40 {
		t.Fatalf("默认随机区间应为 20-40 秒,实际 %d-%d", status.IntervalSeconds, status.IntervalMaxSeconds)
	}
}

// TestAutoCreateMaxIntervalFollowsMin 兼容只传 interval_seconds 的老调用。
func TestAutoCreateMaxIntervalFollowsMin(t *testing.T) {
	f := &fakeBackend{accounts: autoAccount()}
	f.createAliasFn = func(_, _ string) (*hme.CreateResult, error) {
		return &hme.CreateResult{Email: "auto@icloud.com"}, nil
	}
	s := newAutoTestServer(t, f)

	target, onlyMin := 5, 3600
	status, err := s.startAutoCreate(autoCreateStartRequest{
		AccountIDs:      []string{"acc_1"},
		Target:          &target,
		IntervalSeconds: &onlyMin,
	})
	if err != nil {
		t.Fatalf("只传 interval_seconds 应被接受: %v", err)
	}
	if status.IntervalMaxSeconds != onlyMin {
		t.Fatalf("上限应跟随最小值(退化为固定间隔),实际 %d", status.IntervalMaxSeconds)
	}
	s.stopAutoCreate()
}

func TestAutoCreateRejectsInvertedIntervalRange(t *testing.T) {
	f := &fakeBackend{accounts: autoAccount()}
	s := newAutoTestServer(t, f)

	minSeconds, maxSeconds := 100, 50
	if _, err := s.startAutoCreate(autoCreateStartRequest{
		AccountIDs:         []string{"acc_1"},
		IntervalSeconds:    &minSeconds,
		IntervalMaxSeconds: &maxSeconds,
	}); err == nil {
		t.Fatal("上限小于下限应报错")
	}
}

func TestDefaultAutoJitterRangeAndVariety(t *testing.T) {
	seen := make(map[time.Duration]bool)
	for i := 0; i < 300; i++ {
		got := defaultAutoJitter(20*time.Second, 40*time.Second)
		if got < 20*time.Second || got > 40*time.Second {
			t.Fatalf("随机结果 %s 超出 20-40s 区间", got)
		}
		seen[got] = true
	}
	if len(seen) < 10 {
		t.Fatalf("随机性不足:300 次只出现 %d 种取值", len(seen))
	}
	// 两端都应能被取到(等概率闭区间)
	var hitMin, hitMax bool
	for i := 0; i < 4000 && (!hitMin || !hitMax); i++ {
		switch defaultAutoJitter(20*time.Second, 40*time.Second) {
		case 20 * time.Second:
			hitMin = true
		case 40 * time.Second:
			hitMax = true
		}
	}
	if !hitMin || !hitMax {
		t.Fatalf("闭区间两端都应可取到(min=%v max=%v)", hitMin, hitMax)
	}

	if got := defaultAutoJitter(30*time.Second, 30*time.Second); got != 30*time.Second {
		t.Fatalf("上下限相同时应返回该值,实际 %s", got)
	}
	if got := defaultAutoJitter(30*time.Second, 10*time.Second); got != 30*time.Second {
		t.Fatalf("上限小于下限时应回落到下限,实际 %s", got)
	}
}
