import { useCallback, useEffect, useState } from 'react'
import { ApiError, request } from './client'
import type { AccountSummary } from './types'

export function describeError(err: unknown): string {
  return err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态'
}

/**
 * 资源读取状态。
 *
 * loading 由「当前 path 是否已结算」与「请求序号」派生,effect 内不做同步
 * setState,因此不会触发级联渲染;path 为 null 时完全不发请求。
 */
interface ResourceState<T> {
  /** 已结算请求对应的 path(true 表示该 path 的数据已就绪) */
  settledPath: string | null
  data: T | null
  error: string
  /** 已发起的请求序号 */
  version: number
  /** 已完成结算的请求序号 */
  settledVersion: number
}

function createState<T>(): ResourceState<T> {
  return { settledPath: null, data: null, error: '', version: 0, settledVersion: -1 }
}

/** 通用资源读取:path 变化或调用 reload 时重新拉取 */
export function useFetch<T>(path: string | null): {
  data: T | null
  loading: boolean
  error: string
  reload: () => void
} {
  const [state, setState] = useState<ResourceState<T>>(createState<T>)

  useEffect(() => {
    if (!path) return
    let cancelled = false
    const requestedVersion = state.version
    request<T>(path)
      .then((data) => {
        if (cancelled) return
        setState((prev) => ({
          settledPath: path,
          data,
          error: '',
          version: prev.version,
          settledVersion: requestedVersion,
        }))
      })
      .catch((err) => {
        if (cancelled) return
        setState((prev) => ({
          settledPath: path,
          data: null,
          error: describeError(err),
          version: prev.version,
          settledVersion: requestedVersion,
        }))
      })
    return () => {
      cancelled = true
    }
  }, [path, state.version])

  const reload = useCallback(() => {
    setState((prev) => ({ ...prev, version: prev.version + 1 }))
  }, [])

  const loading = Boolean(path) && (state.settledPath !== path || state.settledVersion < state.version)

  return {
    data: state.settledPath === path ? state.data : null,
    loading,
    error: state.settledPath === path ? state.error : '',
    reload,
  }
}

/** 账号列表:所有页面共用的唯一数据源(含手动刷新) */
export function useAccounts(): {
  accounts: AccountSummary[]
  loading: boolean
  error: string
  reload: () => void
} {
  const { data, loading, error, reload } = useFetch<AccountSummary[]>('/api/accounts')
  return { accounts: data ?? [], loading, error, reload }
}
