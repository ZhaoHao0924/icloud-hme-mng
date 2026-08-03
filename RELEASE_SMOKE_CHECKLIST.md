# 发布与真实账户验收清单

本清单用于完成尚未自动化的 Docker、发布二进制和真实 iCloud 账户验收。仅在受控本机环境执行，使用专用测试账户和可删除的 Hide My Email 别名。

## 安全前提

- 不将 Apple ID 密码、App Password、Cookie、API Token 或邮件正文写入仓库、命令历史、截图、日志或测试报告。
- 通过 UI 输入真实凭据；不要把 Apple ID 密码、App Password、Cookie、平台管理员密码或 API Token 拼接到 `docker run`、`curl` 或 PowerShell 命令中。仅自动化烟测可使用其隔离临时管理员密码。
- 在内置 Web UI 中通过顶栏钥匙按钮输入 API Token；确认刷新后必须重新输入，且 URL、Web Storage、Cache Storage、IndexedDB、查询缓存和控制台均不含令牌。
- 将挂载数据目录置于仓库外，或使用单独的临时目录；完成后按本地安全流程删除。
- 使用至少 32 个字符的临时 API Token，并仅通过进程环境变量传递给容器。
- 为别名创建、启停和删除准备专用测试别名，不要操作生产用途的别名。

## 本机构建

1. Linux/macOS 使用 `VERSION=v0.2.0 ./build.sh`；Windows PowerShell 使用 `./build.ps1 -Version v0.2.0`。
2. 确认产物是 Linux amd64 二进制 `build/icloud-hme`，且包含当前 `web/dist` 的哈希资源。
3. 在隔离数据目录启动二进制，访问 `/`、账户深链接和 `/api/health`。确认 HTML 不缓存、哈希资源返回不可变缓存、未知 API 和静态路径仍为 404。
4. Windows 可运行 `./scripts/windows-release-smoke.ps1 -Version v0.2.0`，以临时原生二进制和临时数据目录自动验证嵌入资源、平台管理员初始化/登录/退出、平台会话重启失效、Bearer Token 兼容性、SPA fallback、缓存与安全头、404 及账户重启持久化；该结果补充但不替代 Linux 运行时验收。

## Docker 验收

1. 执行 `docker build --build-arg VERSION=v0.2.0 -t icloud-hme:smoke .`。
2. 以 `-addr 0.0.0.0:8081` 启动容器，挂载隔离的数据目录，并从环境变量注入临时 API Token。
3. 首次启动时，验证未带 Token 的 `/api/health` 返回 401 和 `code: "platform_auth_setup_required"`，错误 Token 返回 `api_token_invalid`，有效 Token 返回 200，且响应仅含服务名、版本、状态和配置可用性。
4. 在浏览器中通过登录页使用临时 Token 创建管理员账户，确认登录后根页面、`/settings`、账户深链接和刷新后的 SPA fallback 都可用；退出登录后平台内容不可访问。重启容器后确认需要重新登录，且 Bearer Token 仍可使用。检查 `/assets/*` 哈希资源、CSP、反嵌入和 MIME 嗅探保护响应头。
5. 通过 UI 新建一个无凭据的测试账户，重启同一挂载目录的容器，确认账户配置仍存在；停止容器后清理测试数据和临时 Token。

## 备份与恢复验收

1. 使用仓库外的隔离数据目录，先停止服务，再运行 `scripts/backup-data.ps1 -ConfirmServiceStopped`。
2. 使用 `scripts/verify-data-backup.ps1` 校验 ZIP 清单和 SHA-256；检查输出未包含账户、别名、Cookie 或密码内容。
3. 使用另一个隔离目标运行 `scripts/restore-data.ps1 -ConfirmServiceStopped -ConfirmRestore`，确认原目标目录保留为 `*.restore-before-*` 回滚副本。
4. 在目标 Docker 主机执行 `docker compose config` 和一次挂载数据目录的启动/重启验证。

## 真实 iCloud 冒烟

1. Cookie 流程：在凭据页更新测试账户 Cookie，确认别名列表可读取；创建、停用、启用并删除专用测试别名。
2. IMAP 流程：为测试账户设置 App Password，向测试别名发送一封无敏感内容的邮件，确认收件箱显示 `IMAP`、收件人筛选正确且邮件未被标记为已读。
3. Web 回退：使用没有 App Password 的独立测试账户，确认同一类测试邮件通过 `Web API` 返回；服务缺少明确收件人时应显示可恢复错误，而不是猜测匹配。
4. Apple ID 登录：对无 2FA 和有 2FA 的受控测试账户分别执行登录；2FA 仅在 UI 输入验证码，确认 OTP 取消、过期和成功路径均不将密码、验证码或 challenge 写入 URL、通知或持久浏览器存储。
5. 浏览器审计：使用开发者工具检查 localStorage、sessionStorage、Cache Storage、IndexedDB、URL、控制台和网络请求，确认不含 Cookie、密码、验证码或完整邮件正文。
6. 仅记录通过/失败、时间、版本、脱敏错误摘要和使用的场景；不要保存屏幕录制、原始响应或凭据。

## 完成记录

- [x] `go test -race ./...` 已在具备 `gcc` 的环境通过：GitHub Actions Run 23。
- [x] Docker 镜像构建、挂载数据目录启动与重启持久化通过：GitHub Actions Run 23。
- [x] 隔离备份、清单校验与回滚恢复烟测通过：`scripts/backup-restore-smoke.ps1`。
- [x] Windows 原生发布烟测已通过：`scripts/windows-release-smoke.ps1 -Version fe306-api-token-smoke -SkipNpmCi -Port 18083` 验证根页面、深链接、静态缓存、安全响应头、Bearer 鉴权、404 和重启持久化；临时服务与数据目录已清理。
- [x] Linux 发布二进制根页面、深链接、静态缓存和健康检查通过：GitHub Actions Run 24，脚本为 `scripts/ci-linux-binary-smoke.sh`。
- [ ] Cookie、IMAP、Web 回退、无 2FA 登录和 2FA 登录均已在受控真实账户完成。
- [ ] QA-008 结果已记录在 `FRONTEND_DEVELOPMENT_PLAN.md`，且没有敏感信息进入仓库。
