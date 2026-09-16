// Command fetch_inbox 是 Go 程序接入 iCloud HME 的最小示例。
//
//	go run examples/fetch_inbox.go -base http://127.0.0.1:8081 -password '你的管理员密码'
//	go run examples/fetch_inbox.go -base http://127.0.0.1:8081 -password '...' -alias xyz@icloud.com -watch
//
// 要点:
//   - 用 net/http/cookiejar 自动维护 hme_session 会话 Cookie
//   - 读取接口(GET)不需要 CSRF;删除等写操作才需要 X-CSRF-Token
//   - 会话过期(401)时自动重新登录一次
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"time"
)

// envelope 是服务端统一响应包裹。
type envelope struct {
	Success bool            `json:"success"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// APIError 保留 HTTP 状态与稳定错误码,便于调用方分支处理。
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

type client struct {
	base     string
	password string
	csrf     string
	http     *http.Client
}

func newClient(base, password string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{
		base:     base,
		password: password,
		http:     &http.Client{Jar: jar, Timeout: 30 * time.Second},
	}
}

// do 发起请求并解码统一包裹;mutate 表示这是写操作,需要带上 CSRF。
func (c *client) do(method, path string, body any, mutate bool) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if mutate && c.csrf != "" {
		request.Header.Set("X-CSRF-Token", c.csrf)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)

	var envelope envelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, &APIError{Status: response.StatusCode, Code: "INVALID_RESPONSE", Message: string(payload[:min(120, len(payload))])}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !envelope.Success {
		return nil, &APIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	return envelope.Data, nil
}

// doWithRelogin 在会话过期时自动重新登录一次。
func (c *client) doWithRelogin(method, path string, body any, mutate bool) (json.RawMessage, error) {
	data, err := c.do(method, path, body, mutate)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == 401 {
		if loginErr := c.login(); loginErr != nil {
			return nil, loginErr
		}
		return c.do(method, path, body, mutate)
	}
	return data, err
}

func (c *client) login() error {
	data, err := c.do("POST", "/api/auth/login", map[string]string{"password": c.password}, false)
	if err != nil {
		return err
	}
	var result struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	c.csrf = result.CSRFToken
	return nil
}

type accountSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ICloudEmail string `json:"icloud_email"`
}

type alias struct {
	Email       string `json:"email"`
	AnonymousID string `json:"anonymousId"`
	Label       string `json:"label"`
	Active      bool   `json:"active"`
	CreatedAt   string `json:"createdAt"`
}

type message struct {
	ID          string `json:"id"`
	From        string `json:"from"`
	To          string `json:"to"`
	Subject     string `json:"subject"`
	Date        string `json:"date"`
	Preview     string `json:"preview"`
	Body        string `json:"body"`
	HTML        string `json:"body_html"`
	ContentType string `json:"content_type"`
}

type inboxResult struct {
	AccountID string    `json:"account_id"`
	Alias     string    `json:"alias"`
	Count     int       `json:"count"`
	Method    string    `json:"method"`
	Messages  []message `json:"messages"`
}

func main() {
	base := flag.String("base", "http://127.0.0.1:8081", "服务地址")
	password := flag.String("password", "", "管理员密码(ICLOUD_HME_ADMIN_PASSWORD)")
	accountFlag := flag.String("account", "", "账号 ID 或名称,缺省用第一个")
	aliasFlag := flag.String("alias", "", "只看发到该别名的邮件")
	limit := flag.Int("limit", 20, "返回上限 1-100")
	days := flag.Int("days", 7, "只看近 N 天 1-90(仅 IMAP 路径生效)")
	messageID := flag.String("message-id", "", "读取指定邮件的正文")
	watch := flag.Bool("watch", false, "轮询新邮件")
	flag.Parse()

	if *password == "" {
		fmt.Fprintln(os.Stderr, "必须提供 -password")
		os.Exit(2)
	}

	c := newClient(*base, *password)
	if err := c.login(); err != nil {
		fatal(err)
	}
	fmt.Println("登录成功:", *base)

	// 账号列表 → 确定 account_id
	raw, err := c.doWithRelogin("GET", "/api/accounts", nil, false)
	if err != nil {
		fatal(err)
	}
	var accounts []accountSummary
	if err := json.Unmarshal(raw, &accounts); err != nil {
		fatal(err)
	}
	if len(accounts) == 0 {
		fmt.Fprintln(os.Stderr, "账号列表为空:请先在管理界面添加 iCloud 账号并配置凭据")
		os.Exit(1)
	}
	accountID := accounts[0].ID
	if *accountFlag != "" {
		found := false
		for _, item := range accounts {
			if item.ID == *accountFlag || item.Name == *accountFlag {
				accountID, found = item.ID, true
			}
		}
		if !found {
			fatal(fmt.Errorf("找不到账号: %s", *accountFlag))
		}
	}
	fmt.Println("使用账号:", accountID)

	// 别名列表
	raw, err = c.doWithRelogin("GET", "/api/aliases?account_id="+url.QueryEscape(accountID), nil, false)
	if err != nil {
		fatal(err)
	}
	var aliasList struct {
		Count   int     `json:"count"`
		Aliases []alias `json:"aliases"`
	}
	if err := json.Unmarshal(raw, &aliasList); err != nil {
		fatal(err)
	}
	fmt.Printf("别名邮箱 %d 个:\n", aliasList.Count)
	for _, item := range aliasList.Aliases {
		state := "停用"
		if item.Active {
			state = "启用"
		}
		fmt.Printf("  %s  [%s]  %s\n", item.Email, state, item.Label)
	}

	// 取件
	params := url.Values{}
	params.Set("account_id", accountID)
	params.Set("limit", fmt.Sprint(*limit))
	params.Set("days", fmt.Sprint(*days))
	if *aliasFlag != "" {
		params.Set("alias", *aliasFlag)
	}

	if *messageID != "" {
		raw, err = c.doWithRelogin("GET", "/api/inbox/"+url.PathEscape(*messageID)+"?"+url.Values{"account_id": {accountID}}.Encode(), nil, false)
		if err != nil {
			fatal(err)
		}
		var detail message
		if err := json.Unmarshal(raw, &detail); err != nil {
			fatal(err)
		}
		fmt.Printf("\n--- %s ---\n发件人: %s\n内容类型: %s\n%s\n", detail.Subject, detail.From, detail.ContentType, detail.Body)
		return
	}

	if *watch {
		fmt.Println("\n开始轮询新邮件(Ctrl-C 退出)…")
		seen := map[string]bool{}
		for {
			result, err := fetchInbox(c, params)
			if err != nil {
				fmt.Fprintln(os.Stderr, "轮询失败:", err)
			} else {
				for _, item := range result.Messages {
					if seen[item.ID] {
						continue
					}
					seen[item.ID] = true
					fmt.Printf("新邮件 [%s] %s ← %s\n", item.ID, item.Subject, item.From)
				}
			}
			time.Sleep(15 * time.Second)
		}
	}

	result, err := fetchInbox(c, params)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("共 %d 封,读取方式: %s\n", result.Count, result.Method)
	for _, item := range result.Messages {
		fmt.Printf("  [%s] %s | %s\n      %s\n", item.ID, item.Date, item.From, item.Subject)
	}
}

func fetchInbox(c *client, params url.Values) (inboxResult, error) {
	raw, err := c.doWithRelogin("GET", "/api/inbox?"+params.Encode(), nil, false)
	if err != nil {
		return inboxResult{}, err
	}
	var result inboxResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return inboxResult{}, err
	}
	return result, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "调用失败:", err)
	os.Exit(1)
}
