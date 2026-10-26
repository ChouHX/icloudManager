import { BrowserRouter, Navigate, Route, Routes, useSearchParams } from 'react-router-dom'
import { Spin } from 'antd'
import { AuthProvider } from './auth/AuthProvider'
import { useAuth } from './auth/useAuth'
import AppLayout from './components/AppLayout'
import LoginPage from './pages/LoginPage'
import AliasesPage from './pages/AliasesPage'
import InboxPage from './pages/InboxPage'
import AccountsPage from './pages/AccountsPage'
import DashboardPage from './pages/DashboardPage'
import GmailPage from './pages/GmailPage'
import PublicInboxPage from './pages/PublicInboxPage'
import TagsPage from './pages/TagsPage'

/** 受保护区域：会话未确定前显示加载，未登录跳转登录页 */
function ProtectedLayout() {
  const { status } = useAuth()

  if (status === 'checking') {
    return (
      <div className="login-screen">
        <Spin size="large" description="正在校验会话…" />
      </div>
    )
  }
  if (status === 'anonymous') {
    return <Navigate to="/login" replace />
  }
  return <AppLayout />
}

/**
 * 根路径入口:带 ?token= 时进入 iCloud 别名取件页,带 ?gtoken= 时进入 Gmail 取件页
 * (均无需登录),否则按正常流程跳转到管理界面。
 */
function RootEntry() {
  const [searchParams] = useSearchParams()
  const gmailToken = searchParams.get('gtoken') ?? ''
  const token = searchParams.get('token') ?? ''
  if (gmailToken) {
    return <PublicInboxPage token={gmailToken} kind="gmail" />
  }
  if (!token) {
    return <Navigate to="/aliases" replace />
  }
  return <PublicInboxPage token={token} />
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/" element={<RootEntry />} />
          <Route path="/login" element={<LoginPage />} />
          <Route element={<ProtectedLayout />}>
            <Route path="/dashboard" element={<DashboardPage />} />
            <Route path="/aliases" element={<AliasesPage />} />
            <Route path="/inbox" element={<InboxPage />} />
            <Route path="/gmail" element={<GmailPage />} />
            <Route path="/accounts" element={<AccountsPage />} />
            <Route path="/tags" element={<TagsPage />} />
            <Route path="*" element={<Navigate to="/aliases" replace />} />
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}
