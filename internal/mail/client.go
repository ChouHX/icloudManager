// Package mail 实现 iCloud 邮件 IMAP 读取客户端。
//
// 通过 Apple 应用专用密码连接 imap.mail.me.com:993,
// 拉取隐私邮箱别名收到的邮件。对应原 Python 项目 icloud_mail.py。
package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	message "github.com/emersion/go-message"
	"github.com/emersion/go-message/charset"
	esmail "github.com/emersion/go-message/mail"
)

const (
	IMAPServer = "imap.mail.me.com"
	IMAPPort   = 993

	// maxRawMessageBytes 是单封邮件原始报文的读取上限(超出即截断)。
	maxRawMessageBytes = 10 << 20
)

// Message 是一封邮件的摘要信息。
type Message struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Preview string `json:"preview"`
}

// MessageOptions 控制单封邮件返回哪些额外内容。
type MessageOptions struct {
	// Sanitize 额外返回清理后的 HTML(移除 script / 事件属性 / javascript: 协议)。
	// 调用方若要自己渲染,建议用这份;需要原始数据则用 BodyHTML。
	Sanitize bool
	// IncludeRaw 额外返回完整 RFC822 报文(base64),含全部头部与 MIME 部分。
	IncludeRaw bool
}

// FullMessage 是一封邮件的完整内容(含正文)。
//
// Body 始终是可读纯文本(供复制、OTP 提取与无 HTML 时展示);
// BodyHTML 是**未经清理**的原始 HTML —— 服务端不做加工,把处理权交给调用方。
// 需要清理版或完整原始报文时,分别用 MessageOptions.Sanitize / IncludeRaw 索取。
type FullMessage struct {
	Message
	Body string `json:"body"`
	// BodyHTML 邮件自带的原始 HTML(未清理)
	BodyHTML string `json:"body_html,omitempty"`
	// BodyHTMLSanitized 清理后的 HTML,仅在请求 sanitize 时出现
	BodyHTMLSanitized string `json:"body_html_sanitized,omitempty"`
	// RawMessage 完整 RFC822 报文的 base64,仅在请求 raw 时出现
	RawMessage string `json:"raw_message,omitempty"`
	// RawSize 原始报文字节数(截断前)
	RawSize     int    `json:"raw_size,omitempty"`
	ContentType string `json:"content_type"`
}

// Client 是 iCloud 邮件 IMAP 客户端。
type Client struct {
	username string
	password string
	server   string
	port     int
	cli      *client.Client
}

// NewClient 创建 IMAP 客户端。需在调用其它方法前先 Connect。
func NewClient(appleID, appPassword string) *Client {
	return NewClientWithServer(appleID, appPassword, IMAPServer, IMAPPort)
}

// NewClientWithServer creates an IMAP client for a custom server.
func NewClientWithServer(username, password, server string, port int) *Client {
	return &Client{username: username, password: password, server: server, port: port}
}

// Connect 连接并登录 IMAP 服务器。已连接且存活时直接复用。
func (c *Client) Connect() error {
	if c.cli != nil {
		if err := c.cli.Noop(); err == nil {
			return nil
		}
		c.forceClose()
	}
	addr := fmt.Sprintf("%s:%d", c.server, c.port)
	cli, err := client.DialTLS(addr, nil)
	if err != nil {
		return fmt.Errorf("IMAP 连接失败: %w", err)
	}
	if err := cli.Login(c.username, c.password); err != nil {
		_ = cli.Logout()
		return fmt.Errorf("IMAP 登录失败 — 请检查邮箱账号、授权码和服务器地址: %w", err)
	}
	c.cli = cli
	return nil
}

// Ping 探测连接是否仍可用(NOOP)。
func (c *Client) Ping() error {
	if c.cli == nil {
		return fmt.Errorf("未连接")
	}
	return c.cli.Noop()
}

// Disconnect 登出并关闭连接。
func (c *Client) Disconnect() {
	if c.cli != nil {
		_ = c.cli.Logout()
		c.cli = nil
	}
}

// forceClose 不发 LOGOUT, 直接掐断(坏连接/池丢弃时用)。
func (c *Client) forceClose() {
	if c.cli != nil {
		_ = c.cli.Terminate()
		c.cli = nil
	}
}

// InboxCount 返回收件箱邮件总数。
func (c *Client) InboxCount() (int, error) {
	if c.cli == nil {
		return 0, fmt.Errorf("未连接")
	}
	mbox, err := c.cli.Select("INBOX", false)
	if err != nil {
		return 0, err
	}
	return int(mbox.Messages), nil
}

// ListInbox 拉取收件箱最近 limit 封邮件摘要。
//
// days 用于过滤只看近 N 天的邮件(0 表示不限制)。
// 返回按时间倒序排列。
func (c *Client) ListInbox(limit int, days int) ([]Message, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if limit <= 0 {
		limit = 50
	}

	mbox, err := c.cli.Select("INBOX", true)
	if err != nil {
		return nil, err
	}
	total := int(mbox.Messages)
	if total == 0 {
		return []Message{}, nil
	}

	// 计算起始序号(只取最近 limit 封)
	from := uint32(1)
	if uint32(limit) < mbox.Messages {
		from = mbox.Messages - uint32(limit) + 1
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	// 拉取完整正文,以便填充 Preview(OTP 验证码在正文中); PEEK 不标已读
	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{
		imap.FetchUid,
		imap.FetchEnvelope,
		imap.FetchInternalDate,
		section.FetchItem(),
	}

	messages := make(chan *imap.Message, limit)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.Fetch(seqset, items, messages)
	}()

	var out []Message
	for msg := range messages {
		m := toMessageWithBody(msg)
		// days 过滤:只保留近 days 天内的邮件。
		// 时间解析失败时保留,避免因个别邮件格式异常而整批丢失。
		if days > 0 {
			if t, err := parseMessageDate(m.Date); err == nil {
				if time.Since(t) > time.Duration(days)*24*time.Hour {
					continue
				}
			}
		}
		out = append(out, m)
	}
	if err := <-done; err != nil {
		return nil, err
	}
	sortMessagesDesc(out)
	return out, nil
}

// parseMessageDate 解析邮件时间。
//
// toMessage 把 Envelope 时间格式化为 RFC3339;部分服务端也可能给出
// RFC1123Z/RFC1123,这里一并兼容。此前只按 RFC1123Z 解析会导致
// days 过滤被静默跳过,因此两种格式都必须支持。
func parseMessageDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("邮件时间为空")
	}
	for _, layout := range []string{time.RFC3339, time.RFC1123Z, time.RFC1123} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析邮件时间: %q", raw)
}

// sortMessagesDesc 按时间倒序排序(新→旧)。
//
// 时间可解析时按真实时间比较,避免不同时区偏移下字符串比较的错序;
// 时间无法解析的邮件统一排到末尾,而不是按字符串乱序出现在列表顶部。
func sortMessagesDesc(messages []Message) {
	sort.SliceStable(messages, func(i, j int) bool {
		left, leftErr := parseMessageDate(messages[i].Date)
		right, rightErr := parseMessageDate(messages[j].Date)
		if leftErr != nil && rightErr != nil {
			return messages[i].Date > messages[j].Date
		}
		if leftErr != nil {
			return false
		}
		if rightErr != nil {
			return true
		}
		return left.After(right)
	})
}

// FindByRecipient 查找发给指定隐私邮箱别名的最近 limit 封邮件(新→旧)。
//
// 先尝试 IMAP TO 搜索; 失败则只扫收件箱最近若干封本地过滤。
func (c *Client) FindByRecipient(recipient string, limit int, days int) ([]Message, error) {
	var out []Message
	err := c.ForEachByRecipient(recipient, limit, days, func(m Message) bool {
		out = append(out, m)
		return true // 收满 limit 为止
	})
	return out, err
}

// ForEachByRecipient 按新→旧遍历发给 recipient 的最近 limit 封邮件。
// onMsg 返回 false 时立即停止(用于 OTP 命中即返回)。
func (c *Client) ForEachByRecipient(recipient string, limit int, days int, onMsg func(Message) bool) error {
	if c.cli == nil {
		return fmt.Errorf("未连接")
	}
	if onMsg == nil {
		return fmt.Errorf("onMsg 不能为空")
	}
	if limit <= 0 {
		limit = 5
	}

	if _, err := c.cli.Select("INBOX", true); err != nil {
		return err
	}

	// 1) 服务端按 To 搜索
	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("To", recipient)
	if days > 0 {
		criteria.Since = time.Now().AddDate(0, 0, -days)
	}
	uids, err := c.cli.UidSearch(criteria)
	if err == nil && len(uids) > 0 {
		// UID 升序 → 取最后 limit 个(最新) → 倒序遍历
		uids = newestUIDs(uids, limit)
		for i := len(uids) - 1; i >= 0; i-- {
			m, ferr := c.fetchOneUID(uids[i])
			if ferr != nil {
				return ferr
			}
			if !onMsg(m) {
				return nil
			}
		}
		return nil
	}

	// 2) fallback: 只扫最近 N 封信封, 命中 To 再拉 body
	return c.forEachRecentMatching(recipient, limit, days, onMsg)
}

// newestUIDs 保留 UID 列表中最新的 limit 个(假定 UID 升序)。
func newestUIDs(uids []uint32, limit int) []uint32 {
	if limit <= 0 || len(uids) <= limit {
		return uids
	}
	return uids[len(uids)-limit:]
}

// forEachRecentMatching 拉取收件箱最近 scan 封(仅 envelope), 本地按 To 过滤后再取 body。
func (c *Client) forEachRecentMatching(recipient string, limit int, days int, onMsg func(Message) bool) error {
	mbox, err := c.cli.Select("INBOX", true)
	if err != nil {
		return err
	}
	total := int(mbox.Messages)
	if total == 0 {
		return nil
	}
	// 只扫最近 scan 封, 避免全箱
	scan := limit * 4
	if scan < 20 {
		scan = 20
	}
	if scan > 80 {
		scan = 80
	}
	if scan > total {
		scan = total
	}
	from := mbox.Messages - uint32(scan) + 1
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	// 仅 envelope + date, 不拉 body
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate}
	messages := make(chan *imap.Message, scan)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.Fetch(seqset, items, messages)
	}()

	type cand struct {
		uid  uint32
		date time.Time
		to   string
	}
	var cands []cand
	recipient = strings.ToLower(recipient)
	for msg := range messages {
		if msg == nil || msg.Envelope == nil {
			continue
		}
		to := ""
		if len(msg.Envelope.To) > 0 {
			parts := make([]string, 0, len(msg.Envelope.To))
			for _, a := range msg.Envelope.To {
				parts = append(parts, a.Address())
			}
			to = strings.Join(parts, ", ")
		}
		if !strings.Contains(strings.ToLower(to), recipient) {
			continue
		}
		if days > 0 && !msg.Envelope.Date.IsZero() {
			if time.Since(msg.Envelope.Date) > time.Duration(days)*24*time.Hour {
				continue
			}
		}
		cands = append(cands, cand{uid: msg.Uid, date: msg.Envelope.Date, to: to})
	}
	if err := <-done; err != nil {
		return err
	}
	// 新→旧
	for i := 0; i < len(cands); i++ {
		for j := i + 1; j < len(cands); j++ {
			if cands[j].date.After(cands[i].date) {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	n := 0
	for _, cd := range cands {
		if n >= limit {
			break
		}
		m, ferr := c.fetchOneUID(cd.uid)
		if ferr != nil {
			return ferr
		}
		n++
		if !onMsg(m) {
			return nil
		}
	}
	return nil
}

// fetchOneUID 拉取单封邮件(含 body preview), 使用 BODY.PEEK 不标已读。
func (c *Client) fetchOneUID(uid uint32) (Message, error) {
	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)
	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}
	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()
	msg := <-messages
	if err := <-done; err != nil {
		return Message{}, err
	}
	if msg == nil {
		return Message{}, fmt.Errorf("邮件不存在 (uid=%d)", uid)
	}
	return toMessageWithBody(msg), nil
}

func (c *Client) fetchByUIDs(uids []uint32, limit int) ([]Message, error) {
	if len(uids) == 0 {
		return []Message{}, nil
	}
	uids = newestUIDs(uids, limit)
	var out []Message
	// 新→旧
	for i := len(uids) - 1; i >= 0; i-- {
		m, err := c.fetchOneUID(uids[i])
		if err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, nil
}

// GetFull 获取单封邮件的完整内容(含正文)。
//
// opts 决定是否额外返回清理后的 HTML 与完整原始报文。
func (c *Client) GetFull(uid uint32, opts MessageOptions) (*FullMessage, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if _, err := c.cli.Select("INBOX", true); err != nil {
		return nil, err
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)

	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, imap.FetchRFC822}
	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()

	msg := <-messages
	if err := <-done; err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, fmt.Errorf("邮件不存在 (uid=%d)", uid)
	}

	full := &FullMessage{Message: toMessage(msg)}
	if r := msg.GetBody(&imap.BodySectionName{}); r != nil {
		// 一次性读入原始报文:正文解析与原始数据导出共用同一份字节
		raw, err := io.ReadAll(io.LimitReader(r, maxRawMessageBytes+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > maxRawMessageBytes {
			raw = raw[:maxRawMessageBytes]
		}

		parts := buildBody(bytes.NewReader(raw))
		full.Body = parts.Plain
		full.BodyHTML = truncateUTF8(parts.HTML, maxMailHTMLBytes)
		full.ContentType = parts.contentType()

		if opts.Sanitize {
			full.BodyHTMLSanitized = sanitizeMailHTML(parts.HTML)
		}
		if opts.IncludeRaw {
			full.RawMessage = base64.StdEncoding.EncodeToString(raw)
			full.RawSize = len(raw)
		}
	}
	return full, nil
}

// Delete 删除收件箱中指定 UID 的邮件。
func (c *Client) Delete(uid uint32) error {
	if c.cli == nil {
		return fmt.Errorf("未连接")
	}
	if uid == 0 {
		return fmt.Errorf("邮件 UID 无效")
	}
	if _, err := c.cli.Select("INBOX", false); err != nil {
		return err
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	if err := c.cli.UidStore(seqset, item, []interface{}{imap.DeletedFlag}, nil); err != nil {
		return err
	}
	return c.cli.Expunge(nil)
}

// ---- 解析工具 ----

func toMessage(msg *imap.Message) Message {
	m := Message{}
	if msg.Uid > 0 {
		m.ID = fmt.Sprintf("%d", msg.Uid)
	}
	if msg.Envelope != nil {
		if len(msg.Envelope.From) > 0 {
			m.From = msg.Envelope.From[0].Address()
		}
		if len(msg.Envelope.To) > 0 {
			addrs := make([]string, 0, len(msg.Envelope.To))
			for _, a := range msg.Envelope.To {
				addrs = append(addrs, a.Address())
			}
			m.To = strings.Join(addrs, ", ")
		}
		m.Subject = decodeHeader(msg.Envelope.Subject)
		if !msg.Envelope.Date.IsZero() {
			m.Date = msg.Envelope.Date.Format(time.RFC3339)
		}
	}
	if m.From != "" {
		m.From = decodeHeader(m.From)
	}
	if m.To != "" {
		m.To = decodeHeader(m.To)
	}
	return m
}

// toMessageWithBody 在 toMessage 基础上解析正文填充 Preview(供 OTP 提取)。
func toMessageWithBody(msg *imap.Message) Message {
	m := toMessage(msg)
	// Fetch 可能用 BODY[] 或 BODY.PEEK[], 两种 section 都试
	for _, section := range []*imap.BodySectionName{{Peek: true}, {}} {
		r := msg.GetBody(section)
		if r == nil {
			continue
		}
		parts := buildBody(r)
		if parts.Plain == "" && parts.HTML == "" {
			continue
		}
		// 列表摘要固定用纯文本
		m.Preview = parts.Plain
		break
	}
	return m
}

// decodeHeader 解码 RFC 2047 编码的邮件头(如 =?UTF-8?B?xxx?=)。
func decodeHeader(s string) string {
	if s == "" {
		return ""
	}
	dec := mime.WordDecoder{CharsetReader: charset.Reader}
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

// bodyParts 是一封邮件解析出的正文各部分。
type bodyParts struct {
	// Plain 是可读纯文本:text/plain 部分,或由 HTML 转换得到
	Plain string
	// HTML 是清理后的原始 HTML,供界面在 sandbox iframe 中渲染
	HTML string
	// TopType 是顶层 Content-Type,仅在整封邮件没有可读正文时用于说明原因
	TopType string
}

// contentType 返回界面应采用的渲染类型。
func (p bodyParts) contentType() string {
	switch {
	case p.HTML != "":
		return "text/html"
	case p.Plain != "":
		return "text/plain"
	default:
		return p.TopType
	}
}

// buildBody 从完整 RFC822 报文提取正文。
//
// 邮件正文常见结构是 multipart/mixed 或 multipart/alternative 嵌套,直接按顶层
// Content-Type 读取会把 MIME 边界当成正文。这里用 go-message 的 mail.Reader
// 遍历所有内联部分:它会递归展开 multipart、解码
// Content-Transfer-Encoding(base64 / quoted-printable)并按 charset 转码。
//
// 附件一律跳过。text/plain 与 text/html 都会收集:HTML 原样保留(不清理),
// 纯文本用于列表摘要、复制与 OTP 提取;只有 HTML 的邮件会由 HTML 反推纯文本。
func buildBody(raw io.Reader) bodyParts {
	reader, err := esmail.CreateReader(raw)
	if err != nil && !message.IsUnknownCharset(err) {
		return bodyParts{}
	}
	defer reader.Close()

	parts := bodyParts{TopType: reader.Header.Get("Content-Type")}

	var plain, htmlPart string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			break
		}

		inline, ok := part.Header.(*esmail.InlineHeader)
		if !ok {
			// 附件:必须读完才能继续遍历,但内容不参与正文
			_, _ = io.Copy(io.Discard, part.Body)
			continue
		}

		mediaType, _, _ := inline.ContentType()
		switch {
		case strings.EqualFold(mediaType, "text/plain") && plain == "":
			data, _ := io.ReadAll(part.Body)
			plain = sanitizePlainPreview(string(data))
		case strings.EqualFold(mediaType, "text/html") && htmlPart == "":
			data, _ := io.ReadAll(part.Body)
			htmlPart = string(data) // 原样保留,清理由调用方按需决定
		default:
			_, _ = io.Copy(io.Discard, part.Body)
		}
	}

	parts.HTML = htmlPart
	if htmlPart != "" {
		// HTML 邮件的纯文本视图:剥标签与样式后的可读文本
		parts.Plain = sanitizePreview(htmlPart)
	}
	if plain != "" {
		// 邮件自带 text/plain 时以它为准(通常是发件人给出的最准确文本)
		parts.Plain = plain
	}
	return parts
}
