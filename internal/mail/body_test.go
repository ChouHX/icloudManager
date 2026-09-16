package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/quotedprintable"
	"strings"
	"testing"
)

func qpEncode(s string) string {
	var buf bytes.Buffer
	w := quotedprintable.NewWriter(&buf)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return buf.String()
}

func b64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// multipartMixedSample 复刻真实 iCloud 收信结构:
//
//	multipart/mixed
//	├── multipart/alternative
//	│   ├── text/plain (quoted-printable)
//	│   └── text/html (base64)
//	└── application/ics 附件 (base64)
//
// 这正是导致「无正文」的形态:旧实现按顶层 Content-Type 读取,
// 把 MIME 边界与各部分头部当成正文,清理后变成空串。
func multipartMixedSample() string {
	const boundaryMixed = "----=_Part_2_1161441528.1789545936523"
	const boundaryAlt = "----=_Part_1_1161441528.1"

	plain := "验证码：654321\n\n请在 10 分钟内完成验证。"
	html := `<html><head><style>body{color:red}</style></head><body><p>验证码：<b>654321</b></p></body></html>`
	ics := "BEGIN:VCALENDAR\nVERSION:2.0\nEND:VCALENDAR"

	var b strings.Builder
	fmt.Fprintf(&b, "From: OpenStage <no-reply@openstageit.com>\r\n")
	fmt.Fprintf(&b, "To: acrylic-tizzy7r@icloud.com\r\n")
	fmt.Fprintf(&b, "Subject: 验证你的邮箱\r\n")
	fmt.Fprintf(&b, "Date: Tue, 16 Sep 2026 16:05:00 +0800\r\n")
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundaryMixed)

	fmt.Fprintf(&b, "--%s\r\n", boundaryMixed)
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundaryAlt)

	fmt.Fprintf(&b, "--%s\r\n", boundaryAlt)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	fmt.Fprintf(&b, "%s\r\n", qpEncode(plain))

	fmt.Fprintf(&b, "--%s\r\n", boundaryAlt)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: base64\r\n\r\n")
	fmt.Fprintf(&b, "%s\r\n", b64Encode(html))
	fmt.Fprintf(&b, "--%s--\r\n", boundaryAlt)

	fmt.Fprintf(&b, "--%s\r\n", boundaryMixed)
	fmt.Fprintf(&b, "Content-Type: application/ics; name=\"invite.ics\"\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&b, "Content-Disposition: attachment; filename=\"invite.ics\"\r\n\r\n")
	fmt.Fprintf(&b, "%s\r\n", b64Encode(ics))
	fmt.Fprintf(&b, "--%s--\r\n", boundaryMixed)

	return b.String()
}

func TestBuildBodyReadsPlainFromMultipartMixed(t *testing.T) {
	parts := buildBody(strings.NewReader(multipartMixedSample()))

	if !strings.Contains(parts.Plain, "验证码：654321") {
		t.Fatalf("未提取到纯文本正文,得到: %q", parts.Plain)
	}
	if !strings.Contains(parts.Plain, "10 分钟内完成验证") {
		t.Fatalf("正文被截断,得到: %q", parts.Plain)
	}
	if strings.Contains(parts.Plain, "_Part_") {
		t.Fatalf("正文混入 MIME 边界: %q", parts.Plain)
	}
	if strings.Contains(parts.Plain, "VCALENDAR") {
		t.Fatalf("附件内容混入正文: %q", parts.Plain)
	}
}

// TestBuildBodyKeepsHTMLForRendering 覆盖界面按 HTML 渲染的需求:
// HTML 部分必须原样保留(含排版标签),同时另给纯文本视图。
func TestBuildBodyKeepsHTMLForRendering(t *testing.T) {
	parts := buildBody(strings.NewReader(multipartMixedSample()))

	if parts.HTML == "" {
		t.Fatal("HTML 正文未被保留,界面无法渲染排版")
	}
	if !strings.Contains(parts.HTML, "<b>654321</b>") {
		t.Fatalf("HTML 结构被破坏: %q", parts.HTML)
	}
	if !strings.Contains(parts.HTML, "color:red") {
		t.Fatalf("邮件自身样式被误删,渲染会失真: %q", parts.HTML)
	}
	if strings.Contains(parts.HTML, "_Part_") {
		t.Fatalf("HTML 混入 MIME 边界: %q", parts.HTML)
	}
	if strings.Contains(parts.HTML, "VCALENDAR") {
		t.Fatalf("附件内容混入 HTML: %q", parts.HTML)
	}
	if parts.contentType() != "text/html" {
		t.Fatalf("有 HTML 时渲染类型应为 text/html,得到 %q", parts.contentType())
	}
	// 纯文本视图仍取自邮件自带的 text/plain
	if !strings.Contains(parts.Plain, "验证码：654321") {
		t.Fatalf("纯文本视图异常: %q", parts.Plain)
	}
}

func TestBuildBodyFallsBackToHTMLWhenNoPlain(t *testing.T) {
	html := `<html><head><style>body{color:red}</style></head><body><p>确认链接：</p><a href="https://example.com/x">点这里</a></body></html>`
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		b64Encode(html) + "\r\n"

	parts := buildBody(strings.NewReader(raw))

	if !strings.Contains(parts.Plain, "确认链接：") || !strings.Contains(parts.Plain, "点这里") {
		t.Fatalf("未从 HTML 反推纯文本,得到: %q", parts.Plain)
	}
	if strings.Contains(parts.Plain, "color:red") {
		t.Fatalf("纯文本视图混入样式: %q", parts.Plain)
	}
	if !strings.Contains(parts.HTML, "example.com/x") {
		t.Fatalf("HTML 正文未被保留: %q", parts.HTML)
	}
	if parts.contentType() != "text/html" {
		t.Fatalf("contentType = %q, want text/html", parts.contentType())
	}
}

func TestBuildBodyDecodesBase64PlainPart(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		b64Encode("Base64 编码的纯文本正文") + "\r\n"

	parts := buildBody(strings.NewReader(raw))

	if parts.Plain != "Base64 编码的纯文本正文" {
		t.Fatalf("base64 正文未解码,得到: %q", parts.Plain)
	}
	if parts.HTML != "" {
		t.Fatalf("纯文本邮件不应产出 HTML: %q", parts.HTML)
	}
	if parts.contentType() != "text/plain" {
		t.Fatalf("contentType = %q, want text/plain", parts.contentType())
	}
}

func TestBuildBodyConvertsCharset(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=iso-8859-1\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		"caf\xe9 latte\r\n"

	parts := buildBody(strings.NewReader(raw))

	if !strings.Contains(parts.Plain, "café") {
		t.Fatalf("字符集未转换,得到: %q", parts.Plain)
	}
}

func TestBuildBodyAttachmentOnlyReturnsTopLevelType(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BOUND\"\r\n\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/pdf; name=\"a.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"Content-Disposition: attachment; filename=\"a.pdf\"\r\n\r\n" +
		b64Encode("%PDF-1.4 fake") + "\r\n" +
		"--BOUND--\r\n"

	parts := buildBody(strings.NewReader(raw))

	if parts.Plain != "" || parts.HTML != "" {
		t.Fatalf("附件邮件不应产出正文,得到 plain=%q html=%q", parts.Plain, parts.HTML)
	}
	if !strings.HasPrefix(parts.contentType(), "multipart/mixed") {
		t.Fatalf("contentType = %q, want 顶层 multipart/mixed", parts.contentType())
	}
}

// TestBuildBodySurvivesInlineStyleHeavyHTML 覆盖真实平台邮件的形态:
// HTML 部分充满内联样式(mso-*、-webkit-* 与大量分号)。
//
// 旧实现按顶层 multipart/mixed 当纯文本读,这类原文会被 sanitizePlainPreview
// 判定成 CSS 并清理成空串,界面因而显示「无正文」。这里断言正文仍被取到。
func TestBuildBodySurvivesInlineStyleHeavyHTML(t *testing.T) {
	html := `<div style="font-family:Arial,sans-serif; mso-line-height-rule:exactly; ` +
		`-webkit-text-size-adjust:none; border-collapse:collapse; color:#333;">验证码 654321</div>` +
		`<table style="mso-table-lspace:0pt; mso-table-rspace:0pt; border-collapse:collapse;">` +
		`<tr><td style="font-family:Arial; -webkit-text-size-adjust:none;">请在 10 分钟内完成验证</td></tr></table>`

	const boundaryMixed = "----=_Part_2_1161441528.1789545936523"
	const boundaryAlt = "----=_Part_1_1"

	var b strings.Builder
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundaryMixed)
	fmt.Fprintf(&b, "--%s\r\n", boundaryMixed)
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundaryAlt)
	fmt.Fprintf(&b, "--%s\r\n", boundaryAlt)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	fmt.Fprintf(&b, "%s\r\n", qpEncode("验证码：654321"))
	fmt.Fprintf(&b, "--%s\r\n", boundaryAlt)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	fmt.Fprintf(&b, "%s\r\n", qpEncode(html))
	fmt.Fprintf(&b, "--%s--\r\n", boundaryAlt)
	fmt.Fprintf(&b, "--%s--\r\n", boundaryMixed)

	parts := buildBody(strings.NewReader(b.String()))

	if parts.Plain == "" {
		t.Fatal("正文被清理成空串(旧实现的表现)")
	}
	if !strings.Contains(parts.Plain, "验证码：654321") {
		t.Fatalf("未取到纯文本正文,得到: %q", parts.Plain)
	}
	if strings.Contains(parts.Plain, "mso-") || strings.Contains(parts.Plain, "_Part_") {
		t.Fatalf("纯文本视图混入样式或 MIME 边界: %q", parts.Plain)
	}
	if !strings.Contains(parts.HTML, "654321") {
		t.Fatalf("HTML 正文缺失: %q", parts.HTML)
	}
}

func TestBuildBodyHandlesGarbage(t *testing.T) {
	for _, raw := range []string{"", "not a mail at all", "Content-Type: multipart/mixed\r\n\r\n", "\x00\x01garbage"} {
		parts := buildBody(strings.NewReader(raw))
		if strings.Contains(parts.Plain, "multipart") || strings.Contains(parts.HTML, "multipart") {
			t.Fatalf("畸形输入不应产出正文,plain=%q html=%q", parts.Plain, parts.HTML)
		}
	}
}

// TestBuildBodyKeepsPlainAndHTMLSeparate 覆盖 alternative 中 HTML 在前、纯文本在后的顺序。
func TestBuildBodyKeepsPlainAndHTMLSeparate(t *testing.T) {
	const boundary = "BOUND"
	html := `<p>HTML 版本</p>`
	plain := "纯文本正文"

	var b strings.Builder
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n", html)
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", plain)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)

	parts := buildBody(strings.NewReader(b.String()))

	if parts.Plain != plain {
		t.Fatalf("纯文本视图应为发件人的 text/plain,得到: %q", parts.Plain)
	}
	if !strings.Contains(parts.HTML, "HTML 版本") {
		t.Fatalf("HTML 视图缺失: %q", parts.HTML)
	}
	if parts.contentType() != "text/html" {
		t.Fatalf("contentType = %q, want text/html", parts.contentType())
	}
}

// TestBuildBodyKeepsHTMLVerbatim 锁定"服务端不加工 HTML"的契约:
// 原始 HTML 原样保留,清理是调用方按需索取的另一份数据。
func TestBuildBodyKeepsHTMLVerbatim(t *testing.T) {
	html := `<html><body onload="boot()"><script>evil()</script><p>验证码：654321</p></body></html>`
	raw := "MIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" + html + "\r\n"

	parts := buildBody(strings.NewReader(raw))

	if !strings.Contains(parts.HTML, "<script>evil()</script>") {
		t.Fatalf("原始 HTML 应原样保留 script,得到: %q", parts.HTML)
	}
	if !strings.Contains(parts.HTML, `onload="boot()"`) {
		t.Fatalf("原始 HTML 应保留事件属性,得到: %q", parts.HTML)
	}
	if !strings.Contains(parts.HTML, "654321") {
		t.Fatalf("原始 HTML 内容缺失: %q", parts.HTML)
	}

	// 同一份 HTML 的清理版应移除可执行内容
	sanitized := sanitizeMailHTML(parts.HTML)
	for _, bad := range []string{"<script", "onload"} {
		if strings.Contains(strings.ToLower(sanitized), bad) {
			t.Fatalf("清理版仍含 %q: %s", bad, sanitized)
		}
	}
	if !strings.Contains(sanitized, "654321") {
		t.Fatalf("清理版不应丢掉正文: %s", sanitized)
	}

	// 纯文本视图照旧可用(用于复制 / OTP 提取)
	if !strings.Contains(parts.Plain, "验证码：654321") {
		t.Fatalf("纯文本视图异常: %q", parts.Plain)
	}
}
