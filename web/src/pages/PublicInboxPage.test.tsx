import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import { http, HttpResponse } from 'msw'
import PublicInboxPage from './PublicInboxPage'
import { renderPage } from '../test/render'
import { server } from '../test/server'

function renderPublic(token = 'tok_test_token') {
  return renderPage(<PublicInboxPage token={token} />, { route: `/?token=${token}`, path: '/' })
}

describe('PublicInboxPage', () => {
  it('凭 token 展示该别名的邮件,且不暴露账号信息', async () => {
    renderPublic()

    expect(await screen.findByText('请验证你的邮箱')).toBeInTheDocument()
    // 标题里带出别名,响应里没有 account_id
    expect(screen.getByText('alpha@icloud.com')).toBeInTheDocument()
    expect(screen.queryByText(/acc_1/)).not.toBeInTheDocument()
    expect(screen.getByText(/只读页面/)).toBeInTheDocument()
  })

  it('打开邮件详情', async () => {
    const user = userEvent.setup()
    renderPublic()
    await screen.findByText('请验证你的邮箱')

    await user.click(screen.getByRole('button', { name: '请验证你的邮箱' }))

    const frame = await screen.findByTitle('邮件正文')
    expect(frame.getAttribute('srcdoc')).toContain('点击链接完成验证')
  })

  it('链接无效时给出明确提示', async () => {
    server.use(
      http.get('/api/share/:token/inbox', () =>
        HttpResponse.json(
          { success: false, code: 'SHARE_INVALID', message: '取件链接无效或已失效' },
          { status: 404 },
        ),
      ),
    )
    renderPublic('expired-token')

    expect(await screen.findByText('取件链接无效或已失效')).toBeInTheDocument()
    expect(screen.getByText(/请向链接提供者索取新的取件链接/)).toBeInTheDocument()
    // 无效链接不应渲染邮件表格
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('提供时间范围与条数筛选', async () => {
    renderPublic()
    await screen.findByText('请验证你的邮箱')

    expect(screen.getByText(/收件箱 · 1 封/)).toBeInTheDocument()
    // 时间范围 + 条数两个下拉
    expect(screen.getAllByRole('combobox').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('近 7 天')).toBeInTheDocument()
  })
})
