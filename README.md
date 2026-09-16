# iCloud Hide My Email 本地管理工具

[English](#english) | 中文

通过逆向 iCloud Web 接口和 IMAP 邮件协议，实现 Apple iCloud 隐藏邮箱别名的创建、列出和邮件收取功能。内置中文管理界面（React 19 + Ant Design 单页应用，随二进制内嵌分发），界面只聚焦两件事：**创建别名** 与 **取件**。

## 功能特性

- ✅ **极简中文界面** — 三个页面（创建别名 / 取件 / 账号设置），浏览器访问 `http://localhost:8081` 即开即用
- ✅ **创建别名** — 一键生成 Hide My Email 地址，支持标签、复制、停用/激活/删除
- ✅ **取件** — 按别名与时间范围筛选邮件；详情按 HTML 排版渲染（sandbox 隔离，脚本与追踪像素都不会生效），可一键切换纯文本，支持删除
- ✅ **双路径读信** — 优先 IMAP (App 专用密码)，缺失时回退 Web API (Cookie)
- ✅ **取件链接** — 为任意别名生成只读链接（`host:port/?token=xxx`），凭链接即可查看该别名的邮件，无需登录
- ✅ **自动建满** — 后台按间隔自动创建别名直到达到目标总数，命中限流自动冷却续跑，界面可看进度与日志
- ✅ **多账号管理** — 多账号并行，Cookie / App 密码 / 收件邮箱集中配置
- ✅ **安全模型** — 单管理员会话、CSRF 校验、登录限流、响应脱敏、严格 CSP

## 快速开始

### 1. 安装

#### 方式一：下载二进制发布版（推荐）

从 [GitHub Releases](https://github.com/xiaozhou26/icloud-hme/releases) 下载对应平台的二进制文件：

| 平台 | 文件 |
|---|---|
| Linux x86_64 | `icloud-hme_linux_amd64` |
| Linux ARM64 | `icloud-hme_linux_arm64` |
| macOS Intel | `icloud-hme_darwin_amd64` |
| macOS Apple Silicon | `icloud-hme_darwin_arm64` |
| Windows x86_64 | `icloud-hme_windows_amd64.exe` |

```bash
# 示例：Linux 下直接运行（必须先设置管理员密码）
export ICLOUD_HME_ADMIN_PASSWORD='change-this-before-running-2026'
chmod +x icloud-hme_linux_amd64
./icloud-hme_linux_amd64
```

#### 方式二：Docker

```bash
# 拉取镜像
docker pull ghcr.io/xiaozhou26/icloud-hme:latest

# 运行（将本机 data 目录挂载进去）
docker run -d \
  --name icloud-hme \
  -p 8081:8081 \
  -v /path/to/data:/app/data \
  -e ICLOUD_HME_ADMIN_PASSWORD='change-this-before-running-2026' \
  ghcr.io/xiaozhou26/icloud-hme:latest
```

> ⚠️ 上面的密码仅为示例，**不可照抄**，请务必更换为至少 8 字符的强密码。

镜像支持 `linux/amd64` 和 `linux/arm64` 双架构，自动适配。

#### 方式三：源码编译（需要 Go 1.26+ 与 Node.js 22.12+ 双工具链）

```bash
# 前置要求: Go 1.26+、Node.js 22.12+
git clone https://github.com/xiaozhou26/icloud-hme.git
cd icloud-hme

# 一键构建（安装前端依赖 → 前端测试 → 前端构建 → Go 测试 → 编译）
./build.sh

# 或者手动分步构建
npm --prefix web ci
npm --prefix web run build
go build -o icloud-hme .
```

### 2. 安全配置（必读）

管理界面与 API 均需要管理员登录，升级后所有 API 都必须先通过 `POST /api/auth/login` 获取会话：

| 环境变量 | 说明 | 默认 |
|---|---|---|
| `ICLOUD_HME_ADMIN_PASSWORD` | 管理员密码，**必填**，至少 8 字符 | 无（缺失时拒绝启动） |
| `ICLOUD_HME_SESSION_TTL` | 会话有效期，超出范围会拒绝启动 | `12h`（范围 `15m`–`168h`） |
| `ICLOUD_HME_SECURE_COOKIE` | 通过 TLS 反向代理部署时设为 `true` | `false` |
| `ICLOUD_HME_IMAP_POOL_SIZE` | 每账号 IMAP 并发连接数（取件并行度，范围 1–50） | `10` |

> **Breaking Change（v0.3+）**：升级后未设置 `ICLOUD_HME_ADMIN_PASSWORD` 将拒绝启动；
> 原有匿名 API 调用将收到 `401 AUTH_REQUIRED`。管理员会话只存内存，进程重启即失效。

### 3. 配置账号

在程序 `data/` 目录下创建 `accounts.json`（参考仓库内 `accounts.json.template`）：

```json
{
  "accounts": {
    "acc_1": {
      "id": "acc_1",
      "name": "主号",
      "real_email": "owner@example.com",
      "icloud_email": "owner@icloud.com",
      "cookies": {
        "X-APPLE-WEBAUTH-TOKEN": "token_value",
        "X-APPLE-WEBAUTH-USER": "v=1:s=1:d=22789132008"
      },
      "host": "icloud.com",
      "proxy": "http://user:pass@host:port",
      "app_password": "xxxx-xxxx-xxxx-xxxx",
      "status": "active"
    }
  }
}
```

> **提示:** 也可以通过管理界面的「账号」页面动态添加账号，无需手动编辑 JSON 文件。`cookies`、`app_password`、`proxy` 都是可选的。

### 4. 启动服务

```bash
# 二进制方式（默认 data 目录）
export ICLOUD_HME_ADMIN_PASSWORD='your-strong-password'
./icloud-hme_linux_amd64

# 指定端口和数据目录
./icloud-hme_linux_amd64 -addr :9090 -data ./my_data

# 调试模式（启用请求日志）
./icloud-hme_linux_amd64 -debug

# 查看完整参数
./icloud-hme_linux_amd64 -h
```

服务默认监听 `:8081`。浏览器打开 `http://localhost:8081` 进入管理界面（创建别名 / 取件 / 账号设置）。完整 API 契约见 [API.md](API.md)。

## API 接口

- 程序接入（获取别名列表 + 取件）：**[API-INTEGRATION.md](API-INTEGRATION.md)**，含 Python / Go / Node 三种可运行示例，以及并发能力说明（跨账号并行、同账号串行）
- 完整接口契约、字段表、错误码与端到端示例：**[API.md](API.md)**

日常只需要两个调用：

### 创建别名

```bash
curl -b jar.txt -X POST http://localhost:8081/api/create \
  -H "Content-Type: application/json" -H "X-CSRF-Token: $CSRF" \
  -d '{"account_id":"acc_1","label":"注册某网站"}'
# → data.email 即新生成的隐私邮箱
```

### 取件

```bash
# 列出邮件(可按别名与天数筛选)
curl -b jar.txt "http://localhost:8081/api/inbox?account_id=acc_1&alias=xyz123@icloud.com&limit=20&days=7"

# 读正文 / 删除
curl -b jar.txt "http://localhost:8081/api/inbox/1042?account_id=acc_1"
curl -b jar.txt -X DELETE "http://localhost:8081/api/inbox/1042?account_id=acc_1" -H "X-CSRF-Token: $CSRF"
```

响应中的 `method` 字段标识数据来源：`imap`(App 专用密码,支持按收件人与天数过滤) 或 `web_api`(Cookie 回退)。注意详情与删除仅支持 IMAP 路径,详见 API.md 的「已知限制」。

### 辅助接口

| 接口 | 用途 |
|---|---|
| `POST /api/auth/login`、`GET /api/auth/session`、`POST /api/auth/logout` | 管理员会话与 CSRF |
| `GET /api/accounts`、`POST /api/accounts`、`PATCH/DELETE /api/accounts/:id` | 账号增删改查 |
| `PUT /api/accounts/:id/cookies`、`POST /api/accounts/:id/password`、`POST /api/accounts/:id/login` | 写入 Cookie / App 专用密码 / 密码登录 |
| `PUT /api/accounts/:id/mailbox`、`PUT /api/accounts/:id/proxy` | 接入外部收件邮箱 / 设置代理 |
| `GET /api/aliases`、`POST /api/aliases/:id/deactivate`、`POST /api/aliases/:id/reactivate`、`DELETE /api/aliases/:id` | 别名管理 |
| `POST /api/reload` | 重新加载 accounts.json |

除登录与会话查询外,所有 `/api/*` 都需要 `hme_session` 会话;非 `GET` 请求还需 `X-CSRF-Token`。响应统一为 `{success, data}` 或 `{success, code, message}`。
## 认证方式

三种凭据按需选择,获取步骤见 [API.md](API.md) 第 9 节:

- **Cookie**(创建别名必需) — 浏览器登录 [icloud.com](https://www.icloud.com) 或 [icloud.com.cn](https://www.icloud.com.cn),导出 Cookie 后粘贴到「账号设置 → 更新 Cookie」,有效期约 24 小时。关键字段 `X-APPLE-WEBAUTH-TOKEN`、`X-APPLE-WEBAUTH-USER`、`X-APPLE-WEBAUTH-HSA-TRUST`、`X-APPLE-DS-WEB-SESSION-TOKEN`
- **App 专用密码**(取件首选) — 在 [appleid.apple.com](https://appleid.apple.com) 生成,走 IMAP,支持按收件人搜索与时间范围过滤
- **外部收件邮箱**(备选) — 接入 QQ / Gmail / Outlook 的 IMAP,无需暴露 iCloud 凭据

取件时服务端按 `外部收件邮箱 → App 专用密码(IMAP) → Cookie(Web API)` 的顺序选择可用凭据。
## 项目架构

```
icloud-hme/
├── main.go                    # 入口: 读取安全配置、加载账号、启动服务
├── API.md                     # HTTP API 契约文档
├── web/                       # 前端工程 (React 19 + TypeScript + Vite + Ant Design)
│   └── src/
│       ├── main.tsx           #   入口: 主题 token、中文语言包、AntD App 上下文
│       ├── App.tsx            #   路由与登录态保护
│       ├── api/               #   契约类型、fetch 封装、资源读取 hooks
│       ├── components/        #   布局骨架与账号/凭据弹窗
│       ├── pages/             #   创建别名、取件、账号设置、登录
│       └── test/              #   vitest + msw 测试基础设施
├── accounts.json           # 账号配置文件 (自动生成)
├── go.mod
└── internal/
    ├── account/
    │   ├── manager.go      # 多账号管理器 (持久化、客户端工厂)
    │   └── public.go       # 公开 DTO (Summary) 与输入校验
    ├── auth/
    │   ├── manager.go      # 管理员会话 + CSRF
    │   └── limiter.go      # 登录失败限流
    ├── hme/
    │   ├── client.go       # iCloud HME Web 客户端 (Cookie 认证)
    │   └── auth.go         # SRP 登录 (账号密码 + 2FA 获取 Cookie)
    ├── mail/
    │   ├── client.go       # IMAP 邮件客户端 (App Password 认证)
    │   └── web_client.go   # Web 邮件客户端 (Cookie 认证,无需 App Password)
    ├── server/
    │   ├── server.go       # 路由分组 (认证 + CSRF)
    │   ├── backend.go      # 业务接口与 Manager 适配器
    │   ├── auth.go         # 登录/会话/退出 handler 与中间件
    │   ├── account_handlers.go  # 账号管理 handler
    │   └── middleware.go   # 安全响应头、请求上限
    └── webui/
        └── embed.go        # 内嵌前端资源 + SPA fallback
```

### 核心模块

- **account.Manager**: 管理多个 iCloud 账号,负责配置持久化和客户端创建
- **hme.Client**: 封装 iCloud HME Web API,支持 Cookie 认证
- **hme.auth**: SRP 协议登录,支持账号密码 + 可选 2FA
- **mail.Client**: IMAP 邮件客户端 (App Password,优先读邮件)
- **mail.WebClient**: 通过 iCloud Web API (mccgateway) 读取邮件,无需 App Password
- **server.Server**: HTTP API 服务 + 管理界面静态资源

## 技术栈

- **Go 1.26+** / **Gin** — HTTP 框架
- **React 19 + TypeScript + Vite 8** — 管理界面
- **Ant Design 6** — UI 组件库（中文语言包 + 主题 token 定制，替代自研弹窗/提示/表格样式）
- **go-imap** — IMAP 协议实现
- **tls-client** — TLS 指纹模拟 (绕过 iCloud 反爬)

## 常见问题

### Q: 创建别名返回 401/403 错误?

**A:** Cookie 已过期，需要重新获取。iCloud Cookie 有效期通常为 24 小时。

### Q: 读取邮件返回超时?

**A:** 检查网络连接，确保可以访问 `imap.mail.me.com:993`。

### Q: 如何查看某个别名收到了哪些邮件?

**A:** 调用 `GET /api/inbox?account_id=acc_1&alias=your_alias@icloud.com`

### Q: 支持同时管理多个 iCloud 账号吗?

**A:** 支持，在 `accounts.json` 中配置多个账号即可，每个账号有独立的 `id`；界面右上角的账号下拉框用于切换。

### Q: 点开邮件提示"account_id 或邮件 ID 无效"?

**A:** 该账号当前走的是 Cookie 回退路径（`method: "web_api"`），它返回的邮件 ID 不是 IMAP UID，无法用于读取正文或删除。给账号配置 App 专用密码或外部收件邮箱即可获得完整取件能力，详见 [API.md](API.md) 的「已知限制」。

## 开发指南

### 本地开发

一键启动前后端（推荐）：

```bash
./dev.sh                  # 后端 127.0.0.1:8081 + 前端 127.0.0.1:5173
./dev.sh --smoke          # 启动就绪后自动跑一遍浏览器冒烟测试
./dev.sh -b 9000 -f 4000  # 自定义端口
./dev.sh --backend-only   # 只起后端(界面走内嵌构建产物)
./dev.sh --stop           # 停止残留的后端/前端进程(上次异常退出时可先执行)
./dev.sh -h               # 全部选项
```

首次运行会自动 `npm ci` 并编译后端；就绪后打印访问地址与操作提示，`Ctrl-C` 同时停止两端；日志分别落在 `build/dev-logs/{api,web}.log`。默认管理员密码 `dev-admin-pass-2026`，服务只监听回环地址，可用 `-p` 或 `ICLOUD_HME_ADMIN_PASSWORD` 覆盖。

手动分步启动：

```bash
# 前端开发模式 (vite dev server, /api 代理到 :8081)
npm --prefix web ci
npm --prefix web run dev

# 后端开发模式
export ICLOUD_HME_ADMIN_PASSWORD='your-strong-password'
go run main.go -debug

# 前端检查 (lint + test + build)
npm --prefix web run check

# 完整构建 (含前端)
./build.sh

# 交叉编译
GOOS=linux GOARCH=amd64 go build -o icloud-hme .
GOOS=windows GOARCH=amd64 go build -o icloud-hme.exe .
```

### 测试

```bash
# Go:单元测试 + 静态检查(与 CI 一致,约 1 秒)
go test ./internal/... .
go vet ./internal/... .

# 前端:jsdom 单测(lint + 23 个用例 + 构建)
npm --prefix web run check

# 前端:浏览器冒烟(需先启动服务,详见 web/README.md)
export ICLOUD_HME_ADMIN_PASSWORD='your-strong-password'
./build/icloud-hme &            # 或 go run main.go -debug
cd web && npx playwright install chromium   # 首次安装浏览器
ICLOUD_HME_ADMIN_PASSWORD='your-strong-password' npm --prefix web run e2e
```

冒烟脚本会用真实 Chromium 打开界面，跑通「登录 → 创建别名 → 取件」，并核对 Ant Design 样式未被 CSP 拦截。iCloud 侧数据由脚本伪造，不需要真实账号。

### 取件链接

在「创建别名」页的操作列点 **取件链接**，会为那个别名生成一条只读链接：

```
http://localhost:8081/?token=V2oPcBxqPBNgxVmjZ6RFPmXsEBpELf2169Yo_f44Ips
```

打开链接的人**不需要登录**，只能看到这一个别名收到的邮件与正文（可切换排版/纯文本），看不到其它别名、账号设置，也没有删除等写操作。链接随时可在同一个弹窗里撤销，撤销后立即失效；重新生成会轮换出新 token。

设计要点：token 是 256 位随机值，不携带任何账号信息；服务端强制按该别名过滤，读正文时还会逐封核对收件人（IMAP UID 是账号级全局编号，不核对就能读到同账号其它别名的邮件）；公开响应不含账号内部标识。每条链接记录命中次数与最近使用时间，存放在 `data/share_links.json`（0600）。

### 自动建满别名

界面「创建别名」页右上角的 **自动建满** 按钮，或在服务运行期间直接调接口：

```bash
# 目标 700(默认),每 20 秒尝试一次,命中限流冷却 30 分钟后继续
curl -b jar.txt -X POST http://localhost:8081/api/autocreate/start \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d '{"account_id":"acc_1"}'

curl -b jar.txt http://localhost:8081/api/autocreate   # 查看进度与日志
curl -b jar.txt -X POST http://localhost:8081/api/autocreate/stop -H "X-CSRF-Token: $CSRF"
```

任务在后台线程里跑，按下面的规则自动收敛：

- **可以多账号并行**：iCloud 的创建限流按账号计算，所以 N 个账号各有独立节流窗口；同一个账号内并发创建没有收益（配额是账号级的）
- **创建间隔随机**：每创建一个别名后随机等待 20–40 秒（可配）再继续，避免固定节拍；首个别名立即创建
- 启动时并发检查每个账号的初始配额（当前别名总数），已达目标的账号直接跳过，不会空转
- 达到 `target`（默认 700）→ `completed`
- iCloud 报告别名总量已达上限 → `completed`（重试无意义）
- 命中速率限制 → `cooling`，等 `cooldown_seconds`（默认 30 分钟）后重新校准并继续
- 其它错误连续 `max_failures`（默认 5）次 → `stopped`，等待人工检查

同一时间只允许一个任务（重复启动返回 `409 TASK_RUNNING`），任务状态存在内存里，**服务重启后不会自动恢复**。完整参数与状态字段见 [API.md](API.md) 的「自动建满别名」。

### 检测别名创建密度

Apple 没有公开别名的创建速率与总量阈值，`cmd/hme-probe` 用来把实际限制测出来：

```bash
# 只读统计:从现有别名的创建时间算出各时间窗口内的峰值,不产生任何写入
go run ./cmd/hme-probe -data ./data -analyze
go run ./cmd/hme-probe -data ./data -analyze -windows 10s,1h -capacity 700

# 主动探测:按间隔连续创建,直到触发 iCloud 限制
go run ./cmd/hme-probe -data ./data -account acc_xxx -probe -count 20 -interval 3s
go run ./cmd/hme-probe -data ./data -probe -count 5 -interval 2s -cleanup
```

`-analyze` 用滑动窗口统计历史峰值，默认窗口为 `1m,5m,30m,1h,24h`（本地时区显示，窗口是闭区间：首尾间隔恰好等于窗口也算同一窗口），并给出已用/剩余额度参考：

```
别名总数: 183
时间范围: 2026-05-04 08:00:00 → 2026-09-16 20:10:00

窗口       最大创建数    出现区间(窗口内最早一次 → 最晚一次)
30m      5        2026-05-04 21:00:00 → 2026-05-04 21:24:00

额度参考: 已用 183/700,剩余约 517
(上限 700 为社区实测经验值,非 Apple 官方数字;你的账号可能不同,可用 -capacity 调整)
```

`-probe` 会**真实创建别名**，用于测当前节流窗口还剩多少余额：

- 报错命中 `429` / `too many` / `rate limit` / `quota` / `exceed` 等特征时立即停止，避免加重限流，并回显原始错误
- 未触发限制时提示用更大的 `-count`（硬上限 50）或更短的 `-interval` 逼近上限
- 结果按 1m / 5m / 30m 窗口汇总成功数，`-cleanup` 可删除本次创建的别名

社区实测经验（非官方数字，仅作起步参考）：**约每 30 分钟可创建 5 × iCloud 家庭成员数个别名，账号总量上限约 700 个**。所以先用 `-analyze` 看 30 分钟窗口的历史峰值，再用 `-probe` 在当前窗口试探余额，两者结合就能判断账号的实际节流点。

### 发布

推送 `v*` tag 到 GitHub 自动触发 CI：

```bash
git tag v0.2.0 && git push origin --tags
```

Actions 会自动构建多平台二进制、Docker 镜像（`ghcr.io/xiaozhou26/icloud-hme`）并创建 Release。

### 代码规范

- 代码注释使用中文
- 错误信息返回给用户时使用中文
- API 响应格式统一: `{success: bool, data: any, message: string}`

## 许可证

MIT License

---
## 社区

友情链接：[LINUX DO](https://linux.do)

## English

A local management tool for Apple iCloud Hide My Email (HME) aliases, supporting creation, listing, and email reading through reverse-engineered iCloud Web API and IMAP protocol. Ships with a built-in Chinese management UI (React 19 + Ant Design SPA embedded in the single binary), focused on two jobs: **creating aliases** and **reading mail**.

### Features

- Built-in UI at `http://localhost:8081` — three pages: aliases, inbox, account settings
- Create HME aliases (with labels, copy, deactivate/reactivate/delete)
- Read emails sent to aliases, filter by alias and time range, open the plain-text body, delete
- Manage multiple iCloud accounts
- Dual-path mail reading: IMAP (App Password) first, Web API (Cookie) fallback
- Security: single-admin session, CSRF checks, login rate limiting, redacted API responses, strict CSP

### Quick Start

#### Option 1: Binary (GitHub Releases)

Download the latest binary from [GitHub Releases](https://github.com/xiaozhou26/icloud-hme/releases):

| Platform | File |
|---|---|
| Linux x86_64 | `icloud-hme_linux_amd64` |
| Linux ARM64 | `icloud-hme_linux_arm64` |
| macOS Intel | `icloud-hme_darwin_amd64` |
| macOS Apple Silicon | `icloud-hme_darwin_arm64` |
| Windows x86_64 | `icloud-hme_windows_amd64.exe` |

```bash
# Linux example (admin password is REQUIRED, min 8 chars)
export ICLOUD_HME_ADMIN_PASSWORD='change-this-before-running-2026'
chmod +x icloud-hme_linux_amd64
./icloud-hme_linux_amd64
```

#### Option 2: Docker

```bash
docker pull ghcr.io/xiaozhou26/icloud-hme:latest

docker run -d \
  --name icloud-hme \
  -p 8081:8081 \
  -v /path/to/data:/app/data \
  -e ICLOUD_HME_ADMIN_PASSWORD='change-this-before-running-2026' \
  ghcr.io/xiaozhou26/icloud-hme:latest
```

> The password above is only an example — do NOT copy it. Use a strong password with at least 8 characters.

#### Option 3: Build from source (Go 1.26+ and Node.js 22.12+)

```bash
git clone https://github.com/xiaozhou26/icloud-hme.git
cd icloud-hme

# One-shot build (frontend deps → frontend test → frontend build → Go test → binary)
./build.sh

# Or step by step
npm --prefix web ci
npm --prefix web run build
go build -o icloud-hme .
```

### Configuration

| Env var | Description | Default |
|---|---|---|
| `ICLOUD_HME_ADMIN_PASSWORD` | Admin password, **required**, min 8 chars | none (refuses to start) |
| `ICLOUD_HME_SESSION_TTL` | Session TTL | `12h` (range `15m`–`168h`) |
| `ICLOUD_HME_SECURE_COOKIE` | Set `true` when deployed behind TLS | `false` |

> **Breaking change (v0.3+)**: without `ICLOUD_HME_ADMIN_PASSWORD` the server refuses to start; all API endpoints now require login (`401 AUTH_REQUIRED`). Admin sessions are in-memory only and are lost on restart.

Create `data/accounts.json` (see `accounts.json.template`) and start the server (default port `:8081`). Open `http://localhost:8081` to use the management UI. Full API contract: [API.md](API.md).
