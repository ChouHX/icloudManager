import { http, HttpResponse } from 'msw'

export const ACCOUNT = {
  id: 'acc_1',
  name: '主号',
  real_email: 'owner@example.com',
  icloud_email: 'owner@icloud.com',
  host: 'icloud.com',
  status: 'active',
  alias_total: 2,
  alias_active: 1,
  has_cookies: true,
  has_app_password: true,
  has_proxy: false,
  last_validated: '2026-08-04T09:00:00+08:00',
  status_message: '',
  created_at: '2026-08-01T09:00:00+08:00',
}

export const SECOND_ACCOUNT = {
  ...ACCOUNT,
  id: 'acc_2',
  name: '备用号',
  icloud_email: 'backup@icloud.com',
}

export const ALIASES = [
  {
    email: 'alpha@icloud.com',
    anonymousId: 'anon_1',
    label: '购物',
    active: true,
    createdAt: '2026-08-02T10:00:00+08:00',
  },
  {
    email: 'beta@icloud.com',
    anonymousId: 'anon_2',
    label: '订阅',
    active: false,
    createdAt: '2026-08-01T10:00:00+08:00',
  },
]

export const MESSAGES = [
  {
    id: '1042',
    from: 'GitHub <noreply@github.com>',
    to: 'alpha@icloud.com',
    subject: '请验证你的邮箱',
    date: '2026-07-09T14:32:10+08:00',
    preview: 'Almost done! To finish setting up your account...',
  },
]

/** 自动建满任务：单个账号的运行中状态 */
const AUTO_ACCOUNT_MAIN = {
  account_id: 'acc_1',
  name: '主号',
  phase: 'running',
  target: 700,
  total: 5,
  remaining: 695,
  created: 3,
  failed: 0,
  consecutive_failures: 0,
  capacity_checked: true,
  next_run_at: '2026-09-16T20:30:20+08:00',
  attempting: true,
  last_attempt_at: '2026-09-16T20:30:20+08:00',
  last_email: 'auto3@icloud.com',
}

const AUTO_ACCOUNT_BACKUP = {
  account_id: 'acc_2',
  name: '备用号',
  phase: 'cooling',
  target: 700,
  total: 12,
  remaining: 688,
  created: 2,
  failed: 1,
  consecutive_failures: 0,
  capacity_checked: true,
  next_run_at: '2026-09-16T21:00:00+08:00',
  attempting: false,
  last_error: '创建别名失败: HTTP 429: Too Many Requests',
}

export const handlers = [
  http.post('/api/auth/login', () =>
    HttpResponse.json({ success: true, data: { csrf_token: 'csrf-token', expires_at: '2026-08-05T22:00:00+08:00' } }),
  ),
  http.get('/api/auth/session', () =>
    HttpResponse.json({ success: true, data: { csrf_token: 'csrf-token', expires_at: '2026-08-05T22:00:00+08:00' } }),
  ),
  http.post('/api/auth/logout', () => HttpResponse.json({ success: true, data: { logged_out: true } })),
  http.get('/api/accounts', () => HttpResponse.json({ success: true, data: [ACCOUNT] })),
  http.get('/api/aliases', () =>
    HttpResponse.json({ success: true, data: { account_id: ACCOUNT.id, count: ALIASES.length, aliases: ALIASES } }),
  ),
  http.post('/api/create', () =>
    HttpResponse.json({
      success: true,
      data: {
        email: 'gamma@icloud.com',
        label: '新用途',
        created_at: '2026-08-06T10:00:00+08:00',
        account_id: 'acc_1',
      },
    }),
  ),
  http.get('/api/inbox', () =>
    HttpResponse.json({
      success: true,
      data: {
        account_id: ACCOUNT.id,
        count: MESSAGES.length,
        messages: MESSAGES,
        method: 'imap',
      },
    }),
  ),
  http.get('/api/inbox/:id', () =>
    HttpResponse.json({
      success: true,
      data: {
        ...MESSAGES[0],
        body: '点击链接完成验证：https://example.com/verify',
        // 原始 HTML(未清理,故意带脚本与事件属性)
        body_html:
          '<p onclick="alert(1)">点击链接完成验证：<a href="https://example.com/verify">verify</a></p><script>evil()</script>',
        // 清理后的版本(界面应使用这份)
        body_html_sanitized:
          '<p>点击链接完成验证：<a href="https://example.com/verify">verify</a></p>',
        content_type: 'text/html',
      },
    }),
  ),
  http.delete('/api/inbox/:id', () => HttpResponse.json({ success: true, data: { id: '1042' } })),
  http.post('/api/aliases/:id/deactivate', () =>
    HttpResponse.json({ success: true, data: { anonymous_id: 'anon_1', success: true } }),
  ),
  http.post('/api/aliases/:id/reactivate', () =>
    HttpResponse.json({ success: true, data: { anonymous_id: 'anon_2', success: true } }),
  ),
  http.delete('/api/aliases/:id', () => HttpResponse.json({ success: true, data: { anonymous_id: 'anon_1' } })),

  // 后台自动建满任务（多账号）
  http.get('/api/autocreate', () =>
    HttpResponse.json({
      success: true,
      data: {
        running: false,
        phase: 'idle',
        target: 700,
        total: 0,
        created: 0,
        failed: 0,
        interval_seconds: 20,
        interval_max_seconds: 40,
        cooldown_seconds: 1800,
        label_prefix: 'auto',
        max_failures: 5,
        max_parallel: 10,
        accounts: [],
        logs: [],
      },
    }),
  ),
  http.post('/api/autocreate/start', () =>
    HttpResponse.json({
      success: true,
      data: {
        running: true,
        phase: 'running',
        account_id: 'acc_1',
        target: 700,
        total: 17,
        created: 5,
        failed: 1,
        interval_seconds: 20,
        interval_max_seconds: 40,
        cooldown_seconds: 1800,
        label_prefix: 'auto',
        max_failures: 5,
        max_parallel: 10,
        started_at: '2026-09-16T20:30:00+08:00',
        next_run_at: '2026-09-16T20:30:20+08:00',
        attempting: true,
        last_attempt_at: '2026-09-16T20:30:20+08:00',
        last_email: 'auto3@icloud.com',
        accounts: [AUTO_ACCOUNT_MAIN, AUTO_ACCOUNT_BACKUP],
        logs: [
          { time: '2026-09-16T20:30:00+08:00', level: 'info', message: '任务启动:2 个账号,目标总数各 700,间隔 20s,冷却 1800s,并发上限 10' },
          { time: '2026-09-16T20:30:02+08:00', level: 'info', message: '[主号] 配额检查:已有 2 个,还需创建 698 个' },
          { time: '2026-09-16T20:30:05+08:00', level: 'warn', message: '[备用号] 命中速率限制,冷却 30m0s 后继续' },
          { time: '2026-09-16T20:30:12+08:00', level: 'info', message: '[主号] 等待 27s 后继续' },
          { time: '2026-09-16T20:30:20+08:00', level: 'success', message: '[主号] 创建成功 auto3@icloud.com(总计 5/700)' },
        ],
      },
    }),
  ),
  http.post('/api/autocreate/stop', () =>
    HttpResponse.json({
      success: true,
      data: {
        running: false,
        phase: 'stopped',
        target: 700,
        total: 17,
        created: 5,
        failed: 1,
        interval_seconds: 20,
        interval_max_seconds: 40,
        cooldown_seconds: 1800,
        label_prefix: 'auto',
        max_failures: 5,
        max_parallel: 10,
        reason: '已手动停止',
        accounts: [
          { ...AUTO_ACCOUNT_MAIN, phase: 'stopped', attempting: false, reason: '已手动停止' },
          { ...AUTO_ACCOUNT_BACKUP, phase: 'stopped', attempting: false, reason: '已手动停止' },
        ],
        logs: [],
      },
    }),
  ),
]
