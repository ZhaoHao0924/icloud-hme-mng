# iCloud Hide My Email 本地管理工具

[English](#english) | 中文

通过逆向 iCloud Web 接口和 IMAP 邮件协议，实现 Apple iCloud 隐藏邮箱别名的创建、列出和邮件收取功能。

## 功能特性

- ✅ **创建 HME 别名** — 自动生成 iCloud 隐藏邮箱地址
- ✅ **列出所有别名** — 查看账号下的所有 HME 别名
- ✅ **收取邮件** — 通过 IMAP 或 Web API 读取发到 HME 别名的邮件
- ✅ **双路径读信** — 邮件读取优先走 IMAP (App Password),无 App Password 时回退 Web API (Cookie)
- ✅ **多账号管理** — 支持多个 iCloud 账号并行管理
- ✅ **双认证模式** — Cookie (创建别名 + 读邮件回退) 和 App Password (IMAP 优先)

## 快速开始

### 1. 安装

#### 方式一：下载二进制发布版（推荐）

从 [GitHub Releases](https://github.com/xiaozhou26/icloud-hme/releases) 下载对应平台的二进制文件：

| 平台                | 文件                           |
| ------------------- | ------------------------------ |
| Linux x86_64        | `icloud-hme_linux_amd64`       |
| Linux ARM64         | `icloud-hme_linux_arm64`       |
| macOS Intel         | `icloud-hme_darwin_amd64`      |
| macOS Apple Silicon | `icloud-hme_darwin_arm64`      |
| Windows x86_64      | `icloud-hme_windows_amd64.exe` |

```bash
# 示例：Linux 下直接运行
chmod +x icloud-hme_linux_amd64
./icloud-hme_linux_amd64
```

#### 方式二：Docker

```bash
# 拉取镜像
docker pull ghcr.io/xiaozhou26/icloud-hme:latest

# 容器需要监听非回环地址，因此必须同时设置 API Token
docker run -d \
  --name icloud-hme \
  -p 8081:8081 \
  -e ICLOUD_HME_API_TOKEN=replace-with-a-random-token-at-least-32-chars \
  -v /path/to/data:/app/data \
  ghcr.io/xiaozhou26/icloud-hme:latest \
  -addr 0.0.0.0:8081
```

镜像支持 `linux/amd64` 和 `linux/arm64` 双架构，自动适配。

#### 方式三：源码编译

```bash
# 前置要求: Go 1.26+
git clone https://github.com/xiaozhou26/icloud-hme.git
cd icloud-hme

# 编译并注入健康检查返回的版本
go build -ldflags="-X main.version=v0.2.0" -o icloud-hme.exe .

# 调试模式（启用 Gin 请求日志）
./icloud-hme.exe -debug
```

### 2. 配置账号

在程序 `data/` 目录下创建 `accounts.json`:

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

`accounts` 固定为数组，每个账户的 `id` 和 `name` 必须非空且 `id` 唯一。`cookies` 固定为 Cookie 名称到值的 JSON 对象，每个账户最多 128 个；`app_password` 是单个字符串，不使用浏览器导出的 Cookie 数组或 `app_passwords` 数组。`host` 省略时默认为 `icloud.com`，`status` 只能是 `pending`、`active` 或 `error`。

也可以通过 API 动态添加账号，无需手动编辑 JSON。`cookies`、`app_password`、`proxy` 均可选；统计、校验时间、错误和 `updated_at` 由服务维护。旧版本写出的 `{"accounts":{"acc_1":{...}}}` 对象格式仍可读取，并会在下次保存时转换为上述数组格式。完整模板见 `accounts.json.template`。

> **安全提示:** 配置文件包含 Cookie、App Password 和可能带凭据的代理 URL。不要提交到版本控制，并只授予服务账户读写权限。

服务保存配置时会先在数据目录写入权限为 `0600` 的临时文件，同步并关闭后再原子替换 `accounts.json`。写入失败会返回错误并回滚对应的内存变更，不会用半写入内容覆盖现有配置。

### 3. 启动服务

```bash
# 二进制方式（默认 data 目录）
./icloud-hme_linux_amd64

# 指定本机端口和数据目录
./icloud-hme_linux_amd64 -addr 127.0.0.1:9090 -data ./my_data

# 允许远程访问（令牌至少 32 个字符）
export ICLOUD_HME_API_TOKEN="$(openssl rand -hex 32)"
./icloud-hme_linux_amd64 -addr 0.0.0.0:9090

# 调试模式（启用请求日志）
./icloud-hme_linux_amd64 -debug

# 查看完整参数
./icloud-hme_linux_amd64 -h
```

服务默认监听 `127.0.0.1:8081`。非回环地址（例如 `0.0.0.0`、`:8081`）必须设置环境变量 `ICLOUD_HME_API_TOKEN`，否则服务会拒绝启动。设置令牌后，所有 `/api` 请求都需要携带：

```http
Authorization: Bearer <ICLOUD_HME_API_TOKEN>
```

内置 Web UI 可通过顶栏钥匙按钮输入 API Token。令牌只保留在当前页面内存中，不会写入 URL、浏览器存储或查询缓存；刷新或关闭页面后需要重新输入。服务端会用 `code: "api_token_invalid"` 标记 API Token 拒绝，以区别于 iCloud Cookie 会话失效。

所有 `/api` 请求体最大 1 MiB。收件箱参数 `limit` 范围为 `1..100`，`days` 范围为 `1..365`；创建别名的 `label` 最多 200 个 Unicode 字符。请求体超限返回 `413`，其他边界错误返回 `400`。

IMAP 收件箱在服务器搜索阶段应用 `days` 并按邮件时间倒序返回。摘要使用 `BODY.PEEK`，每封邮件最多拉取前 64 KiB 原始内容，返回的 `preview` 最多 4 KiB UTF-8 数据且不会把邮件标记为已读。

Web API 收件箱使用 validate 返回的动态 `mccgateway` 并向该网关附加 Cookie；消息按时间倒序，日期为 UTC RFC3339，`preview` 最多 4 KiB。带 `alias` 时仅精确匹配响应中明确的收件人地址；收件人不可验证时返回错误，不使用主题或发件人猜测。

## API 接口

### 系统接口

#### 健康检查

```http
GET /api/health
```

响应包含服务名、构建版本、`ok`/`degraded` 状态和配置可用性，不返回配置路径、账户或凭据。配置不可用时仍返回 HTTP `200`；启用 API Token 后，该接口同样需要 Bearer Token。完整契约见 [API.md](API.md#健康检查)。

### 核心接口

#### 创建 HME 别名

```bash
POST /api/create

# 请求体
{
  "account_id": "acc_1",      # 必填: 账号 ID
  "label": "注册某网站"        # 可选: 别名标签,最多 200 个字符
}

# 响应
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

#### 读取邮件

```bash
GET /api/inbox?account_id=acc_1&alias=xyz123@icloud.com&limit=20&days=7

# 参数说明:
#   account_id - 必填: 账号 ID
#   alias      - 可选: 只读取发到该别名的邮件
#   limit      - 可选: 返回邮件数量 (1..100,默认 20)
#   days       - 可选: 查找最近几天的邮件 (1..365,默认 7,仅 IMAP 模式)

# 响应
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
        "from": "noreply@example.com",
        "to": "xyz123@icloud.com",
        "subject": "欢迎注册",
        "preview": "感谢您的注册...",
        "date": "2026-07-09T14:32:10+08:00"
      }
    ]
  }
}

# 读取方式 (自动选择):
#   method: "imap"    — 通过 App Password 认证 (优先)
#   method: "web_api" — 通过 Cookie 认证,无需 App Password (回退)
```

### 账号管理接口

#### 列出所有账号

```bash
GET /api/accounts

# 响应
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

能力字段只表示凭据是否已配置。账户响应不会返回 Cookie 名称或值、App Password、Apple ID 密码、代理地址或代理凭据。

#### 添加账号

**简化版（cookies 可选）:**

```bash
POST /api/accounts

# 请求体
{
  "name": "新账号",
  "icloud_email": "user@icloud.com", # 无 Cookie 时用于后续密码登录
  "host": "icloud.com",           # 可选
  "proxy": "http://..."           # 可选
}

# 响应 - 状态为 pending,需登录
{
  "success": true,
  "data": {
    "id": "acc_xxx",
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

**完整版（带 Cookie）:**

```bash
POST /api/accounts

# 请求体
{
  "name": "新账号",
  "icloud_email": "user@icloud.com",                         # 可选
  "cookies": "{\"x-apple-session-token\":\"token_value\"}",  # JSON 或 Header 格式,最多 128 个
  "host": "icloud.com",           # 可选
  "proxy": "http://..."           # 可选
}

# 响应
{
  "success": true,
  "data": {
    "id": "acc_3",
    "name": "新账号",
    "status": "active"
  }
}
```

`icloud_email` 仅接受 `@icloud.com`、`@me.com` 或 `@mac.com` 地址。无 Cookie 账号会保持 `pending`，创建时提供该字段后即可调用 `/api/accounts/:id/login/start`；有效 Cookie 账号未显式提供时，服务端会尝试自动推导。

#### 账号登录（两阶段，Cookie 仅服务端保存）

```bash
POST /api/accounts/:id/login/start

# 请求体
{
  "password": "用户的常规iCloud密码"  # 不是 App Password
}

# 无 2FA 时响应 data 直接为安全账户 DTO
# 需要 2FA 时响应
{
  "success": true,
  "data": {
    "status": "otp_required",
    "challenge_id": "temporary-id",
    "expires_in": 300
  }
}

POST /api/accounts/:id/login/verify

# 请求体
{"challenge_id":"temporary-id","otp_code":"123456"}
```

`login/verify` 成功后直接返回安全账户 DTO。challenge 仅在服务端内存保存 5 分钟，绑定账户且只能使用一次；过期、重放或账户不匹配返回 `410`。Cookie 自动写入服务端配置，任何响应都不返回 Cookie、Apple 密码或 OTP。

#### 删除账号

```bash
DELETE /api/accounts/:id

# 响应
{
  "success": true,
  "data": {"id": "acc_3"}
}
```

#### 设置 App Password

```bash
POST /api/accounts/:id/password

# 请求体
{
  "icloud_email": "your_email@icloud.com",
  "app_password": "xxxx-xxxx-xxxx-xxxx"
}

# 响应
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

#### 更新 Cookie

```bash
PUT /api/accounts/:id/cookies

# 请求体
{
  "cookies": {
    "X-APPLE-WEBAUTH-TOKEN": "token_value",
    "X-APPLE-WEBAUTH-USER": "user_value"
  }
}
```

服务端会校验并保存 Cookie。成功响应返回安全账户 DTO，`has_cookies` 为 `true`，不会返回 Cookie 数量、名称或值。

### 别名管理接口

#### 列出所有别名

```bash
GET /api/aliases?account_id=acc_1

# 响应
{
  "success": true,
  "data": {
    "account_id": "acc_1",
    "count": 15,
    "aliases": [
      {
        "email": "xyz123@icloud.com",
        "label": "注册某网站",
        "created_at": "2024-01-15T10:30:00Z"
      }
    ]
  }
}
```

#### 停用别名

```bash
POST /api/aliases/:id/deactivate

# 请求体
{
  "account_id": "acc_1"
}

# 响应
{
  "success": true,
  "data": {
    "anonymous_id": "abc123",
    "success": true
  }
}
```

#### 激活别名

```bash
POST /api/aliases/:id/reactivate

# 请求体
{
  "account_id": "acc_1"
}

# 响应
{
  "success": true,
  "data": {
    "anonymous_id": "abc123",
    "success": true
  }
}
```

#### 删除别名

```bash
DELETE /api/aliases/:id

# 请求体
{
  "account_id": "acc_1"
}

# 响应
{
  "success": true,
  "data": {
    "anonymous_id": "abc123"
  }
}
```

## 认证方式

### 方式一: Cookie 认证 (推荐,功能最完整)

Cookie 认证可实现所有功能:创建别名、读取邮件、管理别名。

**适用范围:**

- 创建/停用/激活/删除 HME 别名 ✅
- 读取邮件 (通过 iCloud Web API,无需 App Password) ✅

**获取 Cookie:**

1. 使用浏览器登录 [icloud.com](https://www.icloud.com) 或 [icloud.com.cn](https://www.icloud.com.cn) (国区)
2. 打开浏览器开发者工具 (F12)
3. 进入 Application → Cookies
4. 导出全部 Cookie 为 `{"key":"value"}` 格式的 JSON

**关键 Cookie (必需):**

- `X-APPLE-WEBAUTH-TOKEN` — 认证 token
- `X-APPLE-WEBAUTH-USER` — 含 dsid (`v=1:s=1:d=22789132008`)
- `X-APPLE-WEBAUTH-HSA-TRUST` — 设备信任 token
- `X-APPLE-DS-WEB-SESSION-TOKEN` — 会话 token

**注意:** 导出的 Cookie 值不要包含多余的引号或转义字符。

### 方式二: App Password 认证 (IMAP,优先读邮件)

App Password 用于 IMAP 读取邮件,是邮件读取的优先路径 (支持服务端按收件人搜索)。

**生成 App Password:**

1. 登录 [appleid.apple.com](https://appleid.apple.com)
2. 进入 "登录和安全" → "App 专用密码"
3. 生成新密码,用于此工具

### 邮件读取双路径

`GET /api/inbox` 自动选择读取方式:

1. **优先: IMAP (App Password)** — 设置了 App Password 时使用,支持服务端按收件人 (`TO`) 搜索
2. **回退: Web API (Cookie)** — 无 App Password 或 IMAP 失败时,通过动态 `mccgateway` 端点读取；仅按明确收件人精确过滤，收件人缺失时显式失败

响应中包含 `"method": "web_api"` 或 `"method": "imap"` 字段,标识实际使用的读取方式。

## 项目架构

```
icloud-hme/
├── main.go                 # 入口: 加载配置、初始化管理器、启动服务
├── accounts.json           # 账号配置文件 (自动生成)
├── go.mod
└── internal/
    ├── account/
    │   └── manager.go      # 多账号管理器 (持久化、客户端工厂)
    ├── hme/
    │   ├── client.go       # iCloud HME Web 客户端 (Cookie 认证)
    │   └── auth.go         # SRP 登录 (账号密码 + 2FA 获取 Cookie)
    ├── mail/
    │   ├── client.go       # IMAP 邮件客户端 (App Password 认证)
    │   └── web_client.go   # Web 邮件客户端 (Cookie 认证,无需 App Password)
    └── server/
        └── server.go       # HTTP API (Gin 路由 + 请求处理)
```

### 核心模块

- **account.Manager**: 管理多个 iCloud 账号,负责配置持久化和客户端创建
- **hme.Client**: 封装 iCloud HME Web API,支持 Cookie 认证
- **hme.auth**: SRP 协议登录,支持账号密码 + 可选 2FA
- **mail.Client**: IMAP 邮件客户端 (App Password,优先读邮件)
- **mail.WebClient**: 通过 iCloud Web API (mccgateway) 读取邮件,无需 App Password
- **server.Server**: HTTP API 服务,提供 RESTful 接口

## 技术栈

- **Go 1.26+**
- **Gin** — HTTP 框架
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

**A:** 支持，在 `accounts.json` 中配置多个账号即可，每个账号有独立的 `id`。

## 开发指南

### 本地开发

```bash
# 安装依赖
go mod download

# 运行 (开发模式，默认 127.0.0.1:8081，带 Gin 请求日志)
go run main.go -debug

# 构建包含嵌入前端的 Linux amd64 发布二进制
VERSION=v0.2.0 ./build.sh

# Windows PowerShell 构建同一 Linux amd64 发布二进制
./build.ps1 -Version v0.2.0

# Windows 原生发布烟测（使用临时数据目录，验证嵌入资源、鉴权、缓存、安全头和重启持久化）
./scripts/windows-release-smoke.ps1 -Version v0.2.0

# 仅编译 Go 服务（未构建前端时会显示最小占位页）
go build -ldflags="-X main.version=v0.2.0" -o icloud-hme.exe .
```

### 前端基础工程

```bash
cd web
npm ci

# 启动 Vite 开发服务器
npm run dev

# 类型检查并构建静态产物
npm run build

# 代码质量和测试
npm run format:check
npm run lint
npm run test
npx playwright install chromium  # 首次运行 Playwright 时执行
npm run test:e2e
```

开发服务器默认地址为 `http://127.0.0.1:5173`，`/api` 请求会代理到 Go 服务的 `http://127.0.0.1:8081`。如 Go 服务使用其他地址，可设置 `VITE_API_PROXY_TARGET` 覆盖代理目标；Mock 模式使用 `npm run dev:mock`，由 MSW 在浏览器内提供接口响应。

`./build.sh`、`./build.ps1` 和 Docker 构建都会先执行前端生产构建，再将 `web/dist` 嵌入 Go 二进制；发布构建无需在运行环境保留 Node 或前端文件。Windows 可运行 `./scripts/windows-release-smoke.ps1 -SkipNpmCi`，以临时原生二进制验证嵌入资源、回环鉴权、SPA 回退、缓存与安全头及重启持久化；该检查不替代 Linux CI、Docker 或受控真实 iCloud 验收。Docker 和真实 iCloud 账户的发布验收步骤见 [RELEASE_SMOKE_CHECKLIST.md](RELEASE_SMOKE_CHECKLIST.md)。

### 发布

推送 `v*` tag 到 GitHub 自动触发 CI：

```bash
git tag v0.2.0 && git push origin --tags
```

Actions 会自动构建多平台二进制、Docker 镜像（`ghcr.io/xiaozhou26/icloud-hme`）并创建 Release。

Pull Request 和 `main` 分支推送会执行 Go 竞态测试、前端回归、Bash 发布构建和 Docker 挂载数据目录烟测；只有这些检查通过后，`v*` tag 才会发布二进制和 GHCR 镜像。

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

A local management tool for Apple iCloud Hide My Email (HME) aliases, supporting creation, listing, and email reading through reverse-engineered iCloud Web API and IMAP protocol.

### Features

- Create HME aliases automatically
- List all aliases for an account
- Read emails sent to HME aliases via IMAP or the iCloud Web API
- Manage multiple iCloud accounts
- Dual authentication: Cookie and App Password

### Quick Start

#### Option 1: Binary (GitHub Releases)

Download the latest binary from [GitHub Releases](https://github.com/xiaozhou26/icloud-hme/releases):

| Platform            | File                           |
| ------------------- | ------------------------------ |
| Linux x86_64        | `icloud-hme_linux_amd64`       |
| Linux ARM64         | `icloud-hme_linux_arm64`       |
| macOS Intel         | `icloud-hme_darwin_amd64`      |
| macOS Apple Silicon | `icloud-hme_darwin_arm64`      |
| Windows x86_64      | `icloud-hme_windows_amd64.exe` |

```bash
# Linux example
chmod +x icloud-hme_linux_amd64
./icloud-hme_linux_amd64
```

#### Option 2: Docker

```bash
docker pull ghcr.io/xiaozhou26/icloud-hme:latest

docker run -d \
  --name icloud-hme \
  -p 8081:8081 \
  -e ICLOUD_HME_API_TOKEN=replace-with-a-random-token-at-least-32-chars \
  -v /path/to/data:/app/data \
  ghcr.io/xiaozhou26/icloud-hme:latest \
  -addr 0.0.0.0:8081
```

#### Option 3: Build from source

```bash
git clone https://github.com/xiaozhou26/icloud-hme.git
cd icloud-hme
VERSION=v0.2.0 ./build.sh
./build/icloud-hme -debug     # enable request logging

# Windows PowerShell builds the same Linux amd64 release binary
./build.ps1 -Version v0.2.0

# Windows-native release smoke with temporary data
./scripts/windows-release-smoke.ps1 -Version v0.2.0
```

For frontend scaffolding:

```bash
cd web
npm ci
npm run dev
npm run build
npm run format:check
npm run lint
npm run test
npx playwright install chromium  # first Playwright run only
npm run test:e2e
```

The Vite development server listens on `http://127.0.0.1:5173` by default, and proxies `/api` requests to the Go service at `http://127.0.0.1:8081`. Set `VITE_API_PROXY_TARGET` when the Go service uses another address. Mock mode is available through `npm run dev:mock` and serves API responses from MSW in the browser.

`./build.sh`, `./build.ps1`, and the Docker build all create the frontend production build first and embed `web/dist` into the Go binary. The release image and binary therefore do not need Node or frontend files at runtime. On Windows, run `./scripts/windows-release-smoke.ps1 -SkipNpmCi` to verify a temporary native binary's embedded assets, loopback authentication, SPA fallback, cache and security headers, and restart persistence. This does not replace Linux CI, Docker, or controlled iCloud-account validation. See [RELEASE_SMOKE_CHECKLIST.md](RELEASE_SMOKE_CHECKLIST.md) for the remaining release checks.

Pull requests and pushes to `main` run Go race tests, frontend regression tests, the Bash release build, and the Docker mounted-data smoke test. A `v*` tag publishes binaries and the GHCR image only after those checks pass.

Create `data/accounts.json` using the canonical array schema in `accounts.json.template`, then start the server. Cookie values must be an object keyed by cookie name, and `app_password` is a single string. Legacy account-map files are accepted and rewritten to the canonical array format on the next save. The file contains secrets and must not be committed.

API request bodies are limited to 1 MiB. Inbox `limit` accepts 1 through 100, `days` accepts 1 through 365, alias labels accept up to 200 Unicode characters, and each account accepts up to 128 cookies.

IMAP inbox queries apply `days` during the server-side search and return messages newest first. Preview fetches use `BODY.PEEK`, read at most the first 64 KiB of each raw message, and return at most 4 KiB of valid UTF-8 without marking messages as read.

Web inbox queries attach account cookies to the validated dynamic `mccgateway`, return UTC RFC3339 timestamps newest first, and cap previews at 4 KiB. Alias filtering uses only explicit recipient addresses; if the upstream response omits them, the request fails explicitly instead of guessing from the subject or sender.

Configuration updates use a synced temporary file and atomic replacement. A persistence failure rolls back the related in-memory change and returns HTTP 500 without replacing the existing configuration with partial content.

The server listens on `127.0.0.1:8081` by default. Non-loopback listeners require an `ICLOUD_HME_API_TOKEN` of at least 32 characters, and API requests must send it as a Bearer token.

In the embedded Web UI, use the key button in the top bar to enter the API token. The token stays only in current-page memory: it is never written to the URL, browser storage, or query cache, and must be entered again after a refresh or page close. API token rejection is identified by `code: "api_token_invalid"`, separately from an expired iCloud Cookie session.

`GET /api/health` returns the service name, build version, `ok`/`degraded` status, and configuration availability without exposing paths, accounts, credentials, or internal errors. It is protected by the same Bearer token policy as every other `/api` route.
