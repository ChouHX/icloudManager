#!/usr/bin/env node
/**
 * Node.js 接入示例：获取别名邮箱列表与取件。
 *
 *   node examples/fetch_inbox.mjs --base http://127.0.0.1:8081 --password '你的管理员密码'
 *   node examples/fetch_inbox.mjs --base http://127.0.0.1:8081 --password '...' --alias xyz@icloud.com
 *   node examples/fetch_inbox.mjs --base http://127.0.0.1:8081 --password '...' --message-id 1042
 *
 * 要点：
 *   - Node 内置 fetch 不管理 Cookie，这里用 Map 手动维护 hme_session
 *   - 读取接口（GET）不需要 CSRF；删除等写操作才需要 X-CSRF-Token
 *   - 会话过期（401）时自动重新登录一次
 */
import { parseArgs } from 'node:util'

const cookies = new Map()

function cookieHeader() {
  return [...cookies].map(([name, value]) => `${name}=${value}`).join('; ')
}

function storeCookies(response) {
  // Node 20+ 提供 getSetCookie()，旧版本回退到 raw()
  const lines = response.headers.getSetCookie?.() ?? response.headers.raw?.()['set-cookie'] ?? []
  for (const line of lines) {
    const [pair] = line.split(';')
    const index = pair.indexOf('=')
    if (index > 0) cookies.set(pair.slice(0, index).trim(), pair.slice(index + 1).trim())
  }
}

class HmeError extends Error {
  constructor(status, code, message) {
    super(`HTTP ${status} ${code}: ${message}`)
    this.status = status
    this.code = code
  }
}

class HmeClient {
  constructor(base, password, timeoutMs = 30000) {
    this.base = base.replace(/\/$/, '')
    this.password = password
    this.timeoutMs = timeoutMs
    this.csrf = ''
  }

  async request(method, path, body, mutate = false) {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), this.timeoutMs)
    let response
    try {
      response = await fetch(this.base + path, {
        method,
        signal: controller.signal,
        headers: {
          Accept: 'application/json',
          ...(body ? { 'Content-Type': 'application/json' } : {}),
          ...(cookies.size ? { Cookie: cookieHeader() } : {}),
          ...(mutate && this.csrf ? { 'X-CSRF-Token': this.csrf } : {}),
        },
        body: body ? JSON.stringify(body) : undefined,
      })
    } finally {
      clearTimeout(timer)
    }
    storeCookies(response)

    const text = await response.text()
    let payload
    try {
      payload = JSON.parse(text || '{}')
    } catch {
      throw new HmeError(response.status, 'INVALID_RESPONSE', text.slice(0, 120))
    }
    if (!response.ok || payload.success === false) {
      throw new HmeError(response.status, payload.code ?? '', payload.message ?? '')
    }
    return payload.data
  }

  async withRelogin(method, path, body, mutate = false) {
    try {
      return await this.request(method, path, body, mutate)
    } catch (err) {
      if (!(err instanceof HmeError) || err.status !== 401 || path.endsWith('/api/auth/login')) throw err
      await this.login()
      return this.request(method, path, body, mutate)
    }
  }

  async login() {
    const data = await this.request('POST', '/api/auth/login', { password: this.password })
    this.csrf = data.csrf_token ?? ''
    return data
  }

  accounts() {
    return this.withRelogin('GET', '/api/accounts')
  }

  aliases(accountId) {
    return this.withRelogin('GET', `/api/aliases?account_id=${encodeURIComponent(accountId)}`)
  }

  inbox(accountId, { alias, limit = 20, days = 7 } = {}) {
    const params = new URLSearchParams({ account_id: accountId, limit: String(limit), days: String(days) })
    if (alias) params.set('alias', alias)
    return this.withRelogin('GET', `/api/inbox?${params}`)
  }

  message(accountId, messageId) {
    const params = new URLSearchParams({ account_id: accountId })
    return this.withRelogin('GET', `/api/inbox/${encodeURIComponent(messageId)}?${params}`)
  }

  deleteMessage(accountId, messageId) {
    const params = new URLSearchParams({ account_id: accountId })
    // 写操作需要 CSRF
    return this.withRelogin('DELETE', `/api/inbox/${encodeURIComponent(messageId)}?${params}`, undefined, true)
  }
}

const { values } = parseArgs({
  options: {
    base: { type: 'string', default: 'http://127.0.0.1:8081' },
    password: { type: 'string' },
    account: { type: 'string' },
    alias: { type: 'string' },
    limit: { type: 'string', default: '20' },
    days: { type: 'string', default: '7' },
    'message-id': { type: 'string' },
  },
})

if (!values.password) {
  console.error('必须提供 --password')
  process.exit(2)
}

const client = new HmeClient(values.base, values.password)

try {
  await client.login()
  console.log('登录成功:', values.base)

  const accounts = await client.accounts()
  if (!accounts?.length) {
    console.error('账号列表为空：请先在管理界面添加 iCloud 账号并配置凭据')
    process.exit(1)
  }
  const account =
    accounts.find((item) => item.id === values.account || item.name === values.account) ?? accounts[0]
  console.log('使用账号:', account.id)

  const { count, aliases } = await client.aliases(account.id)
  console.log(`别名邮箱 ${count} 个:`)
  for (const alias of aliases.slice(0, 10)) {
    console.log(`  ${alias.email}  [${alias.active ? '启用' : '停用'}]  ${alias.label || '-'}`)
  }

  if (values['message-id']) {
    const detail = await client.message(account.id, values['message-id'])
    console.log(`\n--- ${detail.subject} ---`)
    console.log('发件人:', detail.from)
    console.log('内容类型:', detail.content_type)
    console.log(detail.body || '(无正文)')
    process.exit(0)
  }

  const result = await client.inbox(account.id, {
    alias: values.alias,
    limit: Number(values.limit),
    days: Number(values.days),
  })
  console.log(`共 ${result.count} 封，读取方式: ${result.method}`)
  for (const message of result.messages) {
    console.log(`  [${message.id}] ${message.date} | ${message.from}`)
    console.log(`      ${message.subject}`)
  }
} catch (err) {
  console.error('调用失败:', err.message)
  process.exit(1)
}
