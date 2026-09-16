# iCloud Hide My Email HTTP API

本地服务默认监听 `:8081`，全部接口为 JSON over HTTP。

> 只想让别的程序「拿别名列表 + 取件」？直接看 **[API-INTEGRATION.md](API-INTEGRATION.md)**，那份是面向接入的实操说明（含三种语言的示例代码）。本文是完整契约参考。日常只需要两类调用：**创建别名** 与 **取件**（读取发往别名的邮件）。其余接口用于账号与凭据管理。

```text
POST /api/create                                 创建别名
GET  /api/inbox                                  取件：列出邮件
GET  /api/inbox/:message_id                      取件：读取正文
DELETE /api/inbox/:message_id                    取件：删除邮件
```

---

## 1. 通用约定

**响应包裹**（所有接口一致，见 `internal/server/response.go`）

成功：

```json
{ "success": true, "data": { } }
```

失败：

```json
{ "success": false, "code": "VALIDATION_ERROR", "message": "参数错误: account_id 必填" }
```

`code` / `message` 仅在失败时出现（`omitempty`）；`data` 仅在成功时出现。`POST /api/accounts` 是唯一返回 `201` 的接口，其余成功响应均为 `200`。

**鉴权**：除 `POST /api/auth/login` 与 `GET /api/auth/session` 外，所有 `/api/*` 都需要会话 Cookie。缺失或失效返回 `401 AUTH_REQUIRED`。

**CSRF**：所有非 `GET`/`HEAD` 请求必须携带 `X-CSRF-Token`，值取自登录或会话接口返回的 `csrf_token`。校验失败返回 `403 CSRF_INVALID`。

**Cookie**：`hme_session`，`Path=/`、`HttpOnly`、`SameSite=Strict`；TLS 反代部署时设置 `ICLOUD_HME_SECURE_COOKIE=true` 追加 `Secure`。

**请求体**：上限 1 MiB，超限按参数错误返回 `400`。JSON 请求体必须以合法 JSON 提交；多数写接口不检查 `Content-Type`，但别名操作必须携带 JSON 对象（空体 → `400`）。

**响应头**：`/api/*` 一律 `Cache-Control: no-store`。全局安全头包含 `Content-Security-Policy`（`script-src 'self'`，`style-src 'self' 'unsafe-inline'` —— 后者用于 Ant Design 的运行时样式注入）、`X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer`、`Permissions-Policy`。

**登录限流**：同一 IP 在 15 分钟窗口内第 6 次登录请求即拒绝，返回 `429 RATE_LIMITED` 并带 `Retry-After` 头；窗口内登录成功会清零计数。

**时间格式**：均为 RFC3339（如 `2026-01-15T10:30:00+08:00`）。

---

## 2. 认证

### 2.1 登录

```http
POST /api/auth/login
Content-Type: application/json

{ "password": "管理员密码" }
```

密码来自服务启动时的环境变量 `ICLOUD_HME_ADMIN_PASSWORD`（长度 ≥ 8）。成功响应会设置 `hme_session` Cookie：

```json
{
  "success": true,
  "data": {
    "csrf_token": "Qm9ndXNUb2tlbi1FeGFtcGxlLTAwMDAwMDAwMDAwMA",
    "expires_at": "2026-01-15T22:00:00+08:00"
  }
}
```

失败：`401 INVALID_CREDENTIALS`（"管理员密码错误"）、`429 RATE_LIMITED`。

### 2.2 查询会话

```http
GET /api/auth/session
```

有效会话返回与登录相同的 `csrf_token` / `expires_at`；无效返回 `401 AUTH_REQUIRED`。前端在页面加载时调用它来恢复登录态并刷新 CSRF。

### 2.3 退出

```http
POST /api/auth/logout
X-CSRF-Token: <token>
```

```json
{ "success": true, "data": { "logged_out": true } }
```

---

## 3. 创建别名

创建前需要一次账号查询：`account_id` 只能来自 `GET /api/accounts`。

### 3.1 创建 HME 别名

```http
POST /api/create
X-CSRF-Token: <token>
Content-Type: application/json

{ "account_id": "acc_1a2b3c4d", "label": "注册某网站" }
```

| 字段 | 类型 | 必填 | 约束 |
|---|---|---|---|
| `account_id` | string | 是 | 账号 ID，须存在于本地配置 |
| `label` | string | 否 | 备注标签，最长 200 字符（按字符计，非字节） |

响应：

```json
{
  "success": true,
  "data": {
    "email": "xyz123@icloud.com",
    "label": "注册某网站",
    "created_at": "2026-01-15T10:30:00+08:00",
    "account_id": "acc_1a2b3c4d"
  }
}
```

服务端内部执行 iCloud 侧的 generate + reserve 两步，失败时最多重试 5 次。常见错误：
- `400 VALIDATION_ERROR` —— 缺少 `account_id`、`label` 超长
- `400 VALIDATION_ERROR`（"账号未配置 Cookie"）—— 该账号没有可用 Cookie，需先 `PUT /api/accounts/:id/cookies` 或 `POST /api/accounts/:id/login`
- `401 UPSTREAM_UNAUTHORIZED`（"iCloud 会话失效,请更新 Cookie"）
- `502 UPSTREAM_FAILURE`（"创建邮箱失败"，含 iCloud 侧频率限制）

> **Apple 侧的节流**：别名创建受 iCloud 自身限制，触发时表现为上面的 `502`，`message` 是固定文案，原始原因不会透出。社区实测经验（非官方数字，仅作参考）：约每 30 分钟可创建 `5 × iCloud 家庭成员数` 个别名，账号总量上限约 700 个。
>
> 需要判断账号当前还有多少余额时，用仓库内的 `cmd/hme-probe`：`-analyze` 只读统计历史创建峰值与剩余额度，`-probe` 主动创建探测当前节流窗口并可用 `-cleanup` 回收。详见 README 的「检测别名创建密度」。

### 3.2 别名列表

```http
GET /api/aliases?account_id=acc_1a2b3c4d
```

注意别名对象使用 camelCase（沿用 iCloud 原始字段），与账号摘要的 snake_case 不同：

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1a2b3c4d",
    "count": 2,
    "aliases": [
      {
        "email": "xyz123@icloud.com",
        "anonymousId": "abc123",
        "label": "注册某网站",
        "active": true,
        "createdAt": "2026-01-15T10:30:00Z"
      }
    ]
  }
}
```

`createdAt` 可能缺省。界面按创建时间倒序展示。

### 3.3 停用 / 激活 / 删除别名

```http
POST /api/aliases/:id/deactivate       停用
POST /api/aliases/:id/reactivate       激活
DELETE /api/aliases/:id                删除
X-CSRF-Token: <token>
Content-Type: application/json

{ "account_id": "acc_1a2b3c4d" }
```

`:id` 是别名的 `anonymousId`（不超过 256 字节）。停用/激活响应：

```json
{ "success": true, "data": { "anonymous_id": "abc123", "success": true } }
```

外层 `success` 是响应包裹字段，内层 `success` 是 iCloud 侧的操作结果。删除响应为 `{ "anonymous_id": "abc123" }`，删除不可恢复。

`account_id` 必填，缺失或请求体不是合法 JSON 时返回 `400 VALIDATION_ERROR`。

---

### 3.4 取件链接（只读分享）

给某个别名生成一条链接，持有链接的人可以**只读**查看该别名收到的邮件，不需要管理员会话：

```
POST   /api/aliases/:id/share-link     生成链接（需 X-CSRF-Token，幂等）
DELETE /api/aliases/:id/share-link     撤销链接（需 X-CSRF-Token）
GET    /api/share/:token/inbox         公开：邮件列表（无需会话）
GET    /api/share/:token/inbox/:id     公开：邮件正文（无需会话）
```

生成（`:id` 是别名的 `anonymousId`）：

```http
POST /api/aliases/abc123/share-link
X-CSRF-Token: <token>
Content-Type: application/json

{ "account_id": "acc_1a2b3c4d" }
```

```json
{
  "success": true,
  "data": {
    "alias": "alpha@icloud.com",
    "token": "V2oPcBxqPBNgxVmjZ6RFPmXsEBpELf2169Yo_f44Ips",
    "url": "http://localhost:8081/?token=V2oPcBxqPBNgxVmjZ6RFPmXsEBpELf2169Yo_f44Ips",
    "created_at": "2026-01-15T10:30:00+08:00",
    "hits": 0
  }
}
```

同一别名重复调用返回**同一个 token**（幂等），`url` 由请求的 Host 与协议推断，可直接分享。撤销后 token 立即失效，再次生成会轮换出新 token。

公开取件（`GET /api/share/:token/inbox` 支持 `limit`、`days`，取值同第 4 节）：

```json
{
  "success": true,
  "data": {
    "alias": "alpha@icloud.com",
    "count": 2,
    "method": "imap",
    "messages": [
      { "id": "1042", "from": "GitHub <noreply@github.com>", "to": "alpha@icloud.com",
        "subject": "请验证你的邮箱", "date": "2026-01-15T10:30:00+08:00", "preview": "验证码 654321" }
    ]
  }
}
```

`GET /api/share/:token/inbox/:id` 返回与 `/api/inbox/:id` 相同的正文字段（`body` / `body_html` / `body_html_sanitized` / `content_type`）。

安全约定：

- token 是 256 位随机值的 base64url 编码，不携带账号或地址信息，**不可猜测**
- 访问范围被强制限定在 token 对应的那个别名：列表按该地址过滤，读正文时会逐封核对收件人（IMAP UID 是账号级全局编号，不核对就能读到同账号其它别名的邮件）
- 公开响应**不包含** `account_id` 等账号内部标识，也不提供删除等写操作
- token 无效或已撤销返回 `404 SHARE_INVALID`
- 每条链接记录命中次数与最近使用时间（`data/share_links.json`，0600），便于发现异常访问；可在管理界面随时撤销

### 3.5 自动建满别名（后台任务）


```http
GET  /api/autocreate         查询任务状态
POST /api/autocreate/start   启动任务（需 X-CSRF-Token）
POST /api/autocreate/stop    停止任务（需 X-CSRF-Token）
```

启动请求（除账号外都可省略，省略时用默认值）：

```json
{
  "account_ids": ["acc_1a2b3c4d", "acc_5e6f7a8b"],
  "target": 700,
  "interval_seconds": 20,
  "interval_max_seconds": 40,
  "cooldown_seconds": 1800,
  "label_prefix": "auto",
  "max_failures": 5,
  "max_parallel": 10
}
```

| 字段 | 默认 | 范围 | 说明 |
|---|---|---|---|
| `account_ids` | — | 1–20 个 | 要建满的账号，会去重；**可一次勾选多个账号并行创建** |
| `account_id` | — | — | 单账号的兼容写法，可与 `account_ids` 同时给出（合并去重） |
| `target` | `700` | 1–5000 | 每个账号的目标别名总数（含已有），达到即结束 |
| `interval_seconds` | `20` | 5–3600 | 创建后随机等待的**下限**（秒） |
| `interval_max_seconds` | `40` | 不小于下限，≤ 3600 | 创建后随机等待的**上限**（秒）。只传 `interval_seconds` 时上限自动跟随下限，退化为固定间隔 |
| `cooldown_seconds` | `1800` | 60–86400 | 命中速率限制后的冷却时长，默认 30 分钟 |
| `label_prefix` | `auto` | ≤ 50 字符 | 新别名的标签前缀，形如 `auto-1` |
| `max_failures` | `5` | 1–100 | 单个账号连续失败多少次后停止该账号 |
| `max_parallel` | `10` | 1–20 | 同时运行的账号循环数上限 |

**为什么要多账号并行**：iCloud 的创建限流是按账号计算的（经验值：约每 30 分钟 `5 × 家庭成员数`）。同一个账号内并发创建没有收益，只会更快撞上限制；而 N 个账号各有独立节流窗口，并行才能把总吞吐提升到约 N 倍。

状态响应（`start` / `stop` 也返回同一结构）。顶层字段是所有账号的**汇总**，逐账号细节在 `accounts` 里：

```json
{
  "success": true,
  "data": {
    "running": true,
    "phase": "cooling",
    "target": 700,
    "total": 54,
    "created": 8,
    "failed": 1,
    "interval_seconds": 20,
    "cooldown_seconds": 1800,
    "label_prefix": "auto",
    "max_failures": 5,
    "max_parallel": 10,
    "started_at": "2026-01-15T10:30:00+08:00",
    "next_run_at": "2026-01-15T11:00:00+08:00",
    "attempting": true,
    "last_attempt_at": "2026-01-15T10:30:20+08:00",
    "last_email": "auto6@icloud.com",
    "accounts": [
      {
        "account_id": "acc_1a2b3c4d",
        "name": "主号",
        "phase": "running",
        "target": 700,
        "total": 42,
        "remaining": 658,
        "created": 6,
        "failed": 0,
        "consecutive_failures": 0,
        "capacity_checked": true,
        "attempting": true,
        "next_run_at": "2026-01-15T10:30:40+08:00",
        "last_email": "auto6@icloud.com"
      },
      {
        "account_id": "acc_5e6f7a8b",
        "name": "备用号",
        "phase": "cooling",
        "target": 700,
        "total": 12,
        "remaining": 688,
        "created": 2,
        "failed": 1,
        "capacity_checked": true,
        "attempting": false,
        "next_run_at": "2026-01-15T11:00:00+08:00",
        "last_error": "创建别名失败: HTTP 429: Too Many Requests"
      }
    ],
    "reason": "",
    "logs": [
      { "time": "2026-01-15T10:30:05+08:00", "level": "warn", "message": "[备用号] 命中速率限制,冷却 30m0s 后继续" }
    ]
  }
}
```

每个账号条目里的 `capacity_checked` 表示启动阶段的配额检查是否成功；失败时 `capacity_error` 给出原因，此时该账号以 0 为起点进入循环。`remaining` 是「该账号还需创建多少个」= `target - total`。

`phase` 取值与停止条件：

| phase | 含义 |
|---|---|
| `idle` | 从未启动过（进程内无任务） |
| `running` | 正在按间隔创建 |
| `cooling` | 命中速率限制，等待 `cooldown_seconds` 后再试 |
| `completed` | 达到 `target`，或 iCloud 报告别名总量已达上限 |
| `stopped` | 手动停止，或连续失败达到 `max_failures` |

行为约定：

- **全局单任务**：同一时间只允许一个建满任务（其中可含多个账号），重复启动返回 `409 TASK_RUNNING`
- **启动时检查初始配额**：并发拉取每个账号的别名列表，响应里的 `accounts[].total` 是真实起点；已达目标的账号直接标记 `completed` 且不进入循环，配额检查失败的账号以 0 为起点并在日志中告警
- **账号之间互不阻塞**：每个账号一条独立循环，各自持有冷却计时与失败计数；某个账号因连续失败停止，其它账号继续创建
- **创建间隔是随机的**：每成功（或失败）完成一次尝试后，等待一个 `[interval_seconds, interval_max_seconds]` 区间内的随机整秒数再继续，避免固定节拍；首个别名会立即创建，不额外等待
- **限流与配额分开处理**：命中速率限制进 `cooling`，冷却结束后重新校准并继续；iCloud 报告总量已满则直接 `completed`（重试无意义）
- **单次尝试可能较慢**：一次创建包含 iCloud 会话校验与内部重试，可能持续数十秒；期间 `attempting` 为 `true`、`last_attempt_at` 是本次尝试开始时间，前端据此提示"创建中"。点停止会立即把 `phase` 改为 `stopped`，但当前这次尝试要等返回后才真正结束
- **状态仅在内存**：服务重启后任务不恢复，需重新启动
- `logs` 最多保留最近 200 条

## 4. 取件

### 4.1 列出邮件

```http
GET /api/inbox?account_id=acc_1a2b3c4d&alias=xyz123@icloud.com&limit=20&days=7
```

| 参数 | 必填 | 默认 | 取值范围 | 说明 |
|---|---|---|---|---|
| `account_id` | 是 | — | — | 账号 ID |
| `alias` | 否 | 空 | — | 只返回发给该别名的邮件；留空返回收件箱最近邮件 |
| `limit` | 否 | `20` | 1–100 | 返回上限 |
| `days` | 否 | `7` | 1–90 | 只要近 N 天的邮件（**仅 IMAP 路径生效**，见 6.2） |

`limit` / `days` 传入非整数或越界值直接返回 `400 VALIDATION_ERROR`；注意查询串里写了空值（如 `?limit=`）也按非法处理，不会回落默认值。

响应：

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1a2b3c4d",
    "alias": "xyz123@icloud.com",
    "count": 1,
    "method": "imap",
    "messages": [
      {
        "id": "1042",
        "from": "GitHub <noreply@github.com>",
        "to": "xyz123@icloud.com",
        "subject": "[GitHub] Please verify your email address",
        "date": "2026-01-15T10:30:00+08:00",
        "preview": "Almost done! To finish setting up your account..."
      }
    ]
  }
}
```

`method` 标识本轮数据的来源：

- `imap` —— 使用 App 专用密码，服务端支持按收件人搜索，`days` 生效
- `web_api` —— 回退到 Cookie 认证的 iCloud Web 接口

不传 `alias` 时响应中不含 `alias` 键。`preview` 为纯文本摘要（已剥离 HTML/CSS，验证码等关键内容会保留）。

### 4.2 读取邮件正文

```http
GET /api/inbox/1042?account_id=acc_1a2b3c4d[&sanitize=1][&raw=1]
```

| 查询参数 | 说明 |
|---|---|
| `sanitize` | `1` / `true` / `yes` 时额外返回清理后的 HTML（`body_html_sanitized`） |
| `raw` | `1` / `true` / `yes` 时额外返回完整 RFC822 报文（`raw_message`，base64） |

响应在摘要字段基础上追加正文：

```json
{
  "success": true,
  "data": {
    "id": "1042",
    "from": "GitHub <noreply@github.com>",
    "to": "xyz123@icloud.com",
    "subject": "[GitHub] Please verify your email address",
    "date": "2026-01-15T10:30:00+08:00",
    "preview": "Almost done! To finish setting up your account...",
    "body": "点击链接完成验证：https://example.com/verify",
    "body_html": "<p>点击链接完成验证：<a href=\"https://example.com/verify\">verify</a></p>",
    "content_type": "text/html"
  }
}
```

`:message_id` 是 IMAP UID（十进制整数，不可为 0），仅在 `method: "imap"` 下可用 —— 见 6.1。

| 字段 | 说明 |
|---|---|
| `body` | 可读纯文本，始终存在（用于复制、搜索与纯文本视图） |
| `body_html` | 清理后的原始 HTML，供客户端按排版渲染；邮件只有纯文本时该字段不出现 |
| `content_type` | 有 HTML 时为 `text/html`，否则为 `text/plain`；整封邮件没有可读正文时是顶层 MIME 类型 |

**正文提取规则**（`body` 与 `body_html` 共用同一次解析，列表中的 `preview` 取纯文本）：

- 递归解析 `multipart/*`（`mixed` / `alternative` / `related`），附件部分跳过，不会把 MIME 边界或附件内容当成正文
- 自动按 `Content-Transfer-Encoding` 解码（`base64` / `quoted-printable`）并按 `charset` 转码为 UTF-8
- 同时存在 `text/plain` 与 `text/html` 时，`body` 取发件人的纯文本，`body_html` 保留 HTML
- 只有 HTML 时，`body` 由 HTML 剥离标签与样式得到

**渲染安全约定**：`body_html` 是原始 HTML，**不要直接 `innerHTML` 注入主文档**，请放进 sandbox iframe（不授予 `allow-scripts`）或先用 `sanitize=1` 取清理版。仓库前端的做法是：请求时带 `sanitize=1`，把 `body_html_sanitized` 放进 `sandbox` iframe 渲染，同时受 CSP（`script-src 'self'`、`img-src 'self' data:`）兜底——脚本不执行、远程追踪图片不加载、邮件样式不外溢。

### 4.3 删除邮件

```http
DELETE /api/inbox/1042?account_id=acc_1a2b3c4d
X-CSRF-Token: <token>
```

```json
{ "success": true, "data": { "id": "1042" } }
```

邮件从收件箱永久删除，同样只在 IMAP 路径可用。

---

## 5. 账号与凭据

创建别名需要 Cookie，取件需要 Cookie 或 App 专用密码。以下接口用于把这些凭据写进本地账号配置。

### 5.1 列出账号

```http
GET /api/accounts
```

按 `active` → `pending` → `error` 排序，同状态按名称与 ID：

```json
{
  "success": true,
  "data": [
    {
      "id": "acc_1a2b3c4d",
      "name": "主号",
      "real_email": "owner@example.com",
      "icloud_email": "owner@icloud.com",
      "host": "icloud.com",
      "status": "active",
      "alias_total": 15,
      "alias_active": 12,
      "has_cookies": true,
      "has_app_password": true,
      "has_proxy": false,
      "mailbox": {
        "provider": "qq",
        "email": "me@qq.com",
        "imap_host": "imap.qq.com",
        "imap_port": 993
      },
      "last_validated": "2026-01-15T10:30:00+08:00",
      "status_message": "",
      "created_at": "2026-01-01T09:00:00+08:00"
    }
  ]
}
```

`cookies`、`app_password`、`proxy` 原文**永不返回**，只以 `has_cookies` / `has_app_password` / `has_proxy` 布尔值暴露。`mailbox` 仅在配置过收件邮箱时出现，`status_message` 仅在 `pending`（"等待配置或验证凭据"）或 `error`（"凭据验证失败"）时出现。

### 5.2 添加账号

```http
POST /api/accounts
X-CSRF-Token: <token>

{
  "name": "新账号",
  "icloud_email": "owner@icloud.com",
  "host": "icloud.com",
  "cookies": "X-APPLE-WEBAUTH-TOKEN=abc; X-APPLE-WEBAUTH-USER=def",
  "proxy": "http://user:pass@host:port"
}
```

- `name`：去空白后 1–64 字符
- `icloud_email`：`net/mail` 严格校验，且地址必须与输入完全一致
- `host`：仅 `icloud.com` 或 `icloud.com.cn`，缺省 `icloud.com`
- `cookies`：可选，Cookie 头字符串或 `{"key":"value"}` JSON；缺省时账号为 `pending`，不访问网络
- `proxy`：可选，`http` / `https` / `socks5` URL

成功返回 `201` 与账号摘要。

### 5.3 编辑账号

```http
PATCH /api/accounts/:id
X-CSRF-Token: <token>

{ "name": "新名称", "icloud_email": "owner@icloud.com", "host": "icloud.com.cn" }
```

三个字段均可选，至少一个存在。响应为更新后的摘要。

### 5.4 更新 Cookie

```http
PUT /api/accounts/:id/cookies
X-CSRF-Token: <token>

{ "cookies": "a=1; b=2" }
```

也接受对象形式 `{ "cookies": { "a": "1", "b": "2" } }`。服务端会用该 Cookie 调用 iCloud `validate` 校验会话：

- 成功：账号状态变为 `active`，`last_validated` 刷新
- 失败：`400 VALIDATION_ERROR`（"Cookie 校验失败,请检查 Cookie 是否有效或已过期"），不会回显 iCloud 的响应内容

### 5.5 设置 App 专用密码

```http
POST /api/accounts/:id/password
X-CSRF-Token: <token>

{ "icloud_email": "owner@icloud.com", "app_password": "xxxx-xxxx-xxxx-xxxx" }
```

服务端会用 IMAP 连接实际验证，成功后才落盘。验证失败返回 `502 UPSTREAM_FAILURE`（"IMAP 验证失败,请检查邮箱与 App 专用密码"）。配置后取件优先走 IMAP。

### 5.6 iCloud 密码登录（免手工复制 Cookie）

```http
POST /api/accounts/:id/login
X-CSRF-Token: <token>

{ "password": "iCloud 密码", "otp_code": "123456" }
```

`otp_code` 在启用双重认证时填写。两个阶段的响应：

- 未带 OTP 且账号启用了 2FA → `409 OTP_REQUIRED`（"需要提供 OTP 验证码"），前端据此弹出验证码输入
- OTP 错误 → `401 OTP_INVALID`
- 成功 → 返回账号摘要，**不含 Cookie**（Cookie 已在服务端持久化）

### 5.7 接入外部收件邮箱

```http
PUT /api/accounts/:id/mailbox
X-CSRF-Token: <token>

{
  "provider": "qq",
  "email": "me@qq.com",
  "imap_host": "imap.qq.com",
  "imap_port": 993,
  "authorization_code": "邮箱授权码"
}
```

`provider` 可取 `qq` / `gmail` / `outlook` / `custom`；`imap_port` 必须在 1–65535 之间，`imap_host` 不能包含 `://`。服务端会先连接验证再保存：

- 参数非法 → `400 VALIDATION_ERROR`
- 账号不存在 → `404 ACCOUNT_NOT_FOUND`
- IMAP 验证失败 → `502 UPSTREAM_FAILURE`

配置后取件会优先使用该外部邮箱的 IMAP 凭据。

### 5.8 更新代理

```http
PUT /api/accounts/:id/proxy
X-CSRF-Token: <token>

{ "proxy": "http://user:pass@host:port" }
```

传空字符串表示清除代理。代理值从不回显。

### 5.9 删除账号

```http
DELETE /api/accounts/:id
X-CSRF-Token: <token>
```

```json
{ "success": true, "data": { "id": "acc_1a2b3c4d" } }
```

只删除本地配置，不影响 Apple 账号本身。账号不存在返回 `404 ACCOUNT_NOT_FOUND`。

### 5.10 重新加载配置

```http
POST /api/reload
X-CSRF-Token: <token>
```

重新读取 `accounts.json`，响应 `{ "message": "配置已重新加载" }`。手动编辑配置文件后调用即可，无需重启进程。

---

## 6. 已知限制

### 6.1 详情与删除仅支持 IMAP

`GET /api/inbox` 的 `method: "web_api"` 分支返回的 `id` 是 iCloud Web 接口的 threadId（非纯数字），`GET/DELETE /api/inbox/:message_id` 只实现了 IMAP UID 路径。因此仅配置 Cookie 的账号**可以列出邮件，但点开正文会失败**（`400 VALIDATION_ERROR`）。

需要完整取件能力时，给账号配置 App 专用密码（5.5）或外部收件邮箱（5.7）。

### 6.2 `days` 只在 IMAP 路径生效

Web API 回退路径没有时间范围概念，只按 `limit` 截断，此时 `days` 被忽略。

### 6.3 其他

- 别名创建受 iCloud 频率限制，过快会失败（表现为 `502 UPSTREAM_FAILURE` 或 HTTP 429），服务端已内置重试
- Cookie 有效期约 24 小时，过期后返回 `401 UPSTREAM_UNAUTHORIZED`，需重新更新或使用密码登录
- 别名 `preview` 与 `body` 均为纯文本，不渲染 HTML
- 错误码是源码内的字面量约定，没有编译期常量；以本文档与 `internal/server` 中的实际字符串为准

---

## 7. 端到端示例

### 7.1 两个核心调用的最短路径（curl）

```bash
BASE="http://localhost:8081"

# 1) 登录,把会话 Cookie 存进 jar
curl -sc jar.txt -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"password":"你的管理员密码"}'

# 2) 取 CSRF token(GET /api/auth/session 亦可,返回同样的 csrf_token)
CSRF=$(curl -sb jar.txt "$BASE/api/auth/session" | jq -r '.data.csrf_token')

# 3) 拿到 account_id
ACCOUNT=$(curl -sb jar.txt "$BASE/api/accounts" | jq -r '.data[0].id')

# 4) 创建别名
curl -sb jar.txt -X POST "$BASE/api/create" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d "{\"account_id\":\"$ACCOUNT\",\"label\":\"GitHub\"}"
# → data.email 即为新别名

# 5) 取件:只看发到该别名的邮件
curl -sb jar.txt "$BASE/api/inbox?account_id=$ACCOUNT&alias=xyz123@icloud.com&days=3&limit=20"

# 6) 读正文(用上一步的 messages[].id)
curl -sb jar.txt "$BASE/api/inbox/1042?account_id=$ACCOUNT"
```

### 7.2 一次「创建 + 收信」的完整流程（JavaScript）

```js
const BASE = 'http://localhost:8081'
let csrf = ''

async function api(path, options = {}) {
  const method = (options.method ?? 'GET').toUpperCase()
  const res = await fetch(BASE + path, {
    method,
    credentials: 'same-origin',            // 会话 Cookie 是 HttpOnly,靠浏览器自动携带
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      ...(method === 'GET' ? {} : { 'X-CSRF-Token': csrf }),
    },
    body: options.body ? JSON.stringify(options.body) : undefined,
  })
  const payload = await res.json()
  if (!res.ok || payload.success === false) {
    throw new Error(`${res.status} ${payload.code}: ${payload.message}`)
  }
  return payload.data
}

// 登录并保留 CSRF
csrf = (await api('/api/auth/login', { method: 'POST', body: { password: '你的管理员密码' } })).csrf_token

// 创建别名
const [{ id: accountId }] = await api('/api/accounts')
const alias = await api('/api/create', { method: 'POST', body: { account_id: accountId, label: '自动注册' } })
console.log('新别名:', alias.email)

// 轮询收件(找到验证邮件)
for (let i = 0; i < 20; i++) {
  const inbox = await api(`/api/inbox?account_id=${accountId}&alias=${encodeURIComponent(alias.email)}&limit=5&days=1`)
  const target = inbox.messages[0]
  if (target) {
    const full = await api(`/api/inbox/${target.id}?account_id=${accountId}`)
    console.log('主题:', full.subject)
    console.log(full.body)
    break
  }
  await new Promise((r) => setTimeout(r, 3000))
}
```

### 7.3 错误处理模板

```js
try {
  await api('/api/create', { method: 'POST', body: { account_id: accountId } })
} catch (err) {
  // 需要 OTP / 会话失效 / 参数错误分别处理
  if (err.message.includes('UPSTREAM_UNAUTHORIZED')) {
    // Cookie 过期:改为 POST /api/accounts/:id/login 重新登录
  }
}
```

---

## 8. 错误码参考

| 错误码 | HTTP | 触发场景 |
|---|---|---|
| `AUTH_REQUIRED` | 401 | 无会话 Cookie，或会话过期（"请先登录" / "会话已失效,请重新登录"） |
| `INVALID_CREDENTIALS` | 401 | 管理员密码错误 |
| `RATE_LIMITED` | 429 | 15 分钟窗口内第 6 次登录请求，带 `Retry-After` |
| `CSRF_INVALID` | 403 | 缺少会话，或 `X-CSRF-Token` 缺失/不匹配 |
| `VALIDATION_ERROR` | 400 | 参数缺失、越界、body 非法（含超过 1 MiB） |
| `VALIDATION_ERROR` | 404 | `/api/*` 下未知路径（"接口不存在"） |
| `ACCOUNT_NOT_FOUND` | 404 | 账号 ID 不存在 |
| `TASK_RUNNING` | 409 | 已有自动建满任务在运行（`POST /api/autocreate/start`） |
| `SHARE_INVALID` | 404 | 取件链接无效或已被撤销（`/api/share/:token/*`） |
| `OTP_REQUIRED` | 409 | 账号启用 2FA 但未提供 `otp_code` |
| `OTP_INVALID` | 401 | OTP 验证码错误 |
| `UPSTREAM_UNAUTHORIZED` | 401 | iCloud 会话/Cookie 失效，需更新凭据 |
| `UPSTREAM_FAILURE` | 502 | iCloud 或 IMAP 侧失败（创建别名、读取/删除邮件、凭据验证） |
| `INTERNAL_ERROR` | 500 | 服务内部错误（含重新加载配置失败） |

---

## 9. iCloud 侧认证方式

**Cookie（创建别名必需）**：浏览器登录 [icloud.com](https://www.icloud.com) 或 [icloud.com.cn](https://www.icloud.com.cn) 后，从开发者工具复制 Cookie，交给 `PUT /api/accounts/:id/cookies`。关键字段 `X-APPLE-WEBAUTH-TOKEN`、`X-APPLE-WEBAUTH-USER`、`X-APPLE-WEBAUTH-HSA-TRUST`、`X-APPLE-DS-WEB-SESSION-TOKEN`，有效期约 24 小时。也可以直接调用 `POST /api/accounts/:id/login` 用密码换取。

**App 专用密码（取件首选）**：在 [appleid.apple.com](https://appleid.apple.com) 生成，配合 `POST /api/accounts/:id/password` 保存。取件时走 IMAP，支持按收件人搜索与时间范围过滤。

**外部收件邮箱（备选）**：通过 `PUT /api/accounts/:id/mailbox` 接入 QQ / Gmail / Outlook 等 IMAP 邮箱，适合不想暴露 iCloud 凭据的场景。

取件时服务端按 `外部收件邮箱 → App 专用密码(IMAP) → Cookie(Web API)` 的顺序选择可用凭据，响应中的 `method` 字段即最终实际使用的路径。
