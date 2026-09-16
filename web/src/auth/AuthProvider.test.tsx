import { http, HttpResponse } from 'msw'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { beforeEach, describe, expect, it } from 'vitest'
import type { ReactNode } from 'react'
import { AuthProvider } from './AuthProvider'
import { useAuth } from './useAuth'
import { server } from '../test/server'
import LoginPage from '../pages/LoginPage'
import { setCSRFToken } from '../api/client'

const LOGIN_HEADING = 'iCloud 隐私邮箱'

function ProtectedProbe() {
  const { status } = useAuth()
  if (status !== 'authenticated') return <p>loading…</p>
  return <p data-testid="protected">已登录页面</p>
}

/** 模拟 App.tsx 的认证分支逻辑 */
function TestApp({ children }: { children?: ReactNode }) {
  const { status } = useAuth()
  if (status === 'checking') return <p>loading…</p>
  if (status === 'anonymous') return <LoginPage />
  return <>{children ?? <ProtectedProbe />}</>
}

function wrap(node: ReactNode) {
  return (
    <ConfigProvider locale={zhCN}>
      <AntdApp>{node}</AntdApp>
    </ConfigProvider>
  )
}

function renderApp(initialPath = '/accounts') {
  return render(
    wrap(
      <MemoryRouter initialEntries={[initialPath]}>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<TestApp />} />
            <Route path="/accounts" element={<TestApp />} />
            <Route path="/aliases" element={<TestApp />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>,
    ),
  )
}

function renderLogoutProbe() {
  return render(
    wrap(
      <MemoryRouter initialEntries={['/accounts']}>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<TestApp />} />
            <Route path="/accounts" element={<LogoutProbe />} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>,
    ),
  )
}

function LogoutProbe() {
  const { status, logout } = useAuth()
  if (status === 'checking') return <p>loading…</p>
  if (status === 'anonymous') return <LoginPage />
  return (
    <div>
      <p data-testid="protected">已登录页面</p>
      <button onClick={() => void logout()}>退出登录</button>
    </div>
  )
}

describe('AuthProvider + LoginPage', () => {
  beforeEach(() => {
    setCSRFToken(null)
    server.resetHandlers()
  })

  it('首次访问显示 loading,会话校验通过后进入受保护页', async () => {
    server.use(
      http.get('/api/auth/session', () =>
        HttpResponse.json({
          success: true,
          data: {
            csrf_token: 'csrf-abc',
            expires_at: '2026-08-05T22:00:00+08:00',
          },
        }),
      ),
    )
    renderApp()
    expect(screen.getByText('loading…')).toBeInTheDocument()
    expect(await screen.findByTestId('protected')).toBeInTheDocument()
  })

  it('会话无效(401)跳转登录页', async () => {
    server.use(
      http.get('/api/auth/session', () =>
        HttpResponse.json(
          { success: false, code: 'AUTH_REQUIRED', message: '请先登录' },
          { status: 401 },
        ),
      ),
    )
    renderApp()
    expect(await screen.findByRole('heading', { name: LOGIN_HEADING })).toBeInTheDocument()
  })

  it('登录成功进入受保护页', async () => {
    server.use(
      http.get('/api/auth/session', () =>
        HttpResponse.json(
          { success: false, code: 'AUTH_REQUIRED', message: '请先登录' },
          { status: 401 },
        ),
      ),
      http.post('/api/auth/login', () =>
        HttpResponse.json({
          success: true,
          data: {
            csrf_token: 'csrf-new',
            expires_at: '2026-08-05T22:00:00+08:00',
          },
        }),
      ),
    )
    renderApp()
    const user = userEvent.setup()
    await screen.findByRole('heading', { name: LOGIN_HEADING })
    await user.type(screen.getByLabelText(/管理员密码/), 'admin-pass-2026')
    await user.click(screen.getByRole('button', { name: /登\s*录/ }))
    expect(await screen.findByTestId('protected')).toBeInTheDocument()
  })

  it('错误密码显示服务器消息且不回显密码', async () => {
    server.use(
      http.get('/api/auth/session', () =>
        HttpResponse.json(
          { success: false, code: 'AUTH_REQUIRED', message: '请先登录' },
          { status: 401 },
        ),
      ),
      http.post('/api/auth/login', () =>
        HttpResponse.json(
          { success: false, code: 'INVALID_CREDENTIALS', message: '管理员密码错误' },
          { status: 401 },
        ),
      ),
    )
    renderApp()
    const user = userEvent.setup()
    await screen.findByRole('heading', { name: LOGIN_HEADING })
    await user.type(screen.getByLabelText(/管理员密码/), 'wrong-password')
    await user.click(screen.getByRole('button', { name: /登\s*录/ }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('管理员密码错误')
    expect(alert).not.toHaveTextContent('wrong-password')
  })

  it('登录后刷新页面可恢复会话', async () => {
    server.use(
      http.get('/api/auth/session', () =>
        HttpResponse.json({
          success: true,
          data: {
            csrf_token: 'csrf-abc',
            expires_at: '2026-08-05T22:00:00+08:00',
          },
        }),
      ),
    )
    const { unmount } = renderApp()
    expect(await screen.findByTestId('protected')).toBeInTheDocument()
    unmount()
    renderApp()
    expect(await screen.findByTestId('protected')).toBeInTheDocument()
  })

  it('退出调用 logout 并回登录页', async () => {
    server.use(
      http.get('/api/auth/session', () =>
        HttpResponse.json({
          success: true,
          data: {
            csrf_token: 'csrf-abc',
            expires_at: '2026-08-05T22:00:00+08:00',
          },
        }),
      ),
      http.post('/api/auth/logout', () =>
        HttpResponse.json({ success: true, data: { logged_out: true } }),
      ),
    )
    renderLogoutProbe()
    const user = userEvent.setup()
    await screen.findByTestId('protected')
    await user.click(screen.getByRole('button', { name: /退出登录/ }))
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: LOGIN_HEADING })).toBeInTheDocument(),
    )
  })
})
