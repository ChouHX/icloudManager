import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { request, registerUnauthorizedHandler, setCSRFToken } from '../api/client'
import type { LoginResult } from '../api/types'
import { AuthContext, type AuthStatus, type AuthContextValue } from './useAuth'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('checking')
  const checkRef = useRef(false)

  useEffect(() => {
    if (checkRef.current) return
    checkRef.current = true
    let cancelled = false
    request<LoginResult>('/api/auth/session', undefined, () => {
      // session 探测本身 401 不算"会话过期",交给状态判断
    })
      .then((data) => {
        if (cancelled) return
        setCSRFToken(data.csrf_token)
        setStatus('authenticated')
      })
      .catch(() => {
        if (cancelled) return
        setStatus('anonymous')
      })
    return () => {
      cancelled = true
    }
  }, [])

  const logout = useCallback(async () => {
    try {
      await request('/api/auth/logout', { method: 'POST' })
    } catch {
      // 无论请求结果如何都清空本地状态
    }
    setCSRFToken(null)
    setStatus('anonymous')
  }, [])

  const handleUnauthorized = useCallback(() => {
    setCSRFToken(null)
    setStatus('anonymous')
  }, [])

  useEffect(() => {
    registerUnauthorizedHandler(handleUnauthorized)
    return () => registerUnauthorizedHandler(null)
  }, [handleUnauthorized])

  const login = useCallback(async (password: string) => {
    const data = await request<LoginResult>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ password }),
    })
    setCSRFToken(data.csrf_token)
    setStatus('authenticated')
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({ status, login, logout }),
    [status, login, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
