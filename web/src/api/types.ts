/**
 * API 类型定义——与 internal/server 冻结契约保持一致。
 */

/** 统一响应包裹 */
export interface ApiResponse<T> {
  success: boolean
  data?: T
  code?: string
  message?: string
}

/** 账号安全摘要(无秘密字段) */
export interface AccountSummary {
  id: string
  name: string
  real_email: string
  icloud_email: string
  host: string
  status: 'active' | 'pending' | 'error' | string
  alias_total: number
  alias_active: number
  has_cookies: boolean
  has_app_password: boolean
  has_proxy: boolean
  mailbox?: MailboxSummary
  last_validated: string
  status_message?: string
  created_at: string
}

export interface MailboxSummary {
  provider: string
  email: string
  imap_host: string
  imap_port: number
}

/** HME 别名(iCloud 返回字段风格为 camelCase) */
export interface Alias {
  email: string
  anonymousId: string
  label: string
  active: boolean
  createdAt?: string
}

export interface AliasListResult {
  account_id: string
  count: number
  aliases: Alias[]
}

/** 创建别名结果 */
export interface CreateAliasResult {
  email: string
  label: string
  created_at: string
  account_id: string
}

/** 邮件摘要 */
export interface InboxMessage {
  id: string
  from: string
  to: string
  subject: string
  date: string
  preview: string
}

export interface FullMessage extends InboxMessage {
  /** 可读纯文本正文（复制、搜索、无 HTML 时的展示） */
  body: string
  /** 邮件自带的原始 HTML，服务端不做任何加工 */
  body_html?: string
  /** 清理后的 HTML（请求 ?sanitize=1 时返回），界面用这份渲染 */
  body_html_sanitized?: string
  /** 完整 RFC822 报文的 base64（请求 ?raw=1 时返回） */
  raw_message?: string
  /** 原始报文字节数 */
  raw_size?: number
  content_type: string
}

/** 收件箱查询结果 */
export interface InboxResult {
  account_id: string
  alias?: string
  count: number
  messages: InboxMessage[]
  method: 'imap' | 'web_api'
}

/** 登录响应 */
export interface LoginResult {
  csrf_token: string
  expires_at: string
}

/** 后台自动建满任务的一条日志 */
export interface AutoCreateLog {
  time: string
  level: 'info' | 'warn' | 'error' | 'success' | string
  message: string
}

/** 任务中单个账号的进度 */
export interface AutoCreateAccountStatus {
  account_id: string
  name: string
  phase: 'idle' | 'running' | 'cooling' | 'completed' | 'stopped' | string
  target: number
  total: number
  remaining: number
  created: number
  failed: number
  consecutive_failures: number
  /** 启动时的配额检查是否成功 */
  capacity_checked: boolean
  capacity_error?: string
  next_run_at?: string
  attempting: boolean
  last_attempt_at?: string
  last_email?: string
  last_error?: string
  reason?: string
}

/** 自动建满任务状态（顶层字段是所有账号的汇总） */
export interface AutoCreateStatus {
  running: boolean
  phase: 'idle' | 'running' | 'cooling' | 'completed' | 'stopped' | string
  account_id?: string
  target: number
  total: number
  created: number
  failed: number
  /** 创建后随机等待的下限(秒) */
  interval_seconds: number
  /** 创建后随机等待的上限(秒) */
  interval_max_seconds: number
  cooldown_seconds: number
  label_prefix: string
  max_failures: number
  max_parallel: number
  accounts: AutoCreateAccountStatus[]
  started_at?: string
  next_run_at?: string
  attempting?: boolean
  last_attempt_at?: string
  last_email?: string
  last_error?: string
  reason?: string
  logs: AutoCreateLog[]
}

/** 启动自动建满任务的参数（account_ids 支持一次勾选多个账号并行） */
export interface AutoCreateStartRequest {
  account_id?: string
  account_ids: string[]
  target?: number
  interval_seconds?: number
  interval_max_seconds?: number
  cooldown_seconds?: number
  label_prefix?: string
  max_failures?: number
  max_parallel?: number
}
