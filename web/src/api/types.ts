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

export interface AccountCheckResult {
  account: AccountSummary
  checks: { name: string; passed: boolean; message: string }[]
}

/** HME 别名(iCloud 返回字段风格为 camelCase) */
export interface Alias {
  email: string
  anonymousId: string
  label: string
  active: boolean
  createdAt?: string
  /** 已生成的取件链接 token;未生成时不出现 */
  shareToken?: string
}

/** 取件链接(凭 token 只读查看某个别名的邮件) */
export interface ShareLink {
  alias: string
  token: string
  url: string
  created_at: string
  hits?: number
}

/** 凭取件链接获取的收件箱(响应不含账号信息) */
export interface ShareInboxResult {
  alias: string
  count: number
  messages: InboxMessage[]
  method: 'imap' | 'web_api'
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

/**
 * 标签凭据记录——独立的「邮箱/密码 + 项目标签」管理，与 iCloud 账号无关。
 */
export interface TagEntry {
  id: string
  email: string
  password?: string
  /** 网站/服务名称，如 GitHub、Steam */
  site?: string
  /** 使用过该账号的项目标签 */
  tags: string[]
  note?: string
  created_at: string
  updated_at: string
}

/** 标签账号的汇总统计 */
export interface TagStats {
  total: number
  tags: number
  sites: number
  untagged: number
}

/** 单个标签的使用记录数 */
export interface TagUsageCount {
  tag: string
  count: number
}

/** 单个网站的使用记录数 */
export interface SiteUsageCount {
  site: string
  count: number
}

/** 数据看板统计 */
export interface DashboardData {
  accounts: {
    total: number
    alias_total: number
    alias_active: number
    items: AccountSummary[]
  }
  tags: {
    total: number
    tagged: number
    untagged: number
    tag_kinds: number
    site_kinds: number
    by_site: SiteUsageCount[]
    by_tag: TagUsageCount[]
  }
}

/** 标签凭据列表结果(服务端分页) */
export interface TagListResult {
  /** 符合筛选条件的记录总数 */
  count: number
  /** 当前页码,从 1 开始 */
  page?: number
  /** 每页条数 */
  page_size?: number
  /** 总页数 */
  total_pages?: number
  /** 所有记录中出现过的去重标签 */
  tags: string[]
  /** 所有记录中出现过的去重网站名 */
  sites?: string[]
  /** 每个标签的使用记录数(覆盖全部分页) */
  tag_counts?: TagUsageCount[]
  /** 汇总统计卡片数据 */
  stats?: TagStats
  /** 当前页记录 */
  entries: TagEntry[]
}

/** 批量修改标签的筛选条件(与列表筛选一致) */
export interface TagBatchFilter {
  search?: string
  tags?: string[]
  site?: string
  created_from?: string
  created_to?: string
}

/** 批量修改标签的操作:追加 / 移除 / 覆盖 */
export type TagBatchAction = 'add' | 'remove' | 'replace'

/** 批量修改标签的输入 */
export interface TagBatchInput {
  /** 勾选的记录 ID;为空时使用 filter */
  ids?: string[]
  filter?: TagBatchFilter
  action: TagBatchAction
  tags: string[]
}

/** 批量修改标签的结果 */
export interface TagBatchResult {
  affected: number
  action: TagBatchAction
  tags: string[]
}

/** 按勾选 ID 或筛选条件批量导出的输入;两者都为空时导出全部 */
export interface TagExportInput {
  ids?: string[]
  filter?: TagBatchFilter
}

/** 批量删除的输入:ids 优先,其次 filter;只有显式 all=true 才会删除全部 */
export interface TagDeleteInput {
  ids?: string[]
  filter?: TagBatchFilter
  all?: boolean
}

/** 批量删除的结果 */
export interface TagDeleteResult {
  affected: number
}

/** 创建/更新标签凭据的输入 */
export interface TagEntryInput {
  email: string
  password: string
  site: string
  tags: string[]
  note: string
}

/** 批量导入标签凭据的输入 */
export interface TagImportInput {
  /** 每行一条记录 */
  text: string
  /** 自定义分隔符;留空则按内容自动识别 */
  separator: string
  /** 应用到本次导入所有记录的标签 */
  tags: string[]
  /** 行内未指定网站时使用的默认网站 */
  site: string
  /** skip: 跳过已存在(默认);update: 覆盖更新并合并标签 */
  mode: 'skip' | 'update'
}

/** 批量导入结果 */
export interface TagImportResult {
  imported: number
  updated: number
  skipped: number
  invalid: number
  entries?: TagEntry[]
  errors: string[]
}

/** 标签重命名结果 */
export interface TagRenameResult {
  changed: number
  from: string
  to: string
}

/** 标签凭据备份文档 */
export interface TagBackup {
  version: number
  type: string
  exported_at: string
  count: number
  entries: TagEntry[]
}

/** 从备份导入的输入 */
export interface TagRestoreInput {
  version?: number
  type?: string
  entries: TagEntry[]
  /** merge: 跳过已存在(默认);replace: 清空后重建 */
  mode: 'merge' | 'replace'
}

/** 备份导入结果 */
export interface TagRestoreResult {
  imported: number
  skipped: number
  invalid: number
  total: number
  errors: string[]
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
