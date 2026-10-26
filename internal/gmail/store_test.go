package gmail

import (
	"strings"
	"testing"

	"icloud-hme/internal/mail"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestImportParsesFourFields(t *testing.T) {
	s := newTestStore(t)
	res, err := s.Import("user@gmail.com----login-pass----JBSWY3DPEHPK3PXP----abcd efgh ijkl mnop\nbad-line\n", "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Imported != 1 || res.Invalid != 1 {
		t.Fatalf("导入统计不符: %+v", res)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "第 2 行") {
		t.Fatalf("错误信息不符: %+v", res.Errors)
	}

	accounts, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("应有 1 条账号,得到 %d", len(accounts))
	}
	got := accounts[0]
	if got.Email != "user@gmail.com" || !got.HasPassword || !got.HasTOTP || !got.HasAppPassword {
		t.Fatalf("账号字段不符: %+v", got)
	}

	// 密码类字段只写不读
	acc, ok := s.Get(got.ID)
	if !ok {
		t.Fatal("Get 失败")
	}
	if acc.Password != "login-pass" || acc.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("密码字段不符: %+v", acc)
	}
	// 应用专用密码里的空格会被去掉
	if acc.AppPassword != "abcdefghijklmnop" {
		t.Fatalf("应用专用密码未去空格: %q", acc.AppPassword)
	}
}

func TestImportUpdatesExistingEmail(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Import("a@gmail.com----p1----totp1----app1", ""); err != nil {
		t.Fatalf("首次导入: %v", err)
	}
	// 同一邮箱再次导入：空字段（这里跳过 2FA 那一列）不覆盖已有值
	res, err := s.Import("a@gmail.com----p1--------app2", "")
	if err != nil {
		t.Fatalf("二次导入: %v", err)
	}
	if res.Imported != 0 || res.Updated != 1 {
		t.Fatalf("二次导入应为更新: %+v", res)
	}
	accounts, _ := s.List()
	if len(accounts) != 1 {
		t.Fatalf("同邮箱应只有一条记录,得到 %d", len(accounts))
	}
	acc, _ := s.Get(accounts[0].ID)
	if acc.Password != "p1" || acc.TOTPSecret != "totp1" {
		t.Fatalf("空字段不应覆盖已有值: %+v", acc)
	}
	if acc.AppPassword != "app2" {
		t.Fatalf("应用专用密码应更新: %q", acc.AppPassword)
	}
}

func TestCustomSeparatorAndComment(t *testing.T) {
	s := newTestStore(t)
	res, err := s.Import("# 注释\nb@gmail.com:::p:::totp:::app\n", ":::")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Imported != 1 {
		t.Fatalf("导入统计不符: %+v", res)
	}
}

func TestFilterMatch(t *testing.T) {
	cases := []struct {
		name   string
		filter Filter
		msg    mail.Message
		want   bool
	}{
		{
			name:   "空过滤放行全部",
			filter: Filter{},
			msg:    mail.Message{Subject: "Hello", From: "a@x.com"},
			want:   true,
		},
		{
			name:   "命中主题关键词",
			filter: Filter{Include: []string{"验证码"}},
			msg:    mail.Message{Subject: "您的验证码是 1234"},
			want:   true,
		},
		{
			name:   "命中发件人关键词且忽略大小写",
			filter: Filter{Include: []string{"noreply"}},
			msg:    mail.Message{Subject: "hi", From: "NoReply@Example.com"},
			want:   true,
		},
		{
			name:   "未命中包含关键词",
			filter: Filter{Include: []string{"账单"}},
			msg:    mail.Message{Subject: "欢迎注册", From: "a@x.com"},
			want:   false,
		},
		{
			name:   "排除优先于包含",
			filter: Filter{Include: []string{"通知"}, Exclude: []string{"广告"}},
			msg:    mail.Message{Subject: "通知：广告位招租"},
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.Match(tc.msg); got != tc.want {
				t.Fatalf("Match = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterIsGlobalAndPersisted(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	got, err := s.SetFilter(Filter{
		Include: []string{" 验证码 ", "验证码", ""},
		Exclude: []string{"广告"},
		Days:    500,
	})
	if err != nil {
		t.Fatalf("SetFilter: %v", err)
	}
	if len(got.Include) != 1 || got.Include[0] != "验证码" {
		t.Fatalf("关键词应去空白去重: %+v", got.Include)
	}
	if got.Days != maxFilterDays {
		t.Fatalf("天数应被限制为 %d,得到 %d", maxFilterDays, got.Days)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 过滤是全局设置，重新打开后仍然生效
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatalf("重新打开: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	again := reopened.Filter()
	if len(again.Include) != 1 || again.Days != maxFilterDays {
		t.Fatalf("全局过滤未持久化: %+v", again)
	}
}

func TestUpdateKeepsSecretsWhenBlank(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "d@gmail.com", Password: "p", AppPassword: "app"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	accounts, _ := s.List()
	id := accounts[0].ID

	if _, err := s.Update(id, Input{Note: "备注"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	acc, _ := s.Get(id)
	if acc.Password != "p" || acc.AppPassword != "app" || acc.Note != "备注" {
		t.Fatalf("留空字段不应清空密码: %+v", acc)
	}
}

func TestDuplicateEmailRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(Input{Email: "e@gmail.com"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Create(Input{Email: "E@gmail.com"}); err == nil {
		t.Fatal("重复邮箱应报错")
	}
}
