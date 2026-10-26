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

export const TAG_ENTRIES = [
  {
    id: 'tag_1',
    email: 'user@example.com',
    password: 's3cret-pass',
    site: 'GitHub',
    sites: ['GitHub', 'Steam'],
    tags: ['项目A', '开发'],
    note: '公司项目',
    created_at: '2026-08-01T09:00:00+08:00',
    updated_at: '2026-08-02T09:00:00+08:00',
  },
  {
    id: 'tag_2',
    email: 'gamer@example.com',
    password: 'game-pass',
    site: 'Steam',
    sites: ['Steam'],
    tags: ['游戏'],
    note: '',
    created_at: '2026-08-03T09:00:00+08:00',
    updated_at: '2026-08-03T09:00:00+08:00',
  },
]

export const GMAIL_ACCOUNTS = [
  {
    id: 'gm_1',
    email: 'mail@gmail.com',
    note: '',
    filter: { include: ['验证码'], exclude: [], days: 7 },
    status: 'active',
    has_password: true,
    has_totp: true,
    has_app_password: true,
    last_checked: '2026-10-26T09:00:00+08:00',
    last_error: '',
    created_at: '2026-10-20T09:00:00+08:00',
    updated_at: '2026-10-26T09:00:00+08:00',
  },
]

/** 与后端 ListQuery 对应的筛选逻辑,供列表与批量打标签复用 */function filterTagEntries(query: {
  search?: string
  tags?: string[]
  site?: string
  created_from?: string
  created_to?: string
}) {
  const search = (query.search ?? '').trim().toLowerCase()
  const site = (query.site ?? '').trim().toLowerCase()
  const filters = (query.tags ?? []).map((tag) => tag.trim().toLowerCase()).filter(Boolean)
  const from = query.created_from ? new Date(query.created_from).getTime() : null
  const to = query.created_to ? new Date(query.created_to).getTime() : null

  return TAG_ENTRIES.filter((entry) => {
    const entryTags = (entry.tags ?? []).map((tag) => tag.toLowerCase())
    const entrySites = (entry.sites ?? (entry.site ? [entry.site] : [])).map((site) => site.toLowerCase())
    if (site && !entrySites.includes(site)) return false
    if (filters.length > 0 && !entryTags.some((tag) => filters.includes(tag))) return false
    if (from !== null || to !== null) {
      const created = new Date(entry.created_at).getTime()
      if (from !== null && created < from) return false
      if (to !== null && created > to) return false
    }
    if (!search) return true
    return (
      entry.email.toLowerCase().includes(search) ||
      entrySites.some((item) => item.includes(search)) ||
      (entry.note ?? '').toLowerCase().includes(search) ||
      entryTags.some((tag) => tag.includes(search))
    )
  })
}

/** 判断筛选条件是否包含任何有效字段(与后端 ListQuery.hasFilter 对应) */
function hasTagFilter(filter?: {
  search?: string
  tags?: string[]
  site?: string
  created_from?: string
  created_to?: string
}) {
  return Boolean(
    filter &&
      ((filter.search ?? '').trim() ||
        (filter.site ?? '').trim() ||
        (filter.created_from ?? '').trim() ||
        (filter.created_to ?? '').trim() ||
        (filter.tags ?? []).some((tag) => tag.trim() !== '')),
  )
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
  http.get('/api/dashboard', () =>
    HttpResponse.json({
      success: true,
      data: {
        accounts: {
          total: 1,
          alias_total: ACCOUNT.alias_total,
          alias_active: ACCOUNT.alias_active,
          items: [ACCOUNT],
        },
        tags: {
          total: TAG_ENTRIES.length,
          tagged: TAG_ENTRIES.filter((entry) => (entry.tags ?? []).length > 0).length,
          untagged: TAG_ENTRIES.filter((entry) => (entry.tags ?? []).length === 0).length,
          tag_kinds: 3,
          site_kinds: 2,
          by_site: [
            { site: 'GitHub', count: 1 },
            { site: 'Steam', count: 1 },
          ],
          by_tag: [
            { tag: '开发', count: 1 },
            { tag: '游戏', count: 1 },
            { tag: '项目A', count: 1 },
          ],
          by_site_tag: [
            {
              site: 'GitHub',
              total: 1,
              tags: [
                { tag: '开发', count: 1 },
                { tag: '项目A', count: 1 },
              ],
            },
            { site: 'Steam', total: 1, tags: [{ tag: '游戏', count: 1 }] },
          ],
        },
      },
    }),
  ),
  http.get('/api/tags', ({ request }) => {
    const url = new URL(request.url)
    const page = Math.max(1, Number(url.searchParams.get('page') ?? '1') || 1)
    const pageSize = Math.max(1, Number(url.searchParams.get('page_size') ?? '20') || 20)
    const tagParam = (url.searchParams.get('tags') ?? '').trim()
    const filtered = filterTagEntries({
      search: url.searchParams.get('search') ?? '',
      tags: tagParam ? tagParam.split(',') : [],
      site: url.searchParams.get('site') ?? '',
      created_from: url.searchParams.get('created_from') ?? '',
      created_to: url.searchParams.get('created_to') ?? '',
    })

    const start = (page - 1) * pageSize
    const entries = filtered.slice(start, start + pageSize)
    return HttpResponse.json({
      success: true,
      data: {
        count: filtered.length,
        page,
        page_size: pageSize,
        total_pages: Math.ceil(filtered.length / pageSize),
        tags: ['开发', '游戏', '项目A'],
        tag_counts: [
          { tag: '开发', count: 1 },
          { tag: '游戏', count: 1 },
          { tag: '项目A', count: 1 },
        ],
        stats: { total: TAG_ENTRIES.length, tags: 3, sites: 2, untagged: 0 },
        sites: ['GitHub', 'Steam'],
        entries,
      },
    })
  }),
  http.post('/api/tags/batch', async ({ request }) => {
    const body = (await request.json()) as {
      ids?: string[]
      filter?: {
        search?: string
        tags?: string[]
        site?: string
        created_from?: string
        created_to?: string
      }
      action?: 'add' | 'remove' | 'replace'
      tags?: string[]
    }
    if (!body.tags || body.tags.length === 0) {
      return HttpResponse.json(
        { success: false, code: 'VALIDATION_ERROR', message: '请至少填写一个标签' },
        { status: 400 },
      )
    }
    const ids = body.ids ?? []
    const affected =
      ids.length > 0
        ? TAG_ENTRIES.filter((entry) => ids.includes(entry.id)).length
        : filterTagEntries(body.filter ?? {}).length
    return HttpResponse.json({
      success: true,
      data: { affected, action: body.action ?? 'add', tags: body.tags },
    })
  }),

  http.post('/api/tags/batch-delete', async ({ request }) => {
    const body = (await request.json()) as {
      ids?: string[]
      filter?: {
        search?: string
        tags?: string[]
        site?: string
        created_from?: string
        created_to?: string
      }
      all?: boolean
    }
    if (!body.all && !(body.ids ?? []).length && !hasTagFilter(body.filter)) {
      return HttpResponse.json(
        { success: false, code: 'VALIDATION_ERROR', message: '请先筛选或勾选要删除的记录' },
        { status: 400 },
      )
    }
    const affected = body.all
      ? TAG_ENTRIES.length
      : (body.ids ?? []).length > 0
        ? TAG_ENTRIES.filter((entry) => (body.ids ?? []).includes(entry.id)).length
        : filterTagEntries(body.filter ?? {}).length
    return HttpResponse.json({ success: true, data: { affected } })
  }),

  http.post('/api/tags', () =>
    HttpResponse.json({
      success: true,
      data: {
        id: 'tag_3',
        email: 'new@example.com',
        password: '',
        site: '',
        tags: ['项目B'],
        note: '',
        created_at: '2026-08-06T10:00:00+08:00',
        updated_at: '2026-08-06T10:00:00+08:00',
      },
    }),
  ),
  http.put('/api/tags/:id', () =>
    HttpResponse.json({ success: true, data: TAG_ENTRIES[0] }),
  ),
  http.delete('/api/tags/:id', () =>
    HttpResponse.json({ success: true, data: { id: 'tag_1' } }),
  ),
  http.post('/api/tags/import', () =>
    HttpResponse.json({
      success: true,
      data: {
        imported: 2,
        updated: 0,
        skipped: 1,
        invalid: 1,
        sites_appended: 0,
        entries: TAG_ENTRIES,
        errors: ['第 4 行: 邮箱格式不正确'],
      },
    }),
  ),
  http.put('/api/tag-labels/rename', async ({ request }) => {
    const body = (await request.json()) as { from: string; to: string }
    return HttpResponse.json({ success: true, data: { changed: 1, from: body.from, to: body.to } })
  }),
  http.delete('/api/tag-labels/:name', () =>
    HttpResponse.json({ success: true, data: { changed: 1, tag: '开发' } }),
  ),
  http.post('/api/tags/export', async ({ request }) => {
    const body = (await request.json()) as {
      ids?: string[]
      filter?: {
        search?: string
        tags?: string[]
        site?: string
        created_from?: string
        created_to?: string
      }
    }
    const ids = body.ids ?? []
    const entries =
      ids.length > 0
        ? TAG_ENTRIES.filter((entry) => ids.includes(entry.id))
        : filterTagEntries(body.filter ?? {})
    return HttpResponse.json({
      success: true,
      data: {
        version: 1,
        type: 'icloud-hme-tags',
        exported_at: '2026-08-06T10:00:00+08:00',
        count: entries.length,
        entries,
      },
    })
  }),
  http.get('/api/tags/export', () =>
    HttpResponse.json({
      success: true,
      data: {
        version: 1,
        type: 'icloud-hme-tags',
        exported_at: '2026-08-06T10:00:00+08:00',
        count: TAG_ENTRIES.length,
        entries: TAG_ENTRIES,
      },
    }),
  ),
  http.post('/api/tags/restore', () =>
    HttpResponse.json({
      success: true,
      data: { imported: 2, skipped: 1, invalid: 0, total: 3, errors: [] },
    }),
  ),
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

  // 取件链接:管理侧生成/撤销 + 公开侧取件
  http.post('/api/aliases/:id/share-link', () =>
    HttpResponse.json({
      success: true,
      data: {
        alias: 'alpha@icloud.com',
        token: 'tok_test_token',
        url: 'http://localhost:8081/?token=tok_test_token',
        created_at: '2026-09-16T22:00:00+08:00',
        hits: 0,
      },
    }),
  ),
  http.delete('/api/aliases/:id/share-link', () =>
    HttpResponse.json({ success: true, data: { alias: 'alpha@icloud.com', removed: true } }),
  ),
  http.get('/api/share/:token/inbox/:id', () =>
    HttpResponse.json({
      success: true,
      data: {
        ...MESSAGES[0],
        body: '点击链接完成验证：https://example.com/verify',
        body_html_sanitized: '<p>点击链接完成验证：<a href="https://example.com/verify">verify</a></p>',
        content_type: 'text/html',
      },
    }),
  ),
  http.get('/api/share/:token/inbox', () =>
    HttpResponse.json({
      success: true,
      data: { alias: 'alpha@icloud.com', count: MESSAGES.length, messages: MESSAGES, method: 'imap' },
    }),
  ),

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
  http.get('/api/gmail', () =>
    HttpResponse.json({
      success: true,
      data: {
        count: GMAIL_ACCOUNTS.length,
        accounts: GMAIL_ACCOUNTS,
      },
    }),
  ),
  http.post('/api/gmail/import', () =>
    HttpResponse.json({
      success: true,
      data: { imported: 1, updated: 0, invalid: 1, errors: ['第 2 行: 邮箱地址格式无效'] },
    }),
  ),
  http.put('/api/gmail/:id/filter', async ({ params, request }) => {
    const body = (await request.json()) as { include?: string[]; exclude?: string[]; days?: number }
    const account = GMAIL_ACCOUNTS.find((item) => item.id === params.id) ?? GMAIL_ACCOUNTS[0]
    return HttpResponse.json({
      success: true,
      data: {
        ...account,
        filter: { include: body.include ?? [], exclude: body.exclude ?? [], days: body.days ?? 0 },
      },
    })
  }),
  http.post('/api/gmail/:id/share-link', ({ params }) => {
    const account = GMAIL_ACCOUNTS.find((item) => item.id === params.id) ?? GMAIL_ACCOUNTS[0]
    return HttpResponse.json({
      success: true,
      data: {
        email: account.email,
        token: 'gmail-token',
        url: `http://127.0.0.1:5173/?gtoken=gmail-token`,
        created_at: '2026-10-26T10:00:00+08:00',
        hits: 0,
      },
    })
  }),
  http.delete('/api/gmail/:id/share-link', () =>
    HttpResponse.json({ success: true, data: { email: 'mail@gmail.com', removed: true } }),
  ),
  http.post('/api/gmail/:id/check', () =>
    HttpResponse.json({ success: true, data: { id: 'gm_1', status: 'active' } }),
  ),
  http.delete('/api/gmail/:id', () =>
    HttpResponse.json({ success: true, data: { id: 'gm_1', removed: true } }),
  ),
  http.get('/api/gmail/:id/inbox', () =>
    HttpResponse.json({
      success: true,
      data: {
        email: 'mail@gmail.com',
        count: 1,
        method: 'imap',
        messages: [
          {
            id: '101',
            from: 'noreply@example.com',
            to: 'mail@gmail.com',
            subject: '您的验证码是 123456',
            date: '2026-10-26T09:00:00+08:00',
            preview: '验证码 123456',
          },
        ],
      },
    }),
  ),
  http.get('/api/gmail/:id/inbox/:messageId', ({ params }) =>
    HttpResponse.json({
      success: true,
      data: {
        id: String(params.messageId),
        from: 'noreply@example.com',
        to: 'mail@gmail.com',
        subject: '您的验证码是 123456',
        date: '2026-10-26T09:00:00+08:00',
        preview: '验证码 123456',
        body: '验证码 123456',
        content_type: 'text/plain',
      },
    }),
  ),
  http.get('/api/gmail-share/:token/inbox', () =>    HttpResponse.json({
      success: true,
      data: {
        alias: 'mail@gmail.com',
        count: 1,
        method: 'imap',
        messages: [
          {
            id: '101',
            from: 'noreply@example.com',
            to: 'mail@gmail.com',
            subject: '您的验证码是 123456',
            date: '2026-10-26T09:00:00+08:00',
            preview: '验证码 123456',
          },
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
