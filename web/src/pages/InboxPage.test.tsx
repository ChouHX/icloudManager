import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import InboxPage from './InboxPage'
import { renderPage } from '../test/render'

function renderInbox() {
  return renderPage(<InboxPage />, { route: '/inbox?account_id=acc_1', path: '/inbox' })
}

describe('InboxPage', () => {
  it('渲染邮件列表与读取方式', async () => {
    renderInbox()

    expect(await screen.findByText('请验证你的邮箱')).toBeInTheDocument()
    expect(screen.getByText('GitHub <noreply@github.com>')).toBeInTheDocument()
    expect(screen.getByText(/IMAP/)).toBeInTheDocument()
  })

  it('默认按 HTML 渲染正文,并可切换纯文本', async () => {
    // antd Segmented 的隐藏 radio 带 pointer-events: none
    const user = userEvent.setup({ pointerEventsCheck: 0 })
    renderInbox()
    await screen.findByText('请验证你的邮箱')

    await user.click(screen.getByRole('button', { name: '请验证你的邮箱' }))

    // 有 HTML 正文时用隔离 iframe 渲染排版
    const frame = await screen.findByTitle('邮件正文')
    expect(frame).toHaveAttribute('sandbox', 'allow-same-origin')
    expect(frame.getAttribute('srcdoc')).toContain('点击链接完成验证')
    expect(frame.getAttribute('srcdoc')).toContain('https://example.com/verify')
    // 界面渲染的是清理版:原始 HTML 里的脚本与事件属性不应进入 iframe
    expect(frame.getAttribute('srcdoc')).not.toContain('<script>')
    expect(frame.getAttribute('srcdoc')).not.toContain('onclick')
    expect(screen.getByText('内容类型')).toBeInTheDocument()

    // 切到纯文本视图
    await user.click(screen.getByRole('radio', { name: '纯文本' }))
    expect(screen.getByText(/点击链接完成验证/)).toBeInTheDocument()
  })

  it('无正文时按内容类型给出说明', async () => {
    const user = userEvent.setup()
    const { server } = await import('../test/server')
    const { http, HttpResponse } = await import('msw')
    server.use(
      http.get('/api/inbox/:id', () =>
        HttpResponse.json({
          success: true,
          data: { id: '1042', from: 'a@b.com', to: 'alpha@icloud.com', subject: '只有附件', date: '', preview: '', body: '', content_type: 'multipart/mixed; boundary="B"' },
        }),
      ),
    )

    renderInbox()
    await screen.findByText('请验证你的邮箱')
    await user.click(screen.getByRole('button', { name: '请验证你的邮箱' }))

    expect(await screen.findByText(/多部分结构里没有可读的纯文本或 HTML 正文/)).toBeInTheDocument()
  })

  it('删除邮件后重新拉取列表', async () => {
    const user = userEvent.setup()
    let deleteCalled = false
    const { server } = await import('../test/server')
    const { http, HttpResponse } = await import('msw')
    server.use(
      http.delete('/api/inbox/:id', () => {
        deleteCalled = true
        return HttpResponse.json({ success: true, data: { id: '1042' } })
      }),
    )

    renderInbox()
    await screen.findByText('请验证你的邮箱')

    await user.click(screen.getByRole('button', { name: /删\s*除/ }))
    const confirm = await screen.findByRole('tooltip')
    await user.click(within(confirm).getByRole('button', { name: /删\s*除/ }))

    await waitFor(() => expect(deleteCalled).toBe(true))
  })
})
