# 接入指南：别名邮箱列表与取件

面向外部程序的最短接入路径。默认服务地址 `http://localhost:8081`。

完整接口清单（账号管理、后台任务等）见 [API.md](API.md)；本文只讲程序接入取件真正会用到的部分。

---

## 1. 三条调用就够了

```
POST /api/auth/login                          换一个会话 Cookie
GET  /api/accounts                            拿 account_id
GET  /api/aliases?account_id=...              ① 获取别名邮箱列表
GET  /api/inbox?account_id=...&alias=...      ② 取件：邮件列表
GET  /api/inbox/:id?account_id=...            ② 取件：读正文（可选）
```

**读取接口（GET）不需要 CSRF**，只要带上会话 Cookie。只有删除邮件、创建别名这类写操作才需要 `X-CSRF-Token`（值来自登录响应）。

可直接复制运行的示例（三种语言都随仓库提供并已实测）：

| 语言 | 文件 | 运行 |
|---|---|---|
| Python | `examples/icloud_hme_client.py` | `python3 examples/icloud_hme_client.py --base http://127.0.0.1:8081 --password '...'` |
| Go | `examples/fetch_inbox.go` | `go run examples/fetch_inbox.go -base http://127.0.0.1:8081 -password '...'` |
| Node.js | `examples/fetch_inbox.mjs` | `node examples/fetch_inbox.mjs --base http://127.0.0.1:8081 --password '...'` |

Python 版本只用标准库（`urllib` + `http.cookiejar`），不需要安装任何依赖。

---

## 2. 认证

### 2.1 登录换会话

```http
POST /api/auth/login
Content-Type: application/json

{ "password": "管理员密码" }
```

响应头会设置 `hme_session` Cookie，响应体给出 CSRF：

```json
{
  "success": true,
  "data": {
    "csrf_token": "8NvJd7By1NVORxqGAfQWQIWfxoKkBAWbKuT2Y3ysu88",
    "expires_at": "2026-09-17T08:27:55+08:00"
  }
}
```

密码来自服务启动时的环境变量 `ICLOUD_HME_ADMIN_PASSWORD`。**程序侧的职责就是保存这个 Cookie**：Python 用 `http.cookiejar`、Go 用 `net/http/cookiejar`、Node 内置 fetch 需要手动读 `set-cookie`。

### 2.2 CSRF 什么时候需要

| 操作 | 需要 CSRF |
|---|---|
| `GET /api/accounts`、`GET /api/aliases`、`GET /api/inbox`、`GET /api/inbox/:id` | 不需要 |
| `DELETE /api/inbox/:id`、`POST /api/create`、别名停用/激活/删除 | 需要 `X-CSRF-Token` |

所以纯取件场景可以完全忽略 CSRF；只有要删邮件或建别名时才需要保存登录响应里的 `csrf_token`。

### 2.3 会话有效期与续期

- 默认 12 小时，可用环境变量 `ICLOUD_HME_SESSION_TTL` 调整（范围 `15m`–`168h`）
- 长驻程序建议把它设大一些（例如 `168h`），减少重新登录次数
- 会话失效时任何接口都会返回 `401 AUTH_REQUIRED`，此时重新登录一次即可

### 2.4 登录限流

同一 IP 在 15 分钟窗口内第 6 次登录请求会被拒绝（`429 RATE_LIMITED`，带 `Retry-After`），登录成功会清零计数。所以**不要用"每次请求都重新登录"的写法**，只在拿到 401 时重登。

---

## 3. 接口

### 3.1 账号列表

```http
GET /api/accounts
```

```json
{
  "success": true,
  "data": [
    {
      "id": "acc_1a2b3c4d",
      "name": "主号",
      "icloud_email": "owner@icloud.com",
      "host": "icloud.com",
      "status": "active",
      "alias_total": 15,
      "alias_active": 12,
      "has_cookies": true,
      "has_app_password": true,
      "has_proxy": false,
      "last_validated": "2026-09-16T10:00:00+08:00",
      "created_at": "2026-09-01T09:00:00+08:00"
    }
  ]
}
```

`account_id` 从这里取，后续所有接口都要带。程序侧建议按 `name` 缓存映射关系，或在配置里写死 ID。

### 3.2 获取别名邮箱列表

```http
GET /api/aliases?account_id=acc_1a2b3c4d
```

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1a2b3c4d",
    "count": 2,
    "aliases": [
      {
        "email": "alpha@icloud.com",
        "anonymousId": "abc123",
        "label": "注册某网站",
        "active": true,
        "createdAt": "2026-09-16T10:30:00+08:00"
      }
    ]
  }
}
```

| 字段 | 说明 |
|---|---|
| `email` | 别名地址，可直接作为收件人使用 |
| `anonymousId` | iCloud 侧标识，删除/停用/激活时作为路径参数 |
| `label` | 标签，可能为空 |
| `active` | `false` 表示已停用（不再接收邮件） |
| `createdAt` | 创建时间，**可能缺省**（iCloud 未返回时该键不存在） |

注意别名对象是 **camelCase**（沿用 iCloud 原始风格），而账号摘要、邮件都是 snake_case。

### 3.3 取件：邮件列表

```http
GET /api/inbox?account_id=acc_1a2b3c4d&alias=alpha@icloud.com&limit=20&days=7
```

| 参数 | 必填 | 默认 | 范围 | 说明 |
|---|---|---|---|---|
| `account_id` | 是 | — | — | 账号 ID |
| `alias` | 否 | 空 | — | 只返回发给该别名的邮件；留空返回整个收件箱最近的邮件 |
| `limit` | 否 | `20` | 1–100 | 返回条数上限 |
| `days` | 否 | `7` | 1–90 | 只看近 N 天（**仅 IMAP 路径生效**） |

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1a2b3c4d",
    "alias": "alpha@icloud.com",
    "count": 2,
    "method": "imap",
    "messages": [
      {
        "id": "1042",
        "from": "GitHub <noreply@github.com>",
        "to": "alpha@icloud.com",
        "subject": "请验证你的邮箱",
        "date": "2026-09-16T14:32:10+08:00",
        "preview": "验证码 654321"
      }
    ]
  }
}
```

- `method`：`imap`（App 专用密码，支持按收件人与天数过滤）或 `web_api`（Cookie 回退，忽略 `days`）
- `messages[].id` 在 IMAP 路径下是 IMAP UID（十进制字符串），**去重与后续读正文都用它**
- `preview` 是纯文本摘要，已剥离 HTML/CSS，验证码一类内容会保留

### 3.4 取件：读正文

```http
GET /api/inbox/1042?account_id=acc_1a2b3c4d
```

```json
{
  "success": true,
  "data": {
    "id": "1042",
    "from": "GitHub <noreply@github.com>",
    "to": "alpha@icloud.com",
    "subject": "请验证你的邮箱",
    "date": "2026-09-16T14:32:10+08:00",
    "preview": "验证码 654321",
    "body": "验证码：654321",
    "body_html": "<p>验证码：<b>654321</b></p>",
    "content_type": "text/html"
  }
}
```

| 字段 | 说明 |
|---|---|
| `body` | 可读纯文本，始终存在；**直接拿它做验证码/链接提取最省事** |
| `body_html` | **邮件自带的原始 HTML，服务端不加工**（保留原有 script、样式、事件属性），适合你做二次处理或自行清理 |
| `body_html_sanitized` | 带 `sanitize=1` 时返回，已移除可执行内容——要直接渲染就用这份 |
| `raw_message` | 带 `raw=1` 时返回，完整 RFC822 报文的 base64（含全部头部、MIME 部分与附件） |
| `raw_size` | 原始报文字节数（读取上限 10 MiB） |
| `content_type` | 有 HTML 时为 `text/html`，否则 `text/plain` |

需要原始数据时：

```bash
# 原始 HTML + 完整报文(base64)
curl -b jar.txt "$BASE/api/inbox/1042?account_id=$ACCOUNT&sanitize=1&raw=1"

# 只要原始 HTML(不清理)
curl -b jar.txt "$BASE/api/inbox/1042?account_id=$ACCOUNT"
```

渲染前请注意：`body_html` 未经清理，**不要直接塞进主文档的 `innerHTML`**；要么用 `sanitize=1` 取清理版，要么放进 `sandbox` iframe 且不授予 `allow-scripts`。

正文解析已处理 `multipart/*` 嵌套、`base64`/`quoted-printable` 解码与字符集转换，附件会被跳过。

### 3.5 删除邮件（写操作）

```http
DELETE /api/inbox/1042?account_id=acc_1a2b3c4d
X-CSRF-Token: <token>
```

```json
{ "success": true, "data": { "id": "1042" } }
```

### 3.6 创建别名（写操作）

程序里要主动造地址时：

```http
POST /api/create
X-CSRF-Token: <token>
Content-Type: application/json

{ "account_id": "acc_1a2b3c4d", "label": "自动化任务" }
```

```json
{
  "success": true,
  "data": {
    "email": "gamma@icloud.com",
    "label": "自动化任务",
    "created_at": "2026-09-16T14:40:00+08:00",
    "account_id": "acc_1a2b3c4d"
  }
}
```

创建受 iCloud 侧节流限制（经验值：约每 30 分钟 `5 × 家庭成员数`，账号总量约 700；非官方数字）。撞上限时返回 `502 UPSTREAM_FAILURE`，不要无脑重试。

---

## 4. 轮询取件建议

```text
登录（一次）→ 循环 { 取件 → 处理新邮件 → sleep } → 收到 401 时重新登录
```

- **间隔 ≥ 10 秒**：IMAP 路径每次都会真实拉取邮件，太密没有收益。按用途取 10–60 秒；纯等验证码场景 5–10 秒也够用
- **按 `id` 去重**：把已处理的 `messages[].id` 存进内存集合或数据库；同一封邮件的 `id` 稳定
- **用 `alias` 收窄范围**：同时跑多个注册任务时，按别名分别拉取，避免互相干扰
- **错误退避**：`502`（iCloud 侧失败）用 5s → 15s → 60s 退避；`401` 立即重登一次；`429` 读 `Retry-After`
- **别把 `days` 当分页**：它只是时间过滤；要更多邮件就提高 `limit`（上限 100）
- **并发按账号分片**：同一账号的列表接口是串行的，多开只会排队；详见第 6 节「并发能力」

---

## 5. 错误码与处理策略

| 错误码 | HTTP | 程序应该怎么做 |
|---|---|---|
| `AUTH_REQUIRED` | 401 | 重新登录一次；仍失败则报配置错误 |
| `INVALID_CREDENTIALS` | 401 | 密码配置错了，不要重试 |
| `RATE_LIMITED` | 429 | 按 `Retry-After` 等待；检查是否在频繁重登 |
| `CSRF_INVALID` | 403 | 写操作时漏了 `X-CSRF-Token`，或会话已换 |
| `VALIDATION_ERROR` | 400 | 参数问题（缺 `account_id`、`limit`/`days` 越界等） |
| `VALIDATION_ERROR` | 404 | 路径写错 |
| `ACCOUNT_NOT_FOUND` | 404 | `account_id` 失效，重新拉账号列表 |
| `UPSTREAM_UNAUTHORIZED` | 401 | 账号的 iCloud Cookie 失效，需要人工更新凭据 |
| `UPSTREAM_FAILURE` | 502 | iCloud 侧失败（含创建别名被节流），退避重试 |

`message` 是固定文案，不会透出 iCloud 原始响应，适合直接打日志。

---

## 6. 并发能力

同一个账号的取件请求**可以并行**：IMAP 连接池按账号维护多条长连接，默认上限 **10 条**（`ICLOUD_HME_IMAP_POOL_SIZE`，范围 1–50）。

| 场景 | 现状 |
|---|---|
| 多个账号同时取件 | 并行，互不影响 |
| 同一账号并发调 `GET /api/inbox`（列表） | 并行，最多同时 10 条连接；超出上限排队等待，不会失败 |
| 同一账号并发调 `GET /api/inbox/:id`、`DELETE /api/inbox/:id` | 并行，且复用池中长连接（省掉每次 TLS + LOGIN） |
| 账号配了外部收件邮箱后取件 | 并行，每次独立建连 |

实测（本机、单账号 6 并发、假凭据以便观察连接行为）：

```
改造前(每账号单连接)         6 并发总耗时 9.2s    ← 完全串行
ICLOUD_HME_IMAP_POOL_SIZE=2  6 并发总耗时 6.4s    ← 2 路并行
默认(10 条连接)              6 并发总耗时 4.4s    ← 明显改善
```

池的调度语义由单元测试固定（`internal/mail/pool_test.go`）：配置 3 条连接时，12 个并发调用的实测并发度**恰好为 3**，且只建立 3 条连接（其余复用）；池满时排队而非报错，等待超过 60 秒才返回"连接池繁忙"。

两点使用建议：

- **并发上限不是越高越好**：iCloud 对单个账号的 IMAP 连接数有隐藏限制，实测中并发连接数超过约 2–3 条后，服务端侧的握手/登录响应会变慢。10 条是兼顾吞吐与稳定的默认值，账号风控敏感时可调小
- **同账号并发收益有限**：读信本身是低频操作，若只是轮询取件，1–2 条连接通常就够；真正需要线性提升吞吐时，多账号并行的效果更确定

## 7. 取件链接（分享给别人看邮件）

除了用管理员会话调用接口，还可以给单个别名生成一条**只读链接**，让别的程序或人直接取件：

```bash
# 1) 管理员侧生成(幂等,重复调用返回同一个 token)
curl -b jar.txt -X POST "$BASE/api/aliases/abc123/share-link" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d '{"account_id":"acc_1"}'
# → data.url 形如 http://host:port/?token=xxxxx,直接分享这个地址

# 2) 拿到链接的程序/人直接取件(不需要任何 Cookie)
curl "$BASE/api/share/xxxxx/inbox?limit=20&days=7"           # 邮件列表
curl "$BASE/api/share/xxxxx/inbox/1042"                      # 邮件正文

# 3) 不再需要时撤销
curl -b jar.txt -X DELETE "$BASE/api/aliases/abc123/share-link" \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d '{"account_id":"acc_1"}'
```

与管理员接口的差异：

| 项目 | 管理员接口 | 取件链接 |
|---|---|---|
| 认证 | 会话 Cookie（+ 写操作要 CSRF） | 仅需 URL 里的 token |
| 可见范围 | 账号下全部别名 | 仅该 token 对应的一个别名 |
| 可做操作 | 读、删邮件、管理账号 | 只读 |
| 响应字段 | 含 `account_id` | 不含任何账号内部标识 |
| 失效方式 | 会话过期 | 撤销链接，或重新生成（轮换 token） |

token 泄露等同于"该别名收件箱的只读权限"，请按需要分发；链接会记录命中次数与最近使用时间，可在别名列表的弹窗里查看并随时撤销。

## 8. 已知限制

- **读正文与删除仅支持 IMAP 路径**：`method` 为 `web_api` 时，`messages[].id` 不是 IMAP UID，`GET/DELETE /api/inbox/:id` 会返回 `400`。给账号配置 App 专用密码或外部收件邮箱后即可用
- **`days` 仅 IMAP 生效**：Web API 回退路径只按 `limit` 截断
- **需要账号先配置凭据**：Cookie 用于 Web 路径，App 专用密码用于 IMAP 路径；未配置时 `/api/aliases`、`/api/inbox` 会返回 `400 VALIDATION_ERROR`（"账号未配置 Cookie"）或 `401 UPSTREAM_UNAUTHORIZED`
- **会话在内存中**：服务重启后需重新登录
- **接口无跨域**：CSP 为 `default-src 'self'`，同机或同域部署才能直接调用；跨域部署请在前面挂反向代理
