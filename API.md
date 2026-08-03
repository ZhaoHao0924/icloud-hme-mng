# iCloud Hide My Email API 文档

## 概述

HTTP JSON API，所有接口返回统一格式：

```json
{
  "success": true,
  "data": {},
  "message": ""
}
```

### 访问控制

服务默认只监听 `127.0.0.1:8081`。使用 `0.0.0.0`、`:8081` 或其他非回环地址时，必须设置至少 32 个字符的环境变量 `ICLOUD_HME_API_TOKEN`，否则服务会拒绝启动。

配置令牌后，脚本和自动化调用可继续通过 Bearer Token 访问所有业务接口：

```http
Authorization: Bearer <ICLOUD_HME_API_TOKEN>
```

首次初始化管理员账户时也必须携带该 Token。管理员初始化完成后，内置 Web UI 的业务请求可改用平台登录会话，不需要在每次请求中携带 Token；Token 仍可用于脚本、自动化和服务重启后的恢复访问。

Bearer Token 缺失或错误时返回：

```json
{
  "code": "api_token_invalid",
  "success": false,
  "message": "API 访问令牌无效或缺失"
}
```

`code: "api_token_invalid"` 专用于部署 API Token 的缺失或校验失败。客户端应将其与 iCloud 上游会话失效区分处理，不能把该错误引导为 Cookie 恢复。

内置 Web UI 可通过页面顶栏的钥匙按钮输入令牌。令牌只保留在当前页面内存中，不写入 URL、Web Storage、Cache Storage、IndexedDB 或查询缓存；刷新或关闭页面后必须重新输入。

### 平台登录

生产服务启动后会启用平台登录认证。首次打开 Web UI 时需要创建管理员账号；没有默认密码。管理员密码仅以 bcrypt 哈希保存在数据目录的 `platform-auth.json` 中，文件权限为 `0600`。浏览器登录成功后，服务签发仅服务端保存的 12 小时 HttpOnly、SameSite=Strict 会话 Cookie；服务重启、会话到期或退出登录都会使该会话失效。

`/api/auth/*` 用于建立或查询平台会话，不要求已有平台会话：

```http
GET /api/auth/session
POST /api/auth/setup
POST /api/auth/login
POST /api/auth/logout
```

首次初始化与登录请求体均为：

```json
{
  "username": "admin",
  "password": "至少 12 个字符的管理员密码"
}
```

- `GET /api/auth/session` 返回 `authenticated`、`configured`、`username` 和会话过期时间；不会返回密码或 Cookie。
- `POST /api/auth/setup` 仅能在尚未配置管理员时成功一次。服务配置 `ICLOUD_HME_API_TOKEN` 时，该请求必须携带有效 Bearer Token。
- `POST /api/auth/login` 验证管理员账号和密码并建立新会话；账号或密码错误时返回 `401` 与 `code: "platform_auth_invalid"`。
- `POST /api/auth/logout` 会撤销当前浏览器会话并清除 Cookie。
- 除 `/api/auth/*` 外，所有业务接口要求有效的平台会话或有效 Bearer Token。未初始化返回 `401` 和 `code: "platform_auth_setup_required"`；未登录返回 `401` 和 `code: "platform_auth_required"`。

**错误响应:**

- `400 Bad Request` — 参数错误
- `401 Unauthorized` — `code: "api_token_invalid"` 表示 API Token 错误；`platform_auth_setup_required` 和 `platform_auth_required` 表示需要完成平台初始化或登录；其他无上述 code 的 401/403 可表示 iCloud 会话失效
- `413 Payload Too Large` — 请求体超过 1 MiB
- `404 Not Found` — 账号不存在
- `500 Internal Server Error` — 本地账户配置读取或持久化失败
- `502 Bad Gateway` — iCloud 服务错误

### 输入边界

| 输入                          | 限制                                         |
| ----------------------------- | -------------------------------------------- |
| `/api` 请求体                 | 最大 1 MiB（1,048,576 字节），超限返回 `413` |
| `GET /api/inbox` 的 `limit`   | `1..100`，默认 `20`                          |
| `GET /api/inbox` 的 `days`    | `1..365`，默认 `7`                           |
| `POST /api/create` 的 `label` | 最多 200 个 Unicode 字符                     |
| 单个账户的 Cookie             | 最多 128 个                                  |

---

## 账户配置文件

服务从数据目录中的 `accounts.json` 读取账户。规范持久化 schema 如下：

```json
{
  "accounts": [
    {
      "id": "acc_1",
      "name": "主号",
      "real_email": "user@example.com",
      "icloud_email": "your_email@icloud.com",
      "host": "icloud.com",
      "cookies": {
        "X-APPLE-WEBAUTH-TOKEN": "YOUR_WEBAUTH_TOKEN_HERE",
        "X-APPLE-WEBAUTH-USER": "v=1:s=1:d=YOUR_DSID_HERE",
        "X-APPLE-WEBAUTH-HSA-TRUST": "YOUR_TRUST_TOKEN_HERE",
        "X-APPLE-DS-WEB-SESSION-TOKEN": "YOUR_SESSION_TOKEN_HERE"
      },
      "proxy": "",
      "app_password": "xxxx-xxxx-xxxx-xxxx",
      "status": "active",
      "alias_total": 0,
      "alias_active": 0,
      "last_validated": "",
      "last_error": "",
      "created_at": "2026-07-31T09:00:00+08:00"
    }
  ],
  "updated_at": "2026-07-31T09:00:00+08:00"
}
```

- `accounts` 必须是数组；`id`、`name` 必须非空，且 `id` 在文件中唯一。
- `host` 省略时使用 `icloud.com`；`status` 可为 `pending`、`active` 或 `error`，省略时使用 `pending`。
- `cookies` 是 Cookie 名称到值的 JSON 对象，每个账户最多 128 个；`app_password` 是单个字符串。浏览器 Cookie 数组和 `app_passwords` 数组不是持久化 schema。
- `real_email`、`icloud_email`、`proxy`、`cookies` 和 `app_password` 可以为空；统计、校验时间、错误、创建时间和 `updated_at` 由服务维护。
- 旧版本写出的 `{"accounts":{"acc_1":{...}}}` 对象格式只用于迁移兼容；服务下一次保存时会按账户 ID 排序并写回规范数组格式。
- 文件包含敏感凭据，不得提交到版本控制或通过 API 返回原始内容。
- 保存使用同目录临时文件、文件同步和原子替换。保存失败时相关内存变更会回滚，账户变更接口返回 `500`，现有 `accounts.json` 保持不变。

该磁盘 schema 与账户 API 的脱敏响应 DTO 不同。API 只返回后文定义的账户摘要和凭据能力布尔值。

---

## 系统接口

### 健康检查

```http
GET /api/health
```

该接口与其他业务接口一样要求有效的平台会话或 Bearer Token。鉴权通过且服务能够响应时，接口固定返回 HTTP `200`，并设置 `Cache-Control: no-store`。

```json
{
  "success": true,
  "data": {
    "service": "icloud-hme",
    "version": "v0.2.0",
    "status": "ok",
    "config_available": true
  }
}
```

- `version` 来自构建时注入的版本；未注入时为 `dev`。
- `config_available` 表示数据目录及磁盘上的 `accounts.json` 可读取且符合配置 schema；尚未创建配置文件的空数据目录也视为可用。
- 配置不可用时，`status` 为 `degraded`、`config_available` 为 `false`，仍返回 HTTP `200`。
- 响应不会包含数据目录、账户数量、文件名、凭据或内部错误详情。

---

## 核心接口

### 1. 创建 HME 别名

```http
POST /api/create
Content-Type: application/json

{
  "account_id": "acc_1",
  "label": "注册某网站"
}
```

**响应:**

```json
{
  "success": true,
  "data": {
    "email": "xyz123@icloud.com",
    "label": "注册某网站",
    "created_at": "2024-01-15T10:30:00Z",
    "account_id": "acc_1"
  }
}
```

**参数说明:**

- `account_id` (必填) — 账号 ID
- `label` (可选) — 别名标签，最多 200 个 Unicode 字符；省略时默认为 "Created YYYY-MM-DD HH:mm"

**错误情况:**

- `401` — Cookie 过期，需更新
- `502` — iCloud 服务错误，会自动重试 5 次

---

### 2. 读取邮件

```http
GET /api/inbox?account_id=acc_1&alias=xyz123@icloud.com&limit=20&days=7
```

**响应 (走 IMAP,App Password):**

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1",
    "alias": "xyz123@icloud.com",
    "count": 2,
    "method": "imap",
    "messages": [
      {
        "id": "1042",
        "from": "GitHub <noreply@github.com>",
        "to": "xyz123@icloud.com",
        "subject": "[GitHub] Please verify your email address",
        "date": "2026-07-09T14:32:10+08:00",
        "preview": "Almost done! To finish setting up your account, we just need to verify.."
      }
    ]
  }
}
```

**响应 (回退到 Web API,Cookie):** `method` 变为 `web_api`

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1",
    "alias": "xyz123@icloud.com",
    "count": 1,
    "method": "web_api",
    "messages": [
      {
        "id": "AQMkAD...",
        "from": "GitHub <noreply@github.com>",
        "to": "xyz123@icloud.com",
        "subject": "[GitHub] Please verify your email address",
        "date": "2026-07-09T06:32:10Z",
        "preview": "Almost done! To finish setting up your account.."
      }
    ]
  }
}
```

**参数说明:**

- `account_id` (必填) — 账号 ID
- `alias` (可选) — 只返回发到该别名的邮件;不传返回收件箱最近邮件
- `limit` (可选) — 返回邮件数量，范围 `1..100`，默认 20
- `days` (可选) — 查找最近几天的邮件，范围 `1..365`，默认 7 (仅 IMAP 模式)

**邮件读取双路径 (自动选择):**

1. **优先: IMAP (App Password)** — 设置了 App Password 时使用,支持服务端按收件人搜索
2. **回退: Web API (Cookie 认证)** — 无 App Password 或 IMAP 失败时,通过 iCloud mccgateway 端点读取

响应中 `"method": "imap"` 或 `"method": "web_api"` 标识实际使用的读取方式。

IMAP 模式在服务器搜索阶段应用 `days`，结果按邮件时间倒序返回。摘要读取使用 `BODY.PEEK`，每封邮件最多拉取前 64 KiB 原始内容，返回的 `preview` 最多 4 KiB UTF-8 数据且不会因此把邮件标记为已读。

Web API 模式将时间戳规范为 UTC RFC3339、按时间倒序返回，并将 `preview` 限制为 4 KiB UTF-8 数据。`to` 仅来自响应中明确的 `to`/recipient 字段；普通收件箱响应缺少该信息时 `to` 为空，带 `alias` 的请求则返回 `502`，不会根据主题或发件人猜测。

**别名过滤逻辑:**

- **IMAP (`FindByRecipient`):** 先用原生 IMAP `TO` 头搜索 (配合 `days` 时间范围);无结果时拉取最近 `limit*3` 条本地按 `To` 兜底过滤
- **Web API (`FindByAlias`):** 拉取 `limit*2`（至少 50）条后，仅对响应中明确的 `To`/`CC`/`BCC` 地址做不区分大小写的完整地址匹配；任一候选邮件缺少可验证收件人时显式返回错误

**返回字段差异 (两条路径):**

- `id` — IMAP 是 UID 数字串,Web API 是 iCloud GUID
- `from`、`to`、`subject`、`date`、`preview` — 两条路径始终返回这些字符串字段，缺失值为空字符串
- `date` — 两条路径均为 RFC3339；Web API 统一为 UTC
- `preview` — 正文摘要,非完整正文；两条路径最多返回 4 KiB UTF-8 数据

---

## 账号管理接口

### 3. 列出所有账号

```http
GET /api/accounts
```

**响应:**

```json
{
  "success": true,
  "data": [
    {
      "id": "acc_1",
      "name": "主号",
      "real_email": "user@example.com",
      "icloud_email": "user@icloud.com",
      "host": "icloud.com",
      "status": "active",
      "alias_total": 10,
      "alias_active": 8,
      "last_validated": "2026-07-31T10:00:00+08:00",
      "last_error": "",
      "created_at": "2026-07-31T09:00:00+08:00",
      "has_cookies": true,
      "has_app_password": true,
      "proxy_configured": false
    }
  ]
}
```

`has_cookies`、`has_app_password` 和 `proxy_configured` 仅表示对应能力是否已配置。所有返回账户详情的成功响应都使用上述安全 DTO，不包含 Cookie 名称或值、App Password、Apple ID 密码、代理地址或代理凭据。

---

### 4. 添加账号

**简化版（cookies 可选）:**

```http
POST /api/accounts
Content-Type: application/json

{
  "name": "新账号",
  "icloud_email": "user@icloud.com",
  "host": "icloud.com",
  "proxy": "http://user:pass@host:port"
}
```

**完整版（包含 Cookie）:**

```http
POST /api/accounts
Content-Type: application/json

{
  "name": "新账号",
  "icloud_email": "user@icloud.com",
  "cookies": "{\"x-apple-session-token\":\"token_value\"}",
  "host": "icloud.com",
  "proxy": "http://user:pass@host:port"
}
```

**响应:**

```json
{
  "success": true,
  "data": {
    "id": "acc_3",
    "name": "新账号",
    "real_email": "",
    "icloud_email": "user@icloud.com",
    "host": "icloud.com",
    "status": "pending",
    "alias_total": 0,
    "alias_active": 0,
    "last_validated": "",
    "last_error": "",
    "created_at": "2026-07-31T09:00:00+08:00",
    "has_cookies": false,
    "has_app_password": false,
    "proxy_configured": true
  }
}
```

**参数说明:**

- `name` (必填) — 账号名称
- `icloud_email` (可选) — iCloud 邮箱地址；仅接受 `@icloud.com`、`@me.com` 或 `@mac.com`，创建无 Cookie 的账号后发起密码登录时必须提供
- `cookies` (可选) — Cookie 字符串，解析后最多 128 个，支持两种格式:
  - JSON: `"{\"name\":\"value\"}"`
  - Header: `"name1=value1; name2=value2"`
- `host` (可选) — iCloud 域名,默认 `icloud.com`
- `proxy` (可选) — HTTP/SOCKS5 代理

**注意:** 不传 cookies 时,账号状态为 `pending`。创建时提供 `icloud_email` 后，可通过 `/api/accounts/:id/login/start` 发起两阶段密码登录并获取 Cookie；如果 Cookie 会话有效且未显式提供邮箱，服务端会尝试从账户信息推导该字段。

---

### 5. 账号密码登录（两阶段，Cookie 仅服务端保存）

第一阶段提交常规 iCloud 密码（不是 App Password）：

```http
POST /api/accounts/:id/login/start
Content-Type: application/json

{"password":"用户的常规 iCloud 密码"}
```

无 2FA 时会直接完成登录，`data` 为脱敏账户摘要（与账户列表结构相同）：

```json
{
  "success": true,
  "data": {
    "id": "acc_1",
    "name": "主号",
    "status": "active",
    "has_cookies": true
  }
}
```

需要 2FA 时返回内存 challenge（有效期 300 秒）：

```json
{
  "success": true,
  "data": {
    "status": "otp_required",
    "challenge_id": "temporary-id",
    "expires_in": 300
  }
}
```

第二阶段提交验证码：

```http
POST /api/accounts/:id/login/verify
Content-Type: application/json

{"challenge_id":"temporary-id","otp_code":"123456"}
```

验证成功返回脱敏账户摘要，Cookie 永不返回给客户端。challenge 绑定账户、只能使用一次；过期、重放或账户路径不匹配会返回 `410`，验证码错误后需要重新发起 `login/start`。

**错误状态:**

- `400` — 请求字段缺失或验证码不是 6 位数字
- `401` — Apple 拒绝账号密码或验证码
- `403` — 需要先确认 Apple 隐私条款
- `410` — challenge 无效、过期或已消费
- `502` — Apple 服务或网络错误

---

### 6. 删除账号

```http
DELETE /api/accounts/:id
```

**响应:**

```json
{
  "success": true,
  "data": {
    "id": "acc_3"
  }
}
```

**错误情况:**

- `404` — 账号不存在

---

### 7. 设置 App Password

```http
POST /api/accounts/:id/password
Content-Type: application/json

{
  "icloud_email": "your_email@icloud.com",
  "app_password": "xxxx-xxxx-xxxx-xxxx"
}
```

**响应:**

```json
{
  "success": true,
  "data": {
    "id": "acc_1",
    "name": "主号",
    "real_email": "user@example.com",
    "icloud_email": "your_email@icloud.com",
    "host": "icloud.com",
    "status": "active",
    "alias_total": 10,
    "alias_active": 8,
    "last_validated": "2026-07-31T10:00:00+08:00",
    "last_error": "",
    "created_at": "2026-07-31T09:00:00+08:00",
    "has_cookies": true,
    "has_app_password": true,
    "proxy_configured": false
  }
}
```

**参数说明:**

- `icloud_email` (必填) — iCloud 邮箱地址
- `app_password` (必填) — App 专用密码

**用途:** App Password 用于 IMAP 邮件读取，生成方式见 [appleid.apple.com](https://appleid.apple.com)

---

### 8. 更新 Cookie

```http
PUT /api/accounts/:id/cookies
Content-Type: application/json

{
  "cookies": {
    "X-APPLE-WEBAUTH-TOKEN": "token_value",
    "X-APPLE-WEBAUTH-USER": "user_value"
  }
}
```

服务端会校验并保存 Cookie，单个账户最多提交 128 个。成功响应使用第 3 节定义的安全账户 DTO，`has_cookies` 为 `true`，不会返回 Cookie 数量、名称或值。

---

## 别名管理接口

### 9. 列出所有别名

```http
GET /api/aliases?account_id=acc_1
```

**响应:**

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1",
    "count": 15,
    "aliases": [
      {
        "email": "xyz123@icloud.com",
        "anonymousId": "abc123",
        "label": "注册某网站",
        "active": true,
        "createdAt": "2024-01-15T10:30:00Z"
      }
    ]
  }
}
```

`aliases` 按 `createdAt` 从新到旧返回；缺失或无法解析创建时间的别名排在最后。

**参数说明:**

- `account_id` (必填) — 账号 ID

**别名字段:**

- `email` — HME 邮箱地址
- `anonymousId` — 别名唯一标识（用于停用/激活/删除）
- `label` — 用户定义的标签
- `active` — 是否激活
- `createdAt` — 创建时间

---

### 10. 停用别名

```http
POST /api/aliases/:id/deactivate
Content-Type: application/json

{
  "account_id": "acc_1"
}
```

**响应:**

```json
{
  "success": true,
  "data": {
    "anonymous_id": "abc123",
    "success": true
  }
}
```

**参数说明:**

- `:id` (路径参数) — 别名的 `anonymousId`
- `account_id` (必填) — 账号 ID

**说明:** 停用后别名不再接收邮件，但可随时激活恢复

---

### 11. 激活别名

```http
POST /api/aliases/:id/reactivate
Content-Type: application/json

{
  "account_id": "acc_1"
}
```

**响应:**

```json
{
  "success": true,
  "data": {
    "anonymous_id": "abc123",
    "success": true
  }
}
```

**参数说明:**

- `:id` (路径参数) — 别名的 `anonymousId`
- `account_id` (必填) — 账号 ID

**说明:** 激活已停用的别名，恢复邮件接收

---

### 12. 删除别名

```http
DELETE /api/aliases/:id
Content-Type: application/json

{
  "account_id": "acc_1"
}
```

**响应:**

```json
{
  "success": true,
  "data": {
    "anonymous_id": "abc123"
  }
}
```

**参数说明:**

- `:id` (路径参数) — 别名的 `anonymousId`
- `account_id` (必填) — 账号 ID

**注意:** 删除不可恢复！如果直接删除失败，会先停用再删除

---

### 13. 批量创建别名

```http
POST /api/accounts/:id/aliases/batch
Content-Type: application/json

{
  "count": 3,
  "label_prefix": "注册"
}
```

`count` 必须为 `1..20`。`label_prefix` 可选，最多 196 个 Unicode 字符；当一次创建多个别名且设置了前缀时，服务会依次使用 `注册 1`、`注册 2` 等标签。

```json
{
  "success": true,
  "data": {
    "account_id": "acc_1",
    "requested": 3,
    "created": 3,
    "failed": 0,
    "complete": true,
    "aliases": [
      {
        "account_id": "acc_1",
        "email": "abc123@icloud.com",
        "label": "注册 1",
        "created_at": "2026-08-02T09:00:00Z"
      }
    ]
  }
}
```

遇到上游创建失败时，服务会停止后续创建。若已成功创建部分别名，仍返回 `200` 和 `complete: false`，并通过 `created`、`failed`、`error` 表示结果；首个创建即失败时返回相应的 `401` 或 `502` 错误。

### 14. 别名自动化规则

自动化规则按账户保存在 `accounts.json` 的 `alias_automation` 字段中。服务进程运行期间会每分钟检查一次到期规则；重启后会继续使用已保存的下一次执行时间。每个账户的创建、批量创建和自动化执行均串行处理，避免并发刷新 Cookie。

```http
GET /api/accounts/:id/alias-automation
```

未配置规则时返回安全默认值：`enabled: false`、执行间隔 60 分钟、单次上限 5。

```http
PUT /api/accounts/:id/alias-automation
Content-Type: application/json

{
  "enabled": true,
  "interval_minutes": 60,
  "allowed_weekdays": [1, 2, 3, 4, 5],
  "execution_window_start": "09:00",
  "execution_window_end": "17:00",
  "scheduled_batch_size": 0,
  "minimum_active": 0,
  "target_active": 0,
  "max_batch_size": 5,
  "max_total_aliases": 1000,
  "max_failure_count": 3,
  "daily_creation_limit": 20,
  "target_created": 750,
  "label_prefix": ""
}
```

字段约束：

- `interval_minutes`: `5..10080`
- `allowed_weekdays`: 可选的 `0..6` 星期值数组，`0` 为周日；空数组表示每天，元素不可重复
- `execution_window_start` 与 `execution_window_end`: 可同时为空表示全天；设置时均为 `HH:MM`，开始必须早于结束，不支持跨午夜时间窗
- `scheduled_batch_size`: `0..max_batch_size`
- `minimum_active` 与 `target_active`: `0..100`；启用阈值补货时 `target_active` 不得小于 `minimum_active`
- `max_batch_size`: `1..20`
- `max_total_aliases`: `1..1000`；自动化创建前读取的总别名安全上限，默认 `1000`
- `max_failure_count`: `1..10`；连续失败达到该值时自动暂停，默认 `3`
- `daily_creation_limit`: `0..1000`；每日自动化创建上限，`0` 表示不限制
- `target_created`: `0..1000`；`0` 表示不设置累计创建上限
- 启用规则时，至少设置一个大于零的 `scheduled_batch_size`、`minimum_active` 或 `target_created`

每次到期执行时，服务先读取当前别名总数和活跃别名数：总数达到 `max_total_aliases` 时不会创建并自动暂停；最后一轮仅剩部分安全容量时会缩小到该容量后暂停。定时创建会请求 `scheduled_batch_size` 个；当活跃数低于 `minimum_active` 时，库存补货会请求补至 `target_active` 的数量。两种规则同时配置时取较大值，再受 `max_batch_size` 限制。配置 `target_created` 后，服务会累计本规则成功创建的别名数（响应中的 `created_total`）；目标型规则未设置定时数量时，每次按 `max_batch_size` 创建，最后一轮会自动缩小到剩余数量。修改 `target_created` 会开始新的累计周期并将 `created_total` 归零。

工作日和时间窗按部署服务器所在时区解释，仅约束定时调度；到期但不在允许范围的规则会将 `next_run_at` 延后到下一个允许时间，避免每分钟重复扫描。`daily_creation_limit` 仅限制自动化执行（包括立即执行规则），不影响单个或批量手动创建。计数按服务所在时区的自然日重置，并在响应中通过 `daily_created` 和 `daily_created_date` 返回。达到每日上限时，规则仍保持启用，`next_run_at` 会延后到次日零点之后的下一个允许时间，不会触发永久暂停。

`success` 会清零 `consecutive_failure`。`partial` 或 `error` 会使其递增，并将下次运行间隔按 `2` 的幂次退避，最大不超过 7 天。连续失败达到 `max_failure_count` 后规则自动暂停。暂停原因 `pause_reason` 可为 `target_reached`、`alias_limit`、`failure_limit` 或 `manual`；暂停时 `enabled` 为 `false`、`next_run_at` 为空。达到累计目标的规则不能恢复，必须先修改 `target_created`。暂停中的规则调用立即执行接口返回 `409`，避免绕过安全暂停。

```http
POST /api/accounts/:id/alias-automation/run
```

该接口立即按已保存的规则执行一次，并返回 `active_before`、`requested`、`created`、`failed`、`complete`、`status`、新建别名和更新后的 `automation` 状态。`status` 为 `success`、`partial`、`skipped` 或 `error`；执行后会记录 `last_run_at`、`next_run_at`、`last_active`、`last_created` 和脱敏错误摘要。

立即执行不受 `allowed_weekdays` 和执行时间窗限制，但仍会遵守累计创建目标、总别名安全上限、每日上限和暂停状态。

```http
POST /api/accounts/:id/alias-automation/preview
```

该接口仅读取当前别名列表，返回本次可创建数量、总数和活跃数、每日余量、总别名余量、累计目标余量，以及当前计划是否处于允许时间。它不会创建别名、保存刷新 Cookie、写入创建历史或修改自动化规则状态。

```http
POST /api/accounts/:id/alias-automation/pause
POST /api/accounts/:id/alias-automation/resume
```

暂停接口不会改变累计创建进度或每日计数，并将 `pause_reason` 设为 `manual`（累计目标已完成时保留 `target_reached`）。恢复接口会清除暂停状态和连续失败计数、重新计算下次执行时间；规则未配置或累计目标已完成时返回 `400`。

### 15. 别名创建历史与 CSV 导出

```http
GET /api/accounts/:id/alias-creation-history?limit=100
GET /api/accounts/:id/alias-creation-history.csv
```

历史接口返回账户最近创建批次，按创建时间倒序；`limit` 范围为 `1..500`，默认 `100`。每条记录包含稳定的 `batch_id`、触发方式（`manual`、`batch`、`automation_manual` 或 `automation_scheduled`）、请求/成功/失败数量、完成状态、脱敏错误摘要及本批创建的别名。单个创建、批量创建和自动化运行的响应也会返回同一个 `batch_id`，可用于追踪来源。

创建历史最多保留每个账户最近 500 个批次，保存在 `accounts.json` 中。CSV 导出不接受分页参数，始终导出当前保留的全部批次；一行对应一个别名，未创建任何别名的批次仍会输出一行统计记录。历史不包含 Cookie、App Password、Apple ID 密码或上游原始响应。

### 16. 操作日志

```http
GET /api/logs?limit=200
```

返回最近的隐私安全操作日志，`limit` 范围为 `1..500`，默认 `200`。日志按时间倒序，保存最近 7 天；过期记录会在服务启动、写入和每小时清理时自动删除。每条记录只包含时间、级别、操作名称、HTTP 状态和耗时，不包含账户 ID、邮件地址、Cookie、App Password、请求参数、邮件正文或 Apple 原始响应。定时别名自动化在每次运行状态持久化后也会写入 `定时执行别名自动化` 记录。

---

## 使用示例

### curl 示例

```bash
# 创建别名
curl -X POST http://localhost:8081/api/create \
  -H "Content-Type: application/json" \
  -d '{"account_id": "acc_1", "label": "GitHub"}'

# 读取邮件
curl "http://localhost:8081/api/inbox?account_id=acc_1&alias=xyz123@icloud.com&limit=10"

# 列出别名
curl "http://localhost:8081/api/aliases?account_id=acc_1"

# 停用别名
curl -X POST http://localhost:8081/api/aliases/abc123/deactivate \
  -H "Content-Type: application/json" \
  -d '{"account_id": "acc_1"}'

# 删除别名
curl -X DELETE http://localhost:8081/api/aliases/abc123 \
  -H "Content-Type: application/json" \
  -d '{"account_id": "acc_1"}'
```

### Python 示例

```python
import requests

BASE_URL = "http://localhost:8081/api"

# 创建别名
resp = requests.post(f"{BASE_URL}/create", json={
    "account_id": "acc_1",
    "label": "Netflix"
})
print(resp.json())

# 读取邮件
resp = requests.get(f"{BASE_URL}/inbox", params={
    "account_id": "acc_1",
    "alias": "xyz123@icloud.com",
    "limit": 10
})
print(resp.json())

# 列出别名
resp = requests.get(f"{BASE_URL}/aliases", params={"account_id": "acc_1"})
for alias in resp.json()["data"]["aliases"]:
    print(f"{alias['email']} - {alias['label']} (active: {alias['active']})")
```

---

## 认证说明

### Cookie 认证 (推荐,功能最完整)

用于：创建别名、列出别名、停用/激活/删除别名、**读取邮件**

**获取方式:**

1. 浏览器登录 [icloud.com](https://www.icloud.com) 或 [icloud.com.cn](https://www.icloud.com.cn) (国区)
2. F12 → Application → Cookies
3. 导出全部 Cookie 为 `{"key":"value"}` 格式 JSON

**关键 Cookie:**

- `X-APPLE-WEBAUTH-TOKEN` — 认证 token
- `X-APPLE-WEBAUTH-USER` — 含 dsid (`v=1:s=1:d=22789132008`)
- `X-APPLE-WEBAUTH-HSA-TRUST` — 设备信任 token
- `X-APPLE-DS-WEB-SESSION-TOKEN` — 会话 token

**有效期:** 约 24 小时

### App Password 认证 (IMAP 优先)

用于优先通过 IMAP 读取邮件；未配置或 IMAP 失败时才回退 Web API

**获取方式:**

1. 登录 [appleid.apple.com](https://appleid.apple.com)
2. 登录和安全 → App 专用密码
3. 生成新密码

---

## 技术说明

### 邮件读取实现

**Web API 路径** (`internal/mail/web_client.go`):

1. 根据账户区域调用 `setup.icloud.com` 或 `setup.icloud.com.cn` 的 validate 端点获取动态 `mccgateway` URL
2. 校验网关属于对应 iCloud 域，剥离 `:443`，并向实际动态网关 Cookie jar 写入账户 Cookie
3. 调用 `mccgateway/mailws2/v1/thread/search` 读取邮件

**⚠️ 已知坑:**

- `validate` 返回的 mccgateway URL 可能带 `:443` 端口 (如 `p217-mccgateway.icloud.com.cn:443`)
- tls-client 的 cookie jar 按不带端口的 host 存储 cookie
- 带端口请求时 cookie 无法附加,导致 403
- **解决:** 解析 URL 后剥离端口号，并把 Cookie 写入 validate 实际返回的动态网关，而不是固定 `p217` 域

**clientBuildNumber:** 与邮件接口一致,当前 `2624Build13`

**IMAP 路径** (`internal/mail/client.go`):

- 标准 IMAP 协议,连接 `imap.mail.me.com:993`
- 需要 App Password

---

## 错误处理

### 会话失效 (401)

```json
{
  "success": false,
  "message": "iCloud 会话失效，请更新 Cookie: HTTP 401"
}
```

**解决:** 更新 `accounts.json` 中的 Cookie

### iCloud 服务错误 (502)

```json
{
  "success": false,
  "message": "创建邮箱失败: HTTP 429"
}
```

**说明:** 429 错误会自动重试最多 5 次

### 参数错误 (400)

```json
{
  "success": false,
  "message": "参数错误: account_id 必填"
}
```

---

## 限制

- **创建频率**: iCloud 限制别名创建频率，过快会返回 429
- **Cookie 有效期**: 约 24 小时，需定期更新
- **邮件读取**: 依赖 IMAP 连接，超时默认 30 秒
---

## 163 发件 / QQ 收件通知

通知发送目前只支持通过 163 邮箱发件并投递到 QQ 邮箱，不提供通用 SMTP 或其他邮箱服务商配置。163 发件地址必须是 `@163.com`，QQ 收件地址必须是 `@qq.com`、`@vip.qq.com` 或 `@foxmail.com`，SMTP 固定使用 `smtp.163.com:465` 隐式 TLS。密码字段必须填写 163 邮箱授权码，不是 163 登录密码。

### 读取通知配置

`GET /api/notifications/email`

返回值只包含脱敏配置状态，不返回授权码：

```json
{
  "enabled": false,
  "configured": true,
  "provider": "163",
  "smtp_host": "smtp.163.com",
  "smtp_port": 465,
  "sender_email": "sender@163.com",
  "recipient_email": "ops@qq.com"
}
```

### 保存通知配置

`PUT /api/notifications/email`

```json
{
  "enabled": true,
  "sender_email": "sender@163.com",
  "authorization_code": "163 邮箱授权码",
  "recipient_email": "ops@qq.com"
}
```

授权码为空时保留已保存的授权码，便于修改开关或收件地址。服务将配置保存到数据目录下的 `email-notification.json`，文件权限为 `0600`；接口响应、操作日志和通知正文都不会包含授权码、Cookie、密码、别名地址或邮件正文。

### 发送测试邮件

`POST /api/notifications/email/test`

该接口使用已保存的 163 发件、QQ 收件配置发送测试邮件。自动化通知使用有界异步队列，SMTP 失败最多重试 3 次，发送失败或队列已满不会阻塞别名任务。通知事件包括手动/定时自动化结果、自动暂停和 iCloud 会话失效；邮件正文只包含脱敏账户标识、事件、状态、数量和错误摘要。

## Webhook Notifications

Webhook destinations must use HTTPS. Each delivery sends a redacted JSON payload and includes `X-iCloud-HME-Timestamp`, `X-iCloud-HME-Delivery`, and `X-iCloud-HME-Signature` headers. The signature uses HMAC-SHA256 over `{timestamp}.{body}` and is formatted as `sha256={hex}`.

### Read Webhook Configuration

`GET /api/notifications/webhook`

The response never returns the signing secret:

```json
{
  "enabled": true,
  "configured": true,
  "url": "https://hooks.example.com/icloud"
}
```

### Save Webhook Configuration

`PUT /api/notifications/webhook`

```json
{
  "enabled": true,
  "url": "https://hooks.example.com/icloud",
  "secret": "signing secret"
}
```

An empty `secret` keeps the saved secret. The configuration is stored in `webhook-notification.json` with `0600` permissions; the secret is never included in API responses, operation logs, or event payloads.

### Send a Test Webhook

`POST /api/notifications/webhook/test`

This synchronously sends a `webhook_test` event using the saved configuration. Automation results and iCloud session-expiry events use a separate bounded asynchronous queue with up to three attempts, so delivery failures cannot block alias work.

Example event payload:

```json
{
  "event": "alias_automation",
  "account_id": "acco...alue",
  "complete": true,
  "created": 2,
  "failed": 0,
  "requested": 2,
  "status": "success",
  "trigger": "manual"
}
```
