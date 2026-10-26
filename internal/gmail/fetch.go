package gmail

import (
	"errors"
	"fmt"
	"strings"

	"icloud-hme/internal/mail"
)

const (
	// IMAPServer 是 Gmail 的 IMAP 服务器。
	IMAPServer = "imap.gmail.com"
	// IMAPPort 是 IMAP over TLS 端口。
	IMAPPort = 993

	// maxFetchLimit 是单次拉取的上限，避免一次读取过多邮件。
	maxFetchLimit = 100
	// fetchFactor 是过滤前的放大倍数：过滤会丢掉部分邮件，多取一些再截断。
	fetchFactor = 3
)

var (
	// ErrNoAppPassword 表示账号未配置应用专用密码，无法登录 IMAP。
	ErrNoAppPassword = errors.New("该账号未配置应用专用密码，无法取件")
	// ErrFiltered 表示邮件未命中管理员的取件过滤条件。
	ErrFiltered = errors.New("邮件不在可见范围内")
)

// Match 判断一封邮件摘要是否对取件方可见。
//
// 关键词匹配「主题或发件人」，忽略大小写；Exclude 优先级高于 Include。
func (f Filter) Match(m mail.Message) bool {
	haystack := strings.ToLower(m.Subject + "\n" + m.From)

	for _, word := range f.Exclude {
		word = strings.ToLower(strings.TrimSpace(word))
		if word != "" && strings.Contains(haystack, word) {
			return false
		}
	}
	if len(f.Include) == 0 {
		return true
	}
	for _, word := range f.Include {
		word = strings.ToLower(strings.TrimSpace(word))
		if word != "" && strings.Contains(haystack, word) {
			return true
		}
	}
	return false
}

// MatchMessage 判断一封完整邮件是否可见（只用到主题与发件人）。
func (f Filter) MatchMessage(m *mail.FullMessage) bool {
	if m == nil {
		return false
	}
	return f.Match(m.Message)
}

// effectiveDays 返回实际生效的时间窗口：管理员配置的天数会作为上限。
func effectiveDays(filter Filter, requested int) int {
	if filter.Days > 0 && (requested <= 0 || filter.Days < requested) {
		return filter.Days
	}
	return requested
}

// Inbox 拉取指定账号的收件箱，并应用管理员的过滤条件。
//
// 返回的邮件数不超过 limit；method 固定为 imap，便于前端统一展示。
func (s *Store) Inbox(id string, limit, days int) ([]mail.Message, string, error) {
	acc, ok := s.Get(id)
	if !ok {
		return nil, "", fmt.Errorf("账号不存在")
	}
	if acc.AppPassword == "" {
		return nil, "", ErrNoAppPassword
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > maxFetchLimit {
		limit = maxFetchLimit
	}
	days = effectiveDays(acc.Filter, days)

	fetch := limit * fetchFactor
	if fetch > maxFetchLimit {
		fetch = maxFetchLimit
	}

	cli := mail.NewClientWithServer(acc.Email, acc.AppPassword, IMAPServer, IMAPPort)
	if err := cli.Connect(); err != nil {
		s.MarkChecked(id, err.Error())
		return nil, "", fmt.Errorf("连接 Gmail 失败: %w", err)
	}
	defer cli.Disconnect()

	messages, err := cli.ListInbox(fetch, days)
	if err != nil {
		s.MarkChecked(id, err.Error())
		return nil, "", fmt.Errorf("读取 Gmail 收件箱失败: %w", err)
	}
	s.MarkChecked(id, "")

	visible := make([]mail.Message, 0, limit)
	for _, message := range messages {
		if !acc.Filter.Match(message) {
			continue
		}
		visible = append(visible, message)
		if len(visible) >= limit {
			break
		}
	}
	return visible, "imap", nil
}

// Message 读取指定账号的一封邮件，并校验是否在过滤范围内。
func (s *Store) Message(id string, uid uint32, opts mail.MessageOptions) (*mail.FullMessage, error) {
	acc, ok := s.Get(id)
	if !ok {
		return nil, fmt.Errorf("账号不存在")
	}
	if acc.AppPassword == "" {
		return nil, ErrNoAppPassword
	}

	cli := mail.NewClientWithServer(acc.Email, acc.AppPassword, IMAPServer, IMAPPort)
	if err := cli.Connect(); err != nil {
		s.MarkChecked(id, err.Error())
		return nil, fmt.Errorf("连接 Gmail 失败: %w", err)
	}
	defer cli.Disconnect()

	message, err := cli.GetFull(uid, opts)
	if err != nil {
		return nil, err
	}
	// IMAP UID 是账号级全局编号，必须逐封核对过滤条件，
	// 否则拿到取件链接的人可以猜 UID 读到范围外的邮件。
	if !acc.Filter.MatchMessage(message) {
		return nil, ErrFiltered
	}
	return message, nil
}

// Check 用新会话验证应用专用密码是否可用。
func (s *Store) Check(id string) error {
	acc, ok := s.Get(id)
	if !ok {
		return fmt.Errorf("账号不存在")
	}
	if acc.AppPassword == "" {
		return ErrNoAppPassword
	}
	if err := mail.CheckConnection(acc.Email, acc.AppPassword, IMAPServer, IMAPPort); err != nil {
		s.MarkChecked(id, err.Error())
		return fmt.Errorf("Gmail 登录失败: %w", err)
	}
	s.MarkChecked(id, "")
	return nil
}
