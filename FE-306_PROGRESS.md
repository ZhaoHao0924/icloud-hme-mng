# FE-306 Progress Handoff

Updated: 2026-08-03 (Asia/Shanghai)

## Completed

- Alias automation was implemented and pushed in `7c0ba87`.
- iCloud trust-challenge handling was added in `cfc615e`:
  - `HTTP 421` trust challenges are treated as a session-recovery condition.
  - Upstream trust/session tokens are not returned to the UI.
- China-region password login was fixed in `a544717`:
  - `accountLogin` now uses the account's regional setup endpoint.
  - The regional country code and returned WebAuth cookies are handled correctly.
  - A password login is only persisted after HME session validation succeeds.
- Inbox stale alias filtering was fixed in `a8c58e9`:
  - A URL alias filter that no longer belongs to the account is cleared automatically.
  - Inbox loading waits for a selected alias to be validated before sending an alias-filtered request.

## Current Local Acceptance Environment

- Existing backend: `http://127.0.0.1:8081/` (PID `11804`, binary `tmp/icloud-hme-favicon.exe`).
- The service reuses the existing data directory under `C:\Users\86155\AppData\Local\Temp\icloud-hme-manual-smoke-5e3688d68aaf415caeced6a12f6c29f3\data`; the configured account and platform-auth state were preserved.
- `/api/auth/session` returns `configured: true` and `authenticated: false`; no real alias write operation was performed.
- `web/dist` and `internal/webui/dist` contain the same 24 production assets, including the shared favicon.

## Verified Today

- Backend: `go test -count=1 ./...`, `go vet ./...`, and `git diff --check` passed.
- Frontend: `npm run lint`, `npm run typecheck`, `npm run format:check`, `npm test` (23 files / 160 tests), and `npm run build` passed.
- Browser Mock acceptance: `npm run test:e2e:mock` passed (34/34), including scheduling, notification settings, and responsive layout checks.
- Embedded production assets match the frontend build; the running service returns the favicon with HTTP 200 and `image/svg+xml`.
- No real-account or destructive automation operation was made during this verification.

## Next Session

1. Run `go test -race ./...` in Linux CI or another environment with `gcc` and CGO enabled.
2. Complete the Docker/Linux release smoke checks from `RELEASE_SMOKE_CHECKLIST.md`.
3. After platform-admin login, verify alias automation status and the read-only preview with the configured account. Do not create, deactivate, reactivate, delete aliases, or change a real rule without explicit approval.
4. Complete QA-008 with a controlled iCloud account; this requires external credentials and cannot be completed by local mocks.

## 2026-08-02 Verification

- Frontend: `npm test` (22 files, 139 tests), `npm run typecheck`, `npm run lint`, `npm run build` — all passed.
- Backend: `go test ./...`, `go vet ./...` — all passed.
- Embedded assets: `web/dist` → `internal/webui/dist` synced; 21 files, `index.html` SHA-256 match confirmed.
- Release binary: `build.ps1` produces Linux amd64 ELF with embedded `web/dist` assets (verified in prior session).
- Release smoke checklist: Windows native release smoke (`scripts/windows-release-smoke.ps1`) previously validated embedded assets, auth, SPA fallback, cache/security headers, 404, and restart persistence. Docker/Bash/race/real iCloud smoke remain pending remote CI or controlled real-account execution.

## 2026-08-02 Inbox Alias Filter Fix

- Fixed Web API inbox reads when a specific alias is selected and Apple `thread/search` omits recipient fields.
- `FindByAlias` now keeps exact-recipient filtering but fetches thread detail for candidate messages whose digest lacks recipients; it still does not infer matches from subject, sender, or preview text.
- If both digest and detail responses lack explicit recipients, the existing `ErrWebRecipientUnavailable` failure remains.
- Verification passed: `go test ./internal/mail/`, `go test ./internal/server/`, `go test ./...`, `go vet ./...`.

## 2026-08-02 Inbox Alias Real-Account Follow-up

- Real-account read-only probe showed all-mail Web API reads still pass, but alias-filtered reads cannot be verified through Web API because Apple omits recipients from `thread/search` and returns HTTP 403 for the attempted thread-detail recipient check.
- Updated the detail failure classification so this path no longer reports `401 Cookie 会话失效`; it now returns the existing recipient-verification failure as `502 读取邮件失败`, which matches the actual limitation.
- Confirmed Apple Web Mail search query fallback is not reliable in this session: `alias`, `to:alias`, and a random no-match query returned the same thread fingerprint set as the unfiltered inbox and still omitted recipients.
- Restarted local backend on `127.0.0.1:18084` with version `manual-smoke-inbox-alias-detail-classification-fix`; health check passed and alias-filtered API now returns `502` recipient-verification failure instead of `401`.
- Verification passed: `go test ./internal/mail/ ./internal/server/`, `go test ./...`, `go vet ./...`.

## 2026-08-02 App Password Status Notice

- Added an explicit App Password status note on the credentials page:
  - Configured: App Password is configured; inbox reads prefer IMAP and support reliable per-alias filtering.
  - Missing: inbox reads fall back to Web API; Apple may omit verifiable recipients, so per-alias filtering may be unavailable.
- Restarted the local Vite frontend on `http://127.0.0.1:5184/` with `--strictPort`; backend remains on `http://127.0.0.1:18084/`.
- Verification passed: targeted `SecurityPage.test.tsx`, `npm run typecheck`, `npm run lint`, `npm test`, `npm run build`.

## 2026-08-02 Inbox Manual Filter Inputs

- Replaced inbox account and alias pure dropdowns with input + datalist controls.
- Account filter supports choosing from configured accounts or manually entering a configured account ID, name, or iCloud email.
- Alias filter supports choosing from known aliases or manually entering a full email address; manually entered aliases are preserved in the URL and sent to the inbox API instead of being cleared when absent from the alias list.
- Added wrapping helper copy so mobile layouts do not overflow.
- Verification passed: targeted `InboxPage.test.tsx`, `npm run typecheck`, `npm run lint`, `npm test`, `npm run test:e2e:mock` (31/31), `npm run build`.

## 2026-08-02 Inbox Filter Alignment and Timeout Tuning

- Moved the alias helper copy out of the individual alias field and into a full-width toolbar helper row, so the account, alias, days, and limit controls align consistently.
- Scoped inbox toolbar spacing/alignment styles so the filter row remains compact across desktop, tablet, and mobile layouts.
- Increased inbox request timeout from 15s to 60s on the frontend, and increased the Web Mail HTTP client timeout from 30s to 60s on the backend to reduce false timeout failures during slow Apple/IMAP reads.
- Synced the latest `web/dist` build into `internal/webui/dist` and preserved `placeholder.txt`.
- Restarted the local backend on `127.0.0.1:18084` with version `manual-smoke-inbox-alignment-timeout-fix`; health check passed and the existing account data directory was reused.
- Real read-only inbox probe passed through IMAP: `/api/inbox?account_id=acc_20efd29c&days=7&limit=20` returned 20 messages in about 35.7s, confirming the previous 15s frontend timeout was too short for slow mailbox reads.
- Verification passed: `go test ./internal/mail/`, `go test ./...`, targeted `InboxPage.test.tsx`, `npm run typecheck`, `npm run lint`, `npm test` (22 files, 139 tests), `npm run test:e2e:mock` (31/31), `npm run build`.

## 2026-08-02 Inbox Account Email Display

- Changed the inbox account filter to display each account's concrete iCloud email instead of its internal `acc_*` ID.
- Account selection still resolves the displayed email to the internal account ID used by routes and API requests; manual entry by configured ID or account name remains supported.
- Verification passed: targeted `InboxPage.test.tsx` (8/8), `npm run typecheck`, `npm run lint`, `npm run test:e2e:mock` (31/31), and `npm run build`; embedded frontend assets were synced.

## 2026-08-02 Inbox HTML Preview Cleanup

- Replaced regex-only HTML stripping with MIME-aware body parsing that prefers `text/plain` in multipart messages and safely extracts visible text when only HTML is available.
- HTML preview extraction now excludes `head`, `style`, `script`, `noscript`, `template`, SVG, and canvas content, preventing CSS such as `@font-face` from appearing as message text.
- Added transfer-encoding, charset, multipart, HTML entity, and non-visible-content handling without rendering remote email HTML, scripts, or tracking assets in the browser.
- Restarted the backend on `127.0.0.1:18084` as `manual-smoke-mail-preview-html-fix`, reusing the existing account data directory.
- Verification passed: `go test ./internal/mail/`, `go test ./...`, and `go vet ./...`. A real read-only IMAP probe returned 20/20 non-empty previews with zero CSS, HTML-tag, or script leaks.

## 2026-08-02 Inbox Lazy Preview Loading

- Added an IMAP summary mode that fetches only UID, envelope, and date for inbox lists; the existing full-preview list behavior remains available for API compatibility.
- Added `GET /api/inbox/messages/:id` for bounded, safe single-message previews and changed the web client to load only the selected IMAP message body.
- Added a 30-second list cache and a 10-minute per-message preview cache. Switching back to an already viewed message no longer repeats the IMAP body request.
- Preview failures are isolated to the right-hand preview panel with a local retry action, so the inbox list remains usable.
- Real-account read-only measurements improved the 20-message list from roughly 35–45 seconds to 5.9 seconds; the selected message body took another 4.7 seconds. A real browser showed the list in 6.2 seconds and the first preview by 13.1 seconds, with no markup leak or horizontal overflow. A specific-alias summary list returned in 4.4 seconds.
- Restarted the final backend on `127.0.0.1:18084` as `manual-smoke-inbox-lazy-preview-final`, reusing the existing account data directory and embedding the latest production frontend assets.
- Verification passed: `go test ./...`, `go vet ./...`, frontend tests (22 files / 142 tests), targeted tests (31/31), typecheck, lint, format check, mock E2E (31/31), and production build.

## 2026-08-02 Inbox First Preview and Server Cache

- Added `first_preview=true` for lightweight IMAP inbox requests. The server now loads the first message preview through the list request's already authenticated IMAP connection instead of opening a second TLS/login session.
- Added an account-scoped in-memory preview cache with a 10-minute TTL and a 500-entry limit. Message detail requests return cached previews without reconnecting to IMAP.
- App Password updates and account deletion invalidate that account's previews; configuration reload and server shutdown clear the cache.
- First-preview failures remain isolated from the list response, so the frontend can retry through the existing message-detail endpoint.
- Real-account API measurements: cold list plus first preview 6.6s, cached first-message detail 1ms, lightweight list 6.5s, and a specific-alias list plus first preview 7.0s.
- Real browser verification: the first preview was visible in 6.7s with one list request, zero first-message detail requests, and no page errors. The prior browser result was about 13.1s for list plus first preview.
- Restarted the backend on `127.0.0.1:18084` as `manual-smoke-inbox-first-preview-cache` (PID 61784), reusing the existing account data directory and embedding the latest production frontend assets.
- Verification passed: `go test ./...`, `go vet ./...`, frontend tests (22 files / 143 tests), typecheck, lint, format check, mock E2E (31/31), production build, and real read-only API/browser checks.

## 2026-08-02 Inbox Session Reuse and Fast List Delivery

- Added an account-scoped IMAP session pool. It serializes use of a connection per account, retains it for two idle minutes, closes it automatically afterward, retries once with a fresh connection only when a reused session fails, and never shares connections across accounts.
- App Password updates, account deletion, configuration reload, and server shutdown now invalidate IMAP sessions as well as preview cache entries.
- Reused IMAP clients remember a read-only `INBOX` selection, avoiding redundant `SELECT INBOX` commands for list, alias, and preview reads on the same connection.
- Changed the web inbox default from inline `first_preview` to a lightweight list followed by the existing on-demand first-message request. With the pooled session this lets the list render sooner while the detail request reuses the authenticated and selected IMAP connection.
- Real API measurements after clearing in-memory state: list 5.2s, first preview 0.88s, total 6.1s. Reused-session measurements: stable full inbox refresh 1.38s, specific alias refresh 2.73s, cached message detail 1ms.
- Real browser cold-start verification: one list request without `first_preview`, one detail request, list visible in 6.7s, first preview visible in 9.5s, and no page errors. The earlier browser baseline for the first complete preview was about 13.1s.
- Restarted the backend on `127.0.0.1:18084` as `manual-smoke-inbox-fast-list-session-reuse` (PID 32172), reusing the existing account data directory and embedding the latest production frontend assets.
- Verification passed: `go test ./...`, `go vet ./...`, frontend tests (22 files / 143 tests), typecheck, lint, format check, mock E2E (31/31), production build, embedded asset hash check, and real read-only API/browser checks.

## 2026-08-02 Inbox Cursor Pagination

- Added IMAP UID cursor pagination to `GET /api/inbox`:
  - Initial requests return the newest page.
  - Older pages use `before_uid=<next_cursor>`.
  - Responses include `has_more` and `next_cursor`; the cursor is the lowest UID in the current page, so new incoming mail does not create duplicate or skipped rows in later pages.
- Per-alias IMAP reads use the same cursor behavior. When IMAP recipient search falls back to local header filtering, it scans bounded header batches until it can fill the page without losing cursor continuity.
- The Web API Cookie fallback intentionally remains single-page because Apple exposes no reliable continuation token. It returns `has_more: false` instead of fabricating an unreliable next page.
- Updated the inbox UI to append older pages through a `加载更多邮件` control. A refresh resets to the newest page, rather than refetching every previously loaded page.
- Added API, IMAP, component, and browser mock coverage for cursors and responsive pagination. Mock browser suite now has 32 tests.
- Restarted the local backend on `http://127.0.0.1:18084` as `manual-smoke-inbox-cursor-pagination-final`; Vite remains available on `http://127.0.0.1:5184/`.
- Verified with a read-only real-account two-page IMAP request: both pages returned two summaries, the first page returned a cursor, and the pages had no duplicated message IDs. No message content was logged.
- Verification passed: `go test ./...`, `go vet ./...`, `npm run typecheck`, `npm run lint`, `npm run format:check`, `npm test` (22 files / 145 tests), `npm run test:e2e:mock` (32/32), and `npm run build`; `web/dist` was synced into `internal/webui/dist` and served embedded by the final backend binary.

## 2026-08-02 Inbox Fixed Scroll Panel

- Capped the mailbox summary panel height instead of allowing loaded pages to keep extending the page.
- Excess message rows now scroll inside the left pane; the preview pane remains stable. The panel uses viewport-aware desktop, tablet, and mobile limits, preserves a stable scrollbar gutter, and contains overscroll.
- Added a 10-message browser mock scenario that verifies actual vertical overflow, programmatic scrolling, and no horizontal overflow at desktop and mobile widths.
- Restarted the local backend on `http://127.0.0.1:18084` as `manual-smoke-inbox-scroll-panel`; Vite remains available on `http://127.0.0.1:5184/`.
- Verification passed: `npm run typecheck`, `npm run lint`, `npm run format:check`, `npm test` (22 files / 145 tests), `npm run build`, targeted Playwright scroll validation, and mock browser E2E (33/33). The embedded `index.html` served by the backend matches `internal/webui/dist`.

## 2026-08-02 Inbox Ten-Message List and Sticky Sidebar

- Changed the inbox message list to show up to exactly ten equal-height summaries; additional messages scroll inside the list while the load-more control remains below it.
- Made the desktop and tablet sidebar sticky during page scroll, with a viewport-height internal fallback for longer navigation; the mobile header navigation remains non-sticky.
- Expanded the browser mock scenario to twelve messages and added coverage for ten-row capacity, list scrolling, desktop sticky positioning, and mobile layout behavior.
- Verification passed: `npm run typecheck`, `npm run lint`, `npm run format:check`, `npm test` (22 files / 145 tests), `npm run test:e2e:mock` (33/33), `npm run build`, and `go test ./...`.
- Synced production assets into `internal/webui/dist` and restarted the local backend on `http://127.0.0.1:18084` as `manual-smoke-inbox-ten-list-sticky-sidebar`; `/api/health` returned `ok`.

## 2026-08-02 Persistent Operation Logs

- Added a privacy-safe, structured operation log store. It writes daily JSON Lines files under the local data directory, starts cleanup on service launch, and rechecks retention every hour.
- Records are retained for exactly the latest seven days. Expired records are removed automatically, including partial-day cleanup at the seven-day boundary.
- Logged fields are limited to timestamp, operation name, severity, HTTP status, and duration. Request bodies, query strings, account IDs, email addresses, credentials, message content, and upstream response bodies are not stored.
- Added protected `GET /api/logs?limit=200` and a System Settings operation-log view with refresh, severity/status/duration display, empty and error states, and an explicit seven-day automatic-cleanup notice.
- Replaced Gin's default request logger with recovery plus the sanitized operation logger, preventing raw query strings from being emitted by the standard request logger.
- Verification passed: `go test ./...`, `go vet ./...`, `npm run typecheck`, `npm run lint`, `npm run format:check`, `npm test` (22 files / 148 tests), `npm run test:e2e:mock` (33/33), targeted Settings tests (6/6), and production build.
- Restarted the local backend on `http://127.0.0.1:18084` as `manual-smoke-operation-logs-retention`; health is `ok`, and a locally triggered configuration reload was still returned by `/api/logs` after the final restart.

## 2026-08-02 Inbox Preview Space Utilization

- On desktop and tablet layouts, the preview now uses the available height beside the fixed ten-message list before showing an internal scrollbar.
- The preview is capped at the same ten-row panel height (including its border), so exceptionally long content remains scrollable without expanding the page indefinitely.
- Mobile keeps the existing 20rem preview cap because the inbox panes are stacked there.
- Extended the browser mock coverage to assert panel-height alignment, use of the previously blank preview space, and the desktop long-content scroll boundary.
- Verification passed: `npm run typecheck`, `npm run lint`, `npm run format:check`, `npm test` (22 files / 148 tests), `npm run test:e2e:mock` (33/33), `npm run build`, `go test ./...`, and `go vet ./...`.
- Synced `web/dist` into `internal/webui/dist`, verified the embedded page and CSS return HTTP 200, and restarted the local backend on `http://127.0.0.1:18084` as `manual-smoke-inbox-preview-fill`; health is `ok` and the existing account data directory was reused.

## 2026-08-02 Platform Login Authentication

- Added a mandatory platform login layer for the production server. First use creates one administrator account; there is no default password.
- Administrator credentials are stored only as a bcrypt hash in `data/platform-auth.json` with `0600` permissions. Browser sessions are server-side, HttpOnly, SameSite=Strict, limited to 12 hours, and invalidated by logout or service restart.
- Protected business APIs now accept either a valid platform session or the existing Bearer API Token, preserving script and automation access. Remote first-time administrator setup requires the API Token when it is configured.
- Added `/api/auth/session`, `/api/auth/setup`, `/api/auth/login`, and `/api/auth/logout`, protected-route redirection, return-to-source login behavior, a top-bar logout control, and setup/login error states.
- Added browser and unit coverage for protected-route interception, administrator setup/login, logout revocation, session cookie security attributes, Bearer compatibility, and restart invalidation. Updated API, README, release checklist, and Windows release smoke coverage.
- Rebuilt and synced `web/dist` to `internal/webui/dist`, removing superseded embedded hashed assets while preserving `placeholder.txt`.
- Restarted the local backend on `http://127.0.0.1:18084` as `platform-auth-login` (PID `6648`), reusing the existing account data directory. The service is intentionally unconfigured: `/api/auth/session` returns `configured: false`, business APIs return `401 platform_auth_setup_required`, and the actual embedded `/accounts` route redirects to the `创建管理员账户` page without creating credentials.
- Verification passed: `go test ./...`, `go vet ./...`, `npm run typecheck`, `npm run lint`, `npm run format:check`, `npm run build`, `npm test` (23 files / 150 tests), and `npm run test:e2e:mock` (34/34).

## 2026-08-02 Automation Governance and Dry Run

- Added persisted scheduling constraints to account-level alias automation:
  - `allowed_weekdays` uses `0..6` (`0` is Sunday); an empty array means every day.
  - `execution_window_start` and `execution_window_end` use `HH:MM`; both are optional together, and cross-midnight windows are rejected.
  - Scheduled runs outside the configured day/window are deferred to the next allowed time without recording a fake run or repeatedly scanning an overdue rule.
  - Daily-limit deferrals and retry scheduling now also respect the next allowed schedule time.
- Added `POST /api/accounts/:id/alias-automation/preview`:
  - It reads aliases through a read-only client path and returns current requested capacity, total/active counts, daily remaining capacity, total-limit capacity, target remaining capacity, and schedule eligibility.
  - It does not create aliases, save refreshed cookies, write creation history, or mutate rule/run state.
  - Manual `run` behavior remains unchanged: it ignores weekday/window constraints but still obeys target, daily, total-limit, and pause safeguards.
- Added the Automation-page controls for weekday checkboxes, start/end time inputs, schedule status, and an inline `预览执行` result. The form validates time-window pairs and ordering before save.
- Updated `API.md` and `README.md` for the new fields, preview endpoint, local-time semantics, and manual-run behavior.
- Verification passed:
  - Backend: `go test ./...`.
  - Frontend: `npm run lint`, `npm run typecheck`, `npm test` (23 files / 157 tests), and `npm run build`.
  - Browser mock acceptance: `npm run test:e2e:mock` (34/34), including weekday/window save, read-only preview, and desktop/tablet/mobile no-overflow checks.
  - `git diff --check` passed; only existing CRLF conversion warnings were emitted.
- Handoff boundary: no real aliases, account credentials, real automation rules, or running service state were modified in this task. The live `18084` process remains on its prior binary until its data path is provided for a safe restart.

## 2026-08-03 Production Asset Sync and Service Restart

- Ran the standard production build with the existing frontend dependencies and synced `web/dist` into `internal/webui/dist`.
- Built and started the Windows local service as `manual-smoke-fe306` on `127.0.0.1:8081`, reusing the existing data directory. The service loaded one configured account and retained platform authentication state.
- The embedded `index.html` matches both the local embedded file and `web/dist`; the hashed entry asset returned HTTP 200.
- Verified `/api/auth/session` returns `configured=true` and `authenticated=false`. Before platform login, both alias automation status and preview endpoints return `401 platform_auth_required` without reaching the account client.
- Verification passed: `go test ./...`, `go vet ./...`, frontend `npm test` (23 files / 158 tests), `npm run lint`, `npm run format:check`, `npm run typecheck`, `npm run build`, and `npm run test:e2e:mock` (34/34).
- No real aliases, credentials, automation rules, or account state were modified. Read-only automation status/preview and real time-window behavior remain pending a platform-admin login; external webhook implementation remains unspecified until its destination and delivery policy are decided.

## 2026-08-03 Alias List Latency Optimization

- Reused one validated HME client per account for serialized alias operations, avoiding a new TLS client and iCloud `/validate` request on every alias-list request.
- Invalidated the cached client after Cookie updates, account login, configuration reload, account deletion, or a session error. Read-only automation previews continue to use an isolated client and do not persist refreshed Cookies.
- Added a 30-second frontend alias-query stale window so returning to the alias page does not immediately repeat the upstream request.
- Rebuilt the embedded frontend and restarted the local service as `manual-smoke-alias-fast` on `127.0.0.1:8081` (PID `19916`), reusing the existing account data directory. `/api/auth/session` remains configured and unauthenticated, with one account loaded.
- Verification passed: `go test ./...`, `go vet ./...`, frontend `npm test` (23 files / 158 tests), targeted alias/query tests (20/20), `npm run lint`, `npm run format:check`, `npm run typecheck`, production build, and `npm run test:e2e:mock` (34/34).
- No real aliases, credentials, or automation rules were modified. Real-account latency measurement still requires a platform-admin login.

## 2026-08-03 QQ 邮箱通知

- 按产品决定将外发通知收敛为 QQ 邮箱：固定使用 `smtp.qq.com:465` 隐式 TLS，QQ 发件地址接受 `@qq.com`、`@vip.qq.com` 和 `@foxmail.com`。
- 新增 `internal/notification`：配置写入数据目录下的 `qq-email-notification.json`，接口只返回脱敏状态；授权码不进入 API 响应、操作日志或邮件正文。
- 新增受保护接口：`GET/PUT /api/notifications/email` 和 `POST /api/notifications/email/test`。QQ SMTP 认证使用授权码，不使用 QQ 登录密码。
- 自动化手动/定时运行结果、自动暂停和 iCloud 会话失效进入有界异步队列，SMTP 失败最多重试 3 次；队列或发送失败不阻塞别名任务。正文只包含脱敏账户标识、事件、状态、数量和错误摘要。
- 系统设置页新增 QQ 邮箱配置、保存、测试发送和启用开关；授权码输入在保存成功后清空。
- 验证已通过：`go test ./internal/notification ./internal/server`、前端 `npm run typecheck`、设置页测试 7/7。完整 Go/前端回归、生产构建和运行中服务重启待本功能完成后统一执行。

## 2026-08-03 163 发件 / QQ 收件调整

- 按最新产品要求将通知通道调整为 163 发件、QQ 收件：固定使用 `smtp.163.com:465` 隐式 TLS，发件地址限定为 `@163.com`，收件地址限定为 QQ 邮箱域名。
- 配置字段调整为 `sender_email` 和 `recipient_email`，配置文件调整为 `email-notification.json`；旧 QQ 发件配置不会被新服务加载，避免启动时误用不匹配的 SMTP 凭据。
- 设置页改为填写 163 发件邮箱、163 授权码和 QQ 收件邮箱，并保留保存后清空授权码的行为。

## 2026-08-03 Webhook 通知

- 新增独立的 `webhook-notification.json` 配置和 `GET/PUT /api/notifications/webhook`、`POST /api/notifications/webhook/test` 接口；读取响应只返回启用状态、配置状态和 URL，不返回签名密钥。
- Webhook 目标必须使用 HTTPS。投递请求采用 HMAC-SHA256 签名，签名原文为 `{timestamp}.{body}`，并带有投递 ID 和时间戳请求头。
- 自动化运行结果、自动暂停和 iCloud 会话失效事件与邮件共用同一份脱敏事件模型，同时进入独立的有界异步队列，失败最多重试 3 次，不阻塞别名任务。
- 系统设置页新增 Webhook URL、签名密钥、启用开关、保存和测试发送；签名密钥在保存成功后清空。
- 验证已通过：webhook 协议与重试测试、服务器 API 测试、设置页定向测试、前端类型检查和 lint；完整 Go 测试、`go vet ./...`、前端 160 项测试、格式检查、生产构建和浏览器 mock 34/34。
- 已将生产前端同步到 `internal/webui/dist`，并以 `manual-smoke-webhook` 构建版本重启 `127.0.0.1:8081`（PID `2052`），复用原有数据目录；`/api/auth/session` 确认账号配置和平台认证状态仍然存在，未执行真实别名写操作。

## 2026-08-03 Favicon 与回归验收

- 浏览器页签图标复用左上角蓝色云图标，新增 `web/public/assets/favicon.svg` 并接入 `web/index.html`；生产构建和 Go 嵌入目录均包含该资源。
- 当前服务保持在 `127.0.0.1:8081`，未重建数据目录或重置平台管理员配置；`/api/auth/session` 返回账号配置存在但当前浏览器会话未登录。
- 验证通过：`go test -count=1 ./...`、`go vet ./...`、`npm test`（160/160）、`npm run lint`、`npm run typecheck`、`npm run format:check`、`npm run build` 和 `npm run test:e2e:mock`（34/34）。
- `go test -race ./...` 仍待具备 `gcc`/CGO 的 Linux 或 CI 环境；真实 iCloud、Docker 和 Linux 发布烟测仍属于外部验收，不在本机擅自执行写操作。
