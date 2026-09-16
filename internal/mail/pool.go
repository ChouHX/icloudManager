// IMAP 连接池: 按账号维护多条长连接, 避免每次读信都 TLS+Login。
//
// 同一个 iCloud 账号允许最多 maxConns 条并发连接: 多个请求可以真正并行,
// 超出上限的请求排队等待空闲连接(而不是失败)。不同账号互不影响。
//
// go-imap 的 Client 不是并发安全的, 因此每条连接在任一时刻只被一个调用方持有;
// 并发能力由"连接条数"提供, 而不是共享单条连接。
package mail

import (
	"fmt"
	"sync"
	"time"
)

// defaultMaxConns 是每账号默认的最大并发连接数。
const defaultMaxConns = 10

// defaultWaitTimeout 是池已满时, 等待空闲连接的最长时间。
const defaultWaitTimeout = 60 * time.Second

// PoolOptions 是连接池参数, 零值表示使用默认值。
type PoolOptions struct {
	// MaxConns 每账号最大并发连接数(默认 10)。
	MaxConns int
	// IdleClose 空闲超过该时长则下次使用前重建(默认 10 分钟)。
	IdleClose time.Duration
	// WaitTimeout 池满时等待空闲连接的超时(默认 60 秒)。
	WaitTimeout time.Duration
}

// Pool 管理按账号复用的 IMAP 长连接。
type Pool struct {
	mu       sync.Mutex
	accounts map[string]*accountPool
	opts     PoolOptions

	// 以下三处可注入,便于在无网络环境下测试池的调度语义
	dial      func(appleID, appPassword string) (*Client, error)
	check     func(*Client) error
	closeConn func(*Client)
}

type accountPool struct {
	appleID     string
	appPassword string

	mu     sync.Mutex
	idle   []*pooledConn
	total  int // 已创建连接数(含借出中的)
	closed bool

	// notify 用于在连接归还/释放名额时唤醒等待者(容量 1 的信号量语义)
	notify chan struct{}
}

type pooledConn struct {
	client   *Client
	lastUsed time.Time
}

// NewPool 创建连接池,每账号默认最多 10 条连接。
func NewPool() *Pool {
	return NewPoolWithOptions(PoolOptions{})
}

// NewPoolWithOptions 创建连接池;未指定的字段使用默认值。
func NewPoolWithOptions(opts PoolOptions) *Pool {
	if opts.MaxConns <= 0 {
		opts.MaxConns = defaultMaxConns
	}
	if opts.IdleClose < 0 {
		opts.IdleClose = 0
	} else if opts.IdleClose == 0 {
		opts.IdleClose = 10 * time.Minute
	}
	if opts.WaitTimeout <= 0 {
		opts.WaitTimeout = defaultWaitTimeout
	}

	return &Pool{
		accounts: make(map[string]*accountPool),
		opts:     opts,
		dial: func(appleID, appPassword string) (*Client, error) {
			client := NewClient(appleID, appPassword)
			if err := client.Connect(); err != nil {
				return nil, err
			}
			return client, nil
		},
		check: func(client *Client) error { return client.Ping() },
		closeConn: func(client *Client) {
			client.forceClose()
		},
	}
}

// MaxConns 返回每账号最大并发连接数。
func (p *Pool) MaxConns() int { return p.opts.MaxConns }

// Do 借出一条已连接的 Client 执行 fn; 用完连接留在池中复用。
//
// 池内无空闲连接且未达上限时会新建连接; 已达上限则排队等待,
// 等待超过 WaitTimeout 返回错误而不是无限阻塞。
func (p *Pool) Do(appleID, appPassword string, fn func(*Client) error) error {
	if appleID == "" || appPassword == "" {
		return fmt.Errorf("IMAP 凭据为空")
	}
	account := p.accountPool(appleID, appPassword)

	// 复用连接失败时(服务端静默断开等)丢弃并重试一次,用新连接完成本次调用
	for attempt := 0; attempt < 2; attempt++ {
		conn, fresh, err := p.acquire(account)
		if err != nil {
			return err
		}

		err = fn(conn.client)
		if err == nil {
			p.release(account, conn)
			return nil
		}

		if isLikelyConnErr(err) {
			p.discard(account, conn)
			if !fresh {
				continue // 复用连接坏了,换一条新的重试
			}
			return err
		}

		p.release(account, conn)
		return err
	}
	return fmt.Errorf("IMAP 连接不可用")
}

// Close 关闭池内全部连接。
func (p *Pool) Close() {
	p.mu.Lock()
	pools := make([]*accountPool, 0, len(p.accounts))
	for _, account := range p.accounts {
		pools = append(pools, account)
	}
	p.accounts = make(map[string]*accountPool)
	p.mu.Unlock()

	for _, account := range pools {
		account.closeAll(p)
	}
}

// ---------------------------------------------------------------------------
// 内部实现
// ---------------------------------------------------------------------------

func (p *Pool) accountPool(appleID, appPassword string) *accountPool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if account, ok := p.accounts[appleID]; ok {
		if account.appPassword != appPassword {
			// 凭据变更:丢弃空闲连接,借出中的连接归还后自然淘汰
			account.appPassword = appPassword
			account.dropIdle(p)
		}
		return account
	}
	account := &accountPool{
		appleID:     appleID,
		appPassword: appPassword,
		notify:      make(chan struct{}, 1),
	}
	p.accounts[appleID] = account
	return account
}

// acquire 借出一条连接。fresh 表示这是本次新建的连接。
func (p *Pool) acquire(account *accountPool) (conn *pooledConn, fresh bool, err error) {
	deadline := time.Now().Add(p.opts.WaitTimeout)

	for {
		account.mu.Lock()
		if account.closed {
			account.mu.Unlock()
			return nil, false, fmt.Errorf("IMAP 连接池已关闭")
		}

		if n := len(account.idle); n > 0 {
			conn := account.idle[n-1]
			account.idle = account.idle[:n-1]
			account.mu.Unlock()

			if p.opts.IdleClose > 0 && !conn.lastUsed.IsZero() && time.Since(conn.lastUsed) > p.opts.IdleClose {
				p.discard(account, conn)
				continue
			}
			if err := p.check(conn.client); err != nil {
				p.discard(account, conn)
				continue
			}
			return conn, false, nil
		}

		if account.total < p.opts.MaxConns {
			account.total++ // 先占名额,再在锁外建连(避免持锁做网络 IO)
			account.mu.Unlock()

			client, err := p.dial(account.appleID, account.appPassword)
			if err != nil {
				account.mu.Lock()
				account.total--
				account.mu.Unlock()
				account.wake()
				return nil, true, err
			}
			return &pooledConn{client: client, lastUsed: time.Now()}, true, nil
		}

		account.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, false, fmt.Errorf("IMAP 连接池繁忙:该账号已有 %d 条连接在忙,请稍后重试", p.opts.MaxConns)
		}
		if remaining > 200*time.Millisecond {
			remaining = 200 * time.Millisecond
		}
		select {
		case <-account.notify:
		case <-time.After(remaining):
		}
	}
}

// release 归还连接。
func (p *Pool) release(account *accountPool, conn *pooledConn) {
	conn.lastUsed = time.Now()

	account.mu.Lock()
	if account.closed {
		account.mu.Unlock()
		p.closeConn(conn.client)
		return
	}
	account.idle = append(account.idle, conn)
	account.mu.Unlock()
	account.wake()
}

// discard 丢弃不可用连接并释放名额。
func (p *Pool) discard(account *accountPool, conn *pooledConn) {
	p.closeConn(conn.client)

	account.mu.Lock()
	if account.total > 0 {
		account.total--
	}
	account.mu.Unlock()
	account.wake()
}

// wake 唤醒一个等待中的调用方(非阻塞)。
func (account *accountPool) wake() {
	select {
	case account.notify <- struct{}{}:
	default:
	}
}

// dropIdle 关闭并丢弃全部空闲连接(凭据变更时使用)。
func (account *accountPool) dropIdle(p *Pool) {
	account.mu.Lock()
	idle := account.idle
	account.idle = nil
	account.total -= len(idle)
	if account.total < 0 {
		account.total = 0
	}
	account.mu.Unlock()

	for _, conn := range idle {
		p.closeConn(conn.client)
	}
	account.wake()
}

// closeAll 关闭该账号的全部空闲连接并标记为已关闭。
func (account *accountPool) closeAll(p *Pool) {
	account.mu.Lock()
	idle := account.idle
	account.idle = nil
	account.total -= len(idle)
	if account.total < 0 {
		account.total = 0
	}
	account.closed = true
	account.mu.Unlock()

	for _, conn := range idle {
		p.closeConn(conn.client)
	}
	account.wake()
}

func isLikelyConnErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// 常见断连/IO 错误关键字
	for _, k := range []string{
		"connection reset", "broken pipe", "EOF", "i/o timeout",
		"use of closed", "not connected", "connection refused",
		"IMAP 连接", "wsarecv", "wsasend",
	} {
		if containsFold(s, k) {
			return true
		}
	}
	return false
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		len(sub) == 0 ||
		indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	// 小写 ASCII 子串查找, 够用
	sl := toLowerASCII(s)
	subl := toLowerASCII(sub)
	for i := 0; i+len(subl) <= len(sl); i++ {
		if sl[i:i+len(subl)] == subl {
			return i
		}
	}
	return -1
}

func toLowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
