package mail

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestPool 构造一个不依赖网络的池,用于验证调度语义:
// dial/check/closeConn 全部替换为内存实现,Client 用零值。
func newTestPool(opts PoolOptions) (*Pool, *int64, *int64) {
	pool := NewPoolWithOptions(opts)
	var dials, closes int64
	pool.dial = func(string, string) (*Client, error) {
		atomic.AddInt64(&dials, 1)
		return &Client{}, nil
	}
	pool.check = func(*Client) error { return nil }
	pool.closeConn = func(*Client) { atomic.AddInt64(&closes, 1) }
	return pool, &dials, &closes
}

// TestPoolRunsUpToMaxConnsInParallel 是这次改造的核心断言:
// 同一账号的并发调用数应等于配置的连接数上限,而不是被串行化。
func TestPoolRunsUpToMaxConnsInParallel(t *testing.T) {
	const maxConns = 3
	pool, dials, _ := newTestPool(PoolOptions{MaxConns: maxConns, WaitTimeout: 5 * time.Second})

	var current, peak atomic.Int32
	var wg sync.WaitGroup
	const total = 12

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := pool.Do("a@icloud.com", "pw", func(*Client) error {
				running := current.Add(1)
				for {
					observed := peak.Load()
					if running <= observed || peak.CompareAndSwap(observed, running) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				current.Add(-1)
				return nil
			})
			if err != nil {
				t.Errorf("第 %d 个调用失败: %v", index, err)
			}
		}(i)
	}
	wg.Wait()

	if got := peak.Load(); got != maxConns {
		t.Fatalf("并发度应为 %d,实际 %d", maxConns, got)
	}
	if got := atomic.LoadInt64(dials); got != maxConns {
		t.Fatalf("12 次调用只应建立 %d 条连接(其余复用),实际 %d", maxConns, got)
	}
}

// TestPoolReusesIdleConnection 串行调用应复用同一条连接。
func TestPoolReusesIdleConnection(t *testing.T) {
	pool, dials, _ := newTestPool(PoolOptions{MaxConns: 4})

	for i := 0; i < 5; i++ {
		if err := pool.Do("a@icloud.com", "pw", func(*Client) error { return nil }); err != nil {
			t.Fatalf("第 %d 次调用失败: %v", i, err)
		}
	}
	if got := atomic.LoadInt64(dials); got != 1 {
		t.Fatalf("串行调用应只建 1 条连接,实际 %d", got)
	}
}

// TestPoolQueuesWhenFull 池满时排队等待,而不是直接失败。
func TestPoolQueuesWhenFull(t *testing.T) {
	pool, _, _ := newTestPool(PoolOptions{MaxConns: 1, WaitTimeout: 5 * time.Second})

	release := make(chan struct{})
	first := make(chan struct{})
	go func() {
		_ = pool.Do("a@icloud.com", "pw", func(*Client) error {
			close(first)
			<-release
			return nil
		})
	}()
	<-first

	done := make(chan error, 1)
	go func() {
		done <- pool.Do("a@icloud.com", "pw", func(*Client) error { return nil })
	}()

	// 第二个调用此时应在排队,而不是已经失败
	select {
	case err := <-done:
		t.Fatalf("池满时应排队等待,实际提前返回: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("排队后应成功执行,实际: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("排队调用未在连接释放后继续")
	}
}

// TestPoolWaitTimeout 池满且超时返回明确错误。
func TestPoolWaitTimeout(t *testing.T) {
	pool, _, _ := newTestPool(PoolOptions{MaxConns: 1, WaitTimeout: 60 * time.Millisecond})

	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_ = pool.Do("a@icloud.com", "pw", func(*Client) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	err := pool.Do("a@icloud.com", "pw", func(*Client) error { return nil })
	if err == nil {
		t.Fatal("等待超时应返回错误")
	}
	if !containsFold(err.Error(), "繁忙") {
		t.Fatalf("超时错误应说明池繁忙,实际: %v", err)
	}
	close(release)
}

// TestPoolRetriesWithFreshConnection 复用连接报连接类错误时,应换新连接重试一次。
func TestPoolRetriesWithFreshConnection(t *testing.T) {
	pool, dials, closes := newTestPool(PoolOptions{MaxConns: 2})

	// 预热一条空闲连接
	if err := pool.Do("a@icloud.com", "pw", func(*Client) error { return nil }); err != nil {
		t.Fatalf("预热失败: %v", err)
	}

	calls := 0
	err := pool.Do("a@icloud.com", "pw", func(*Client) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("connection reset by peer")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("重试后应成功,实际: %v", err)
	}
	if calls != 2 {
		t.Fatalf("应恰好执行两次(失败一次 + 重试一次),实际 %d", calls)
	}
	if got := atomic.LoadInt64(dials); got != 2 {
		t.Fatalf("坏连接被丢弃后应新建连接,期望 2 条,实际 %d", got)
	}
	if got := atomic.LoadInt64(closes); got != 1 {
		t.Fatalf("应关闭 1 条坏连接,实际 %d", got)
	}
}

// TestPoolDoesNotRetryFreshConnectionFailure 新建连接就失败时不重试(凭据/网络问题重试无意义)。
func TestPoolDoesNotRetryFreshConnectionFailure(t *testing.T) {
	pool, dials, _ := newTestPool(PoolOptions{MaxConns: 2})

	calls := 0
	err := pool.Do("a@icloud.com", "pw", func(*Client) error {
		calls++
		return fmt.Errorf("connection reset by peer")
	})
	if err == nil {
		t.Fatal("应返回错误")
	}
	if calls != 1 {
		t.Fatalf("新连接失败不应重试,实际执行 %d 次", calls)
	}
	if got := atomic.LoadInt64(dials); got != 1 {
		t.Fatalf("应只建立 1 条连接,实际 %d", got)
	}

	// 名额必须已释放:下一次调用仍能新建连接
	if err := pool.Do("a@icloud.com", "pw", func(*Client) error { return nil }); err != nil {
		t.Fatalf("失败后名额未释放,后续调用无法建连: %v", err)
	}
}

// TestPoolDialFailureReleasesSlot dial 失败也应释放名额。
func TestPoolDialFailureReleasesSlot(t *testing.T) {
	pool, _, _ := newTestPool(PoolOptions{MaxConns: 1})

	failing := true
	pool.dial = func(string, string) (*Client, error) {
		if failing {
			return nil, fmt.Errorf("dial tcp: i/o timeout")
		}
		return &Client{}, nil
	}

	if err := pool.Do("a@icloud.com", "pw", func(*Client) error { return nil }); err == nil {
		t.Fatal("建连失败应返回错误")
	}
	failing = false
	if err := pool.Do("a@icloud.com", "pw", func(*Client) error { return nil }); err != nil {
		t.Fatalf("建连失败后名额应已释放,实际: %v", err)
	}
}

// TestPoolDropsIdleOnPasswordChange 凭据变更后不复用旧连接。
func TestPoolDropsIdleOnPasswordChange(t *testing.T) {
	pool, dials, closes := newTestPool(PoolOptions{MaxConns: 2})

	if err := pool.Do("a@icloud.com", "old-pw", func(*Client) error { return nil }); err != nil {
		t.Fatalf("首次调用失败: %v", err)
	}
	if err := pool.Do("a@icloud.com", "new-pw", func(*Client) error { return nil }); err != nil {
		t.Fatalf("换凭据后调用失败: %v", err)
	}

	if got := atomic.LoadInt64(closes); got != 1 {
		t.Fatalf("旧密码的空闲连接应被关闭,实际关闭 %d 条", got)
	}
	if got := atomic.LoadInt64(dials); got != 2 {
		t.Fatalf("换凭据后应重新建连,期望 2 条,实际 %d", got)
	}
}

// TestPoolAccountsAreIndependent 一个账号占满不影响其它账号。
func TestPoolAccountsAreIndependent(t *testing.T) {
	pool, _, _ := newTestPool(PoolOptions{MaxConns: 1, WaitTimeout: 2 * time.Second})

	busy := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = pool.Do("a@icloud.com", "pw", func(*Client) error {
			close(busy)
			<-release
			return nil
		})
	}()
	<-busy

	done := make(chan error, 1)
	go func() {
		done <- pool.Do("b@icloud.com", "pw", func(*Client) error { return nil })
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("另一账号不应被阻塞,实际: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("另一账号的调用被错误地串行化了")
	}
	close(release)
}

func TestPoolRejectsEmptyCredentials(t *testing.T) {
	pool, _, _ := newTestPool(PoolOptions{})
	if err := pool.Do("", "pw", func(*Client) error { return nil }); err == nil {
		t.Fatal("空账号应报错")
	}
	if err := pool.Do("a@icloud.com", "", func(*Client) error { return nil }); err == nil {
		t.Fatal("空密码应报错")
	}
}

func TestPoolDefaultMaxConnsIsTen(t *testing.T) {
	if got := NewPool().MaxConns(); got != 10 {
		t.Fatalf("默认每账号连接数应为 10,实际 %d", got)
	}
	if got := NewPoolWithOptions(PoolOptions{MaxConns: 0}).MaxConns(); got != 10 {
		t.Fatalf("未指定时也应回落 10,实际 %d", got)
	}
	if got := NewPoolWithOptions(PoolOptions{MaxConns: 25}).MaxConns(); got != 25 {
		t.Fatalf("指定值应生效,实际 %d", got)
	}
}

func TestPoolCloseClosesIdleConnections(t *testing.T) {
	pool, _, closes := newTestPool(PoolOptions{MaxConns: 3})
	for i := 0; i < 3; i++ {
		if err := pool.Do("a@icloud.com", "pw", func(*Client) error { return nil }); err != nil {
			t.Fatalf("调用失败: %v", err)
		}
	}
	pool.Close()
	if got := atomic.LoadInt64(closes); got != 1 {
		t.Fatalf("串行调用只建了一条连接,Close 应关闭它,实际关闭 %d 条", got)
	}
}
