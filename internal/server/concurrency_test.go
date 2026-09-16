package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"icloud-hme/internal/account"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/mail"
)

// threadSafeBackend 包装 fakeBackend,把并发访问到的字段改成原子计数,
// 这样并发测试暴露的竞态只可能来自服务端代码,而不是测试替身。
type threadSafeBackend struct {
	*fakeBackend
	inboxCalls   int64
	messageCalls int64
	deleteCalls  int64
	aliasCalls   int64
}

func (b *threadSafeBackend) ListInbox(q InboxQuery) (InboxResult, error) {
	atomic.AddInt64(&b.inboxCalls, 1)
	return InboxResult{
		AccountID: q.AccountID,
		Count:     1,
		Messages:  []mail.Message{{ID: "1042", Subject: "并发测试", To: "alpha@icloud.com"}},
		Method:    "imap",
	}, nil
}

func (b *threadSafeBackend) GetMessage(accountID string, uid uint32, _ mail.MessageOptions) (*mail.FullMessage, error) {
	atomic.AddInt64(&b.messageCalls, 1)
	return &mail.FullMessage{
		Message:     mail.Message{ID: "1042", Subject: "并发测试"},
		Body:        "正文",
		ContentType: "text/plain",
	}, nil
}

func (b *threadSafeBackend) DeleteMessage(accountID string, uid uint32) error {
	atomic.AddInt64(&b.deleteCalls, 1)
	return nil
}

func (b *threadSafeBackend) ListAliases(accountID string) ([]hme.Alias, error) {
	atomic.AddInt64(&b.aliasCalls, 1)
	return []hme.Alias{{Email: "a@icloud.com", AnonymousID: "anon_1", Active: true}}, nil
}

func (b *threadSafeBackend) ListAccounts() []account.Summary {
	return []account.Summary{{ID: "acc_1", Name: "主号", Status: "active"}}
}

// TestConcurrentReadRequestsAreSafe 并发打取件相关接口,配合 -race 检测竞态。
func TestConcurrentReadRequestsAreSafe(t *testing.T) {
	backend := &threadSafeBackend{fakeBackend: &fakeBackend{}}
	s := mustServer(t, backend, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	session, _ := login(t, ts, "admin-pass-2026-strong")

	const workers = 12
	paths := []struct {
		method string
		path   string
	}{
		{"GET", "/api/inbox?account_id=acc_1&limit=20&days=7"},
		{"GET", "/api/inbox/1042?account_id=acc_1"},
		{"GET", "/api/aliases?account_id=acc_1"},
		{"GET", "/api/accounts"},
	}

	var wg sync.WaitGroup
	errs := make(chan string, workers*len(paths))
	for _, target := range paths {
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(method, path string) {
				defer wg.Done()
				req := authedReq(t, ts, method, path, "")
				req.AddCookie(&http.Cookie{Name: "hme_session", Value: session})
				status, body, _ := do(t, req)
				if status != http.StatusOK {
					errs <- fmt.Sprintf("%s %s → %d %s", method, path, status, body)
				}
			}(target.method, target.path)
		}
	}
	wg.Wait()
	close(errs)
	for message := range errs {
		t.Fatalf("并发读请求失败: %s", message)
	}

	if got := atomic.LoadInt64(&backend.inboxCalls); got != workers {
		t.Fatalf("收件箱接口调用次数应为 %d,实际 %d", workers, got)
	}
	if got := atomic.LoadInt64(&backend.messageCalls); got != workers {
		t.Fatalf("详情接口调用次数应为 %d,实际 %d", workers, got)
	}
}

// TestConcurrentWriteRequestsAreSafe 并发删除邮件(写操作,带 CSRF)。
func TestConcurrentWriteRequestsAreSafe(t *testing.T) {
	backend := &threadSafeBackend{fakeBackend: &fakeBackend{}}
	s := mustServer(t, backend, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	session, csrf := login(t, ts, "admin-pass-2026-strong")

	const workers = 10
	var wg sync.WaitGroup
	errs := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			req := authedReq(t, ts, "DELETE", fmt.Sprintf("/api/inbox/%d?account_id=acc_1", 1000+index), "")
			req.AddCookie(&http.Cookie{Name: "hme_session", Value: session})
			req.Header.Set("X-CSRF-Token", csrf)
			status, body, _ := do(t, req)
			if status != http.StatusOK {
				errs <- fmt.Sprintf("DELETE #%d → %d %s", index, status, body)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for message := range errs {
		t.Fatalf("并发写请求失败: %s", message)
	}
	if got := atomic.LoadInt64(&backend.deleteCalls); got != workers {
		t.Fatalf("删除接口调用次数应为 %d,实际 %d", workers, got)
	}
}

// TestConcurrentStatusAndTaskStart 并发查询任务状态与启动任务:
// 任务启动必须只成功一次,其余返回 409,且不出现数据竞争。
func TestConcurrentStatusAndTaskStart(t *testing.T) {
	backend := &threadSafeBackend{fakeBackend: &fakeBackend{accounts: autoAccount()}}
	s := mustServer(t, backend, Config{AdminPassword: "admin-pass-2026-strong", SessionTTL: time.Hour})
	// 不注入 sleep:任务保持运行中,才能验证并发启动只成功一次
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	session, csrf := login(t, ts, "admin-pass-2026-strong")

	const workers = 8
	var wg sync.WaitGroup
	var started, rejected int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := authedReq(t, ts, "POST", "/api/autocreate/start", `{"account_ids":["acc_1"],"target":10,"interval_seconds":3600}`)
			req.AddCookie(&http.Cookie{Name: "hme_session", Value: session})
			req.Header.Set("X-CSRF-Token", csrf)
			status, body, _ := do(t, req)
			switch status {
			case http.StatusOK:
				atomic.AddInt64(&started, 1)
			case http.StatusConflict:
				var payload struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal([]byte(body), &payload)
				if payload.Code != "TASK_RUNNING" {
					t.Errorf("冲突响应错误码应为 TASK_RUNNING,得到 %q", payload.Code)
				}
				atomic.AddInt64(&rejected, 1)
			default:
				t.Errorf("意外状态码 %d: %s", status, body)
			}
		}()
	}
	wg.Wait()

	if atomic.LoadInt64(&started) != 1 {
		t.Fatalf("并发启动只应成功一次,实际 %d", started)
	}
	if atomic.LoadInt64(&rejected) != workers-1 {
		t.Fatalf("其余请求应返回 409,实际 %d", rejected)
	}
	s.stopAutoCreate()
}
