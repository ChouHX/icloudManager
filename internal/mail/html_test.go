package mail

import (
	"strings"
	"testing"
)

// sanitizeMailHTML 是 sandbox iframe 之外的纵深防御,这里固化它必须拦下的东西。
func TestSanitizeMailHTMLRemovesExecutableContent(t *testing.T) {
	raw := `<div>正文</div>` +
		`<script>alert(1)</script>` +
		`<SCRIPT SRC="https://evil.example/x.js"></SCRIPT>` +
		`<iframe src="https://evil.example"></iframe>` +
		`<object data="x.swf"></object>` +
		`<embed src="x.swf">` +
		`<img src="logo.png" onerror="alert(1)" onload='steal()'>` +
		`<a href="javascript:alert(1)">点我</a>` +
		`<a href="JaVaScRiPt:alert(2)">再点</a>` +
		`<base href="https://evil.example/">` +
		`<meta http-equiv="refresh" content="0;url=https://evil.example">`

	out := sanitizeMailHTML(raw)

	for _, bad := range []string{"<script", "<SCRIPT", "<iframe", "<object", "<embed", "onerror", "onload", "javascript:", "JaVaScRiPt:", "<base", "http-equiv"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(bad)) {
			t.Fatalf("HTML 清理遗漏 %q: %s", bad, out)
		}
	}
	for _, keep := range []string{"正文", "logo.png", "点我", "再点"} {
		if !strings.Contains(out, keep) {
			t.Fatalf("清理过度,丢失 %q: %s", keep, out)
		}
	}
}

func TestSanitizeMailHTMLKeepsRenderableStructure(t *testing.T) {
	raw := `<table width="600" style="border-collapse:collapse;"><tr><td>` +
		`<img src="data:image/png;base64,iVBORw0KGgo=" alt="logo">` +
		`<a href="https://example.com/verify" target="_blank">验证</a>` +
		`</td></tr></table>` +
		`<style>.x{color:#0f766e;}</style>`

	out := sanitizeMailHTML(raw)

	for _, keep := range []string{"<table", "border-collapse:collapse", "data:image/png;base64", "https://example.com/verify", "<style>", "target=\"_blank\""} {
		if !strings.Contains(out, keep) {
			t.Fatalf("应保留 %q,实际: %s", keep, out)
		}
	}
}

func TestSanitizeMailHTMLNeutralizesDataHTML(t *testing.T) {
	out := sanitizeMailHTML(`<a href="data:text/html;base64,PHNjcmlwdD4=">x</a>`)
	if strings.Contains(out, "data:text/html") {
		t.Fatalf("非图片 data: URL 应被中和: %s", out)
	}
}

func TestSanitizeMailHTMLTruncatesOversizedBody(t *testing.T) {
	huge := "<p>" + strings.Repeat("很长的正文内容", maxMailHTMLBytes/10) + "</p>"
	out := sanitizeMailHTML(huge)

	if len(out) > maxMailHTMLBytes+64 {
		t.Fatalf("超长 HTML 未被截断,长度 %d", len(out))
	}
	if !strings.Contains(out, "已截断") {
		t.Fatal("截断后应留下标记")
	}
	// 截断不能切坏 UTF-8
	if strings.Contains(out, "\ufffd") {
		t.Fatal("截断产生了非法 UTF-8 字符")
	}
}

func TestSanitizeMailHTMLStripsControlCharacters(t *testing.T) {
	out := sanitizeMailHTML("<p>a\x00b\x07c</p>")
	if strings.Contains(out, "\x00") || strings.Contains(out, "\x07") {
		t.Fatalf("控制字符未清理: %q", out)
	}
}
