package mail

import (
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/utf7"
)

func TestIsJunkMailbox(t *testing.T) {
	cases := []struct {
		name string
		info *imap.MailboxInfo
		want bool
	}{
		{"junk 属性", &imap.MailboxInfo{Name: "[Gmail]/垃圾邮件", Attributes: []string{imap.JunkAttr}}, true},
		{"spam 属性", &imap.MailboxInfo{Name: "Junk", Attributes: []string{"\\\\Spam"}}, true},
		{"常见英文名", &imap.MailboxInfo{Name: "Junk E-mail"}, true},
		{"Gmail 英文垃圾箱", &imap.MailboxInfo{Name: "[Gmail]/Spam"}, true},
		{"Gmail 中文垃圾箱", &imap.MailboxInfo{Name: "[Gmail]/垃圾邮件"}, true},
		{"UTF-7 中文垃圾箱", &imap.MailboxInfo{Name: encodeUTF7(t, "[Gmail]/垃圾邮件")}, true},
		{"普通文件夹", &imap.MailboxInfo{Name: "[Gmail]/All Mail"}, false},
		{"收件箱", &imap.MailboxInfo{Name: "INBOX"}, false},
		{"不可选中的垃圾箱", &imap.MailboxInfo{Name: "Spam", Attributes: []string{imap.NoSelectAttr}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isJunkMailbox(tc.info); got != tc.want {
				t.Fatalf("isJunkMailbox(%q) = %v, want %v", tc.info.Name, got, tc.want)
			}
		})
	}
}

func TestToMessageKeepsNumericIDAndFolder(t *testing.T) {
	msg := &imap.Message{Uid: 1042, Envelope: &imap.Envelope{}}

	inbox := toMessage(msg, "")
	if inbox.Folder != InboxFolder {
		t.Fatalf("收件箱 folder = %q, want %q", inbox.Folder, InboxFolder)
	}
	if inbox.ID != "1042" {
		t.Fatalf("收件箱 ID = %q, want 1042", inbox.ID)
	}

	junk := toMessage(msg, "[Gmail]/Spam")
	if junk.Folder != "[Gmail]/Spam" {
		t.Fatalf("垃圾邮件 folder = %q", junk.Folder)
	}
	// ID 始终是 UID,文件夹由 Folder / folder 参数区分,避免 URL 中出现斜杠。
	if junk.ID != "1042" {
		t.Fatalf("垃圾邮件 ID = %q, want 1042", junk.ID)
	}
}

func TestNormalizeFolder(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		if got := normalizeFolder(raw); got != InboxFolder {
			t.Fatalf("normalizeFolder(%q) = %q, want %q", raw, got, InboxFolder)
		}
	}
	if got := normalizeFolder(" [Gmail]/Spam "); got != "[Gmail]/Spam" {
		t.Fatalf("normalizeFolder 应去掉首尾空格,得到 %q", got)
	}
}

// encodeUTF7 生成 IMAP LIST 返回的 modified UTF-7 文件夹名。
func encodeUTF7(t *testing.T, raw string) string {
	t.Helper()
	encoded, err := utf7.Encoding.NewEncoder().String(raw)
	if err != nil {
		t.Fatalf("编码 %q 失败: %v", raw, err)
	}
	return encoded
}

func TestSortRefsDesc(t *testing.T) {
	now := time.Now()
	refs := []messageRef{
		{folder: InboxFolder, uid: 1, date: now.Add(-2 * time.Hour)},
		{folder: "[Gmail]/Spam", uid: 2},
		{folder: InboxFolder, uid: 3, date: now},
	}
	sortRefsDesc(refs)

	want := []uint32{3, 1, 2}
	for i, uid := range want {
		if refs[i].uid != uid {
			t.Fatalf("排序后第 %d 个 uid = %d, want %d", i, refs[i].uid, uid)
		}
	}
}
