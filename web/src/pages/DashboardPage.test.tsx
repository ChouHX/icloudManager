import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import DashboardPage from './DashboardPage'
import { renderPage } from '../test/render'

function renderDashboard() {
  return renderPage(<DashboardPage />, { route: '/dashboard', path: '/dashboard' })
}

/** 读取某个统计卡片的值 */
function statValue(label: string): string | null {
  const strip = document.querySelector('.stat-strip') as HTMLElement
  const node = within(strip).getByText(label)
  return node.parentElement?.querySelector('.stat-value')?.textContent ?? null
}

describe('DashboardPage', () => {
  it('展示 iCloud 账号别名与标签分布统计', async () => {
    renderDashboard()

    expect(await screen.findByText('iCloud 账号与别名')).toBeInTheDocument()

    expect(statValue('iCloud 账号')).toBe('1')
    expect(statValue('别名总数')).toBe('2')
    expect(statValue('活跃别名')).toBe('1')
    expect(statValue('标签账号')).toBe('2')
    expect(statValue('已打标签')).toBe('2')
    expect(statValue('未打标签')).toBe('0')
    expect(statValue('标签种类')).toBe('3')
    expect(statValue('网站种类')).toBe('2')

    // iCloud 账号明细
    const accountCard = screen.getByText('iCloud 账号与别名').closest('.ant-card') as HTMLElement
    expect(within(accountCard).getByText('主号')).toBeInTheDocument()
    expect(within(accountCard).getByText('owner@icloud.com')).toBeInTheDocument()
    expect(within(accountCard).getByText('2')).toBeInTheDocument()
    expect(within(accountCard).getByText('1')).toBeInTheDocument()

    // 按网站统计
    const siteCard = screen.getByText('按网站统计').closest('.ant-card') as HTMLElement
    expect(within(siteCard).getByText('GitHub')).toBeInTheDocument()
    expect(within(siteCard).getByText('Steam')).toBeInTheDocument()

    // 按标签统计
    const tagCard = screen.getByText('按标签统计').closest('.ant-card') as HTMLElement
    for (const tag of ['开发', '游戏', '项目A']) {
      expect(within(tagCard).getByText(tag)).toBeInTheDocument()
    }
  })
})
