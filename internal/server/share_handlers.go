// Package server - 隐私邮箱取件链接。
//
// 管理侧(需会话)为别名生成/撤销取件链接;公开侧凭 token 只读取件,
// 无需登录,但**只能看到该 token 对应别名收到的邮件**。
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/mail"
)

// codeShareInvalid 是取件链接无效的稳定错误码。
const codeShareInvalid = "SHARE_INVALID"

// requestBaseURL 依据当前请求推断对外地址,用于拼出可直接分享的链接。
//
// 反向代理部署时 Host 通常被保留,X-Forwarded-Proto 用来识别 HTTPS;
// 这里只影响生成出来的链接文本,不参与鉴权。
func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host
	if host == "" {
		host = "127.0.0.1:8081"
	}
	return scheme + "://" + host
}

// aliasByAnonymousID 在当前账号的别名里按 anonymousId 找到邮箱地址。
func (s *Server) aliasByAnonymousID(accountID, anonymousID string) (string, error) {
	aliases, err := s.be.ListAliases(accountID)
	if err != nil {
		return "", err
	}
	for _, alias := range aliases {
		if alias.AnonymousID == anonymousID {
			return alias.Email, nil
		}
	}
	return "", nil
}

// createShareLinkHandler 处理 POST /api/aliases/:id/share-link。
//
// 幂等:同一别名重复调用返回同一个 token,便于前端"生成即复制"。
func (s *Server) createShareLinkHandler(c *gin.Context) {
	accountID, anonymousID, valid := validateAliasAction(c)
	if !valid {
		return
	}

	email, err := s.aliasByAnonymousID(accountID, anonymousID)
	if err != nil {
		backendFail(c, err)
		return
	}
	if email == "" {
		failCode(c, http.StatusNotFound, "VALIDATION_ERROR", "别名不存在")
		return
	}

	link, err := s.share.Ensure(accountID, email)
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "生成取件链接失败")
		return
	}

	ok(c, gin.H{
		"alias":      email,
		"token":      link.Token,
		"url":        requestBaseURL(c) + "/?token=" + link.Token,
		"created_at": link.CreatedAt,
		"hits":       link.Hits,
	})
}

// revokeShareLinkHandler 处理 DELETE /api/aliases/:id/share-link。
//
// 撤销后 token 立即失效,重新生成会得到全新的 token。
func (s *Server) revokeShareLinkHandler(c *gin.Context) {
	accountID, anonymousID, valid := validateAliasAction(c)
	if !valid {
		return
	}

	email, err := s.aliasByAnonymousID(accountID, anonymousID)
	if err != nil {
		backendFail(c, err)
		return
	}
	if email == "" {
		failCode(c, http.StatusNotFound, "VALIDATION_ERROR", "别名不存在")
		return
	}

	removed, err := s.share.Revoke(accountID, email)
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "撤销取件链接失败")
		return
	}
	ok(c, gin.H{"alias": email, "removed": removed})
}

// shareInboxHandler 处理 GET /api/share/:token/inbox(公开)。
func (s *Server) shareInboxHandler(c *gin.Context) {
	link, found := s.share.Get(c.Param("token"))
	if !found {
		failCode(c, http.StatusNotFound, codeShareInvalid, "取件链接无效或已失效")
		return
	}

	limit, err := parseInboxInt(c.DefaultQuery("limit", "20"), 1, 100)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误: limit 需为 1-100 的整数")
		return
	}
	days, err := parseInboxInt(c.DefaultQuery("days", "7"), 1, 90)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误: days 需为 1-90 的整数")
		return
	}

	// 强制按该 token 的别名过滤,调用方无法换成别的地址
	result, err := s.be.ListInbox(InboxQuery{
		AccountID: link.AccountID,
		Alias:     link.Alias,
		Limit:     limit,
		Days:      days,
	})
	if err != nil {
		backendFail(c, err)
		return
	}
	s.share.Touch(link.Token)

	// 响应里不包含 account_id,避免暴露账号内部标识
	ok(c, gin.H{
		"alias":    link.Alias,
		"count":    result.Count,
		"messages": result.Messages,
		"method":   result.Method,
	})
}

// shareMessageHandler 处理 GET /api/share/:token/inbox/:message_id(公开)。
func (s *Server) shareMessageHandler(c *gin.Context) {
	link, found := s.share.Get(c.Param("token"))
	if !found {
		failCode(c, http.StatusNotFound, codeShareInvalid, "取件链接无效或已失效")
		return
	}

	uid, err := parseMessageUID(c.Param("message_id"))
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "邮件 ID 无效")
		return
	}

	message, err := s.be.GetMessage(link.AccountID, uid, mail.MessageOptions{Sanitize: true})
	if err != nil {
		backendFail(c, err)
		return
	}

	// IMAP UID 是账号级全局编号,必须逐封核对收件人,
	// 否则拿到链接的人能读到同一账号下其它别名的邮件。
	if !messageBelongsToAlias(message, link.Alias) {
		failCode(c, http.StatusNotFound, codeShareInvalid, "邮件不存在")
		return
	}
	s.share.Touch(link.Token)

	message.To = link.Alias // 只回显该别名,不暴露账号其它地址
	ok(c, message)
}

// messageBelongsToAlias 判断邮件是否发给指定别名。
//
// 只核对收件人(To):刻意不匹配 From 等内容,避免放宽可见范围。
// 收件人头缺失的邮件(例如密送投递)会按不可见处理。
func messageBelongsToAlias(message *mail.FullMessage, alias string) bool {
	if message == nil {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(alias))
	if target == "" {
		return false
	}
	return strings.Contains(strings.ToLower(message.To), target)
}

// parseMessageUID 解析邮件 ID(IMAP UID,不可为 0)。
func parseMessageUID(raw string) (uint32, error) {
	uid, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil || uid == 0 {
		return 0, fmt.Errorf("邮件 ID 无效")
	}
	return uint32(uid), nil
}
