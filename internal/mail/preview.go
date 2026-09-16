package mail

import (
	stdhtml "html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxMailHTMLBytes 限制单封邮件用于渲染的 HTML 体积。
const maxMailHTMLBytes = 1 << 20

var (
	htmlTagRE        = regexp.MustCompile(`(?s)<[^>]+>`)
	htmlNoiseBlockRE = regexp.MustCompile(`(?is)<(style|script|head|title|noscript)\b[^>]*>.*?</(style|script|head|title|noscript)\s*>`)
	htmlCommentRE    = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlBreakRE      = regexp.MustCompile(`(?i)<br\s*/?>`)
	htmlBlockEndRE   = regexp.MustCompile(`(?i)</(p|div|tr|h[1-6])\s*>`)
	htmlListItemRE   = regexp.MustCompile(`(?i)<li\b[^>]*>`)

	cssSignalRE      = regexp.MustCompile(`(?i)(@font-face|@media|@import|@supports|@keyframes|(^|[\s;{])(-webkit-|-moz-|-ms-|mso-)[\w-]*|(^|[\s;{])(font-family|text-size-adjust|border-collapse|mso-table-[\w-]+)\s*:)`)
	cssDeclarationRE = regexp.MustCompile(`(?i)(^|[;{])\s*[-a-z_][\w-]*\s*:\s*[^;{}]+`)

	// 邮件 HTML 清理规则(渲染前的纵深防御,见 sanitizeMailHTML)
	mailScriptBlockRE = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>|<script\b[^>]*/?>`)
	mailFrameBlockRE  = regexp.MustCompile(`(?is)<(iframe|frame|frameset|object|embed|applet|portal)\b[^>]*>.*?</(iframe|frame|frameset|object|applet)\s*>|<(iframe|frame|frameset|object|embed|applet|portal)\b[^>]*/?>`)
	mailBaseMetaRE    = regexp.MustCompile(`(?is)<base\b[^>]*>|<meta\b[^>]*http-equiv\s*=\s*["']?\s*refresh[^>]*>`)
	mailEventAttrRE   = regexp.MustCompile(`(?i)\son[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	mailURLAttrRE     = regexp.MustCompile(`(?i)\b(href|src|xlink:href|action|formaction|background)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
	mailCtrlRE        = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`)
)

// sanitizeMailHTML 清理邮件 HTML,使其可以放进 sandbox iframe 安全渲染。
//
// 这是 iframe sandbox 之外的纵深防御:移除可执行内容(script、object、embed)、
// 会劫持页面导航的标签(base、meta refresh)、内联事件属性(on*)以及
// javascript:/vbscript: 之类的 URL 协议。邮件自身的 <style> 保留,
// 它只在 iframe 内起作用,同时也决定邮件的排版还原度。
func sanitizeMailHTML(raw string) string {
	if raw == "" {
		return ""
	}
	out := mailCtrlRE.ReplaceAllString(raw, "")
	out = mailScriptBlockRE.ReplaceAllString(out, "")
	out = mailFrameBlockRE.ReplaceAllString(out, "")
	out = mailBaseMetaRE.ReplaceAllString(out, "")
	out = mailEventAttrRE.ReplaceAllString(out, "")
	out = mailURLAttrRE.ReplaceAllStringFunc(out, neutralizeDangerousURL)

	if len(out) > maxMailHTMLBytes {
		out = truncateUTF8(out, maxMailHTMLBytes) + "\n<!-- 正文过长,已截断 -->"
	}
	return out
}

// neutralizeDangerousURL 把危险协议的属性值替换为 "#",保留原有引号风格。
func neutralizeDangerousURL(match string) string {
	eq := strings.IndexByte(match, '=')
	if eq < 0 {
		return match
	}
	name := strings.TrimSpace(match[:eq])
	raw := strings.TrimSpace(match[eq+1:])

	quote := ""
	value := raw
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') {
		quote = string(raw[0])
		value = strings.TrimSuffix(raw[1:], quote)
	}

	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(lower, "javascript:") ||
		strings.HasPrefix(lower, "vbscript:") ||
		(strings.HasPrefix(lower, "data:") && !strings.HasPrefix(lower, "data:image/")) {
		return name + "=" + quote + "#" + quote
	}
	return match
}

// truncateUTF8 按字节上限截断,且不切断多字节字符。
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// sanitizePreview converts an email preview to readable text and removes
// presentation-only HTML/CSS that mail clients commonly include.
func sanitizePreview(raw string) string {
	text := stripHTML(raw)
	return sanitizePlainPreview(text)
}

func sanitizePlainPreview(raw string) string {
	text := normalizePreview(raw)
	if text == "" || !looksLikeCSS(text) {
		return text
	}

	// Some upstream APIs return the contents of a <style> block without the
	// surrounding tags. Remove balanced CSS rules while preserving any text
	// that follows the stylesheet.
	text = normalizePreview(stripCSSRules(text))
	if text == "" || looksLikeCSS(text) {
		return ""
	}
	return text
}

// stripHTML removes tags and invisible document sections while keeping common
// line breaks so the preview remains readable.
func stripHTML(raw string) string {
	raw = htmlNoiseBlockRE.ReplaceAllString(raw, "")
	raw = htmlCommentRE.ReplaceAllString(raw, "")
	raw = htmlBreakRE.ReplaceAllString(raw, "\n")
	raw = htmlBlockEndRE.ReplaceAllString(raw, "\n")
	raw = htmlListItemRE.ReplaceAllString(raw, "\n- ")
	raw = htmlTagRE.ReplaceAllString(raw, "")
	return normalizePreview(stdhtml.UnescapeString(raw))
}

func normalizePreview(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func looksLikeCSS(text string) bool {
	braces := strings.Count(text, "{")
	semicolons := strings.Count(text, ";")
	signals := len(cssSignalRE.FindAllStringIndex(text, -1))
	declarations := len(cssDeclarationRE.FindAllStringIndex(text, -1))
	trimmed := strings.TrimSpace(text)
	selectorStart := strings.HasPrefix(trimmed, ".") ||
		strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "@")

	if braces == 0 {
		return signals >= 2 && semicolons >= 3
	}
	if selectorStart && declarations >= 1 {
		return true
	}
	return (signals >= 2 && semicolons >= 2) ||
		(braces >= 3 && declarations >= 3 && semicolons >= 3)
}

func stripCSSRules(text string) string {
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			return text
		}
		close := matchingBrace(text, open)
		if close < 0 {
			return text
		}

		start := open
		for start > 0 {
			if text[start-1] == '}' || text[start-1] == '\n' || text[start-1] == '\r' {
				break
			}
			start--
		}
		text = text[:start] + text[close+1:]
	}
}

func matchingBrace(text string, open int) int {
	depth := 0
	var quote byte
	escaped := false
	for i := open; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
