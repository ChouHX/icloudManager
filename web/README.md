# 管理界面（web）

iCloud HME 的管理界面前端：React 19 + TypeScript + Vite 8 + Ant Design 6。构建产物输出到 `../internal/webui/dist`，由 Go 侧 `//go:embed` 内嵌进二进制，最终以单个可执行文件分发。

## 页面

- **创建别名** `/aliases` — 主页面：一键创建新别名，复制地址，查看/筛选/停用/激活/删除已有别名
- **取件** `/inbox` — 按账号与别名读取邮件；详情默认按 HTML 渲染（见下），可切换纯文本，支持删除
- **账号设置** `/accounts` — 账号增删改与凭据配置（Cookie / App 专用密码 / 收件邮箱 / 代理）
- **登录** `/login` — 管理员密码登录

## 邮件正文渲染

`GET /api/inbox/:id` 返回 `body`（纯文本）与 `body_html`（清理后的原始 HTML）。`src/components/MailBody.tsx` 把 HTML 放进 `sandbox="allow-same-origin"` 的 iframe：

- 不授予 `allow-scripts`，邮件里的脚本不会执行，也不会篡改管理界面
- 邮件样式被隔离在 iframe 内，不会污染主页面
- 只授予 `allow-same-origin` 是为了测量内容高度做自适应；渲染仍以服务端清理（移除 script / 事件属性 / `javascript:`）× CSP（`script-src 'self'`、`img-src 'self' data:`）作为防线
- 远程图片会被 CSP 拦下（避免追踪像素泄露阅读状态与 IP），界面会明确提示；确需显示可在 `internal/server/middleware.go` 的 CSP 中放宽 `img-src`

## 命令

```bash
npm ci            # 安装依赖
npm run dev       # 开发模式（/api 代理到 127.0.0.1:8081）
npm run build     # 类型检查 + 构建到 ../internal/webui/dist
npm run test:run  # vitest（jsdom + msw）
npm run lint      # eslint
npm run check     # lint + test + build
npm run e2e       # 浏览器冒烟（需要后端已启动，见下）
```

## 本地测试

### 单元测试（jsdom + msw，不需要后端）

```bash
npm run test        # watch 模式
npm run test:run    # 单次运行
npm run lint        # eslint
npm run check       # lint + test + build，等价 CI 的前端步骤
```

页面测试使用 `src/test/handlers.ts` 里的假数据，覆盖创建别名、列表筛选、取件详情、删除邮件、账号配置弹窗等主流程，并用 `src/test/render.tsx` 包裹 Ant Design 上下文与内存路由。

### 浏览器冒烟（真实 Chromium，需要后端在线）

jsdom 覆盖不到 CSP 与真实渲染，`e2e/smoke.mjs` 补这一层：

```bash
# 1. 另开一个终端启动后端
export ICLOUD_HME_ADMIN_PASSWORD='your-strong-password'
go run main.go -debug            # 或 ./build/icloud-hme

# 2. 首次需要安装浏览器
npx playwright install chromium

# 3. 运行冒烟
export ICLOUD_HME_ADMIN_PASSWORD='your-strong-password'
npm run e2e
```

脚本用真实管理员密码登录运行中的服务，iCloud 侧数据（账号 / 别名 / 邮件）通过路由拦截伪造，因此不需要真实 iCloud 账号，也不会访问外网。它覆盖：登录跳转、Ant Design 样式是否被 CSP 拦截、主题色、别名列表与状态标记、创建别名、收件列表与读取方式、邮件正文按 HTML 渲染、邮件脚本被沙箱拦截、追踪图片被 CSP 拦截、纯文本切换、账号页凭据标签、页面级 JS 异常。脚本注入的邮件 HTML 里故意带 `<script>` 与远程追踪像素，用来证明这两道防线生效。

可用环境变量：

| 变量 | 默认 | 说明 |
|---|---|---|
| `BASE_URL` | `http://127.0.0.1:8081` | 被测服务地址 |
| `ICLOUD_HME_ADMIN_PASSWORD` | `e2e-admin-pass-2026` | 管理员密码，需与后端启动时一致 |
| `HEADLESS` | `true` | 设为 `false` 可看到浏览器操作过程 |

### 手动联调

```bash
npm run dev   # vite dev server：改前端代码即时热更新，/api 代理到 127.0.0.1:8081
```

代理目标默认 `http://127.0.0.1:8081`，可用 `HME_API_TARGET` 覆盖（仓库根的 `./dev.sh` 会在自定义端口时自动注入）。开发模式下 React 会输出完整告警，`./dev.sh --smoke` 正是借助这一点发现生产构建中被剥离的废弃 API 用法。

## 约定

- **样式全部来自 Ant Design**：主题 token 在 `src/main.tsx` 统一定义；`src/styles.css` 只保留页面骨架与少量排版类，不再维护组件级 CSS
- **接口契约**：`src/api/types.ts` 与 `internal/server` 的响应结构一一对应。注意账号摘要是 snake_case，别名对象沿用 iCloud 原始 camelCase
- **数据读取**：`src/api/hooks.ts` 的 `useFetch` / `useAccounts` 统一处理请求、错误与 loading；loading 由「请求是否已结算」派生，effect 内不做同步 setState
- **表单初始化**：弹窗用 `destroyOnHidden + preserve={false}` 在每次打开时重新挂载，避免在 effect 里同步表单值
- **CSRF**：`src/api/client.ts` 自动为非 GET 请求附加 `X-CSRF-Token`，401 触发全局登出
- **CSP**：服务端策略为 `style-src 'self' 'unsafe-inline'`（Ant Design 运行时样式注入所需），`script-src` 仍为 `'self'`

## 测试

`src/test/` 提供 vitest + msw 基础设施：`handlers.ts` 是该工具的接口假数据，`render.tsx` 负责包裹 AntD 上下文与内存路由。页面测试覆盖创建别名、列表筛选、取件与详情、删除邮件等主流程。
