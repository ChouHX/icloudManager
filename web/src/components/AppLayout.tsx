import { Button, Layout, Menu, Tooltip } from 'antd'
import {
  InboxOutlined,
  LogoutOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  TagsOutlined,
} from '@ant-design/icons'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/useAuth'

const NAV_ITEMS = [
  { key: '/aliases', icon: <TagsOutlined />, label: '创建别名' },
  { key: '/inbox', icon: <InboxOutlined />, label: '取件' },
  { key: '/accounts', icon: <SettingOutlined />, label: '账号设置' },
]

/** 应用骨架：顶栏导航 + 内容区 */
export default function AppLayout() {
  const { logout } = useAuth()
  const navigate = useNavigate()
  const { pathname } = useLocation()

  const selected = NAV_ITEMS.find((item) => pathname.startsWith(item.key))?.key ?? '/aliases'

  async function handleLogout() {
    await logout()
    navigate('/login', { replace: true })
  }

  return (
    <Layout className="app-shell">
      <header className="app-header">
        <div className="app-brand">
          <span className="app-brand-mark" aria-hidden="true">
            <SafetyCertificateOutlined />
          </span>
          <span className="app-brand-text">
            <strong>iCloud 隐私邮箱</strong>
            <small>Hide My Email</small>
          </span>
        </div>

        <Menu
          className="app-nav"
          mode="horizontal"
          selectedKeys={[selected]}
          items={NAV_ITEMS}
          onClick={({ key }) => navigate(key)}
          disabledOverflow
        />

        <Tooltip title="退出登录">
          <Button
            type="text"
            icon={<LogoutOutlined />}
            aria-label="退出登录"
            onClick={() => void handleLogout()}
          >
            退出
          </Button>
        </Tooltip>
      </header>

      <main className="app-main">
        <Outlet />
      </main>
    </Layout>
  )
}
