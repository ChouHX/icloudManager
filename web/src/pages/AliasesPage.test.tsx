import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import AliasesPage from './AliasesPage'
import { renderPage } from '../test/render'
import { server } from '../test/server'
import { ACCOUNT, ALIASES, SECOND_ACCOUNT } from '../test/handlers'
import { downloadAliasExport } from '../utils/exportAliases'

vi.mock('../utils/exportAliases', () => ({ downloadAliasExport: vi.fn() }))

afterEach(() => vi.mocked(downloadAliasExport).mockReset())

function renderAliases() {
  return renderPage(<AliasesPage />, { route: '/aliases?account_id=acc_1', path: '/aliases' })
}

describe('AliasesPage', () => {
  it('渲染别名列表与启用状态', async () => {
    renderAliases()

    expect(await screen.findByText('alpha@icloud.com')).toBeInTheDocument()
    const table = within(screen.getByRole('table'))
    expect(table.getByText('beta@icloud.com')).toBeInTheDocument()
    expect(table.getByText('已启用')).toBeInTheDocument()
    expect(table.getByText('已停用')).toBeInTheDocument()
    expect(table.getByText('购物')).toBeInTheDocument()
  })

  it('按状态筛选别名', async () => {
    // antd Segmented 的隐藏 radio 带 pointer-events: none,需关闭指针事件校验
    const user = userEvent.setup({ pointerEventsCheck: 0 })
    renderAliases()
    await screen.findByText('alpha@icloud.com')

    await user.click(screen.getByText('已停用', { selector: '.ant-segmented-item-label' }))

    expect(screen.queryByText('alpha@icloud.com')).not.toBeInTheDocument()
    expect(screen.getByText('beta@icloud.com')).toBeInTheDocument()
  })

  it('创建别名成功后展示新地址', async () => {
    const user = userEvent.setup()
    renderAliases()
    await screen.findByText('alpha@icloud.com')

    await user.click(screen.getByRole('button', { name: /创建别名/ }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText(/标签/), '新用途')
    await user.click(within(dialog).getByRole('button', { name: /创\s*建/ }))

    expect(await screen.findByText('gamma@icloud.com')).toBeInTheDocument()
    expect(screen.getByText('别名创建成功')).toBeInTheDocument()
  })

  it('没有账号时引导前往账号设置', async () => {
    const { server } = await import('../test/server')
    const { http, HttpResponse } = await import('msw')
    server.use(http.get('/api/accounts', () => HttpResponse.json({ success: true, data: [] })))

    renderAliases()

    expect(await screen.findByText('还没有账号')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /去添加账号/ })).toBeInTheDocument()
  })

  it('一键导出当前账号全部别名，不受分页、搜索和状态筛选影响', async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 })
    const aliases = Array.from({ length: 25 }, (_, i) => ({
      ...ALIASES[i % 2], email: `alias${i}@icloud.com`, anonymousId: `anon_${i}`,
    }))
    server.use(http.get('/api/aliases', () => HttpResponse.json({
      success: true, data: { account_id: ACCOUNT.id, count: aliases.length, aliases },
    })))
    renderAliases()
    await screen.findByText('alias0@icloud.com')
    expect(screen.queryByText('alias23@icloud.com')).not.toBeInTheDocument()

    await user.type(screen.getByPlaceholderText('搜索邮箱或标签'), 'alias0@')
    await user.click(screen.getByText('已启用', { selector: '.ant-segmented-item-label' }))
    await user.click(screen.getByRole('button', { name: /一键导出/ }))

    expect(downloadAliasExport).toHaveBeenCalledExactlyOnceWith(ACCOUNT, aliases)
    expect(await screen.findByText('已导出「主号」的 25 个别名')).toBeInTheDocument()
  })

  it('切换账号加载期间禁用导出，加载完成后仅导出新账号', async () => {
    const user = userEvent.setup()
    let finishLoading!: () => void
    const pending = new Promise<void>((resolve) => { finishLoading = resolve })
    const backupAliases = [{ ...ALIASES[0], email: 'backup-alias@icloud.com' }]
    server.use(
      http.get('/api/accounts', () => HttpResponse.json({ success: true, data: [ACCOUNT, SECOND_ACCOUNT] })),
      http.get('/api/aliases', async ({ request }) => {
        const accountId = new URL(request.url).searchParams.get('account_id')
        if (accountId === SECOND_ACCOUNT.id) await pending
        const aliases = accountId === SECOND_ACCOUNT.id ? backupAliases : ALIASES
        return HttpResponse.json({ success: true, data: { account_id: accountId, count: aliases.length, aliases } })
      }),
    )
    renderAliases()
    expect(screen.getByRole('button', { name: /一键导出/ })).toBeDisabled()
    await screen.findByText('alpha@icloud.com')
    await user.click(screen.getByRole('button', { name: /一键导出/ }))
    expect(downloadAliasExport).toHaveBeenLastCalledWith(ACCOUNT, ALIASES)

    try {
      await user.click(screen.getByRole('combobox', { name: '选择账号' }))
      await user.click(await screen.findByText('备用号', { selector: '.ant-select-item-option-content' }))
      expect(screen.getByRole('button', { name: /一键导出/ })).toBeDisabled()
      await user.click(screen.getByRole('button', { name: /一键导出/ }))
      expect(downloadAliasExport).toHaveBeenCalledTimes(1)
    } finally {
      finishLoading()
    }
    await screen.findByText('backup-alias@icloud.com')
    await user.click(screen.getByRole('button', { name: /一键导出/ }))
    expect(downloadAliasExport).toHaveBeenLastCalledWith(SECOND_ACCOUNT, backupAliases)
  }, 15000)

  it.each(['empty', 'error', 'wrong-account'])('列表不可导出时禁用按钮：%s', async (state) => {
    const aliases = state === 'wrong-account' ? ALIASES : []
    server.use(http.get('/api/aliases', () => state === 'error'
      ? HttpResponse.json({ success: false, message: '读取别名失败' }, { status: 500 })
      : HttpResponse.json({ success: true, data: {
        account_id: state === 'wrong-account' ? SECOND_ACCOUNT.id : ACCOUNT.id,
        count: aliases.length, aliases,
      } })))
    renderAliases()
    await screen.findByText(state === 'error' ? '读取别名失败' : `别名列表 · ${aliases.length}`)
    expect(screen.getByRole('button', { name: /一键导出/ })).toBeDisabled()
    expect(downloadAliasExport).not.toHaveBeenCalled()
  })

  it('下载失败后提示重试', async () => {
    vi.mocked(downloadAliasExport).mockImplementationOnce(() => { throw new Error('download unavailable') })
    const user = userEvent.setup()
    renderAliases()
    await screen.findByText('alpha@icloud.com')
    await user.click(screen.getByRole('button', { name: /一键导出/ }))
    expect(await screen.findByText('导出失败，请重试')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('button', { name: /一键导出/ })).toBeEnabled())
  })
})
