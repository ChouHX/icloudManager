import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import GmailPage from './GmailPage'
import { renderPage } from '../test/render'

function renderGmail() {
  return renderPage(<GmailPage />, { route: '/gmail', path: '/gmail' })
}

describe('GmailPage', () => {
  it('展示 Gmail 账号与取件过滤条件', async () => {
    renderGmail()

    expect(await screen.findByText('mail@gmail.com')).toBeInTheDocument()
    // 2FA 密钥与应用专用密码直接展示
    expect(screen.getByText('2FA 密钥')).toBeInTheDocument()
    expect(screen.getByText('JBSWY3DPEHPK3PXP')).toBeInTheDocument()
    expect(screen.getByText('应用专用密码')).toBeInTheDocument()
    expect(screen.getByText('abcdefghijklmnop')).toBeInTheDocument()
    // 过滤条件是全局的，展示在列表标题上
    expect(screen.getByText(/账号列表 · 全局过滤：仅含 验证码/)).toBeInTheDocument()
    // 操作收进下拉菜单，不再单独占用取件链接列
    expect(screen.getByRole('button', { name: /操作/ })).toBeInTheDocument()
    expect(screen.queryByText('取件链接', { selector: 'th' })).not.toBeInTheDocument()
  })

  it('批量导入账号', async () => {
    const user = userEvent.setup()
    renderGmail()
    await screen.findByText('mail@gmail.com')

    await user.click(screen.getByRole('button', { name: /批量导入/ }))
    const dialog = await screen.findByRole('dialog')
    await user.type(
      within(dialog).getByLabelText('账号列表'),
      'a@gmail.com----pass----totp----app',
    )
    await user.click(within(dialog).getByRole('button', { name: /开始导入/ }))

    expect(await within(dialog).findByText('导入完成')).toBeInTheDocument()
    expect(within(dialog).getByText('第 2 行: 邮箱地址格式无效')).toBeInTheDocument()
  })

  it('保存全局取件过滤条件', async () => {
    const user = userEvent.setup()
    renderGmail()
    await screen.findByText('mail@gmail.com')

    await user.click(screen.getByRole('button', { name: /全局过滤/ }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('全局取件过滤（对所有 Gmail 邮箱生效）')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: /保\s*存/ }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(await screen.findByText('全局取件过滤已保存')).toBeInTheDocument()
  })

  it('批量导出取件链接', async () => {
    const user = userEvent.setup()
    renderGmail()
    await screen.findByText('mail@gmail.com')

    await user.click(screen.getByRole('button', { name: /导出全部链接/ }))
    expect(await screen.findByText('已导出 1 条取件链接')).toBeInTheDocument()
  })

  it('从操作菜单生成并复制取件链接', async () => {
    const user = userEvent.setup()
    renderGmail()
    await screen.findByText('mail@gmail.com')

    await user.click(screen.getByRole('button', { name: /操作/ }))
    await user.click(await screen.findByText('生成并复制链接'))
    expect(await screen.findByText(/取件链接已生成/)).toBeInTheDocument()
  })
})
