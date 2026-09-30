package account

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"icloud-hme/internal/mail"
)

func TestCheckAccountResultsAndPersistence(t *testing.T) {
	for _, scenario := range []struct {
		name, wantStatus                            string
		cookies, app, mailbox, failCookie, failIMAP bool
		wantChecks                                  int
	}{
		{name: "未配置", wantStatus: "pending"},
		{name: "仅 Cookie", cookies: true, wantStatus: "active", wantChecks: 1},
		{name: "仅 App 密码", app: true, wantStatus: "active", wantChecks: 1},
		{name: "仅外部邮箱", mailbox: true, wantStatus: "active", wantChecks: 1},
		{name: "全部通过", cookies: true, app: true, mailbox: true, wantStatus: "active", wantChecks: 3},
		{name: "Cookie 失效仍检测 IMAP", cookies: true, app: true, mailbox: true, failCookie: true, wantStatus: "error", wantChecks: 3},
		{name: "IMAP 失败", cookies: true, app: true, mailbox: true, failIMAP: true, wantStatus: "error", wantChecks: 3},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			m, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			acc := &Account{ID: "acc_test", Name: "主号", ICloudEmail: "owner@icloud.com", Status: "error", LastError: "old-secret", LastValidated: "2026-01-01T00:00:00Z"}
			if scenario.cookies {
				acc.Cookies = map[string]string{"token": "cookie-secret"}
			}
			if scenario.app {
				acc.AppPassword = "app-secret"
			}
			if scenario.mailbox {
				acc.Mailbox = &MailboxConfig{Email: "owner@example.com", Password: "mailbox-secret", IMAPHost: "imap.example.com", IMAPPort: 993}
			}
			m.accounts[acc.ID] = acc
			var imapChecks []MailboxConfig
			result, err := m.checkAccount(acc.ID, func(snap *Account) {
				if !scenario.cookies {
					t.Fatal("不应检测未配置的 Cookie")
				}
				snap.Cookies["token"] = "refreshed-secret"
				snap.Status = "active"
				if scenario.failCookie {
					snap.Status = "error"
					snap.LastError = "upstream-secret"
				}
			}, func(config MailboxConfig) error {
				imapChecks = append(imapChecks, config)
				if scenario.failIMAP {
					return errors.New("upstream-secret")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Account.Status != scenario.wantStatus || len(result.Checks) != scenario.wantChecks {
				t.Fatalf("unexpected result: %+v", result)
			}
			wantIMAP := 0
			if scenario.app {
				wantIMAP++
			}
			if scenario.mailbox {
				wantIMAP++
			}
			if len(imapChecks) != wantIMAP {
				t.Fatalf("IMAP checks: %d, want %d", len(imapChecks), wantIMAP)
			}
			if scenario.app && (imapChecks[0].IMAPHost != mail.IMAPServer || imapChecks[0].Password != "app-secret") {
				t.Fatal("App 密码未使用 iCloud IMAP")
			}
			if scenario.mailbox && imapChecks[len(imapChecks)-1].Password != "mailbox-secret" {
				t.Fatal("外部邮箱未使用自身授权码")
			}
			for _, item := range result.Checks {
				wantPassed := !scenario.failIMAP
				if item.Name == "Cookie" {
					wantPassed = !scenario.failCookie
				}
				if item.Passed != wantPassed {
					t.Fatalf("unexpected item: %+v", item)
				}
			}
			reloaded, err := NewManager(m.dataDir)
			if err != nil {
				t.Fatal(err)
			}
			saved, _ := reloaded.GetAccount(acc.ID)
			if saved.Status != scenario.wantStatus {
				t.Fatal("检测状态未持久化")
			}
			if scenario.cookies && saved.Cookies["token"] != "refreshed-secret" {
				t.Fatal("刷新后的 Cookie 未保存")
			}
			if (saved.LastValidated != acc.LastValidated) != (scenario.wantStatus == "active") {
				t.Fatal("最近验证时间应仅在全部检测成功后更新")
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "secret") {
				t.Fatalf("检测响应泄露秘密: %s", raw)
			}
		})
	}
}

func TestCheckAccountDoesNotOverwriteConcurrentChanges(t *testing.T) {
	for _, change := range []string{"cookies", "mailbox", "metadata", "delete"} {
		t.Run(change, func(t *testing.T) {
			m, err := NewManager(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			m.accounts["acc_test"] = &Account{ID: "acc_test", Name: "旧名称", Cookies: map[string]string{"token": "old"}, Status: "pending", Mailbox: &MailboxConfig{Email: "owner@example.com", Password: "old", IMAPHost: "imap.example.com", IMAPPort: 993}}
			_, err = m.checkAccount("acc_test", func(snap *Account) {
				snap.Status = "active"
				snap.Cookies["token"] = "refreshed"
				m.mu.Lock()
				defer m.mu.Unlock()
				switch change {
				case "cookies":
					m.accounts["acc_test"].Cookies["token"] = "new"
				case "mailbox":
					m.accounts["acc_test"].Mailbox.Password = "new"
				case "metadata":
					m.accounts["acc_test"].Name = "新名称"
				case "delete":
					delete(m.accounts, "acc_test")
				}
			}, func(config MailboxConfig) error { return nil })
			switch change {
			case "cookies", "mailbox":
				if !errors.Is(err, ErrCheckCredentialsChanged) {
					t.Fatalf("want conflict, got %v", err)
				}
				if m.accounts["acc_test"].Status != "pending" {
					t.Fatal("旧检测覆盖了账号状态")
				}
			case "metadata":
				if err != nil || m.accounts["acc_test"].Name != "新名称" {
					t.Fatal("检测不应覆盖名称编辑", err)
				}
			case "delete":
				if err == nil || len(m.accounts) != 0 {
					t.Fatal("检测不应恢复已删除账号")
				}
			}
		})
	}
}

func TestCheckAccountSaveFailure(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.accounts["acc_test"] = &Account{ID: "acc_test", Status: "active"}
	m.dataFile = filepath.Join(m.dataDir, "missing", "accounts.json")
	if _, err := m.CheckAccount("acc_test"); err == nil {
		t.Fatal("保存失败时不应报告检测成功")
	}
	acc, _ := m.GetAccount("acc_test")
	if acc.Status != "active" {
		t.Fatal("保存失败后应保留之前的状态")
	}
}
