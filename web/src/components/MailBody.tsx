import { useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Segmented, Typography } from 'antd'
import type { FullMessage } from '../api/types'

type ViewMode = 'html' | 'text'

/** iframe 内部兜底排版：邮件自带样式优先，这里只保证可读性 */
const FRAME_STYLE = `
:root { color-scheme: light; }
html, body { margin: 0; padding: 0; }
body {
  padding: 14px;
  background: #ffffff;
  color: #1f2937;
  font: 14px/1.6 -apple-system, BlinkMacSystemFont, "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
  word-break: break-word;
  overflow-wrap: anywhere;
}
img { max-width: 100%; height: auto; }
table { max-width: 100%; }
a { color: #0f766e; }
pre { white-space: pre-wrap; }
`

function buildSrcDoc(html: string): string {
  // target="_blank" 让邮件里的链接不会把父页面导航走
  return `<!doctype html><html><head><meta charset="utf-8"><base target="_blank"><style>${FRAME_STYLE}</style></head><body>${html}</body></html>`
}

/** 正文为空时按内容类型给出可判断的说明，而不是笼统的「无正文」 */
function emptyHint(contentType: string): string {
  const type = (contentType || '').toLowerCase()
  if (type.startsWith('multipart/')) {
    return '这封邮件的多部分结构里没有可读的纯文本或 HTML 正文（通常只包含附件）。'
  }
  if (type.startsWith('application/') || type.startsWith('image/') || type.startsWith('audio/') || type.startsWith('video/')) {
    return `这封邮件没有正文，内容类型为 ${contentType}。`
  }
  return '（无正文）'
}

/**
 * 邮件正文渲染。
 *
 * HTML 正文放进 sandbox iframe：只授予 allow-same-origin（用于测量高度），
 * 不授予 allow-scripts，因此邮件里的脚本无法执行，邮件样式也隔离在 iframe 内。
 * 服务端另有清理（移除 script / 事件属性 / javascript: 协议）作为第二道防线。
 */
export default function MailBody({ message }: { message: FullMessage }) {
  // 界面渲染清理后的版本(纵深防御);原始 HTML 由接口的 body_html 提供给外部程序
  const html = message.body_html_sanitized || message.body_html || ''
  const hasHTML = Boolean(html)
  const [mode, setMode] = useState<ViewMode>(hasHTML ? 'html' : 'text')
  const frameRef = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState(320)

  // 远程图片会被 CSP 的 img-src 拦下（隐私保护：避免追踪像素）
  const hasRemoteImages = useMemo(
    () => /<img[^>]+src\s*=\s*["']?https?:/i.test(html),
    [html],
  )

  // iframe 高度跟随内容，避免正文被裁掉或出现大片空白
  useEffect(() => {
    if (mode !== 'html') return
    const frame = frameRef.current
    if (!frame) return

    let observer: ResizeObserver | null = null

    const measure = () => {
      const doc = frame.contentDocument
      if (!doc) return
      const next = Math.max(doc.documentElement?.scrollHeight ?? 0, doc.body?.scrollHeight ?? 0)
      if (next <= 0) return
      const capped = Math.min(next + 4, 1400)
      setHeight((prev) => (Math.abs(prev - capped) > 1 ? capped : prev))
    }

    const attach = () => {
      measure()
      const doc = frame.contentDocument
      if (!doc?.documentElement) return
      observer?.disconnect()
      observer = new ResizeObserver(() => requestAnimationFrame(measure))
      observer.observe(doc.documentElement)
    }

    // 用 rAF 把首次测量移出 effect 同步执行阶段
    const raf = requestAnimationFrame(attach)
    frame.addEventListener('load', attach)
    return () => {
      cancelAnimationFrame(raf)
      frame.removeEventListener('load', attach)
      observer?.disconnect()
    }
  }, [mode, message.id, html])

  if (!hasHTML && !message.body) {
    return <pre className="mail-body">{emptyHint(message.content_type)}</pre>
  }

  if (!hasHTML) {
    return <pre className="mail-body">{message.body}</pre>
  }

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 8 }}>
        <Segmented
          size="small"
          value={mode}
          onChange={(value) => setMode(value as ViewMode)}
          options={[
            { value: 'html', label: '排版' },
            { value: 'text', label: '纯文本' },
          ]}
        />
      </div>

      {mode === 'html' ? (
        <iframe
          ref={frameRef}
          title="邮件正文"
          sandbox="allow-same-origin"
          srcDoc={buildSrcDoc(html)}
          style={{ width: '100%', height, border: '1px solid #e6e8eb', borderRadius: 8, background: '#fff' }}
        />
      ) : (
        <pre className="mail-body">{message.body || emptyHint(message.content_type)}</pre>
      )}

      {mode === 'html' && hasRemoteImages && (
        <Alert
          type="info"
          showIcon
          title="远程图片已被阻止"
          description="为避免追踪像素泄露阅读状态与 IP，浏览器安全策略只允许白名单外的图片来源。需要查看图片时可在服务端放开 img-src。"
          style={{ marginTop: 12 }}
        />
      )}

      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 12, marginBottom: 0 }}>
        正文在隔离沙箱中渲染：脚本不会执行，样式不会影响管理界面本身。
      </Typography.Paragraph>
    </div>
  )
}
