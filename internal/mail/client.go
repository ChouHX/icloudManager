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
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/utf7"
	message "github.com/emersion/go-message"
	"github.com/emersion/go-message/charset"
	esmail "github.com/emersion/go-message/mail"
)

const (
	IMAPServer = "imap.mail.me.com"
	IMAPPort   = 993

	// InboxFolder 是主收件箱的 IMAP 名称。
	InboxFolder = "INBOX"

	// maxRawMessageBytes 是单封邮件原始报文的读取上限(超出即截断)。
	maxRawMessageBytes = 10 << 20
)

// Message 是一封邮件的摘要信息。
type Message struct {
	ID string `json:"id"`
	// Folder 是邮件所在 IMAP 文件夹;收件箱为 INBOX,垃圾邮件为服务端名称。
	Folder  string `json:"folder,omitempty"`
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

// CheckConnection 用新会话验证凭据和收件箱访问权限，不读取或修改邮件。
func CheckConnection(username, password, server string, port int) error {
	const timeout = 15 * time.Second
	cli, err := client.DialWithDialerTLS(&net.Dialer{Timeout: timeout}, net.JoinHostPort(server, fmt.Sprint(port)), nil)
	if err != nil {
		return err
	}
	defer cli.Terminate()
	cli.Timeout = timeout
	if err := cli.Login(username, password); err != nil {
		return err
	}
	_, err = cli.Select("INBOX", true)
	return err
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

// ListInbox 拉取收件箱与垃圾邮件文件夹最近 limit 封邮件摘要。
//
// days 用于过滤只看近 N 天的邮件(0 表示不限制)。
// 返回按时间倒序排列;垃圾邮件文件夹不可用时不影响主收件箱。
func (c *Client) ListInbox(limit int, days int) ([]Message, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	if limit <= 0 {
		limit = 50
	}

	refs, err := c.recentRefs(InboxFolder, limit, days)
	if err != nil {
		return nil, err
	}
	// 垃圾邮件里的验证码同样要能取到,但单个文件夹失败不应拖垮整次取件。
	for _, folder := range c.junkFolders() {
		more, ferr := c.recentRefs(folder, limit, days)
		if ferr != nil {
			continue
		}
		refs = append(refs, more...)
	}
	return c.fetchRefs(refs, limit)
}

// messageRef 是跨文件夹合并排序用的轻量邮件引用(不含正文)。
type messageRef struct {
	folder string
	uid    uint32
	date   time.Time
}

// sortRefsDesc 按时间倒序排序;时间缺失的引用排在末尾。
func sortRefsDesc(refs []messageRef) {
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].date.IsZero() {
			return false
		}
		if refs[j].date.IsZero() {
			return true
		}
		return refs[i].date.After(refs[j].date)
	})
}

// collectRefs 把 FETCH 回来的信封整理成引用列表,顺带做 days 过滤。
// 时间缺失时保留,避免因个别邮件格式异常而整批丢失。
func collectRefs(folder string, days int, messages <-chan *imap.Message) []messageRef {
	var refs []messageRef
	for msg := range messages {
		if msg == nil {
			continue
		}
		ref := messageRef{folder: folder, uid: msg.Uid}
		if msg.Envelope != nil {
			ref.date = msg.Envelope.Date
		}
		if days > 0 && !ref.date.IsZero() && time.Since(ref.date) > time.Duration(days)*24*time.Hour {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

// recentRefs 拉取单个文件夹最近 limit 封邮件的引用(仅信封, 不取正文)。
func (c *Client) recentRefs(folder string, limit int, days int) ([]messageRef, error) {
	mbox, err := c.cli.Select(folder, true)
	if err != nil {
		return nil, err
	}
	total := int(mbox.Messages)
	if total == 0 {
		return nil, nil
	}

	// 计算起始序号(只取最近 limit 封)
	from := uint32(1)
	if uint32(limit) < mbox.Messages {
		from = mbox.Messages - uint32(limit) + 1
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate}
	messages := make(chan *imap.Message, limit)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.Fetch(seqset, items, messages)
	}()
	refs := collectRefs(folder, days, messages)
	if err := <-done; err != nil {
		return nil, err
	}
	return refs, nil
}

// fetchRefs 合并排序后取前 limit 封,按文件夹批量拉取正文。
//
// 每个文件夹只发一次 FETCH,避免跨文件夹合并截断时重复逐封读信。
func (c *Client) fetchRefs(refs []messageRef, limit int) ([]Message, error) {
	sortRefsDesc(refs)
	if limit > 0 && len(refs) > limit {
		refs = refs[:limit]
	}

	order := make([]string, 0, 2)
	groups := make(map[string][]uint32)
	for _, ref := range refs {
		if _, ok := groups[ref.folder]; !ok {
			order = append(order, ref.folder)
		}
		groups[ref.folder] = append(groups[ref.folder], ref.uid)
	}

	out := make([]Message, 0, len(refs))
	for _, folder := range order {
		msgs, err := c.fetchMessagesByUID(folder, groups[folder])
		if err != nil {
			return out, err
		}
		out = append(out, msgs...)
	}
	sortMessagesDesc(out)
	return out, nil
}

// fetchMessagesByUID 在指定文件夹内批量拉取邮件(含 body preview), PEEK 不标已读。
func (c *Client) fetchMessagesByUID(folder string, uids []uint32) ([]Message, error) {
	if len(uids) == 0 {
		return []Message{}, nil
	}
	if _, err := c.cli.Select(folder, true); err != nil {
		return nil, err
	}
	seqset := new(imap.SeqSet)
	for _, uid := range uids {
		seqset.AddNum(uid)
	}
	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}
	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()

	out := make([]Message, 0, len(uids))
	for msg := range messages {
		if msg == nil {
			continue
		}
		out = append(out, toMessageWithBody(msg, folder))
	}
	if err := <-done; err != nil {
		return nil, err
	}
	return out, nil
}

// junkFolders 返回服务端的垃圾邮件文件夹(可能不止一个)。
//
// 优先用 SPECIAL-USE 属性(\Junk / \Spam)识别;部分服务端(尤其
// Gmail 的非英文界面)不带属性,再按常见文件夹名兜底。
func (c *Client) junkFolders() []string {
	if c.cli == nil {
		return nil
	}
	infos := make(chan *imap.MailboxInfo)
	done := make(chan error, 1)
	go func() {
		done <- c.cli.List("", "*", infos)
	}()

	var out []string
	seen := make(map[string]bool)
	for info := range infos {
		if info == nil || info.Name == "" || seen[info.Name] || !isJunkMailbox(info) {
			continue
		}
		seen[info.Name] = true
		out = append(out, info.Name)
	}
	if err := <-done; err != nil {
		return nil
	}
	return out
}

// isJunkMailbox 判断 LIST 返回的文件夹是否为垃圾邮件文件夹。
func isJunkMailbox(info *imap.MailboxInfo) bool {
	for _, attr := range info.Attributes {
		switch strings.ToLower(attr) {
		case strings.ToLower(imap.JunkAttr), "\\spam":
			return true
		case strings.ToLower(imap.NoSelectAttr):
			return false
		}
	}
	// LIST 返回的是 modified UTF-7(中文文件夹名会被编码),先解码再匹配。
	name := strings.TrimSpace(info.Name)
	if decoded, err := utf7.Encoding.NewDecoder().String(name); err == nil {
		name = decoded
	}
	name = strings.ToLower(name)
	if idx := strings.LastIndexAny(name, "/."); idx >= 0 {
		name = name[idx+1:]
	}
	switch strings.TrimSpace(name) {
	case "spam", "junk", "junk e-mail", "junk email", "bulk mail", "垃圾邮件", "垃圾郵件":
		return true
	}
	return false
}

// normalizeFolder 把空文件夹名归一为收件箱。
func normalizeFolder(folder string) string {
	if folder = strings.TrimSpace(folder); folder == "" {
		return InboxFolder
	}
	return folder
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
//
// 收件箱与垃圾邮件文件夹都会查;垃圾邮件文件夹出错时只跳过该文件夹。
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

	refs, err := c.recipientRefs(InboxFolder, recipient, limit, days)
	if err != nil {
		return err
	}
	for _, folder := range c.junkFolders() {
		more, ferr := c.recipientRefs(folder, recipient, limit, days)
		if ferr != nil {
			continue
		}
		refs = append(refs, more...)
	}
	messages, err := c.fetchRefs(refs, limit)
	if err != nil {
		return err
	}
	for _, m := range messages {
		if !onMsg(m) {
			return nil
		}
	}
	return nil
}

// recipientRefs 在单个文件夹内查找发给 recipient 的最近 limit 封邮件引用。
//
// 先尝试 IMAP TO 搜索; 搜索不可用时退回扫描最近若干封信封,本地按 To 过滤。
func (c *Client) recipientRefs(folder, recipient string, limit int, days int) ([]messageRef, error) {
	if _, err := c.cli.Select(folder, true); err != nil {
		return nil, err
	}

	criteria := imap.NewSearchCriteria()
	criteria.Header.Add("To", recipient)
	if days > 0 {
		criteria.Since = time.Now().AddDate(0, 0, -days)
	}
	uids, err := c.cli.UidSearch(criteria)
	if err == nil && len(uids) > 0 {
		// UID 升序 → 取最后 limit 个(最新)
		return c.refsForUIDs(folder, newestUIDs(uids, limit), days)
	}

	return c.scanRecentRefs(folder, recipient, limit, days)
}

// refsForUIDs 取指定 UID 的信封与日期(不取正文)。
func (c *Client) refsForUIDs(folder string, uids []uint32, days int) ([]messageRef, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	seqset := new(imap.SeqSet)
	for _, uid := range uids {
		seqset.AddNum(uid)
	}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchInternalDate}
	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() {
		done <- c.cli.UidFetch(seqset, items, messages)
	}()
	refs := collectRefs(folder, days, messages)
	if err := <-done; err != nil {
		return nil, err
	}
	return refs, nil
}

// newestUIDs 保留 UID 列表中最新的 limit 个(假定 UID 升序)。
func newestUIDs(uids []uint32, limit int) []uint32 {
	if limit <= 0 || len(uids) <= limit {
		return uids
	}
	return uids[len(uids)-limit:]
}

// scanRecentRefs 扫描该文件夹最近若干封信封, 本地按 To 过滤。
//
// 服务端不支持按收件人搜索时的兜底路径。
func (c *Client) scanRecentRefs(folder, recipient string, limit int, days int) ([]messageRef, error) {
	mbox, err := c.cli.Select(folder, true)
	if err != nil {
		return nil, err
	}
	total := int(mbox.Messages)
	if total == 0 {
		return nil, nil
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

	var refs []messageRef
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
		refs = append(refs, messageRef{folder: folder, uid: msg.Uid, date: msg.Envelope.Date})
	}
	if err := <-done; err != nil {
		return nil, err
	}
	return refs, nil
}

// GetFull 获取指定文件夹中单封邮件的完整内容(含正文)。
//
// opts 决定是否额外返回清理后的 HTML 与完整原始报文。
func (c *Client) GetFull(folder string, uid uint32, opts MessageOptions) (*FullMessage, error) {
	if c.cli == nil {
		return nil, fmt.Errorf("未连接")
	}
	folder = normalizeFolder(folder)
	if _, err := c.cli.Select(folder, true); err != nil {
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

	full := &FullMessage{Message: toMessage(msg, folder)}
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

// Delete 删除指定文件夹中指定 UID 的邮件。
func (c *Client) Delete(folder string, uid uint32) error {
	if c.cli == nil {
		return fmt.Errorf("未连接")
	}
	if uid == 0 {
		return fmt.Errorf("邮件 UID 无效")
	}
	folder = normalizeFolder(folder)
	if _, err := c.cli.Select(folder, false); err != nil {
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

func toMessage(msg *imap.Message, folder string) Message {
	folder = normalizeFolder(folder)
	m := Message{Folder: folder}
	if msg.Uid > 0 {
		m.ID = strconv.FormatUint(uint64(msg.Uid), 10)
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
func toMessageWithBody(msg *imap.Message, folder string) Message {
	m := toMessage(msg, folder)
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
