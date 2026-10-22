import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import TagsPage from './TagsPage'
import { renderPage } from '../test/render'
import { server } from '../test/server'

function renderTags() {
  return renderPage(<TagsPage />, { route: '/tags', path: '/tags' })
}

describe('TagsPage', { timeout: 30000 }, () => {
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

  it('tags 为 null 时不应崩溃', async () => {
    server.use(
      http.get('/api/tags', () =>
        HttpResponse.json({
          success: true,
          data: {
            count: 1,
            tags: [],
            entries: [
              {
                id: 'tag_null',
                email: 'nulltags@example.com',
                tags: null,
                created_at: '2026-08-06T10:00:00+08:00',
                updated_at: '2026-08-06T10:00:00+08:00',
              },
            ],
          },
        }),
      ),
    )
    renderTags()

    expect(await screen.findByText('nulltags@example.com')).toBeInTheDocument()
    expect(screen.getAllByText('项目标签').length).toBeGreaterThan(0)
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

    const tagSelect = screen.getByText('按项目标签筛选').closest('.ant-select') as HTMLElement
    await user.click(within(tagSelect).getByRole('combobox'))
    await user.click(await screen.findByTitle('游戏'))

    expect(await screen.findByText('gamer@example.com')).toBeInTheDocument()
    expect(screen.queryByText('user@example.com')).not.toBeInTheDocument()
  })

  it('没有筛选条件时不允许批量修改全部记录', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    await user.click(screen.getByRole('button', { name: /批量打标签/ }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('当前没有筛选条件')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: /执\s*行/ })).toBeDisabled()
  })

  it('按网站筛选记录', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    const select = screen.getByText('按网站筛选').closest('.ant-select') as HTMLElement
    await user.click(within(select).getByRole('combobox'))
    await user.click(await screen.findByTitle('GitHub'))

    expect(await screen.findByText('user@example.com')).toBeInTheDocument()
    expect(screen.queryByText('gamer@example.com')).not.toBeInTheDocument()
  })

  it('按当前筛选结果批量打标签', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    const select = screen.getByText('按网站筛选').closest('.ant-select') as HTMLElement
    await user.click(within(select).getByRole('combobox'))
    await user.click(await screen.findByTitle('GitHub'))
    await waitFor(() => expect(screen.queryByText('gamer@example.com')).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: /批量打标签/ }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('当前筛选结果（1 条）')).toBeInTheDocument()

    await user.type(within(dialog).getByRole('combobox'), '新标签')
    await user.keyboard('{Enter}')
    await user.click(within(dialog).getByRole('button', { name: /执\s*行/ }))

    expect(await screen.findByText('已为 1 条记录更新标签')).toBeInTheDocument()
  })

  it('按勾选记录批量打标签', async () => {
    const user = userEvent.setup()
    renderTags()
    const email = await screen.findByText('user@example.com')
    await user.click(within(email.closest('tr')!).getByRole('checkbox'))

    await user.click(screen.getByRole('button', { name: /批量打标签/ }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('已勾选的记录（1 条）')).toBeInTheDocument()

    await user.type(within(dialog).getByRole('combobox'), '勾选标签')
    await user.keyboard('{Enter}')
    await user.click(within(dialog).getByRole('button', { name: /执\s*行/ }))

    expect(await screen.findByText('已为 1 条记录更新标签')).toBeInTheDocument()
  })

  it('搜索可过滤记录', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    await user.type(screen.getByPlaceholderText('搜索邮箱、网站、备注或标签'), 'GitHub')
    await waitFor(() => expect(screen.queryByText('gamer@example.com')).not.toBeInTheDocument())
    expect(await screen.findByText('user@example.com')).toBeInTheDocument()
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
      .getAllByText('开发')
      .map((el) => el.closest('tr'))
      .find((tr) => tr && within(tr as HTMLElement).queryByRole('button', { name: /重命名/ })) as HTMLElement | undefined
    expect(row).toBeTruthy()
    await user.click(within(row!).getByRole('button', { name: /重命名/ }))
    const input = within(row!).getByRole('textbox')
    await user.clear(input)
    await user.type(input, '前端')
    await user.click(within(row!).getByRole('button', { name: /保\s*存/ }))

    expect(await screen.findByText(/已将「开发」重命名为「前端」/)).toBeInTheDocument()
  })

  it('从备份导入数据', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    await user.click(screen.getByRole('button', { name: /备份迁移/ }))
    const dialog = await screen.findByRole('dialog')

    const backup = {
      version: 1,
      type: 'icloud-hme-tags',
      entries: [{ email: 'restored@example.com', tags: ['项目C'] }],
    }
    fireEvent.change(within(dialog).getByLabelText('备份 JSON 内容'), {
      target: { value: JSON.stringify(backup) },
    })
    await user.click(within(dialog).getByRole('button', { name: /导入备份/ }))

    expect(await within(dialog).findByText('导入完成')).toBeInTheDocument()
    expect(within(dialog).getByText('新增')).toBeInTheDocument()
    expect(within(dialog).getByText('跳过')).toBeInTheDocument()
  })

  it('批量导出支持勾选、筛选与全部记录', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    const createSpy = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock')
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    try {
      // 多选导出：只包含勾选记录
      const email = screen.getByText('user@example.com')
      await user.click(within(email.closest('tr')!).getByRole('checkbox'))
      await user.click(screen.getByRole('button', { name: /批量导出/ }))
      let dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText('已勾选的记录（1 条）')).toBeInTheDocument()
      await user.click(within(dialog).getByRole('button', { name: /导\s*出/ }))
      await waitFor(() => expect(createSpy).toHaveBeenCalledTimes(1))
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

      let blob = createSpy.mock.calls.at(-1)?.[0] as Blob
      const bom = Array.from(new Uint8Array(await blob.arrayBuffer()).slice(0, 3))
      expect(bom).toEqual([0xef, 0xbb, 0xbf])
      let csv = await blob.text()
      expect(csv).toContain('"user@example.com"')
      expect(csv).not.toContain('gamer@example.com')

      // 按筛选导出：只包含当前筛选结果
      const toolbar = document.querySelector('.list-toolbar') as HTMLElement
      const siteSelect = toolbar.querySelectorAll('.ant-select')[1] as HTMLElement
      await user.click(within(siteSelect).getByRole('combobox'))
      await user.click(await screen.findByTitle('GitHub'))
      await waitFor(() => expect(screen.queryByText('gamer@example.com')).not.toBeInTheDocument())

      await user.click(screen.getByRole('button', { name: /批量导出/ }))
      dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText('当前筛选结果（1 条）')).toBeInTheDocument()
      await user.click(within(dialog).getByRole('button', { name: /导\s*出/ }))
      await waitFor(() => expect(createSpy).toHaveBeenCalledTimes(2))
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

      blob = createSpy.mock.calls.at(-1)?.[0] as Blob
      csv = await blob.text()
      expect(csv).toContain('"user@example.com"')
      expect(csv).not.toContain('gamer@example.com')

      // 清空筛选后导出全部
      await user.click(siteSelect.querySelector('.ant-select-clear') as HTMLElement)
      await waitFor(() => expect(screen.getByText('gamer@example.com')).toBeInTheDocument())

      await user.click(screen.getByRole('button', { name: /批量导出/ }))
      dialog = await screen.findByRole('dialog')
      expect(within(dialog).getByText('全部记录（2 条）')).toBeInTheDocument()
      await user.click(within(dialog).getByRole('button', { name: /导\s*出/ }))
      await waitFor(() => expect(createSpy).toHaveBeenCalledTimes(3))

      blob = createSpy.mock.calls.at(-1)?.[0] as Blob
      csv = await blob.text()
      expect(csv).toContain('"user@example.com"')
      expect(csv).toContain('"gamer@example.com"')
    } finally {
      createSpy.mockRestore()
      clickSpy.mockRestore()
    }
  })

  it('批量删除支持勾选、筛选与全部记录', async () => {
    const user = userEvent.setup()
    renderTags()
    await screen.findByText('user@example.com')

    // 多选删除：只删除勾选记录
    const email = screen.getByText('user@example.com')
    await user.click(within(email.closest('tr')!).getByRole('checkbox'))
    await user.click(screen.getByRole('button', { name: /批量删除/ }))
    let dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('已勾选的记录（1 条）')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: /删\s*除/ }))
    expect(await screen.findByText('已删除 1 条记录')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    // 按筛选删除
    const toolbar = document.querySelector('.list-toolbar') as HTMLElement
    const siteSelect = toolbar.querySelectorAll('.ant-select')[1] as HTMLElement
    await user.click(within(siteSelect).getByRole('combobox'))
    await user.click(await screen.findByTitle('GitHub'))
    await waitFor(() => expect(screen.queryByText('gamer@example.com')).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: /批量删除/ }))
    dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('当前筛选结果（1 条）')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: /删\s*除/ }))
    expect(await screen.findByText('已删除 1 条记录')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())

    // 清空筛选后删除全部
    await user.click(siteSelect.querySelector('.ant-select-clear') as HTMLElement)
    await waitFor(() => expect(screen.getByText('gamer@example.com')).toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: /批量删除/ }))
    dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('全部记录（2 条）')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: /删\s*除/ }))
    expect(await screen.findByText('已删除 2 条记录')).toBeInTheDocument()
  })
})
