import { createContext, useContext } from 'react'

export type AuthStatus = 'checking' | 'anonymous' | 'authenticated'

export interface AuthContextValue {
  status: AuthStatus
  login: (password: string) => Promise<void>
  logout: () => Promise<void>
}

/** 认证上下文:由 AuthProvider 提供,这里单独声明以便组件与 hook 分文件导出 */
export const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth 必须在 AuthProvider 内使用')
  }
  return ctx
}
