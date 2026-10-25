import type { AccountSummary, DashboardData } from '../api/types'

/**
 * 本地调试用的 Mock 数据(仅在 `import.meta.env.DEV` 且 URL 带 `?mock=1` 时使用)。
 *
 * 用法: npm --prefix web run dev 后打开 http://127.0.0.1:5173/dashboard?mock=1
 * 生产构建不会走这个分支。
 */
function mockAccount(
  id: string,
  name: string,
  email: string,
  status: AccountSummary['status'],
  aliasTotal: number,
  aliasActive: number,
): AccountSummary {
  return {
    id,
    name,
    real_email: email,
    icloud_email: email,
    host: 'icloud.com',
    status,
    alias_total: aliasTotal,
    alias_active: aliasActive,
    has_cookies: true,
    has_app_password: true,
    has_proxy: false,
    last_validated: '2026-10-20T10:00:00+08:00',
    created_at: '2026-08-01T09:00:00+08:00',
  }
}

export const MOCK_DASHBOARD_DATA: DashboardData = {
  accounts: {
    total: 8,
    alias_total: 186,
    alias_active: 143,
    items: [
      mockAccount('acc_1', '主号', 'owner@icloud.com', 'active', 68, 61),
      mockAccount('acc_2', '备用号', 'backup@icloud.com', 'active', 42, 35),
      mockAccount('acc_3', '游戏专用', 'game@icloud.com', 'active', 25, 22),
      mockAccount('acc_4', '购物小号', 'shop@icloud.com', 'active', 18, 12),
      mockAccount('acc_5', '测试环境', 'test@icloud.com', 'pending', 12, 5),
      mockAccount('acc_6', '海外账号', 'global@icloud.com', 'active', 9, 8),
      mockAccount('acc_7', '临时注册', 'temp@icloud.com', 'error', 8, 0),
      mockAccount('acc_8', '归档账号', 'archive@icloud.com', 'pending', 4, 0),
    ],
  },
  tags: {
    total: 64,
    tagged: 52,
    untagged: 12,
    tag_kinds: 6,
    site_kinds: 7,
    by_site: [
      { site: 'GitHub', count: 18 },
      { site: 'Steam', count: 12 },
      { site: 'Google', count: 9 },
      { site: 'X', count: 7 },
      { site: 'Discord', count: 6 },
      { site: 'OpenAI', count: 5 },
      { site: 'Amazon', count: 4 },
    ],
    by_tag: [
      { tag: '项目A', count: 21 },
      { tag: '游戏', count: 14 },
      { tag: '开发', count: 11 },
      { tag: '订阅', count: 8 },
      { tag: '测试', count: 6 },
      { tag: '待整理', count: 4 },
    ],
    by_site_tag: [
      {
        site: 'GitHub',
        total: 18,
        tags: [
          { tag: '项目A', count: 9 },
          { tag: '开发', count: 7 },
          { tag: '测试', count: 3 },
        ],
      },
      {
        site: 'Steam',
        total: 12,
        tags: [
          { tag: '游戏', count: 10 },
          { tag: '订阅', count: 3 },
        ],
      },
      {
        site: 'Google',
        total: 9,
        tags: [
          { tag: '项目A', count: 4 },
          { tag: '订阅', count: 3 },
        ],
      },
    ],
  },
}
