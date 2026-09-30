import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import AccountsPage from './AccountsPage'
import { renderPage } from '../test/render'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { ACCOUNT } from '../test/handlers'

function renderAccounts() {
  return renderPage(<AccountsPage />, { route: '/accounts', path: '/accounts' })
}

async function accountRow() {
  const name = await screen.findByText('主号')
  return within(name.closest('tr')!)
}

// CI 上 jsdom 渲染 Ant Design 菜单和弹窗较慢；异步断言仍保留各自的等待上限。
describe('AccountsPage', { timeout: 15000 }, () => {
  it('渲染账号摘要与凭据标签', async () => {
    renderAccounts()

    expect(await screen.findByText('主号')).toBeInTheDocument()
    expect(screen.getByText('Cookie')).toBeInTheDocument()
    expect(screen.getByText('App 密码')).toBeInTheDocument()
    expect(screen.getByText('1 / 2')).toBeInTheDocument()
    expect(screen.getByText('正常')).toBeInTheDocument()
  })

  it('从凭据配置菜单打开更新 Cookie 弹窗', async () => {
    const user = userEvent.setup()
    renderAccounts()
    const row = await accountRow()

    await user.click(row.getByRole('button', { name: /凭据配置/ }))
    await user.click(await screen.findByText('更新 Cookie'))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('Cookie')).toBeInTheDocument()
  })

  it('打开添加账号弹窗', async () => {
    const user = userEvent.setup()
    renderAccounts()
    await screen.findByText('主号')

    await user.click(screen.getByRole('button', { name: /添加账号/ }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('名称')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('iCloud 邮箱')).toBeInTheDocument()
  })

  it('检测中禁止重复检测，完成后显示逐项结果并刷新账号状态', async () => {
    const user = userEvent.setup()
    let finishCheck!: () => void
    const pending = new Promise<void>((resolve) => { finishCheck = resolve })
    let checked = false
    let calls = 0
    const updated = { ...ACCOUNT, status: 'error', status_message: '凭据验证失败' }
    server.use(
      http.get('/api/accounts', () => HttpResponse.json({ success: true, data: [checked ? updated : ACCOUNT] })),
      http.post('/api/accounts/:id/check', async ({ params }) => {
        expect(params.id).toBe(ACCOUNT.id)
        calls++
        await pending
        checked = true
        return HttpResponse.json({ success: true, data: {
          account: updated,
          checks: [
            { name: 'Cookie', passed: false, message: 'Cookie 已失效，请更新' },
            { name: 'App 专用密码', passed: true, message: 'iCloud IMAP 连接正常' },
          ],
        } })
      }),
    )
    renderAccounts()
    const row = await accountRow()
    await user.click(row.getByRole('button', { name: /凭据配置/ }))
    await user.click(await screen.findByText('账号检测'))
    const trigger = await row.findByRole('button', { name: /检测中/ })
    await user.click(trigger)
    try {
      const menu = within(screen.getByRole('menu'))
      expect(await menu.findByRole('menuitem', { name: /检测中/ })).toHaveAttribute('aria-disabled', 'true')
      expect(calls).toBe(1)
    } finally {
      finishCheck()
    }
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('账号检测：主号')
    expect(within(dialog).getByText('Cookie 已失效，请更新')).toBeInTheDocument()
    expect(within(dialog).getByText('iCloud IMAP 连接正常')).toBeInTheDocument()
    expect(await screen.findByText('异常')).toBeInTheDocument()
    expect(row.getByRole('button', { name: /凭据配置/ })).toBeInTheDocument()
  }, 20000)

  it.each(['active', 'pending'])('显示检测结果：%s', async (status) => {
    const user = userEvent.setup()
    const updated = { ...ACCOUNT, status, last_validated: '2026-09-30T12:00:00Z' }
    server.use(http.post('/api/accounts/:id/check', () => HttpResponse.json({ success: true, data: {
      account: updated,
      checks: status === 'active' ? [{ name: 'Cookie', passed: true, message: 'iCloud 会话有效' }] : [],
    } })))
    renderAccounts()
    const row = await accountRow()
    await user.click(row.getByRole('button', { name: /凭据配置/ }))
    await user.click(await screen.findByText('账号检测'))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(status === 'active' ? 'iCloud 会话有效' : /账号尚未配置凭据/)).toBeInTheDocument()
  })

  it('检测请求失败后显示原因并允许重试', async () => {
    const user = userEvent.setup()
    server.use(http.post('/api/accounts/:id/check', () => HttpResponse.json(
      { success: false, code: 'ACCOUNT_CHANGED', message: '账号凭据已变更，请重新检测' }, { status: 409 },
    )))
    renderAccounts()
    const row = await accountRow()
    await user.click(row.getByRole('button', { name: /凭据配置/ }))
    await user.click(await screen.findByText('账号检测'))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('账号凭据已变更，请重新检测')).toBeInTheDocument()
    await waitFor(() => expect(row.getByRole('button', { name: /凭据配置/ })).toBeInTheDocument())
  })
})
