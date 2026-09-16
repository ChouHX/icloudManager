import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import AccountsPage from './AccountsPage'
import { renderPage } from '../test/render'

function renderAccounts() {
  return renderPage(<AccountsPage />, { route: '/accounts', path: '/accounts' })
}

describe('AccountsPage', () => {
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
    await screen.findByText('主号')

    await user.click(screen.getByRole('button', { name: /凭据配置/ }))
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
})
