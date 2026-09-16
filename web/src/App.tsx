import { BrowserRouter, Navigate, Route, Routes, useSearchParams } from 'react-router-dom'
import { Spin } from 'antd'
import { AuthProvider } from './auth/AuthProvider'
import { useAuth } from './auth/useAuth'
import AppLayout from './components/AppLayout'
import LoginPage from './pages/LoginPage'
import AliasesPage from './pages/AliasesPage'
import InboxPage from './pages/InboxPage'
import AccountsPage from './pages/AccountsPage'
import PublicInboxPage from './pages/PublicInboxPage'

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
 * 根路径入口:带 ?token= 时进入只读取件页(无需登录),
 * 否则按正常流程跳转到管理界面。
 */
function RootEntry() {
  const [searchParams] = useSearchParams()
  const token = searchParams.get('token') ?? ''
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
            <Route path="/aliases" element={<AliasesPage />} />
            <Route path="/inbox" element={<InboxPage />} />
            <Route path="/accounts" element={<AccountsPage />} />
            <Route path="*" element={<Navigate to="/aliases" replace />} />
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}
