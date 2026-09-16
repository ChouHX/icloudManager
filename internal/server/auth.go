// Package server - 登录/会话/退出 handler 与认证中间件。
//
// auth.Manager 不依赖 Gin;此处只负责 Cookie/Header 与 HTTP 状态映射。
package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/auth"
)

// authManager 是 auth.Manager 的别名,便于 handler 签名。
type authManager = auth.Manager

// sessionCookieName 是管理员会话 Cookie 名称。
const sessionCookieName = "hme_session"

// sessionIDFromCookie 从请求 Cookie 提取 session ID。
func sessionIDFromCookie(c *gin.Context) string {
	if cookie, err := c.Request.Cookie(sessionCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

// requireSession 校验会话,失败返回 401/AUTH_REQUIRED。
func requireSession(mgr *authManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := sessionIDFromCookie(c)
		if sessionID == "" {
			failCode(c, http.StatusUnauthorized, "AUTH_REQUIRED", "请先登录")
			return
		}
		if _, ok := mgr.Validate(sessionID); !ok {
			failCode(c, http.StatusUnauthorized, "AUTH_REQUIRED", "会话已失效,请重新登录")
			return
		}
		c.Set("session_id", sessionID)
		c.Next()
	}
}

// setSessionCookie 设置会话 Cookie(固定属性:Path=/、HttpOnly、SameSite=Strict)。
func setSessionCookie(c *gin.Context, sessionID string, expiresAt time.Time, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
	})
}

// clearSessionCookie 清除会话 Cookie(属性与 setSessionCookie 保持一致)。
func clearSessionCookie(c *gin.Context, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// handleLogin 处理 POST /api/auth/login。
func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}

	ip := c.ClientIP()
	allowed, retryAfter := s.limiter.Allow(ip)
	if !allowed {
		c.Header("Retry-After", formatRetryAfter(retryAfter))
		failCode(c, http.StatusTooManyRequests, "RATE_LIMITED", "登录尝试过于频繁,请稍后再试")
		return
	}

	sessionID, sess, valid := s.auth.Login(req.Password)
	if !valid {
		failCode(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "管理员密码错误")
		return
	}
	s.limiter.Success(ip)
	setSessionCookie(c, sessionID, sess.ExpiresAt, s.cfg.SecureCookie)
	ok(c, gin.H{
		"csrf_token": sess.CSRFToken,
		"expires_at": sess.ExpiresAt.Format(time.RFC3339),
	})
}

// formatRetryAfter 把等待时间格式化为秒数(Retry-After 头规范)。
func formatRetryAfter(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

// handleSession 处理 GET /api/auth/session。
func (s *Server) handleSession(c *gin.Context) {
	sessionID := sessionIDFromCookie(c)
	if sessionID == "" {
		failCode(c, http.StatusUnauthorized, "AUTH_REQUIRED", "请先登录")
		return
	}
	sess, valid := s.auth.Validate(sessionID)
	if !valid {
		failCode(c, http.StatusUnauthorized, "AUTH_REQUIRED", "会话已失效,请重新登录")
		return
	}
	ok(c, gin.H{
		"csrf_token": sess.CSRFToken,
		"expires_at": sess.ExpiresAt.Format(time.RFC3339),
	})
}

// handleLogout 处理 POST /api/auth/logout(需会话 + CSRF)。
func (s *Server) handleLogout(c *gin.Context) {
	sessionID := sessionIDFromCookie(c)
	if sessionID != "" {
		s.auth.Logout(sessionID)
	}
	clearSessionCookie(c, s.cfg.SecureCookie)
	ok(c, gin.H{"logged_out": true})
}
