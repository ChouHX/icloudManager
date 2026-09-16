#!/usr/bin/env node
/**
 * 浏览器冒烟测试:用真实 Chromium 打开管理界面,跑通「登录 → 创建别名 → 取件」三条主流程。
 *
 * 它补充单测覆盖不到的部分:CSP 是否放行 Ant Design 的运行时样式、
 * 组件库在真实浏览器里的渲染与交互、以及 SPA 路由与静态资源的配合。
 *
 * 用法:
 *   ICLOUD_HME_ADMIN_PASSWORD='你的密码' npm run e2e
 *   BASE_URL=http://127.0.0.1:9000 npm run e2e
 *
 * 前置条件:
 *   1. 服务已在 BASE_URL 上运行(见仓库 README 的启动说明)
 *   2. 浏览器已安装: npx playwright install chromium
 *
 * iCloud 侧数据(账号、别名、邮件)由本脚本用路由拦截伪造,不会访问真实 iCloud,
 * 因此无需准备真实账号即可验证界面与接口契约。
 */

import { chromium } from 'playwright'

const BASE_URL = (process.env.BASE_URL ?? 'http://127.0.0.1:8081').replace(/\/$/, '')
const PASSWORD = process.env.ICLOUD_HME_ADMIN_PASSWORD ?? 'e2e-admin-pass-2026'
const HEADLESS = process.env.HEADLESS !== 'false'

const ACCOUNT = {
  id: 'acc_1', name: '主号', real_email: 'owner@example.com', icloud_email: 'owner@icloud.com',
  host: 'icloud.com', status: 'active', alias_total: 2, alias_active: 1,
  has_cookies: true, has_app_password: true, has_proxy: false,
  last_validated: '2026-08-04T09:00:00+08:00', status_message: '', created_at: '2026-08-01T09:00:00+08:00',
}

const ALIASES = [
  { email: 'alpha@icloud.com', anonymousId: 'anon_1', label: '购物', active: true, createdAt: '2026-08-02T10:00:00+08:00' },
  { email: 'beta@icloud.com', anonymousId: 'anon_2', label: '订阅', active: false, createdAt: '2026-08-01T10:00:00+08:00' },
]

const MESSAGE = {
  id: '1042', from: 'GitHub <noreply@github.com>', to: 'alpha@icloud.com',
  subject: '请验证你的邮箱', date: '2026-07-09T14:32:10+08:00',
  preview: '点击链接完成验证',
}

// 故意在 HTML 里放脚本与追踪像素:验证沙箱隔离与 CSP 是否兜住
const MAIL_HTML_SANITIZED = `<html><head><style>.brand{color:#0f766e}</style></head><body>
<p class="brand">点击链接完成验证：</p>
<p><a href="https://example.com/verify">verify</a></p>
<img src="https://tracker.example/pixel.gif" alt="pixel">
</body></html>`

const MAIL_HTML = `<html><head><style>.brand{color:#0f766e}</style></head><body>
<p class="brand">点击链接完成验证：</p>
<p><a href="https://example.com/verify">verify</a></p>
<script>window.__mailScriptRan = true; document.body.innerHTML = '<p>脚本已执行</p>'</script>
<img src="https://tracker.example/pixel.gif" alt="pixel">
</body></html>`

const results = []
const consoleErrors = []

// 预期内的安全拦截记录,不算页面缺陷:
//  - 登录前的会话探测 401
//  - 沙箱按设计拦下邮件里的脚本
//  - CSP 按设计拦下邮件里的远程(追踪)图片
const EXPECTED_CONSOLE_NOISE = [
  /401/,
  /Blocked script execution/,
  /violates the following Content Security Policy/,
]

function check(name, passed, detail = '') {
  results.push({ name, passed, detail })
  console.log(`${passed ? '✓' : '✗'} ${name}${detail && !passed ? ` — ${detail}` : ''}`)
}

const json = (data) => ({ status: 200, contentType: 'application/json', body: JSON.stringify({ success: true, data }) })

async function launchBrowser() {
  const attempts = [
    ['playwright 自带 chromium', {}],
    ['系统 Chrome', { channel: 'chrome' }],
    ['系统 Chromium', { executablePath: '/usr/bin/chromium' }],
    ['系统 google-chrome', { executablePath: '/usr/bin/google-chrome' }],
  ]
  const failures = []
  for (const [name, options] of attempts) {
    try {
      return await chromium.launch({ headless: HEADLESS, ...options })
    } catch (err) {
      failures.push(`${name}: ${String(err.message).split('\n')[0]}`)
    }
  }
  throw new Error(
    `无法启动浏览器,请先执行 \`npx playwright install chromium\`。已尝试:\n  ${failures.join('\n  ')}`,
  )
}

async function main() {
  // 先确认服务在线,给出可执行的提示而不是让 Playwright 抛超时
  try {
    const res = await fetch(BASE_URL, { redirect: 'manual' })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
  } catch (err) {
    console.error(`无法访问 ${BASE_URL}: ${err.message}`)
    console.error('请先启动服务,例如:')
    console.error("  export ICLOUD_HME_ADMIN_PASSWORD='your-strong-password'")
    console.error('  go run main.go -debug        # 或 ./build/icloud-hme')
    process.exit(1)
  }

  const browser = await launchBrowser()
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: 'zh-CN' })
  const page = await context.newPage()
  page.on('console', (msg) => {
    if (msg.type() !== 'error') return
    const text = msg.text()
    if (EXPECTED_CONSOLE_NOISE.some((pattern) => pattern.test(text))) return
    consoleErrors.push(text)
  })
  page.on('pageerror', (err) => consoleErrors.push(`页面异常: ${err.message}`))

  await page.route('**/api/accounts', (route) => route.fulfill(json([ACCOUNT])))
  await page.route('**/api/aliases**', (route) =>
    route.fulfill(json({ account_id: ACCOUNT.id, count: ALIASES.length, aliases: ALIASES })),
  )
  await page.route('**/api/create', (route) =>
    route.fulfill(json({ email: 'gamma@icloud.com', label: '新用途', created_at: '2026-08-06T10:00:00+08:00', account_id: ACCOUNT.id })),
  )
  await page.route('**/api/inbox**', (route) => {
    const path = new URL(route.request().url()).pathname
    if (/^\/api\/inbox\/[^/]+$/.test(path)) {
      return route.fulfill(json({
        ...MESSAGE,
        body: '点击链接完成验证：https://example.com/verify',
        body_html: MAIL_HTML,
        body_html_sanitized: MAIL_HTML_SANITIZED,
        content_type: 'text/html',
      }))
    }
    return route.fulfill(json({ account_id: ACCOUNT.id, count: 1, messages: [MESSAGE], method: 'imap' }))
  })

  try {
    // 1. 未登录时落到登录页
    await page.goto(BASE_URL, { waitUntil: 'networkidle' })
    await page.waitForURL('**/login')
    check('未登录访问跳转登录页', await page.getByRole('heading', { name: 'iCloud 隐私邮箱' }).isVisible())

    // 2. 用真实管理员密码登录(打到运行中的服务)
    await page.getByLabel('管理员密码').fill(PASSWORD)
    await page.getByRole('button', { name: /登\s*录/ }).click()
    await page.waitForURL('**/aliases')

    // 3. 组件库样式是否被 CSP 拦截:主题变量、注入的 style 标签与渲染像素
    await page.getByRole('button', { name: /创建别名/ }).waitFor()
    const theme = await page.evaluate(() => {
      const root = document.querySelector('.ant-app') ?? document.body
      return {
        primaryVar: getComputedStyle(root).getPropertyValue('--ant-color-primary').trim(),
        styleTags: document.querySelectorAll('style').length,
        rules: [...document.styleSheets].reduce((n, s) => {
          try {
            return n + s.cssRules.length
          } catch {
            return n
          }
        }, 0),
      }
    })
    check('Ant Design 样式已注入且未被 CSP 拦截', theme.styleTags > 10 && theme.rules > 500, JSON.stringify(theme))
    check('主题色生效', theme.primaryVar.toLowerCase() === '#0f766e', theme.primaryVar)

    // 4. 别名列表
    await page.getByText('alpha@icloud.com').waitFor()
    const table = page.locator('tbody tr')
    check('别名列表渲染', (await table.count()) >= 2, `行数 ${await table.count()}`)
    check('停用状态标记', await page.getByText('已停用').first().isVisible())

    // 5. 自动建满面板(读取真实后端的任务状态)
    await page.getByRole('button', { name: /自动建满/ }).click()
    const panel = page.locator('.ant-drawer').filter({ hasText: '自动建满别名' })
    await panel.getByText('未运行').waitFor()
    check('自动建满面板可打开', await panel.getByLabel('每账号目标总数').isVisible())
    check('面板支持多选账号', (await panel.getByLabel('账号（可多选，并行创建）').count()) > 0)
    check('面板展示任务参数', await panel.getByLabel('间隔最小（秒）').inputValue().then((v) => v === '20'))
    check('面板支持随机间隔上限', await panel.getByLabel('间隔最大（秒）').inputValue().then((v) => v === '40'))
    await page.locator('.ant-drawer-close').last().click()
    await page.waitForTimeout(400)

    // 6. 创建别名
    await page.getByRole('button', { name: /创建别名/ }).click()
    await page.getByLabel('标签（可选）').fill('新用途')
    await page.getByRole('button', { name: /创\s*建$/ }).click()
    await page.getByText('别名创建成功').waitFor()
    check('创建别名返回新地址', await page.getByText('gamma@icloud.com').first().isVisible())
    await page.locator('.ant-modal-footer button').first().click()

    // 7. 取件
    await page.getByRole('menuitem', { name: '取件' }).click()
    await page.waitForURL('**/inbox**')
    await page.getByText('请验证你的邮箱').waitFor()
    const method = await page.getByText(/读取方式/).first().innerText()
    check('收件列表与读取方式', method.includes('IMAP'), method)

    await page.getByRole('button', { name: '请验证你的邮箱' }).click()
    const mailFrame = page.frameLocator('iframe[title="邮件正文"]')
    await mailFrame.getByText(/点击链接完成验证/).waitFor()
    check('邮件正文按 HTML 渲染', await mailFrame.getByText(/点击链接完成验证/).isVisible())
    check('邮件样式在沙箱内生效', await mailFrame.locator('.brand').evaluate((el) => getComputedStyle(el).color).then((c) => c === 'rgb(15, 118, 110)'))

    // 沙箱隔离:mock 数据故意带 script,它不应执行也不应改写内容
    check('邮件脚本未执行', (await page.evaluate(() => Boolean(window.__mailScriptRan))) === false)
    check('界面使用清理版正文', (await mailFrame.locator('script').count()) === 0)
    check('邮件脚本未篡改正文', (await mailFrame.getByText('脚本已执行').count()) === 0)
    check(
      '远程图片被 CSP 阻止',
      (await mailFrame.locator('img').evaluate((el) => el.naturalWidth === 0)) === true,
    )

    // 纯文本视图
    await page.locator('.ant-segmented-item-label', { hasText: '纯文本' }).click()
    await page.getByText(/点击链接完成验证/).first().waitFor()
    check('可切换纯文本视图', await page.locator('pre.mail-body').isVisible())

    await page.locator('.ant-drawer-close').click()
    await page.waitForTimeout(400)

    // 8. 账号设置页
    await page.getByRole('menuitem', { name: '账号设置' }).click()
    await page.waitForURL('**/accounts**')
    await page.getByText('主号').first().waitFor()
    const credentialTag = page.locator('tbody .ant-tag', { hasText: 'Cookie' }).first()
    let credentialVisible = true
    try {
      await credentialTag.waitFor({ state: 'visible', timeout: 5000 })
    } catch {
      credentialVisible = false
    }
    check('账号页渲染凭据状态', credentialVisible)

    check('无页面级 JS 异常', consoleErrors.length === 0, consoleErrors.join(' | '))
  } catch (err) {
    check('冒烟流程执行完成', false, err.message)
  } finally {
    await browser.close()
  }

  const failed = results.filter((r) => !r.passed)
  console.log(`\n${results.length - failed.length}/${results.length} 项通过`)
  process.exit(failed.length === 0 ? 0 : 1)
}

await main()
