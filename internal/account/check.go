package account

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"icloud-hme/internal/mail"
)

var ErrCheckCredentialsChanged = errors.New("账号凭据已变更，请重新检测")

// CheckItem 只包含固定的检测说明，不返回上游错误或凭据。
type CheckItem struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

type CheckResult struct {
	Account Summary     `json:"account"`
	Checks  []CheckItem `json:"checks"`
}

// CheckAccount 重新验证已配置的凭据；IMAP 使用新连接，避免旧会话掩盖密码失效。
func (m *Manager) CheckAccount(id string) (CheckResult, error) {
	return m.checkAccount(id, (*Account).validateCookies, func(config MailboxConfig) error {
		return mail.CheckConnection(config.Email, config.Password, config.IMAPHost, config.IMAPPort)
	})
}

func (m *Manager) checkAccount(id string, checkCookies func(*Account), checkMailbox func(MailboxConfig) error) (CheckResult, error) {
	original, ok := m.GetAccount(id)
	if !ok {
		return CheckResult{}, fmt.Errorf("账号不存在: %s", id)
	}
	snap := copyAccount(original)
	checks := make([]CheckItem, 0, 3)
	if len(snap.Cookies) > 0 {
		checkCookies(snap)
		item := CheckItem{Name: "Cookie", Passed: snap.Status == "active", Message: "iCloud 会话有效"}
		if !item.Passed {
			item.Message = "Cookie 检测失败，请检查 Cookie 是否过期及网络或代理配置"
		}
		checks = append(checks, item)
	}
	if snap.AppPassword != "" {
		config := MailboxConfig{Email: firstNonEmpty(snap.ICloudEmail, snap.RealEmail), Password: snap.AppPassword, IMAPHost: mail.IMAPServer, IMAPPort: mail.IMAPPort}
		passed := isICloudDomain(config.Email) && checkMailbox(config) == nil
		item := CheckItem{Name: "App 专用密码", Passed: passed, Message: "iCloud IMAP 连接正常"}
		if !passed {
			item.Message = "IMAP 检测失败，请检查 iCloud 邮箱、App 专用密码及网络连接"
		}
		checks = append(checks, item)
	}
	if snap.Mailbox != nil {
		config := *snap.Mailbox
		passed := config.Email != "" && config.Password != "" && config.IMAPHost != "" && config.IMAPPort > 0 && config.IMAPPort <= 65535 && checkMailbox(config) == nil
		item := CheckItem{Name: "收件邮箱", Passed: passed, Message: "收件邮箱 IMAP 连接正常"}
		if !passed {
			item.Message = "收件邮箱检测失败，请检查邮箱、授权码、IMAP 配置及网络连接"
		}
		checks = append(checks, item)
	}

	status := "active"
	var failures []string
	for _, item := range checks {
		if !item.Passed {
			status = "error"
			failures = append(failures, item.Message)
		}
	}
	if len(checks) == 0 {
		status = "pending"
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.accounts[id]
	if !ok {
		return CheckResult{}, fmt.Errorf("账号不存在: %s", id)
	}
	// 检测期间允许编辑账号；旧检测不能覆盖新凭据及其验证状态。
	if !reflect.DeepEqual(cur.Cookies, original.Cookies) || cur.Host != original.Host || cur.Proxy != original.Proxy ||
		cur.AppPassword != original.AppPassword || cur.ICloudEmail != original.ICloudEmail || cur.RealEmail != original.RealEmail ||
		!reflect.DeepEqual(cur.Mailbox, original.Mailbox) {
		return CheckResult{}, ErrCheckCredentialsChanged
	}
	updated := copyAccount(cur)
	updated.Cookies = cloneCookies(snap.Cookies)
	updated.RealEmail, updated.ICloudEmail = snap.RealEmail, snap.ICloudEmail
	updated.AliasTotal, updated.AliasActive = snap.AliasTotal, snap.AliasActive
	updated.Status, updated.LastError = status, strings.Join(failures, "；")
	if status == "active" {
		updated.LastValidated = time.Now().Format(time.RFC3339)
	}
	m.accounts[id] = updated
	if err := m.save(); err != nil {
		m.accounts[id] = cur
		return CheckResult{}, err
	}
	return CheckResult{Account: updated.Summary(), Checks: checks}, nil
}
