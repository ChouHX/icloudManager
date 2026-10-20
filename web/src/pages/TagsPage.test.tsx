import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import TagsPage from './TagsPage'
import { renderPage } from '../test/render'

function renderTags() {
  return renderPage(<TagsPage />, { route: '/tags', path: '/tags' })
}

describe('TagsPage', { timeout: 15000 }, () => {
  it('渲染标签凭据记录与项目标签', async () => {
    renderTags()

    expect(await screen.findByText('user@example.com')).toBeInTheDocument()
    expect(screen.getByText('GitHub')).toBeInTheDocument()
    expect(screen.getByText('gamer@example.com')).toBeInTheDocument()
    expect(screen.getByText('项目A')).toBeInTheDocument()
    expect(screen.getByText('游戏')).toBeInTheDocument()
    // 密码默认打码
    expect(screen.queryByText('s3cret-pass')).not.toBeInTheDocument()
  })

  it('可切换显示密码', async () => {
    const user = userEvent.setup()
    renderTags()

    const email = await screen.findByText('user@example.com')
    const row = within(email.closest('tr')!)
    await user.click(row.getByRole('button', { name: '显示密码' }))
    expect(await screen.findByText('s3cret-pass')).toBeInTheDocument()
  })

  it('打开添加记录弹窗并提交', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    await user.click(screen.getByRole('button', { name: /添加记录/ }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('邮箱 / 账号')).toBeInTheDocument()
    expect(within(dialog).getByPlaceholderText('留空表示暂不记录密码')).toBeInTheDocument()
    expect(within(dialog).getByText('项目标签')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('邮箱 / 账号'), 'new@example.com')
    await user.click(within(dialog).getByRole('button', { name: /保\s*存/ }))
    expect(await screen.findByText('记录已添加')).toBeInTheDocument()
  })

  it('按项目标签筛选记录', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    await user.click(screen.getByRole('combobox', { name: '' }))
    await user.click(await screen.findByTitle('游戏'))

    expect(await screen.findByText('gamer@example.com')).toBeInTheDocument()
    expect(screen.queryByText('user@example.com')).not.toBeInTheDocument()
  })

  it('搜索可过滤记录', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    await user.type(screen.getByPlaceholderText('搜索邮箱、网站、备注或标签'), 'GitHub')
    expect(await screen.findByText('user@example.com')).toBeInTheDocument()
    expect(screen.queryByText('gamer@example.com')).not.toBeInTheDocument()
  })

  it('删除记录', async () => {
    const user = userEvent.setup()
    renderTags()
    const email = await screen.findByText('user@example.com')
    const row = within(email.closest('tr')!)

    await user.click(row.getByRole('button', { name: /删除/ }))
    const confirm = await waitFor(() => {
      const btn = document.querySelector<HTMLButtonElement>('.ant-popconfirm-buttons .ant-btn-primary')
      if (!btn) throw new Error('popconfirm not open')
      return btn
    })
    await user.click(confirm)
    expect(await screen.findByText('记录已删除')).toBeInTheDocument()
  })
})
