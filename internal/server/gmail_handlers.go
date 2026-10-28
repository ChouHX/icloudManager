// Package server - Gmail 取件。
//
// 管理员导入 Gmail 账号（账号/密码/2FA 密钥/应用专用密码），可配置取件过滤；
// 每个邮箱可生成一个取件链接，未认证用户凭 token 只读查看**过滤后**的邮件。
package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/gmail"
	"icloud-hme/internal/mail"
)

// gmailShareAccountPrefix 让取件链接存储能区分 Gmail 与 iCloud 别名。
const gmailShareAccountPrefix = "gmail:"

// gmailShareKey 返回取件链接里使用的账号键。
func gmailShareKey(id string) string {
	return gmailShareAccountPrefix + id
}

// gmailIDFromShareKey 从账号键还原 Gmail 账号 ID。
func gmailIDFromShareKey(key string) (string, bool) {
	if !strings.HasPrefix(key, gmailShareAccountPrefix) {
		return "", false
	}
	id := strings.TrimPrefix(key, gmailShareAccountPrefix)
	return id, id != ""
}

// gmailError 把 gmail 包的领域错误映射为稳定的 HTTP 错误。
func gmailError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gmail.ErrNoAppPassword):
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, gmail.ErrFiltered):
		failCode(c, http.StatusNotFound, "MESSAGE_NOT_FOUND", "邮件不存在")
	default:
		failCode(c, http.StatusBadGateway, "GMAIL_ERROR", err.Error())
	}
}

// listGmailHandler 处理 GET /api/gmail,返回全部账号与取件链接状态。
func (s *Server) listGmailHandler(c *gin.Context) {
	accounts, err := s.gmail.List()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取 Gmail 账号失败")
		return
	}
	items := make([]gin.H, 0, len(accounts))
	for _, account := range accounts {
		item := gin.H{
			"id":               account.ID,
			"email":            account.Email,
			"note":             account.Note,
			"status":           account.Status,
			"has_password":     account.HasPassword,
			"has_totp":         account.HasTOTP,
			"has_app_password": account.HasAppPassword,
			"last_checked":     account.LastChecked,
			"last_error":       account.LastError,
			"created_at":       account.CreatedAt,
			"updated_at":       account.UpdatedAt,
		}
		if link, ok := s.share.GetByAlias(gmailShareKey(account.ID), account.Email); ok {
			item["share_token"] = link.Token
			item["share_hits"] = link.Hits
			item["share_created_at"] = link.CreatedAt
			item["share_last_used_at"] = link.LastUsedAt
		}
		items = append(items, item)
	}
	ok(c, gin.H{"count": len(items), "filter": s.gmail.Filter(), "accounts": items})
}

// importGmailHandler 处理 POST /api/gmail/import。
func (s *Server) importGmailHandler(c *gin.Context) {
	var req struct {
		Text      string `json:"text"`
		Separator string `json:"separator"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	result, err := s.gmail.Import(req.Text, req.Separator)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, result)
}

// createGmailHandler 处理 POST /api/gmail。
func (s *Server) createGmailHandler(c *gin.Context) {
	var req gmail.Input
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	account, err := s.gmail.Create(req)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, account)
}

// updateGmailHandler 处理 PUT /api/gmail/:id。
func (s *Server) updateGmailHandler(c *gin.Context) {
	var req gmail.Input
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	account, err := s.gmail.Update(c.Param("id"), req)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, account)
}

// updateGmailFilterHandler 处理 PUT /api/gmail/filter（全局过滤，对所有邮箱生效）。
func (s *Server) updateGmailFilterHandler(c *gin.Context) {
	var req gmail.Filter
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	filter, err := s.gmail.SetFilter(req)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, filter)
}

// deleteGmailHandler 处理 DELETE /api/gmail/:id,同时撤销其取件链接。
func (s *Server) deleteGmailHandler(c *gin.Context) {
	id := c.Param("id")
	account, found := s.gmail.Get(id)
	if !found {
		failCode(c, http.StatusNotFound, "VALIDATION_ERROR", "账号不存在")
		return
	}
	removed, err := s.gmail.Delete(id)
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "删除失败")
		return
	}
	if removed {
		_, _ = s.share.Revoke(gmailShareKey(id), account.Email)
	}
	ok(c, gin.H{"id": id, "removed": removed})
}

// checkGmailHandler 处理 POST /api/gmail/:id/check,验证应用专用密码。
func (s *Server) checkGmailHandler(c *gin.Context) {
	if err := s.gmail.Check(c.Param("id")); err != nil {
		gmailError(c, err)
		return
	}
	ok(c, gin.H{"id": c.Param("id"), "status": "active"})
}

// createGmailShareHandler 处理 POST /api/gmail/:id/share-link。
func (s *Server) createGmailShareHandler(c *gin.Context) {
	account, found := s.gmail.Get(c.Param("id"))
	if !found {
		failCode(c, http.StatusNotFound, "VALIDATION_ERROR", "账号不存在")
		return
	}
	link, err := s.share.Ensure(gmailShareKey(account.ID), account.Email)
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "生成取件链接失败")
		return
	}
	ok(c, gin.H{
		"email":      account.Email,
		"token":      link.Token,
		"url":        requestBaseURL(c) + "/?gtoken=" + link.Token,
		"created_at": link.CreatedAt,
		"hits":       link.Hits,
	})
}

// exportGmailShareLinksHandler 处理 POST /api/gmail/share-links。
//
// 为每个 Gmail 账号确保一条取件链接，并返回「账号----取件链接」文本，
// 便于管理员一次性导出全部分发。
func (s *Server) exportGmailShareLinksHandler(c *gin.Context) {
	accounts, err := s.gmail.List()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取 Gmail 账号失败")
		return
	}

	base := requestBaseURL(c)
	items := make([]gin.H, 0, len(accounts))
	lines := make([]string, 0, len(accounts))
	for _, account := range accounts {
		link, err := s.share.Ensure(gmailShareKey(account.ID), account.Email)
		if err != nil {
			failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "生成取件链接失败")
			return
		}
		url := base + "/?gtoken=" + link.Token
		lines = append(lines, account.Email+"----"+url)
		items = append(items, gin.H{"email": account.Email, "url": url, "token": link.Token})
	}

	ok(c, gin.H{
		"count": len(items),
		"items": items,
		"text":  strings.Join(lines, "\n"),
	})
}

// revokeGmailShareHandler 处理 DELETE /api/gmail/:id/share-link。
func (s *Server) revokeGmailShareHandler(c *gin.Context) {
	account, found := s.gmail.Get(c.Param("id"))
	if !found {
		failCode(c, http.StatusNotFound, "VALIDATION_ERROR", "账号不存在")
		return
	}
	removed, err := s.share.Revoke(gmailShareKey(account.ID), account.Email)
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "撤销取件链接失败")
		return
	}
	ok(c, gin.H{"email": account.Email, "removed": removed})
}

// gmailInboxHandler 处理 GET /api/gmail/:id/inbox(管理员预览,同样应用过滤)。
func (s *Server) gmailInboxHandler(c *gin.Context) {
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
	account, found := s.gmail.Get(c.Param("id"))
	if !found {
		failCode(c, http.StatusNotFound, "VALIDATION_ERROR", "账号不存在")
		return
	}
	messages, method, err := s.gmail.Inbox(account.ID, limit, days)
	if err != nil {
		gmailError(c, err)
		return
	}
	ok(c, gin.H{
		"email":    account.Email,
		"count":    len(messages),
		"messages": messages,
		"method":   method,
		"filter":   s.gmail.Filter(),
	})
}

// gmailMessageHandler 处理 GET /api/gmail/:id/inbox/:message_id(管理员预览)。
func (s *Server) gmailMessageHandler(c *gin.Context) {
	uid, err := parseMessageUID(c.Param("message_id"))
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "邮件 ID 无效")
		return
	}
	message, err := s.gmail.Message(c.Param("id"), strings.TrimSpace(c.Query("folder")), uid, mail.MessageOptions{Sanitize: true})
	if err != nil {
		gmailError(c, err)
		return
	}
	ok(c, message)
}

// shareGmailInboxLimit 是取件页固定展示的邮件数。
//
// 取件方不能自选条数与时间范围:固定最近 10 封、不限时间
// (若管理员设置了全局过滤的时间窗口,则以该窗口为准)。
const shareGmailInboxLimit = 10

// shareGmailInboxHandler 处理 GET /api/gmail-share/:token/inbox(公开)。
func (s *Server) shareGmailInboxHandler(c *gin.Context) {
	accountID, account, okLink := s.gmailShareAccount(c, c.Param("token"))
	if !okLink {
		return
	}

	// 忽略调用方传入的 limit/days,固定最近 10 封、不限时间。
	messages, method, err := s.gmail.Inbox(accountID, shareGmailInboxLimit, 0)
	if err != nil {
		gmailError(c, err)
		return
	}
	if link, ok := s.share.Get(c.Param("token")); ok {
		s.share.Touch(link.Token)
	}

	// 不返回账号 ID 与过滤条件，避免暴露内部信息
	ok(c, gin.H{
		"alias":    account.Email,
		"count":    len(messages),
		"messages": messages,
		"method":   method,
	})
}

// shareGmailMessageHandler 处理 GET /api/gmail-share/:token/inbox/:message_id(公开)。
func (s *Server) shareGmailMessageHandler(c *gin.Context) {
	accountID, account, okLink := s.gmailShareAccount(c, c.Param("token"))
	if !okLink {
		return
	}
	uid, err := parseMessageUID(c.Param("message_id"))
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "邮件 ID 无效")
		return
	}

	message, err := s.gmail.Message(accountID, strings.TrimSpace(c.Query("folder")), uid, mail.MessageOptions{Sanitize: true})
	if err != nil {
		gmailError(c, err)
		return
	}
	if link, ok := s.share.Get(c.Param("token")); ok {
		s.share.Touch(link.Token)
	}
	message.To = account.Email
	ok(c, message)
}

// gmailShareAccount 校验 token 并返回其对应的 Gmail 账号。
func (s *Server) gmailShareAccount(c *gin.Context, token string) (string, gmail.Summary, bool) {
	link, found := s.share.Get(token)
	if !found {
		failCode(c, http.StatusNotFound, codeShareInvalid, "取件链接无效或已失效")
		return "", gmail.Summary{}, false
	}
	accountID, isGmail := gmailIDFromShareKey(link.AccountID)
	if !isGmail {
		failCode(c, http.StatusNotFound, codeShareInvalid, "取件链接无效或已失效")
		return "", gmail.Summary{}, false
	}
	account, exists := s.gmail.Get(accountID)
	if !exists {
		failCode(c, http.StatusNotFound, codeShareInvalid, "取件链接无效或已失效")
		return "", gmail.Summary{}, false
	}
	return accountID, account.Summary(), true
}
