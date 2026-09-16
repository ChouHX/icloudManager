import { useState } from 'react'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import AutoCreatePanel from './AutoCreatePanel'
import { ACCOUNT, SECOND_ACCOUNT } from '../test/handlers'
import { renderPage } from '../test/render'

function renderPanel(onProgress = vi.fn()) {
  return renderPage(
    <AutoCreatePanel
      open
      accountId={ACCOUNT.id}
      accounts={[ACCOUNT, SECOND_ACCOUNT]}
      onClose={() => {}}
      onProgress={onProgress}
    />,
    { route: '/aliases', path: '/aliases' },
  )
}

describe('AutoCreatePanel', () => {
  it('展示未运行状态、默认参数与账号多选', async () => {
    renderPanel()

    expect(await screen.findByText('自动建满别名')).toBeInTheDocument()
    const drawer = screen.getByRole('dialog')
    expect(within(drawer).getByText('未运行')).toBeInTheDocument()
    expect(within(drawer).getByLabelText('每账号目标总数')).toHaveValue('700')
    expect(within(drawer).getByLabelText('间隔最小（秒）')).toHaveValue('20')
    expect(within(drawer).getByLabelText('间隔最大（秒）')).toHaveValue('40')
    expect(within(drawer).getByText('20–40 秒（随机）')).toBeInTheDocument()
    expect(within(drawer).getByLabelText('并发上限')).toHaveValue('10')
    // 当前页选中的账号默认勾选(antd 多选把已选项渲染为 selection-item)
    const selected = [...drawer.querySelectorAll('.ant-select-selection-item')].map((el) => el.getAttribute('title'))
    expect(selected).toContain('主号')
    expect(within(drawer).getByText(/iCloud 的创建限流按账号计算/)).toBeInTheDocument()
  })

  it('启动后展示逐账号进度与日志', async () => {
    const user = userEvent.setup()
    const onProgress = vi.fn()
    renderPanel(onProgress)
    await screen.findByText('未运行')

    await user.click(screen.getByRole('button', { name: /启\s*动/ }))

    // 顶层状态与账号行都会出现"创建中"
    expect((await screen.findAllByText('创建中')).length).toBeGreaterThan(0)
    expect(screen.getByText('17 / 1400')).toBeInTheDocument()
    // 两个账号各自一行
    expect(screen.getByText('2 个账号')).toBeInTheDocument()
    expect(screen.getByText('5 / 700（还需 695）')).toBeInTheDocument()
    expect(screen.getByText('12 / 700（还需 688）')).toBeInTheDocument()
    expect(screen.getByText('冷却中')).toBeInTheDocument()
    expect(screen.getByText(/最近创建：auto3@icloud.com/)).toBeInTheDocument()
    expect(screen.getByText(/最近错误：创建别名失败: HTTP 429/)).toBeInTheDocument()
    expect(screen.getByText(/\[主号\] 创建成功 auto3@icloud.com/)).toBeInTheDocument()
    expect(screen.getByText(/\[主号\] 等待 27s 后继续/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /停止任务/ })).toBeInTheDocument()
    expect(onProgress).toHaveBeenCalled()
  })

  // 回归 React error #185:父组件传内联箭头函数时,曾因依赖变化触发无限刷新
  it('父组件传入不稳定回调时不会陷入无限渲染', async () => {
    const user = userEvent.setup()
    let renders = 0

    function Harness() {
      renders++
      const [, setTick] = useState(0)
      return (
        <AutoCreatePanel
          open
          accountId={ACCOUNT.id}
          accounts={[ACCOUNT]}
          onClose={() => {}}
          // 故意每次渲染都传入新函数,并在回调里更新父组件状态
          // (对应真实场景:onProgress 会调用 reload() 触发刷新)
          onProgress={() => {
            renders++
            setTick((value) => value + 1)
          }}
        />
      )
    }

    renderPage(<Harness />, { route: '/aliases', path: '/aliases' })
    await screen.findByText('未运行')
    await user.click(screen.getByRole('button', { name: /启\s*动/ }))
    await screen.findByRole('button', { name: /停止任务/ })
    // 给足时间让潜在循环暴露。修复前这里会持续自我触发,
    // 300ms 内即可渲染数十次;正常情况只有个位数。
    await new Promise((resolve) => setTimeout(resolve, 500))

    expect(renders).toBeLessThan(20)
  })

  it('运行中可停止任务', async () => {
    const user = userEvent.setup()
    renderPanel()
    await screen.findByText('未运行')
    await user.click(screen.getByRole('button', { name: /启\s*动/ }))
    await screen.findByRole('button', { name: /停止任务/ })

    await user.click(screen.getByRole('button', { name: /停止任务/ }))

    // 顶层状态与每个账号行都会显示"已停止"
    expect((await screen.findAllByText('已停止')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('已手动停止').length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: /启\s*动/ })).toBeInTheDocument()
  })
})
