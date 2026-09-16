import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import AliasesPage from './AliasesPage'
import { renderPage } from '../test/render'

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
})
