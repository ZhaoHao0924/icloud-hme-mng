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

从 [GitHub Releases](https://github.com/ZhaoHao0924/icloud-hme-mng/releases) 下载对应平台的二进制文件：

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
docker pull ghcr.io/zhaohao0924/icloud-hme-mng:latest

# 容器需要监听非回环地址，因此必须同时设置 API Token
docker run -d \
  --name icloud-hme \
  --restart unless-stopped \
  -p 127.0.0.1:8081:8081 \
  -e ICLOUD_HME_API_TOKEN=replace-with-a-random-token-at-least-32-chars \
  -v /path/to/data:/app/data \
  ghcr.io/zhaohao0924/icloud-hme-mng:latest \
  -addr 0.0.0.0:8081
```

推荐使用仓库中的 `compose.yaml` 管理持久化数据、健康检查和自动重启。部署与离线备份/恢复步骤见 [DEPLOYMENT.md](DEPLOYMENT.md)。

镜像支持 `linux/amd64` 和 `linux/arm64` 双架构，自动适配。

每次 `main` 分支推送在验证和 Docker 冒烟测试通过后，GitHub Actions 会自动构建并发布 `:main` 以及对应提交的 `:sha-<commit>` 多架构镜像，可直接用于测试或快速部署。生产环境建议使用 `v*` 版本标签；版本标签会发布固定版本镜像并在稳定版本时更新 `:latest`。

#### 方式三：源码编译

```bash
# 前置要求: Go 1.26+、Node.js 22.12.0+、npm
git clone https://github.com/ZhaoHao0924/icloud-hme-mng.git
cd icloud-hme-mng

# 编译并注入健康检查返回的版本（当前操作系统）
go build -ldflags="-X main.version=v0.2.0" -o icloud-hme .

# 调试模式（启用 Gin 请求日志）
./icloud-hme -debug
```

`go build` 只编译当前操作系统的 Go 服务；如果没有先构建并嵌入 `web/dist`，Web UI 可能显示最小占位页。生产发布请使用 `build.sh`、`build.ps1` 或 Docker 构建，它们会先构建前端并嵌入完整 Web UI。

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

服务默认监听 `127.0.0.1:8081`。非回环地址（例如 `0.0.0.0`、`:8081`）必须设置环境变量 `ICLOUD_HME_API_TOKEN`，否则服务会拒绝启动。设置令牌后，脚本和自动化可继续使用：

```http
Authorization: Bearer <ICLOUD_HME_API_TOKEN>
```

内置 Web UI 首次访问会要求创建管理员账户，此后必须登录才能进入平台或执行操作。没有默认密码；管理员密码仅以 bcrypt 哈希保存在数据目录的 `platform-auth.json` 中。登录会话是 12 小时的 HttpOnly、SameSite=Strict Cookie，只保存在服务端内存，因此服务重启、会话到期或主动退出后都需要重新登录。

远程部署时，首次创建管理员账户需在登录页同时输入 API Token；之后浏览器使用平台登录会话即可。API Token 仍适用于脚本、自动化以及故障恢复，内置 Web UI 也可通过顶栏钥匙按钮临时输入它。令牌只保留在当前页面内存中，不会写入 URL、浏览器存储或查询缓存；刷新或关闭页面后需要重新输入。服务端会用 `code: "api_token_invalid"` 标记 API Token 拒绝，以区别于 iCloud Cookie 会话失效。

所有 `/api` 请求体最大 1 MiB。收件箱参数 `limit` 范围为 `1..100`，`days` 范围为 `1..365`；创建别名的 `label` 最多 200 个 Unicode 字符。请求体超限返回 `413`，其他边界错误返回 `400`。

IMAP 收件箱在服务器搜索阶段应用 `days` 并按邮件时间倒序返回。摘要使用 `BODY.PEEK`，每封邮件最多拉取前 64 KiB 原始内容，返回的 `preview` 最多 4 KiB UTF-8 数据且不会把邮件标记为已读。

Web API 收件箱使用 validate 返回的动态 `mccgateway` 并向该网关附加 Cookie；消息按时间倒序，日期为 UTC RFC3339，`preview` 最多 4 KiB。带 `alias` 时仅精确匹配响应中明确的收件人地址；收件人不可验证时返回错误，不使用主题或发件人猜测。

## 部署指南

本节给出 Linux、macOS、Windows、Docker 和 Docker Compose 的生产部署方式。运行时只需要发布二进制或容器镜像；只有从源码构建时才需要 Go、Node.js 和 npm。无论选择哪种方式，都必须把 `data` 指向持久化目录。该目录保存账号 Cookie、App Password、平台管理员认证、邮件/Webhook 通知配置、自动化进度、创建历史和操作日志，不能使用临时目录，也不要把它提交到 Git 或暴露在静态文件目录下。

### 0. 通用准备

1. 从 [GitHub Releases](https://github.com/ZhaoHao0924/icloud-hme-mng/releases) 下载与你的 CPU 架构匹配的文件，或使用 [GHCR 镜像](https://github.com/ZhaoHao0924/icloud-hme-mng/pkgs/container/icloud-hme-mng)。当前发布文件为：Linux `amd64`/`arm64`、macOS Intel `amd64`/Apple Silicon `arm64`、Windows `amd64`。
2. 规划一个不会被清理的绝对数据路径。若之前已经配置过账号，启动时必须继续使用原来的数据目录，例如 `-data /var/lib/icloud-hme` 或 `-data C:\ProgramData\icloud-hme\data`；不要因为换了启动方式就重新使用空的 `./data`。
3. 服务默认监听 `127.0.0.1:8081`。原生二进制只有在监听非回环地址时必须设置至少 32 个字符的 `ICLOUD_HME_API_TOKEN`；Docker 容器内部监听 `0.0.0.0:8081`，因此 Docker 和 Compose 始终要配置该 Token。生产环境优先使用回环监听加 HTTPS 反向代理。
4. 首次启动打开 Web UI，创建管理员账号并登录。项目没有默认管理员密码；平台登录会话有效期为 12 小时，服务重启、会话过期或退出后需要重新登录。远程首次创建管理员账号时，还要在登录页输入 API Token。

生成 Token 的示例：

```
# Linux/macOS
export ICLOUD_HME_API_TOKEN="$(openssl rand -hex 32)"
```

### 1. Linux 原生二进制

适用于 Debian、Ubuntu、Rocky Linux、Alpine（使用对应的 libc/系统环境验证）等 Linux 主机。发布二进制是静态构建的，通常不需要安装 Go 或 Node.js。

#### 直接运行

```
# 以 amd64 为例；ARM64 主机请下载 icloud-hme_linux_arm64
install -d -m 700 /opt/icloud-hme /var/lib/icloud-hme
install -m 0755 ./icloud-hme_linux_amd64 /opt/icloud-hme/icloud-hme

# 如果已有账号数据，把原数据目录填到 -data，不要覆盖或重新初始化它
export ICLOUD_HME_API_TOKEN="$(openssl rand -hex 32)"
/opt/icloud-hme/icloud-hme -addr 127.0.0.1:8081 -data /var/lib/icloud-hme
```

访问 `http://127.0.0.1:8081` 完成首次管理员设置。需要从其他机器访问时，建议保留回环监听并配置下方的反向代理；如果确实要直接监听网卡，请改为 `-addr 0.0.0.0:8081`，同时保留 Token 并限制防火墙来源。

#### 使用 systemd 托管（推荐）

先创建专用服务用户和权限受限的数据目录。已有数据目录请先停掉旧服务，再把它迁移或设置为该用户可读写的目录；不要用空目录替代原目录。

```
sudo useradd --system --home /var/lib/icloud-hme --shell /usr/sbin/nologin icloud-hme
sudo install -d -o icloud-hme -g icloud-hme -m 700 /var/lib/icloud-hme
sudo install -d -o root -g root -m 755 /opt/icloud-hme
sudo install -m 0755 ./icloud-hme_linux_amd64 /opt/icloud-hme/icloud-hme
sudo install -m 0600 /dev/null /etc/icloud-hme.env
sudoedit /etc/icloud-hme.env
```

```
ICLOUD_HME_API_TOKEN=replace-with-a-random-token-at-least-32-chars
TZ=Asia/Shanghai
```

创建 `/etc/systemd/system/icloud-hme.service`：

```
[Unit]
Description=iCloud Hide My Email management service
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=icloud-hme
Group=icloud-hme
WorkingDirectory=/opt/icloud-hme
EnvironmentFile=/etc/icloud-hme.env
ExecStart=/opt/icloud-hme/icloud-hme -addr 127.0.0.1:8081 -data /var/lib/icloud-hme
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/icloud-hme

[Install]
WantedBy=multi-user.target
```

启用、查看状态和日志：

```
sudo systemctl daemon-reload
sudo systemctl enable --now icloud-hme
sudo systemctl status icloud-hme --no-pager
sudo journalctl -u icloud-hme -n 200 --no-pager
sudo journalctl -u icloud-hme -f
```

升级时先在 Web UI 暂停自动化规则，停止服务并完成备份，再只替换 `/opt/icloud-hme/icloud-hme`，最后执行 `sudo systemctl start icloud-hme`。确认服务健康、账号列表和自动化状态后，再保留旧二进制作为短期回滚副本。

### 2. macOS 原生二进制

Intel Mac 使用 `icloud-hme_darwin_amd64`，Apple Silicon（M1/M2/M3/M4）使用 `icloud-hme_darwin_arm64`。运行环境不需要 Go 或 Node.js。

#### 前台运行

```
mkdir -p "$HOME/Applications/icloud-hme" "$HOME/Library/Application Support/icloud-hme/data"
cp ./icloud-hme_darwin_arm64 "$HOME/Applications/icloud-hme/icloud-hme"
chmod 755 "$HOME/Applications/icloud-hme/icloud-hme"
chmod 700 "$HOME/Library/Application Support/icloud-hme/data"

# 从浏览器下载的文件可能带有 Gatekeeper 隔离属性，只移除该文件的属性
xattr -d com.apple.quarantine "$HOME/Applications/icloud-hme/icloud-hme" 2>/dev/null || true

"$HOME/Applications/icloud-hme/icloud-hme" -addr 127.0.0.1:8081 -data "$HOME/Library/Application Support/icloud-hme/data"
```

如果已有数据，请把 `-data` 改成原来的绝对路径。不要全局关闭 Gatekeeper；如果系统仍然阻止运行，应在“系统设置 > 隐私与安全性”中确认这一次打开操作。要允许脚本通过 Bearer Token 访问，可以在当前终端临时设置 `ICLOUD_HME_API_TOKEN`；监听回环地址时不强制要求它。

#### 使用 launchd 常驻

需要登录用户启动时，可创建 `~/Library/LaunchAgents/com.icloud-hme.plist`。将下面的 `/Users/your-user` 和二进制名称替换为实际值；LaunchAgent 中使用绝对路径，不能依赖 `$HOME` 展开：

```
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.icloud-hme</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/your-user/Applications/icloud-hme/icloud-hme</string>
    <string>-addr</string>
    <string>127.0.0.1:8081</string>
    <string>-data</string>
    <string>/Users/your-user/Library/Application Support/icloud-hme/data</string>
  </array>
  <key>WorkingDirectory</key>
  <string>/Users/your-user/Applications/icloud-hme</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/Users/your-user/Library/Logs/icloud-hme.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/your-user/Library/Logs/icloud-hme.error.log</string>
</dict>
</plist>
```

```
mkdir -p "$HOME/Library/Logs"
chmod 600 "$HOME/Library/LaunchAgents/com.icloud-hme.plist"
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.icloud-hme.plist"
launchctl kickstart -k "gui/$(id -u)/com.icloud-hme"
launchctl print "gui/$(id -u)/com.icloud-hme"
```

如果在 plist 中添加 `ICLOUD_HME_API_TOKEN`，该文件就是敏感配置，应保持 `0600`，不要提交到仓库。升级时先执行 `launchctl bootout "gui/$(id -u)/com.icloud-hme"`，备份并替换二进制，再重新 `bootstrap`。

### 3. Windows 原生二进制

Windows 使用 `icloud-hme_windows_amd64.exe`。建议把程序放在 `C:\Program Files\icloud-hme`，把数据放在 `C:\ProgramData\icloud-hme\data`，这样升级程序时不会误动账号数据。

#### PowerShell 前台运行

首次安装到 `Program Files` 和 `ProgramData` 时请以管理员身份打开 PowerShell；如果不使用这两个目录，也可以选择当前用户有写权限的绝对路径。

```
$root = Join-Path $env:ProgramFiles "icloud-hme"
$dataDir = Join-Path $env:ProgramData "icloud-hme\data"
New-Item -ItemType Directory -Force -Path $root, $dataDir | Out-Null
Copy-Item .\icloud-hme_windows_amd64.exe (Join-Path $root "icloud-hme.exe")
Unblock-File (Join-Path $root "icloud-hme.exe")

# 仅对当前 PowerShell 进程设置 Token；不要把明文 Token 写进提交到仓库的脚本
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
$env:ICLOUD_HME_API_TOKEN = ([BitConverter]::ToString($bytes) -replace '-', '').ToLowerInvariant()

& (Join-Path $root "icloud-hme.exe") -addr 127.0.0.1:8081 -data $dataDir
```

如果之前已有账号，请把 `$dataDir` 改成旧数据目录，并确认该目录包含原来的 `accounts.json`。当前终端关闭后，以上环境变量不会自动保留。监听非回环地址时使用 `-addr 0.0.0.0:8081`，但必须设置 Token，并在 Windows 防火墙中仅允许可信网段。

例如只允许局域网 `192.168.1.0/24` 访问直连端口（管理员 PowerShell）：

```
New-NetFirewallRule -DisplayName "iCloud HME 8081 from LAN" -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8081 -RemoteAddress 192.168.1.0/24
```

#### 使用任务计划程序常驻

推荐任务计划程序以 `SYSTEM` 或专用服务账户运行，并让服务保持回环监听；远程访问交给 HTTPS 反向代理。以下命令需要管理员 PowerShell，任务创建前应把程序和数据目录的 ACL 授予实际运行账户：

```
$root = Join-Path $env:ProgramFiles "icloud-hme"
$dataDir = Join-Path $env:ProgramData "icloud-hme\data"
icacls (Join-Path $env:ProgramData "icloud-hme") /grant:r "SYSTEM:(OI)(CI)F" /T
$action = New-ScheduledTaskAction `
  -Execute (Join-Path $root "icloud-hme.exe") `
  -Argument "-addr 127.0.0.1:8081 -data `"$dataDir`"" `
  -WorkingDirectory $root
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName "icloud-hme" -Action $action -Trigger $trigger -Principal $principal -Force
Start-ScheduledTask -TaskName "icloud-hme"
Get-ScheduledTask -TaskName "icloud-hme"
```

该示例使用回环监听，因此不依赖 Token。若必须直接对外监听，使用受保护的服务账户环境变量或 WinSW/NSSM 等服务包装器注入 Token，不要把 Token 写入公开脚本。升级前执行 `Stop-ScheduledTask -TaskName "icloud-hme"`，完成备份后替换 exe，再执行 `Start-ScheduledTask`。

### 4. Docker 镜像

发布镜像为 `ghcr.io/zhaohao0924/icloud-hme-mng`，支持 `linux/amd64` 和 `linux/arm64`。生产环境建议使用具体版本标签（例如 `v0.2.0`），确认无误后再考虑 `latest`。容器内服务必须监听 `0.0.0.0:8081`，所以必须提供至少 32 个字符的 Token。

#### Linux/macOS shell

```
docker pull ghcr.io/zhaohao0924/icloud-hme-mng:latest
mkdir -p ./data
chmod 700 ./data
export ICLOUD_HME_API_TOKEN="$(openssl rand -hex 32)"
docker run -d --name icloud-hme --restart unless-stopped --init -p 127.0.0.1:8081:8081 -e "ICLOUD_HME_API_TOKEN=$ICLOUD_HME_API_TOKEN" -e TZ=Asia/Shanghai -v "$(pwd)/data:/app/data" ghcr.io/zhaohao0924/icloud-hme-mng:latest -addr 0.0.0.0:8081 -data /app/data
```

上述端口映射只允许宿主机访问；反向代理也应连接 `127.0.0.1:8081`。查看状态和日志：

```
docker ps --filter name=icloud-hme
docker logs --tail 200 icloud-hme
curl -fsS -H "Authorization: Bearer $ICLOUD_HME_API_TOKEN" http://127.0.0.1:8081/api/health
```

#### Windows PowerShell

```
$dataDir = (New-Item -ItemType Directory -Force .\data).FullName
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
$token = ([BitConverter]::ToString($bytes) -replace '-', '').ToLowerInvariant()
docker pull ghcr.io/zhaohao0924/icloud-hme-mng:latest
docker run -d --name icloud-hme --restart unless-stopped --init -p 127.0.0.1:8081:8081 --env "ICLOUD_HME_API_TOKEN=$token" --env "TZ=Asia/Shanghai" --mount "type=bind,source=$dataDir,target=/app/data" ghcr.io/zhaohao0924/icloud-hme-mng:latest -addr 0.0.0.0:8081 -data /app/data
```

Docker 容器删除或重建不会删除 bind mount 中的宿主机数据；但不要使用会清理卷或数据目录的命令，升级前仍应停容器并备份。

### 5. Docker Compose

仓库中的 `compose.yaml` 当前是“从源码 checkout 构建镜像”的方案，不是直接拉取 GHCR 镜像。它已经配置了数据 bind mount、健康检查、自动重启、`init` 和 `no-new-privileges`。

Linux/macOS：

```
git clone https://github.com/ZhaoHao0924/icloud-hme-mng.git
cd icloud-hme-mng
cp .env.example .env
mkdir -p /srv/icloud-hme/data
chmod 700 /srv/icloud-hme/data
vi .env
```

至少设置以下值，并把 `ICLOUD_HME_DATA_DIR` 改为实际持久化路径：

```
ICLOUD_HME_API_TOKEN=replace-with-a-random-token-at-least-32-chars
ICLOUD_HME_BIND_ADDRESS=127.0.0.1
ICLOUD_HME_PORT=8081
ICLOUD_HME_DATA_DIR=/srv/icloud-hme/data
ICLOUD_HME_IMAGE=icloud-hme:local
ICLOUD_HME_VERSION=dev
TZ=Asia/Shanghai
```

启动和日常操作：

```
docker compose up -d --build
docker compose ps
docker compose logs --tail 200 app
docker compose restart app
docker compose stop app
docker compose start app
docker compose down
```

Compose 会把容器内的 `0.0.0.0:8081` 映射到宿主机的 `ICLOUD_HME_BIND_ADDRESS`；默认是 `127.0.0.1:8081`。健康检查访问 `/api/auth/session`，不显示账号凭据。首次启动后打开 `http://127.0.0.1:8081`。Windows PowerShell 使用 `Copy-Item .env.example .env`，并建议在 `.env` 中使用 Docker 可识别的路径，例如 `D:/Services/icloud-hme/data`。`docker compose down` 只移除容器，不会移除 bind mount 数据目录。

如果不需要本地源码构建，可使用上面的 `docker run` 直接部署 GHCR 版本；Compose 文件本身仍会执行本地 Dockerfile 构建，更新源码后用 `docker compose up -d --build` 重建。

### 6. 反向代理与 HTTPS

不要把 Go 服务的明文 HTTP 端口直接暴露到公网。原生部署将服务绑定在 `127.0.0.1:8081`；Compose 将 `ICLOUD_HME_BIND_ADDRESS` 保持为 `127.0.0.1`；再由 Caddy、Nginx 或现有网关负责 TLS、域名和外部访问控制。公开访问必须使用 HTTPS，尤其是管理员登录和通知配置页面。

Caddy 示例（DNS 已指向此主机，Caddy 负责自动申请证书）：

```
hme.example.com {
    reverse_proxy 127.0.0.1:8081
}
```

Nginx 示例（证书路径按实际环境替换）：

```
server {
    listen 443 ssl http2;
    server_name hme.example.com;
    ssl_certificate /etc/letsencrypt/live/hme.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/hme.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

反向代理不替代平台管理员登录。代理后首次打开域名仍需创建管理员并登录；脚本或自动化使用 API 时继续发送 `Authorization: Bearer <ICLOUD_HME_API_TOKEN>`。若直接绑定非回环地址，必须同时配置 Token、主机防火墙和可信来源限制。

### 7. 首次验证与已有账号数据

1. 确认进程参数中的 `-data` 或 Compose 的 `ICLOUD_HME_DATA_DIR` 指向预期目录，并确认其中已有 `accounts.json`。这一步决定服务加载哪一批账号数据。
2. 浏览器访问本机地址或反向代理域名，首次创建管理员账号，然后登录平台。
3. 逐个检查账号、别名列表、收件箱和自动化规则。仅查看和“自动化预览”不会创建或删除别名。
4. 用 Bearer Token 检查服务健康状态：

```
curl -fsS -H "Authorization: Bearer $ICLOUD_HME_API_TOKEN" http://127.0.0.1:8081/api/health
```

浏览器会话可通过 `GET /api/auth/session` 检查登录状态；该接口也用于 Compose 健康检查。健康检查不会返回 Cookie、App Password、账号详情或数据路径。

### 8. 安全、备份与升级

- Linux/macOS 数据目录建议目录权限 `0700`、敏感文件权限 `0600`；Windows 使用专用服务账户和 ACL；Docker bind mount 的宿主机目录也要限制访问。
- `accounts.json` 可能包含 iCloud Cookie、App Password 和代理凭据；`platform-auth.json` 保存管理员密码哈希；邮件通知配置可能包含 163 邮箱授权码；Webhook 配置包含签名密钥。不要把这些文件、`.env`、launchd plist 或日志上传到公共位置。
- 备份前先在 Web UI 暂停自动化规则，再停止服务或容器，确保 `accounts.json`、平台认证和日志处于同一时间点。详细的带校验清单、恢复前确认和回滚目录流程见 [DEPLOYMENT.md](DEPLOYMENT.md)。
- Linux/macOS 可在停止服务后使用受限备份目录执行 `tar`；Windows 可使用仓库中的 `scripts/backup-data.ps1` 和 `scripts/verify-data-backup.ps1`。恢复前必须先保留旧数据目录，验证恢复内容后再删除回滚副本。
- 升级只替换二进制、容器镜像或源码构建产物，始终复用原数据目录。升级后检查健康状态、账号和自动化配置；出现问题时停止新版本并回滚二进制/镜像，不能用空目录启动来“修复”配置。
- `DELETE /api/aliases/:id`、停用别名和删除账号会改变真实 iCloud 数据。不要用这些操作做部署验收，也不要执行 `docker compose down -v`、`rm -rf` 或清空数据目录来排查问题；先备份并使用只读检查或自动化预览。

完整的 Compose 备份、恢复和离线烟测命令集中在 [DEPLOYMENT.md](DEPLOYMENT.md)。

## API 接口

### 系统接口

#### 健康检查

```http
GET /api/health
```

响应包含服务名、构建版本、`ok`/`degraded` 状态和配置可用性，不返回配置路径、账户或凭据。配置不可用时仍返回 HTTP `200`；该接口需要有效的平台登录会话或 Bearer Token。完整契约见 [API.md](API.md#健康检查)。

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

### 别名自动化

账户详情页新增“自动化”标签页，可完成以下操作：

- 一次批量创建 `1..20` 个别名
- 按固定间隔定时创建指定数量
- 当活跃别名低于库存阈值时自动补充到目标数量
- 设置累计创建目标；达到目标后自动停用，最后一轮只创建剩余数量
- 设置每日自动创建上限；达到额度后保留规则并在次日继续
- 限制定时调度的执行日和时间窗，并在不允许的时段自动延后到下一个允许时间
- 设置总别名安全上限和连续失败上限；达到安全上限或连续失败阈值会自动暂停
- 显式暂停或恢复规则，查看累计/每日创建进度、最近一次执行结果、下次执行时间、连续失败计数、暂停原因与错误摘要，并可预览或立即执行已保存的规则
- 查看可追溯的创建批次历史，并导出 CSV

规则按账户保存在 `accounts.json` 的 `alias_automation` 字段中。服务运行时每分钟检查到期规则，服务重启后会继续使用已保存的执行状态。执行日和时间窗按部署服务器时区解释，且只约束定时调度；立即执行仍受总量、每日额度和暂停状态保护。单账户的手动创建、批量创建和自动化创建会串行执行，避免并发操作覆盖刷新后的 Cookie。连续失败按指数退避安排下次尝试，最长 7 天。手动暂停不会改变创建进度；恢复会重新启用规则并清零连续失败计数。修改累计创建目标会开始新的累计周期，界面会要求确认。每日自动创建上限只作用于自动化运行（含“立即执行规则”），到达后将在服务所在时区的次日零点之后的下一个允许时间继续，不会永久停用规则。预览执行只读取别名清单，不会创建别名、保存 Cookie 或写入规则和历史。每次定时运行会写入系统设置中的操作日志，日志仅保存最近 7 天，且不含账户、别名或凭据数据。

批量接口为 `POST /api/accounts/:id/aliases/batch`，规则接口为 `GET`/`PUT /api/accounts/:id/alias-automation`，暂停/恢复接口为 `POST /api/accounts/:id/alias-automation/pause` 和 `POST /api/accounts/:id/alias-automation/resume`，预览接口为 `POST /api/accounts/:id/alias-automation/preview`，立即执行接口为 `POST /api/accounts/:id/alias-automation/run`。创建历史可通过 `GET /api/accounts/:id/alias-creation-history` 查询，并通过 `GET /api/accounts/:id/alias-creation-history.csv` 导出。完整字段和响应契约见 [API.md](API.md#13-批量创建别名)。

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

生产部署默认使用 [compose.yaml](compose.yaml)：它将数据目录绑定挂载到 `/app/data`、要求 API Token、默认仅绑定本机端口并启用容器健康检查与自动重启。备份必须在服务停止后执行，备份 ZIP 通过 SHA-256 清单验证；恢复会保留原数据目录作为回滚副本。完整命令见 [DEPLOYMENT.md](DEPLOYMENT.md)。

### 发布

推送 `v*` tag 到 GitHub 自动触发 CI：

```bash
git tag v0.2.0 && git push origin --tags
```

Actions 会自动构建多平台二进制、Docker 镜像（`ghcr.io/zhaohao0924/icloud-hme-mng`）并创建 Release。

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

- Create individual or batched HME aliases
- Schedule per-account alias creation and replenish active alias inventory by threshold
- List all aliases for an account
- Read emails sent to HME aliases via IMAP or the iCloud Web API
- Manage multiple iCloud accounts
- Dual authentication: Cookie and App Password

### Quick Start

#### Option 1: Binary (GitHub Releases)

Download the latest binary from [GitHub Releases](https://github.com/ZhaoHao0924/icloud-hme-mng/releases):

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
docker pull ghcr.io/zhaohao0924/icloud-hme-mng:latest

docker run -d \
  --name icloud-hme \
  --restart unless-stopped \
  -p 127.0.0.1:8081:8081 \
  -e ICLOUD_HME_API_TOKEN=replace-with-a-random-token-at-least-32-chars \
  -v /path/to/data:/app/data \
  ghcr.io/zhaohao0924/icloud-hme-mng:latest \
  -addr 0.0.0.0:8081 -data /app/data
```

#### Option 3: Build from source

```bash
git clone https://github.com/ZhaoHao0924/icloud-hme-mng.git
cd icloud-hme-mng
VERSION=v0.2.0 ./build.sh
./build/icloud-hme -debug     # enable request logging

# Windows PowerShell builds the same Linux amd64 release binary
./build.ps1 -Version v0.2.0

# Windows-native release smoke with temporary data
./scripts/windows-release-smoke.ps1 -Version v0.2.0
```

For Compose deployment, persistent-data backup, archive verification, and rollback-aware restore instructions, see [DEPLOYMENT.md](DEPLOYMENT.md).

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

API request bodies are limited to 1 MiB. Inbox `limit` accepts 1 through 100, `days` accepts 1 through 365, alias labels accept up to 200 Unicode characters, batch creation accepts 1 through 20 aliases, and each account accepts up to 128 cookies.

The account Automation tab configures interval-based creation, threshold replenishment, cumulative targets, daily creation limits, permitted weekdays, and an execution window. Rules are persisted in `accounts.json`, checked each minute while the service is running, and retain their last/next run state across restarts. Weekdays and the execution window use the deployment server time zone and apply only to scheduled runs; a manual run still obeys the cumulative, daily, total-limit, and pause safeguards. The daily limit applies only to automation work, defers additional work to the next permitted time after local midnight, and does not permanently disable the rule. Rules can be paused and resumed explicitly without discarding progress; changing a cumulative target begins a new progress cycle. A read-only preview reports current capacity without creating aliases, saving cookies, or writing rule/history state. Alias operations for each account are serialized so concurrent manual and automated work cannot overwrite refreshed cookies. The tab also exposes account-scoped creation history with stable batch IDs and CSV export. See [API.md](API.md#13-批量创建别名) for the request and response contracts.

IMAP inbox queries apply `days` during the server-side search and return messages newest first. Preview fetches use `BODY.PEEK`, read at most the first 64 KiB of each raw message, and return at most 4 KiB of valid UTF-8 without marking messages as read.

Web inbox queries attach account cookies to the validated dynamic `mccgateway`, return UTC RFC3339 timestamps newest first, and cap previews at 4 KiB. Alias filtering uses only explicit recipient addresses; if the upstream response omits them, the request fails explicitly instead of guessing from the subject or sender.

Configuration updates use a synced temporary file and atomic replacement. A persistence failure rolls back the related in-memory change and returns HTTP 500 without replacing the existing configuration with partial content.

The server listens on `127.0.0.1:8081` by default. Non-loopback listeners require an `ICLOUD_HME_API_TOKEN` of at least 32 characters. The Bearer token remains available for scripts, automation, and the initial remote administrator setup.

Webhook notifications are configured separately in System Settings. The destination must use HTTPS. Payloads contain only redacted automation and session summaries and are signed with HMAC-SHA256; the signing secret is stored in `webhook-notification.json` with protected permissions and is never returned by the API. `GET`/`PUT /api/notifications/webhook` manage the redacted configuration, and `POST /api/notifications/webhook/test` sends a signed test event. Webhook delivery has its own bounded queue and up to three attempts, so endpoint failures cannot block alias work.

The embedded Web UI requires a local administrator setup on first use and a platform login afterwards. The password is stored only as a bcrypt hash in the data directory. Successful login establishes a 12-hour HttpOnly, SameSite=Strict server-side session; restart, expiry, and logout require a new login. Remote initial setup also needs the API token. Use the key button in the top bar only when a temporary Bearer token is needed. The token stays only in current-page memory: it is never written to the URL, browser storage, or query cache, and must be entered again after a refresh or page close. API token rejection is identified by `code: "api_token_invalid"`, separately from platform-login and expired iCloud Cookie errors.

`GET /api/health` returns the service name, build version, `ok`/`degraded` status, and configuration availability without exposing paths, accounts, credentials, or internal errors. It requires a valid platform session or Bearer token, like every business API route.

### Deployment Guide

This section covers native Linux, macOS, and Windows deployments, Docker, Docker Compose, reverse proxies, first-run authentication, and maintenance. A release binary or container image is enough at runtime; Go, Node.js, and npm are required only for source builds. Always use a persistent data directory because it contains iCloud Cookies, App Passwords, platform administrator authentication, notification settings, automation state, creation history, and operation logs.

### Common prerequisites

1. Download the artifact matching the host architecture from [GitHub Releases](https://github.com/ZhaoHao0924/icloud-hme-mng/releases), or use `ghcr.io/zhaohao0924/icloud-hme-mng`. Releases include Linux `amd64`/`arm64`, macOS Intel `amd64`/Apple Silicon `arm64`, and Windows `amd64`.
2. Choose a persistent absolute data path. If accounts were configured previously, keep using the same directory with `-data` or `ICLOUD_HME_DATA_DIR`; starting with a new empty `./data` directory makes the existing accounts appear to be missing.
3. Native binaries listen on `127.0.0.1:8081` by default. A native process must receive an `ICLOUD_HME_API_TOKEN` of at least 32 characters when it listens on a non-loopback address. Docker and Compose listen on `0.0.0.0:8081` inside the container, so they always need the token. Prefer loopback plus an HTTPS reverse proxy for external access.
4. On first launch, open the Web UI, create the local administrator, and sign in. There is no default administrator password. The server-side platform session lasts 12 hours and is lost on restart, expiry, or logout. Remote initial setup also requires the API token in the setup page.

Generate a token on Linux or macOS with:

```
export ICLOUD_HME_API_TOKEN="$(openssl rand -hex 32)"
```

Source builds require Go 1.26+ and Node.js 22.12.0+ with npm. `build.sh` and `build.ps1` produce a Linux amd64 binary; they do not produce the native Windows executable. For native Windows deployment, use the Windows release artifact or follow the frontend embedding steps used by the release workflow.

### Linux native binary

The release binary is statically built and normally needs no Go or Node.js installation.

#### Run in the foreground

```
# Use icloud-hme_linux_arm64 on an ARM64 host.
install -d -m 700 /opt/icloud-hme /var/lib/icloud-hme
install -m 0755 ./icloud-hme_linux_amd64 /opt/icloud-hme/icloud-hme

# Point -data at the existing data directory when accounts already exist.
export ICLOUD_HME_API_TOKEN="$(openssl rand -hex 32)"
/opt/icloud-hme/icloud-hme -addr 127.0.0.1:8081 -data /var/lib/icloud-hme
```

Open `http://127.0.0.1:8081` for the first administrator setup. For access from another machine, keep the process on loopback and use the reverse proxy example below. Directly binding `0.0.0.0:8081` requires the token and a firewall rule that limits trusted source addresses.

#### Run with systemd

Create a dedicated service account and grant it access only to the application and data directories. If the data directory already exists, stop the old process and adjust its ownership instead of creating an empty replacement.

```
sudo useradd --system --home /var/lib/icloud-hme --shell /usr/sbin/nologin icloud-hme
sudo install -d -o icloud-hme -g icloud-hme -m 700 /var/lib/icloud-hme
sudo install -d -o root -g root -m 755 /opt/icloud-hme
sudo install -m 0755 ./icloud-hme_linux_amd64 /opt/icloud-hme/icloud-hme
sudo install -m 0600 /dev/null /etc/icloud-hme.env
sudoedit /etc/icloud-hme.env
```

```
ICLOUD_HME_API_TOKEN=replace-with-a-random-token-at-least-32-chars
TZ=Asia/Shanghai
```

Create `/etc/systemd/system/icloud-hme.service`:

```
[Unit]
Description=iCloud Hide My Email management service
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=icloud-hme
Group=icloud-hme
WorkingDirectory=/opt/icloud-hme
EnvironmentFile=/etc/icloud-hme.env
ExecStart=/opt/icloud-hme/icloud-hme -addr 127.0.0.1:8081 -data /var/lib/icloud-hme
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/icloud-hme

[Install]
WantedBy=multi-user.target
```

Enable and inspect it:

```
sudo systemctl daemon-reload
sudo systemctl enable --now icloud-hme
sudo systemctl status icloud-hme --no-pager
sudo journalctl -u icloud-hme -n 200 --no-pager
sudo journalctl -u icloud-hme -f
```

For an upgrade, pause automation in the Web UI, stop the service, create and verify a backup, replace only `/opt/icloud-hme/icloud-hme`, and start the service again. Keep the old binary until the new version has passed health, account, and automation checks.

### macOS native binary

Use `icloud-hme_darwin_amd64` on Intel Macs and `icloud-hme_darwin_arm64` on Apple Silicon. No Go or Node.js installation is needed at runtime.

#### Run in the foreground

```
mkdir -p "$HOME/Applications/icloud-hme" "$HOME/Library/Application Support/icloud-hme/data"
cp ./icloud-hme_darwin_arm64 "$HOME/Applications/icloud-hme/icloud-hme"
chmod 755 "$HOME/Applications/icloud-hme/icloud-hme"
chmod 700 "$HOME/Library/Application Support/icloud-hme/data"

# Remove only this downloaded file's quarantine attribute if Gatekeeper added one.
xattr -d com.apple.quarantine "$HOME/Applications/icloud-hme/icloud-hme" 2>/dev/null || true

"$HOME/Applications/icloud-hme/icloud-hme" -addr 127.0.0.1:8081 -data "$HOME/Library/Application Support/icloud-hme/data"
```

Change `-data` to the existing absolute data path when migrating an installation. Do not disable Gatekeeper globally; if macOS still blocks the first launch, approve this specific application in System Settings > Privacy & Security. A loopback listener does not require the API token, although you can set it temporarily for scripts.

#### Run with launchd

For a per-user service, create `~/Library/LaunchAgents/com.icloud-hme.plist`. Replace `/Users/your-user` with the real home directory. launchd requires absolute paths:

```
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.icloud-hme</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/your-user/Applications/icloud-hme/icloud-hme</string>
    <string>-addr</string>
    <string>127.0.0.1:8081</string>
    <string>-data</string>
    <string>/Users/your-user/Library/Application Support/icloud-hme/data</string>
  </array>
  <key>WorkingDirectory</key>
  <string>/Users/your-user/Applications/icloud-hme</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/Users/your-user/Library/Logs/icloud-hme.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/your-user/Library/Logs/icloud-hme.error.log</string>
</dict>
</plist>
```

```
mkdir -p "$HOME/Library/Logs"
chmod 600 "$HOME/Library/LaunchAgents/com.icloud-hme.plist"
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.icloud-hme.plist"
launchctl kickstart -k "gui/$(id -u)/com.icloud-hme"
launchctl print "gui/$(id -u)/com.icloud-hme"
```

If a token is added to the plist, the plist becomes secret material and must remain mode `0600`. For an upgrade, run `launchctl bootout "gui/$(id -u)/com.icloud-hme"`, back up the data, replace the binary, and bootstrap the plist again.

### Windows native binary

Use `icloud-hme_windows_amd64.exe`. Keep the executable under `C:\Program Files\icloud-hme` and persistent data under `C:\ProgramData\icloud-hme\data`, so replacing the executable cannot accidentally replace account data.

#### Run with PowerShell

Run the initial installation from an elevated PowerShell when using `Program Files` and `ProgramData`; otherwise choose absolute paths writable by the current user.

```
$root = Join-Path $env:ProgramFiles "icloud-hme"
$dataDir = Join-Path $env:ProgramData "icloud-hme\data"
New-Item -ItemType Directory -Force -Path $root, $dataDir | Out-Null
Copy-Item .\icloud-hme_windows_amd64.exe (Join-Path $root "icloud-hme.exe")
Unblock-File (Join-Path $root "icloud-hme.exe")

# This token exists only in the current PowerShell process.
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
$env:ICLOUD_HME_API_TOKEN = ([BitConverter]::ToString($bytes) -replace '-', '').ToLowerInvariant()

& (Join-Path $root "icloud-hme.exe") -addr 127.0.0.1:8081 -data $dataDir
```

When existing accounts are present, set `$dataDir` to the old directory and verify that it contains `accounts.json`. The environment variable above is not retained after the terminal closes. A non-loopback listener requires the token and a Windows Firewall rule limited to trusted networks.

For example, this administrator PowerShell rule permits only the LAN `192.168.1.0/24` to reach a directly bound port:

```
New-NetFirewallRule -DisplayName "iCloud HME 8081 from LAN" -Direction Inbound -Action Allow -Protocol TCP -LocalPort 8081 -RemoteAddress 192.168.1.0/24
```

#### Run with Task Scheduler

A scheduled task can run as `SYSTEM` or a dedicated service account while the application remains on loopback and a reverse proxy handles external access. Run this from an elevated PowerShell and grant the selected account access to the application and data directories:

```
$root = Join-Path $env:ProgramFiles "icloud-hme"
$dataDir = Join-Path $env:ProgramData "icloud-hme\data"
icacls (Join-Path $env:ProgramData "icloud-hme") /grant:r "SYSTEM:(OI)(CI)F" /T
$action = New-ScheduledTaskAction `
  -Execute (Join-Path $root "icloud-hme.exe") `
  -Argument "-addr 127.0.0.1:8081 -data `"$dataDir`"" `
  -WorkingDirectory $root
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName "icloud-hme" -Action $action -Trigger $trigger -Principal $principal -Force
Start-ScheduledTask -TaskName "icloud-hme"
Get-ScheduledTask -TaskName "icloud-hme"
```

This loopback example does not need a token. If direct non-loopback access is unavoidable, inject the token through a protected service-account environment or a service wrapper such as WinSW/NSSM; do not commit a plaintext token in a launcher script. Before upgrading, stop the task, back up the data, replace the exe, and start the task again.

### Docker image

The published image is `ghcr.io/zhaohao0924/icloud-hme-mng` and supports `linux/amd64` and `linux/arm64`. Pin a release tag such as `v0.2.0` in production and use `latest` only when that update policy is acceptable. The container listens on `0.0.0.0:8081`, so a token of at least 32 characters is required.

After verification and Docker smoke tests pass, every push to `main` publishes the multi-architecture `:main` image and a matching `:sha-<commit>` image. Use `:main` for staging or fast deployment of the current branch; pin a `v*` release tag for production. Stable release tags also update `:latest`.

#### Linux/macOS shell

```
docker pull ghcr.io/zhaohao0924/icloud-hme-mng:latest
mkdir -p ./data && chmod 700 ./data
export ICLOUD_HME_API_TOKEN="$(openssl rand -hex 32)"
docker run -d --name icloud-hme --restart unless-stopped --init -p 127.0.0.1:8081:8081 -e "ICLOUD_HME_API_TOKEN=$ICLOUD_HME_API_TOKEN" -e TZ=Asia/Shanghai -v "$(pwd)/data:/app/data" ghcr.io/zhaohao0924/icloud-hme-mng:latest -addr 0.0.0.0:8081 -data /app/data
docker ps --filter name=icloud-hme
docker logs --tail 200 icloud-hme
curl -fsS -H "Authorization: Bearer $ICLOUD_HME_API_TOKEN" http://127.0.0.1:8081/api/health
```

The host port is loopback-only; point the reverse proxy at `127.0.0.1:8081`. Removing or recreating the container does not remove a bind-mounted host directory, but stop the container and back up the directory before an upgrade.

#### Windows PowerShell

```
$dataDir = (New-Item -ItemType Directory -Force .\data).FullName
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
$token = ([BitConverter]::ToString($bytes) -replace '-', '').ToLowerInvariant()
docker pull ghcr.io/zhaohao0924/icloud-hme-mng:latest
docker run -d --name icloud-hme --restart unless-stopped --init -p 127.0.0.1:8081:8081 --env "ICLOUD_HME_API_TOKEN=$token" --env "TZ=Asia/Shanghai" --mount "type=bind,source=$dataDir,target=/app/data" ghcr.io/zhaohao0924/icloud-hme-mng:latest -addr 0.0.0.0:8081 -data /app/data
```

### Docker Compose

The repository `compose.yaml` builds from the checked-out source tree. It is not a pull-only GHCR deployment. It configures a bind-mounted data directory, health check, restart policy, `init`, and `no-new-privileges`.

On Linux/macOS:

```
git clone https://github.com/ZhaoHao0924/icloud-hme-mng.git
cd icloud-hme-mng
cp .env.example .env
mkdir -p /srv/icloud-hme/data && chmod 700 /srv/icloud-hme/data
vi .env
```

Set at least these values and replace the data path with the persistent directory used by this installation:

```
ICLOUD_HME_API_TOKEN=replace-with-a-random-token-at-least-32-chars
ICLOUD_HME_BIND_ADDRESS=127.0.0.1
ICLOUD_HME_PORT=8081
ICLOUD_HME_DATA_DIR=/srv/icloud-hme/data
ICLOUD_HME_IMAGE=icloud-hme:local
ICLOUD_HME_VERSION=dev
TZ=Asia/Shanghai
```

Start and operate the service:

```
docker compose up -d --build
docker compose ps
docker compose logs --tail 200 app
docker compose restart app
docker compose stop app
docker compose start app
docker compose down
```

Compose maps the container's `0.0.0.0:8081` to `ICLOUD_HME_BIND_ADDRESS`, which defaults to host loopback. Its health check calls `/api/auth/session` and does not expose account credentials. Open `http://127.0.0.1:8081` after the first start. In Windows PowerShell, copy `.env.example` with `Copy-Item` and use a Docker-compatible path such as `D:/Services/icloud-hme/data`. `docker compose down` removes the container but not the bind-mounted host data.

If a local source checkout is not desired, use the `docker run` example above for the published GHCR image. This Compose file still builds the local Dockerfile whenever `docker compose up -d --build` is run.

### Reverse proxy and HTTPS

Do not expose the application's plain HTTP port directly to the public internet. Keep a native process on `127.0.0.1:8081`, or keep `ICLOUD_HME_BIND_ADDRESS=127.0.0.1` in Compose, and let Caddy, Nginx, or an existing gateway terminate TLS and enforce network policy. Public access must use HTTPS, especially for administrator login and notification settings.

Caddy example:

```
hme.example.com {
    reverse_proxy 127.0.0.1:8081
}
```

Nginx example:

```
server {
    listen 443 ssl http2;
    server_name hme.example.com;
    ssl_certificate /etc/letsencrypt/live/hme.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/hme.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

A reverse proxy does not replace the platform administrator login. The first visit through the proxy still creates and signs in to the administrator. Scripts and automation should continue to send `Authorization: Bearer <ICLOUD_HME_API_TOKEN>`. Direct non-loopback binding requires the token, host firewall, and trusted-source restrictions.

### First-run validation and existing data

1. Confirm that `-data` or `ICLOUD_HME_DATA_DIR` points to the intended directory and, for an existing installation, that it contains the original `accounts.json`.
2. Open the local URL or proxy domain, create the administrator on first use, and sign in.
3. Inspect accounts, aliases, inboxes, and automation rules. Read-only views and automation previews do not create or delete aliases.
4. Check service health with the bearer token:

```
curl -fsS -H "Authorization: Bearer $ICLOUD_HME_API_TOKEN" http://127.0.0.1:8081/api/health
```

The browser session endpoint is `GET /api/auth/session`; Compose also uses it for its health check. Health responses do not expose Cookies, App Passwords, account details, or data paths.

### Security, backup, and upgrades

- Use mode `0700` for Linux/macOS data directories and mode `0600` for secret files. On Windows use a dedicated service account and restrictive ACLs. Restrict access to Docker bind-mounted directories as well.
- `accounts.json` may contain iCloud Cookies, App Passwords, and proxy credentials. `platform-auth.json` stores the administrator password hash. Email notification data may contain the 163 authorization code, and Webhook data contains the signing secret. Never publish these files, `.env`, launchd plists, or logs.
- Before backing up, pause automation in the Web UI and stop the service or container so accounts, platform authentication, and logs represent one point in time. See [DEPLOYMENT.md](DEPLOYMENT.md) for verified archive, restore confirmation, and rollback-directory procedures.
- On Linux/macOS, create archives into a restricted backup directory only after stopping the service. On Windows, use `scripts/backup-data.ps1` and `scripts/verify-data-backup.ps1`. Keep the old data directory until the restored copy has been accepted.
- During an upgrade replace only the binary, image, or build output and keep the original data path. Verify health, accounts, and automation after restart; roll back the binary or image if needed instead of starting with an empty directory.
- `DELETE /api/aliases/:id`, alias deactivation, and account deletion change real iCloud data. Do not use them for deployment acceptance. Do not run `docker compose down -v`, `rm -rf`, or clear the data directory while troubleshooting; back up first and use read-only checks or automation preview.
