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

  it('批量导入账号并统一打标签', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    await user.click(screen.getByRole('button', { name: /批量导入/ }))
    const dialog = await screen.findByRole('dialog')

    await user.type(
      within(dialog).getByLabelText('账号列表'),
      'a@example.com----pass1\nb@example.com----pass2',
    )
    await user.type(within(dialog).getByRole('combobox'), '项目C')
    await user.keyboard('{Enter}')
    await user.click(within(dialog).getByRole('button', { name: /开始导入/ }))

    expect(await within(dialog).findByText('导入完成')).toBeInTheDocument()
    expect(within(dialog).getByText('新增')).toBeInTheDocument()
    expect(within(dialog).getByText('第 4 行: 邮箱格式不正确')).toBeInTheDocument()
  })

  it('标签管理支持重命名标签', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    await user.click(screen.getByRole('button', { name: /标签管理/ }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getAllByText('使用记录数').length).toBeGreaterThan(0)

    const row = within(dialog)
      .getAllByRole('row')
      .find((r) => within(r).queryByText('开发') && within(r).queryByRole('button', { name: /重命名/ }))
    expect(row).toBeTruthy()
    await user.click(within(row!).getByRole('button', { name: /重命名/ }))
    const input = within(row!).getByRole('textbox')
    await user.clear(input)
    await user.type(input, '前端')
    await user.click(within(row!).getByRole('button', { name: /保\s*存/ }))

    expect(await screen.findByText(/已将「开发」重命名为「前端」/)).toBeInTheDocument()
  })
})
