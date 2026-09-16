// Package server - 后台自动建满别名任务。
//
// 任务按固定间隔创建别名,直到别名总数达到目标值,或被 iCloud 判定为配额已满。
// 命中速率限制时进入冷却等待(默认 30 分钟,与 iCloud 的节流窗口一致)再继续,
// 因此任务可以无人值守地长时间运行。
//
// 支持一次勾选多个账号:每个账号一条独立循环(goroutine),各自持有自己的
// 节流窗口、冷却计时与失败计数 —— iCloud 的创建限流是按账号计的,多账号并行
// 才能线性提升总吞吐;同一个账号内并行创建没有收益,反而更快撞上限制。
//
// 约束:
//   - 全局单任务:同一时间只允许一个建满任务(可包含多个账号),避免任务互相干扰
//   - 启动前对每个账号做一次配额检查(当前别名总数),已达目标的账号直接跳过
//   - 状态在内存中(进程重启后不自动恢复)
package server

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/hme"
)

const (
	// 任务参数的默认值与边界。
	defaultAutoTarget      = 700
	defaultAutoInterval    = 20 * time.Second
	defaultAutoIntervalMax = 40 * time.Second
	defaultAutoCooldown    = 30 * time.Minute
	defaultAutoLabelPrefix = "auto"
	defaultAutoMaxFailures = 5
	defaultAutoMaxParallel = 10

	minAutoTarget      = 1
	maxAutoTarget      = 5000
	minAutoIntervalSec = 5
	maxAutoIntervalSec = 3600
	minAutoCooldownSec = 60
	maxAutoCooldownSec = 86400
	maxAutoLabelRunes  = 50
	minAutoMaxFailures = 1
	maxAutoMaxFailures = 100
	maxAutoAccounts    = 20
	maxAutoCreateLogs  = 200
	autoMinSleepChunk  = 500 * time.Millisecond

	// initialCapacityTimeout 限制启动阶段的配额检查时长。
	// 超时未返回的账号按"未检查"处理,交给后台循环自行校准。
	initialCapacityTimeout = 20 * time.Second

	phaseIdle      = "idle"
	phaseRunning   = "running"
	phaseCooling   = "cooling"
	phaseCompleted = "completed"
	phaseStopped   = "stopped"
)

// ErrAutoCreateRunning 表示已有任务在运行。
var ErrAutoCreateRunning = errors.New("已有自动建满任务在运行,请先停止")

// AutoCreateLog 是任务日志的一条记录。
type AutoCreateLog struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// AutoCreateAccountStatus 是单个账号在任务中的进度。
type AutoCreateAccountStatus struct {
	AccountID           string `json:"account_id"`
	Name                string `json:"name"`
	Phase               string `json:"phase"`
	Target              int    `json:"target"`
	Total               int    `json:"total"`
	Remaining           int    `json:"remaining"`
	Created             int    `json:"created"`
	Failed              int    `json:"failed"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	// CapacityChecked 表示启动时的配额检查是否成功(失败时会给出 CapacityError)
	CapacityChecked bool   `json:"capacity_checked"`
	CapacityError   string `json:"capacity_error,omitempty"`
	NextRunAt       string `json:"next_run_at,omitempty"`
	Attempting      bool   `json:"attempting"`
	LastAttemptAt   string `json:"last_attempt_at,omitempty"`
	LastEmail       string `json:"last_email,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// AutoCreateStatus 是任务状态快照。
//
// 顶层字段是所有账号的汇总(空任务时为零值),逐账号细节在 accounts 里。
type AutoCreateStatus struct {
	Running   bool   `json:"running"`
	Phase     string `json:"phase"`
	AccountID string `json:"account_id,omitempty"`
	Target    int    `json:"target"`
	Total     int    `json:"total"`
	Created   int    `json:"created"`
	Failed    int    `json:"failed"`

	IntervalSeconds    int    `json:"interval_seconds"`
	IntervalMaxSeconds int    `json:"interval_max_seconds"`
	CooldownSeconds    int    `json:"cooldown_seconds"`
	LabelPrefix        string `json:"label_prefix"`
	MaxFailures        int    `json:"max_failures"`
	MaxParallel        int    `json:"max_parallel"`

	StartedAt     string `json:"started_at,omitempty"`
	NextRunAt     string `json:"next_run_at,omitempty"`
	Attempting    bool   `json:"attempting"`
	LastAttemptAt string `json:"last_attempt_at,omitempty"`
	LastEmail     string `json:"last_email,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	Reason        string `json:"reason,omitempty"`

	Accounts []AutoCreateAccountStatus `json:"accounts"`
	Logs     []AutoCreateLog           `json:"logs"`
}

type autoCreateTask struct {
	cancel context.CancelFunc
	status AutoCreateStatus
}

// autoCreateStartRequest 是启动任务的请求体。
//
// 单账号用 account_id,多账号用 account_ids,两者可同时给出(会去重合并)。
type autoCreateStartRequest struct {
	AccountID          string   `json:"account_id"`
	AccountIDs         []string `json:"account_ids"`
	Target             *int     `json:"target"`
	IntervalSeconds    *int     `json:"interval_seconds"`
	IntervalMaxSeconds *int     `json:"interval_max_seconds"`
	CooldownSeconds    *int     `json:"cooldown_seconds"`
	LabelPrefix        *string  `json:"label_prefix"`
	MaxFailures        *int     `json:"max_failures"`
	MaxParallel        *int     `json:"max_parallel"`
}

// ---------------------------------------------------------------------------
// 状态读写
// ---------------------------------------------------------------------------

func (s *Server) autoStatusSnapshot() AutoCreateStatus {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if s.auto == nil {
		return AutoCreateStatus{Phase: phaseIdle, Accounts: []AutoCreateAccountStatus{}, Logs: []AutoCreateLog{}}
	}
	snapshot := s.auto.status
	snapshot.Accounts = append([]AutoCreateAccountStatus(nil), s.auto.status.Accounts...)
	snapshot.Logs = append([]AutoCreateLog(nil), s.auto.status.Logs...)
	aggregateAutoStatus(&snapshot)
	return snapshot
}

// updateAuto 在锁内修改任务级状态。
func (s *Server) updateAuto(apply func(status *AutoCreateStatus)) {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if s.auto == nil {
		return
	}
	apply(&s.auto.status)
}

// updateAutoAccount 在锁内修改某个账号的进度。
func (s *Server) updateAutoAccount(accountID string, apply func(account *AutoCreateAccountStatus)) {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if s.auto == nil {
		return
	}
	for i := range s.auto.status.Accounts {
		if s.auto.status.Accounts[i].AccountID == accountID {
			apply(&s.auto.status.Accounts[i])
			return
		}
	}
}

// aggregateAutoStatus 由各账号进度重算顶层汇总。
func aggregateAutoStatus(status *AutoCreateStatus) {
	if len(status.Accounts) == 0 {
		return
	}

	total, created, failed := 0, 0, 0
	active, cooling, stopped := 0, 0, 0
	attempting := false
	var earliestNext, lastAttempt time.Time
	lastEmail, lastError := "", ""

	for _, account := range status.Accounts {
		total += account.Total
		created += account.Created
		failed += account.Failed
		if account.Attempting {
			attempting = true
		}
		switch account.Phase {
		case phaseRunning:
			active++
		case phaseCooling:
			cooling++
			active++
		case phaseStopped:
			stopped++
		}
		if account.NextRunAt != "" {
			if parsed, err := time.Parse(time.RFC3339, account.NextRunAt); err == nil {
				if earliestNext.IsZero() || parsed.Before(earliestNext) {
					earliestNext = parsed
				}
			}
		}
		if account.LastEmail != "" {
			lastEmail = account.LastEmail
		}
		if account.LastError != "" {
			lastError = account.LastError
		}
		if account.LastAttemptAt != "" {
			if parsed, err := time.Parse(time.RFC3339, account.LastAttemptAt); err == nil && parsed.After(lastAttempt) {
				lastAttempt = parsed
			}
		}
	}

	status.Total, status.Created, status.Failed = total, created, failed
	status.Attempting = attempting
	status.LastEmail, status.LastError = lastEmail, lastError
	if !lastAttempt.IsZero() {
		status.LastAttemptAt = lastAttempt.Format(time.RFC3339)
	}
	if earliestNext.IsZero() {
		status.NextRunAt = ""
	} else {
		status.NextRunAt = earliestNext.Format(time.RFC3339)
	}

	switch {
	case active > 0 && cooling == active:
		status.Running, status.Phase = true, phaseCooling
	case active > 0:
		status.Running, status.Phase = true, phaseRunning
	case stopped > 0:
		status.Running, status.Phase = false, phaseStopped
	default:
		status.Running, status.Phase = false, phaseCompleted
	}

	// 汇总说明:单账号直接沿用它的结束原因,多账号才拼一句总览
	if !status.Running {
		if len(status.Accounts) == 1 && status.Accounts[0].Reason != "" {
			status.Reason = status.Accounts[0].Reason
			return
		}
		done, ok := 0, 0
		for _, account := range status.Accounts {
			switch account.Phase {
			case phaseCompleted:
				done++
			case phaseStopped:
				ok++
			}
		}
		switch {
		case ok > 0 && done > 0:
			status.Reason = fmt.Sprintf("%d 个账号已完成,%d 个已停止(请检查),共创建 %d 个别名", done, ok, created)
		case ok > 0:
			status.Reason = fmt.Sprintf("%d 个账号已停止(请检查),共创建 %d 个别名", ok, created)
		default:
			status.Reason = fmt.Sprintf("%d 个账号已完成,共创建 %d 个别名", done, created)
		}
	}
}

func (s *Server) appendAutoLog(level, format string, args ...any) {
	s.updateAuto(func(status *AutoCreateStatus) {
		appendLog(&status.Logs, "", level, format, args...)
	})
}

// appendAutoLogFor 记录带账号前缀的日志,便于多账号任务区分来源。
func (s *Server) appendAutoLogFor(name, level, format string, args ...any) {
	s.updateAuto(func(status *AutoCreateStatus) {
		appendLog(&status.Logs, name, level, format, args...)
	})
}

func appendLog(logs *[]AutoCreateLog, prefix, level, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if prefix != "" {
		message = "[" + prefix + "] " + message
	}
	*logs = append(*logs, AutoCreateLog{
		Time:    time.Now().Format(time.RFC3339),
		Level:   level,
		Message: message,
	})
	if len(*logs) > maxAutoCreateLogs {
		*logs = (*logs)[len(*logs)-maxAutoCreateLogs:]
	}
}

// ---------------------------------------------------------------------------
// 任务生命周期
// ---------------------------------------------------------------------------

// startAutoCreate 校验参数、做初始配额检查,然后并行启动各账号的创建循环。
func (s *Server) startAutoCreate(req autoCreateStartRequest) (AutoCreateStatus, error) {
	accountIDs, err := s.normalizeAutoAccounts(req)
	if err != nil {
		return AutoCreateStatus{}, err
	}

	status := AutoCreateStatus{
		Running:            true,
		Phase:              phaseRunning,
		AccountID:          accountIDs[0],
		Target:             intOr(req.Target, defaultAutoTarget),
		IntervalSeconds:    intOr(req.IntervalSeconds, int(defaultAutoInterval.Seconds())),
		IntervalMaxSeconds: intOr(req.IntervalMaxSeconds, int(defaultAutoIntervalMax.Seconds())),
		CooldownSeconds:    intOr(req.CooldownSeconds, int(defaultAutoCooldown.Seconds())),
		LabelPrefix:        defaultAutoLabelPrefix,
		MaxFailures:        intOr(req.MaxFailures, defaultAutoMaxFailures),
		MaxParallel:        intOr(req.MaxParallel, defaultAutoMaxParallel),
		StartedAt:          time.Now().Format(time.RFC3339),
		Logs:               []AutoCreateLog{},
	}
	if req.LabelPrefix != nil {
		status.LabelPrefix = strings.TrimSpace(*req.LabelPrefix)
	}
	// 只给了最小间隔时,上限自动跟随:老的单参数调用(interval_seconds=3600)
	// 会退化成固定间隔,而不是因为上限小于下限被拒。
	if req.IntervalMaxSeconds == nil && status.IntervalMaxSeconds < status.IntervalSeconds {
		status.IntervalMaxSeconds = status.IntervalSeconds
	}

	if err := validateAutoCreateParams(&status); err != nil {
		return AutoCreateStatus{}, err
	}

	names := make(map[string]string, len(accountIDs))
	for _, summary := range s.be.ListAccounts() {
		names[summary.ID] = summary.Name
	}
	status.Accounts = make([]AutoCreateAccountStatus, 0, len(accountIDs))
	for _, id := range accountIDs {
		status.Accounts = append(status.Accounts, AutoCreateAccountStatus{
			AccountID: id,
			Name:      names[id],
			Phase:     phaseRunning,
			Target:    status.Target,
			Remaining: status.Target,
		})
	}

	s.autoMu.Lock()
	if s.auto != nil && s.auto.status.Running {
		s.autoMu.Unlock()
		return AutoCreateStatus{}, ErrAutoCreateRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.auto = &autoCreateTask{cancel: cancel, status: status}
	s.autoMu.Unlock()

	s.appendAutoLog("info", "任务启动:%d 个账号,目标总数各 %d,创建后随机等待 %d-%ds,冷却 %ds,并发上限 %d",
		len(accountIDs), status.Target, status.IntervalSeconds, status.IntervalMaxSeconds, status.CooldownSeconds, status.MaxParallel)

	// 初始配额检查:并发拉一次别名列表,得到每个账号的起点与剩余空间
	s.checkInitialCapacity(accountIDs, names)

	// 已达目标的账号不进入循环
	pending := make([]AutoCreateAccountStatus, 0, len(accountIDs))
	for _, account := range s.autoStatusSnapshot().Accounts {
		if account.Remaining <= 0 {
			s.markAccountDone(account.AccountID, phaseCompleted, fmt.Sprintf("已有别名 %d 个,已达到目标 %d", account.Total, account.Target))
			s.appendAutoLogFor(account.Name, "info", "跳过:已有 %d 个别名,已达到目标", account.Total)
			continue
		}
		pending = append(pending, account)
	}

	if len(pending) == 0 {
		s.finishAutoCreateIfDone()
		s.appendAutoLog("info", "任务完成:所有账号均已达到目标,无需创建")
		return s.autoStatusSnapshot(), nil
	}

	minInterval := time.Duration(status.IntervalSeconds) * time.Second
	maxInterval := time.Duration(status.IntervalMaxSeconds) * time.Second
	cooldown := time.Duration(status.CooldownSeconds) * time.Second
	sem := make(chan struct{}, status.MaxParallel)

	var wg sync.WaitGroup
	for _, account := range pending {
		account := account
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s.runAutoCreateAccount(ctx, account.AccountID, account.Name, minInterval, maxInterval, cooldown)
		}()
	}
	go func() {
		wg.Wait()
		s.finishAutoCreateIfDone()
	}()

	return s.autoStatusSnapshot(), nil
}

// normalizeAutoAccounts 合并 account_id / account_ids,去重并校验存在性。
func (s *Server) normalizeAutoAccounts(req autoCreateStartRequest) ([]string, error) {
	candidates := make([]string, 0, len(req.AccountIDs)+1)
	if id := strings.TrimSpace(req.AccountID); id != "" {
		candidates = append(candidates, id)
	}
	for _, id := range req.AccountIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			candidates = append(candidates, trimmed)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("参数错误: 至少要选择一个账号(account_id 或 account_ids)")
	}

	known := make(map[string]bool)
	for _, summary := range s.be.ListAccounts() {
		known[summary.ID] = true
	}

	seen := make(map[string]bool, len(candidates))
	ids := make([]string, 0, len(candidates))
	missing := make([]string, 0)
	for _, id := range candidates {
		if seen[id] {
			continue
		}
		seen[id] = true
		if !known[id] {
			missing = append(missing, id)
			continue
		}
		ids = append(ids, id)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("账号不存在: %s", strings.Join(missing, ", "))
	}
	if len(ids) > maxAutoAccounts {
		return nil, fmt.Errorf("参数错误: 一次最多选择 %d 个账号", maxAutoAccounts)
	}
	return ids, nil
}

func validateAutoCreateParams(status *AutoCreateStatus) error {
	switch {
	case status.Target < minAutoTarget || status.Target > maxAutoTarget:
		return fmt.Errorf("参数错误: target 需为 %d-%d", minAutoTarget, maxAutoTarget)
	case status.IntervalSeconds < minAutoIntervalSec || status.IntervalSeconds > maxAutoIntervalSec:
		return fmt.Errorf("参数错误: interval_seconds 需为 %d-%d", minAutoIntervalSec, maxAutoIntervalSec)
	case status.IntervalMaxSeconds < status.IntervalSeconds || status.IntervalMaxSeconds > maxAutoIntervalSec:
		return fmt.Errorf("参数错误: interval_max_seconds 需不小于 interval_seconds 且不超过 %d", maxAutoIntervalSec)
	case status.CooldownSeconds < minAutoCooldownSec || status.CooldownSeconds > maxAutoCooldownSec:
		return fmt.Errorf("参数错误: cooldown_seconds 需为 %d-%d", minAutoCooldownSec, maxAutoCooldownSec)
	case status.MaxFailures < minAutoMaxFailures || status.MaxFailures > maxAutoMaxFailures:
		return fmt.Errorf("参数错误: max_failures 需为 %d-%d", minAutoMaxFailures, maxAutoMaxFailures)
	case status.MaxParallel < 1 || status.MaxParallel > maxAutoAccounts:
		return fmt.Errorf("参数错误: max_parallel 需为 1-%d", maxAutoAccounts)
	case len([]rune(status.LabelPrefix)) > maxAutoLabelRunes:
		return fmt.Errorf("参数错误: label_prefix 最长 %d 字符", maxAutoLabelRunes)
	case status.LabelPrefix == "":
		status.LabelPrefix = defaultAutoLabelPrefix
	}
	return nil
}

// checkInitialCapacity 并发检查每个账号的当前别名总数,并写入起始进度。
//
// 单个账号检查失败(如凭据失效)不会中断启动:该账号标记为未检查,
// 由后台循环在第一次尝试时自行暴露问题。
func (s *Server) checkInitialCapacity(accountIDs []string, names map[string]string) {
	type result struct {
		id    string
		total int
		err   error
	}
	results := make(chan result, len(accountIDs))
	for _, id := range accountIDs {
		go func(id string) {
			total, err := s.recalibrate(id)
			results <- result{id: id, total: total, err: err}
		}(id)
	}

	deadline := time.After(initialCapacityTimeout)
	for collected := 0; collected < len(accountIDs); collected++ {
		select {
		case item := <-results:
			name := names[item.id]
			if item.err != nil {
				s.updateAutoAccount(item.id, func(account *AutoCreateAccountStatus) {
					account.CapacityError = item.err.Error()
					account.Total = 0
					account.Remaining = account.Target
				})
				s.appendAutoLogFor(name, "warn", "配额检查失败,暂以 0 为起点: %v", item.err)
				continue
			}
			remaining := 0
			s.updateAutoAccount(item.id, func(account *AutoCreateAccountStatus) {
				account.CapacityChecked = true
				account.Total = item.total
				account.Remaining = account.Target - item.total
				if account.Remaining < 0 {
					account.Remaining = 0
				}
				remaining = account.Remaining
			})
			s.appendAutoLogFor(name, "info", "配额检查:已有 %d 个,还需创建 %d 个", item.total, remaining)
		case <-deadline:
			s.appendAutoLog("warn", "配额检查超时(%s),未返回的账号由后台循环自行校准", initialCapacityTimeout)
			return
		}
	}
}

// runAutoCreateAccount 是单个账号的创建循环。
//
// 每次尝试结束后按 [minInterval, maxInterval] 随机等待,再进行下一次尝试:
// 固定节拍更容易被上游识别为机器行为,随机间隔也更接近人工操作。
// 首次尝试立即执行,不额外等待。
func (s *Server) runAutoCreateAccount(ctx context.Context, accountID, name string, minInterval, maxInterval, cooldown time.Duration) {
	for {
		created := 0
		s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) { created = account.Created })
		label := fmt.Sprintf("%s-%d", s.autoStatusSnapshot().LabelPrefix, created+1)

		s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) {
			account.NextRunAt = ""
			account.Attempting = true
			account.LastAttemptAt = time.Now().Format(time.RFC3339)
		})

		result, err := s.be.CreateAlias(accountID, label)
		s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) { account.Attempting = false })

		// 防御:实现方返回 (nil, nil) 时按失败处理,避免后台循环 panic 拖垮整个服务
		if err == nil && result == nil {
			err = fmt.Errorf("创建别名返回了空结果")
		}

		switch {
		case err == nil:
			reached := false
			s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) {
				account.Created++
				account.Total++
				account.Remaining--
				if account.Remaining < 0 {
					account.Remaining = 0
				}
				account.ConsecutiveFailures = 0
				account.LastEmail = result.Email
				account.LastError = ""
				reached = account.Total >= account.Target
			})
			total := 0
			s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) { total = account.Total })
			s.appendAutoLogFor(name, "success", "创建成功 %s(总计 %d/%d)", result.Email, total, s.autoStatusSnapshot().Target)

			if reached {
				s.markAccountDone(accountID, phaseCompleted, fmt.Sprintf("已达到目标总数 %d", s.autoStatusSnapshot().Target))
				s.appendAutoLogFor(name, "info", "完成:已达到目标总数")
				return
			}

		case hme.IsQuotaExhausted(err):
			// 配额已满:该账号到此为止,其它账号继续
			s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) { account.Failed++ })
			s.markAccountDone(accountID, phaseCompleted, "iCloud 报告别名总量已达上限")
			s.appendAutoLogFor(name, "info", "完成:iCloud 报告配额已满 — %v", err)
			return

		case hme.IsThrottled(err):
			s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) {
				account.Failed++
				account.Phase = phaseCooling
				account.LastError = err.Error()
				account.NextRunAt = time.Now().Add(cooldown).Format(time.RFC3339)
			})
			s.appendAutoLogFor(name, "warn", "命中速率限制,冷却 %s 后继续 — %v", cooldown, err)

			if !s.accountSleep(ctx, accountID, cooldown) {
				return
			}
			s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) { account.Phase = phaseRunning })

			if refreshed, err := s.recalibrate(accountID); err == nil {
				done := false
				s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) {
					account.Total = refreshed
					account.Remaining = account.Target - refreshed
					if account.Remaining < 0 {
						account.Remaining = 0
					}
					done = account.Total >= account.Target
				})
				s.appendAutoLogFor(name, "info", "冷却结束,重新校准总数: %d", refreshed)
				if done {
					s.markAccountDone(accountID, phaseCompleted, fmt.Sprintf("冷却期间已达到目标 %d", s.autoStatusSnapshot().Target))
					return
				}
			} else {
				s.appendAutoLogFor(name, "warn", "冷却结束但校准失败,继续按本地计数: %v", err)
			}

		default:
			failures := 0
			s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) {
				account.Failed++
				account.ConsecutiveFailures++
				account.LastError = err.Error()
				failures = account.ConsecutiveFailures
			})
			s.appendAutoLogFor(name, "error", "创建失败(%d/%d): %v", failures, s.autoStatusSnapshot().MaxFailures, err)

			if failures >= s.autoStatusSnapshot().MaxFailures {
				s.markAccountDone(accountID, phaseStopped, fmt.Sprintf("连续失败 %d 次,已停止(请检查账号状态)", failures))
				s.appendAutoLogFor(name, "error", "连续失败达到上限,该账号停止")
				return
			}
		}

		// 本次尝试结束:随机等待后再进行下一次
		wait := s.autoJitterOr()(minInterval, maxInterval)
		s.appendAutoLogFor(name, "info", "等待 %s 后继续", wait.Round(time.Second))
		if !s.accountSleep(ctx, accountID, wait) {
			return
		}
	}
}

// markAccountDone 结束某个账号的循环。
func (s *Server) markAccountDone(accountID, phase, reason string) {
	s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) {
		account.Phase = phase
		account.NextRunAt = ""
		account.Attempting = false
		if reason != "" {
			account.Reason = reason
		}
	})
}

// finishAutoCreateIfDone 在所有账号结束后收尾任务。
func (s *Server) finishAutoCreateIfDone() {
	status := s.autoStatusSnapshot()
	if status.Running {
		return
	}
	s.updateAuto(func(current *AutoCreateStatus) {
		current.Running = false
		if current.Reason == "" {
			current.Reason = fmt.Sprintf("任务结束,共创建 %d 个别名", current.Created)
		}
	})
	s.appendAutoLog("info", "任务结束:%s", s.autoStatusSnapshot().Reason)
}

// stopAutoCreate 停止正在运行的任务(所有账号一起停)。
func (s *Server) stopAutoCreate() AutoCreateStatus {
	s.autoMu.Lock()
	if s.auto == nil {
		s.autoMu.Unlock()
		return AutoCreateStatus{Phase: phaseIdle, Accounts: []AutoCreateAccountStatus{}, Logs: []AutoCreateLog{}}
	}
	task := s.auto
	s.autoMu.Unlock()

	task.cancel()
	s.updateAuto(func(status *AutoCreateStatus) {
		status.Running = false
		status.Phase = phaseStopped
		status.NextRunAt = ""
		if status.Reason == "" {
			status.Reason = "已手动停止"
		}
		for i := range status.Accounts {
			if status.Accounts[i].Phase == phaseRunning || status.Accounts[i].Phase == phaseCooling {
				status.Accounts[i].Phase = phaseStopped
				if status.Accounts[i].Reason == "" {
					status.Accounts[i].Reason = "已手动停止"
				}
			}
			status.Accounts[i].NextRunAt = ""
			status.Accounts[i].Attempting = false
		}
	})
	s.appendAutoLog("info", "任务已手动停止(已创建 %d 个)", s.autoStatusSnapshot().Created)
	return s.autoStatusSnapshot()
}

// recalibrate 重新读取别名总数。
func (s *Server) recalibrate(accountID string) (int, error) {
	aliases, err := s.be.ListAliases(accountID)
	if err != nil {
		return 0, err
	}
	return len(aliases), nil
}

// accountSleep 可被取消的等待,返回 false 表示任务已被取消。
//
// 等待期间维护该账号的 NextRunAt,便于前端展示下一次执行时间。
func (s *Server) accountSleep(ctx context.Context, accountID string, d time.Duration) bool {
	if s.autoSleep != nil {
		return s.autoSleep(ctx, d)
	}
	deadline := time.Now().Add(d)
	s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) {
		account.NextRunAt = deadline.Format(time.RFC3339)
	})
	defer s.updateAutoAccount(accountID, func(account *AutoCreateAccountStatus) { account.NextRunAt = "" })

	for {
		if !time.Now().Before(deadline) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(autoMinSleepChunk):
		}
	}
}

// autoJitterOr 返回生效的随机间隔发生器(测试可注入固定值)。
func (s *Server) autoJitterOr() func(min, max time.Duration) time.Duration {
	if s.autoJitter != nil {
		return s.autoJitter
	}
	return defaultAutoJitter
}

// defaultAutoJitter 在 [min, max] 闭区间内等概率取一个时长。
//
// 上下限都是整秒时按整秒取值(符合"随机 20-40 秒"的直觉,闭区间两端都能取到),
// 否则退化为按纳秒等概率取值。
func defaultAutoJitter(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	if min%time.Second == 0 && max%time.Second == 0 {
		minSeconds := int64(min / time.Second)
		maxSeconds := int64(max / time.Second)
		return time.Duration(minSeconds+rand.Int64N(maxSeconds-minSeconds+1)) * time.Second
	}
	span := int64(max - min)
	return min + time.Duration(rand.Int64N(span+1))
}

func intOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

// startAutoCreateHandler 处理 POST /api/autocreate/start。
func (s *Server) startAutoCreateHandler(c *gin.Context) {
	var req autoCreateStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误: 请求体必须是 JSON")
		return
	}

	status, err := s.startAutoCreate(req)
	if err != nil {
		if errors.Is(err, ErrAutoCreateRunning) {
			failCode(c, http.StatusConflict, "TASK_RUNNING", err.Error())
			return
		}
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, status)
}

// stopAutoCreateHandler 处理 POST /api/autocreate/stop。
func (s *Server) stopAutoCreateHandler(c *gin.Context) {
	ok(c, s.stopAutoCreate())
}

// autoCreateStatusHandler 处理 GET /api/autocreate。
func (s *Server) autoCreateStatusHandler(c *gin.Context) {
	ok(c, s.autoStatusSnapshot())
}
