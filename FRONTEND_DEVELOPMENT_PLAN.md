# iCloud HME 前端开发计划

> 文档状态：生效中
>
> 创建日期：2026-07-31
>
> 当前阶段：Phase 0 / Phase 3 / Phase 6 验收；Phase 8 进行中
> 适用范围：本仓库后续前端建设及为前端联调所需的后端调整

## 1. 文档用途

本文档是前端开发的长期执行基线，用于统一产品范围、技术方案、接口契约、实施顺序和验收标准。

后续开发遵循以下规则：

1. 一次只推进一个阶段，阶段门槛满足后再进入下一阶段。
2. 开始任务前将对应复选框和进度表更新为“进行中”。
3. 完成任务后记录实际改动、验证命令和遗留问题。
4. 新需求先写入本文档并评估影响，不直接绕过规划实施。
5. Apple 私有接口行为与本文档冲突时，以实际验证结果为准，并同步更新风险和契约。

## 2. 当前项目基线

当前仓库是一个 Go 后端服务，没有前端工程。

- HTTP 框架：Gin
- 数据存储：`data/accounts.json`
- HME 操作：iCloud Web 私有接口
- 邮件读取：IMAP 优先，iCloud Web API 回退
- 认证方式：Cookie、App Password、Apple ID 密码 + SRP + 2FA
- 当前 HTTP 路由：15 条 `/api/*` 路由
- 当前构建方式：Go 二进制和 Docker
- 当前测试：已有 Go 单元测试覆盖访问策略、账户响应脱敏、配置 schema、Cookie 所有权、并发访问、持久化回滚、输入边界、健康检查、两阶段登录、IMAP 关键逻辑，以及 Web 邮件动态网关 Cookie、消息契约和收件人降级；无前端测试

该项目处理 Cookie、Apple ID 密码、App Password 和邮件内容，所有设计都必须按照敏感数据应用处理。

## 3. 产品目标与边界

### 3.1 目标

- 提供账户、认证凭据、HME 别名和收件箱的完整图形化操作入口。
- 首屏直接进入账户工作台，不增加营销页或功能介绍页。
- 保持本地优先和单二进制部署体验。
- 支持桌面和移动端，主要优化桌面高频管理操作。
- 对加载、空数据、超时、会话过期和 Apple 服务异常提供明确状态。
- 浏览器响应、缓存和日志中不暴露 Cookie 或 App Password。

### 3.2 首期不包含

- 公网 SaaS、多租户、注册和计费系统。
- 完整邮件客户端能力，例如发信、附件管理、文件夹管理和已读状态同步。
- HME 使用统计、图表报表或营销型仪表盘。
- 在浏览器中展示或导出原始 Cookie、App Password。
- Apple 私有接口之外的新业务能力。

## 4. 已确定的架构决策

1. 前端采用 React + TypeScript SPA。
2. 开发环境由 Vite 代理 `/api` 到 Go 服务，不开放宽泛 CORS。
3. 生产环境前后端同源，静态资源最终通过 `go:embed` 打入 Go 二进制。
4. 默认服务仅监听 `127.0.0.1`；需要非回环监听时必须显式启用 API Token，可信反向代理场景下 Go 服务仍优先保持本机监听。
5. 服务端只返回脱敏账户 DTO，前端只接收 `has_cookies`、`has_app_password` 等能力标记。
6. Apple 远端写操作不做乐观更新，成功后重新获取服务端数据。
7. 创建别名等非幂等操作不在前端自动重试。
8. 界面默认中文，文本集中管理，为后续英文支持保留结构。
9. UI 定位为安静、紧凑的操作工具，以表格、标签页和对话框为主，不使用卡片嵌套和装饰性大图。

## 5. 后端前置整改

以下任务是正式前端联调的前置条件。前端脚手架可以使用 MSW 模拟接口并行准备，但不得绕过这些问题上线。

| ID | 优先级 | 任务 | 完成标准 |
| ------ | ------ | ------------------------ | ------------------------------------------------------------------- |
| BE-001 | P0 | 收紧服务访问边界 | 默认监听 `127.0.0.1`；非回环监听必须启用 Bearer Token |
| BE-002 | P0 | 账户响应脱敏 | 账户 API 不返回 Cookie、App Password；返回认证能力布尔值 |
| BE-003 | P0 | 统一账户配置结构 | 代码、模板、README 和 API 文档使用同一 JSON schema |
| BE-004 | P0 | 修复 Cookie 并发与持久化 | 客户端使用 Cookie 副本；无 map 数据竞争；配置原子写入且错误不被忽略 |
| BE-005 | P0 | 增加参数边界 | 限制 `limit`、`days`、标签长度、Cookie 数量和请求体大小 |
| BE-006 | P1 | 修复 IMAP 邮件逻辑 | `days` 有效；结果倒序；连接失败正确释放；预览大小受控 |
| BE-007 | P1 | 修复 Web 邮件回退 | 动态网关携带 Cookie；能可靠获得或判断收件人；结果字段契约稳定 |
| BE-008 | P1 | 补全 pending 账户流程 | 添加账户时可以提交 `icloud_email`，随后能够发起密码登录 |
| BE-009 | P1 | 重构 SRP/2FA 登录 | 正确使用服务端挑战；登录拆为 start/verify 两阶段；challenge 有 TTL |
| BE-010 | P1 | 增加健康检查 | `GET /api/health` 返回服务、版本和配置可用状态，不包含敏感信息 |
| BE-011 | P1 | 支持前端静态资源 | SPA 静态文件、缓存头和前端路由 fallback 正常，`/api` 不受影响 |

### 5.1 安全账户 DTO

前端期望的账户摘要至少包含：

```json
{
  "id": "acc_xxx",
  "name": "主账号",
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
```

禁止出现在响应中的字段：

- 原始 Cookie 名和值
- App Password
- Apple ID 登录密码
- 代理密码或完整带凭据代理 URL

### 5.2 登录接口目标契约

```text
POST /api/accounts/:id/login/start
POST /api/accounts/:id/login/verify
```

`login/start` 可能直接成功，也可能返回：

直接成功时返回脱敏账户摘要；需要 OTP 时返回：

```json
{
  "status": "otp_required",
  "challenge_id": "temporary-id",
  "expires_in": 300
}
```

`login/verify` 使用 `challenge_id` 和 `otp_code` 完成认证。成功响应只返回脱敏账户摘要，不向前端返回 Cookie。

## 6. 前端技术方案

| 领域 | 方案 |
| ---------- | ------------------------------ |
| 构建 | Vite + TypeScript |
| UI | React |
| 路由 | React Router |
| 服务端状态 | TanStack Query |
| 表单 | React Hook Form + Zod |
| 无障碍组件 | Radix UI primitives |
| 图标 | Lucide React |
| 样式 | CSS Variables + CSS Modules |
| 单元测试 | Vitest + React Testing Library |
| 接口模拟 | MSW |
| 端到端测试 | Playwright |

### 6.1 计划目录

```text
web/
  package.json
  vite.config.ts
  src/
    app/              # 路由、Provider、错误边界
    api/              # HTTP 客户端、DTO、Zod schema
    components/       # 通用表格、状态、对话框、空状态
    features/
      accounts/
      aliases/
      inbox/
      security/
      system/
    styles/           # 设计令牌、全局与响应式样式
    test/             # MSW handlers、fixtures、测试工具
internal/
  webui/              # go:embed 入口和前端构建产物
```

## 7. 页面与路由

| 路由 | 页面 | 主要能力 |
| ------------------------ | ---------- | -------------------------------------------- |
| `/accounts` | 账户工作台 | 状态汇总、账户表格、添加、删除、配置重载 |
| `/accounts/:id/aliases` | 别名管理 | 创建、搜索、状态筛选、复制、停用、恢复、删除 |
| `/accounts/:id/inbox` | 收件箱 | 别名筛选、日期范围、数量限制、刷新、邮件摘要 |
| `/accounts/:id/security` | 凭据管理 | Cookie 更新、Apple 登录、2FA、App Password |
| `/settings` | 系统设置 | 服务健康、版本、数据目录、配置重载 |

未知路由返回应用内 404；账户不存在时返回账户列表并显示错误通知。

### 7.1 应用框架

- 桌面端：固定侧栏、顶部账户上下文、主工作区。
- 移动端：紧凑顶部栏、抽屉导航、单列内容。
- 当前账户页面共享账户标题和 `别名 / 收件箱 / 凭据` 标签页。
- 页面标题使用正常工具级字号，不使用 Hero 或超大标题。

### 7.2 账户工作台

- 顶部状态带：账户总数、正常、待登录、异常。
- 账户表格：名称、邮箱、区域、状态、别名、认证能力、最后校验、操作。
- 行操作：打开、更新 Cookie、设置 App Password、删除。
- 添加账户对话框：名称、iCloud 邮箱、区域、可选代理。
- 空状态只提供“添加账户”主要操作。
- 错误状态展示 `last_error` 摘要和对应修复操作。

### 7.3 别名管理

- 搜索邮箱和标签。
- `全部 / 使用中 / 已停用` 分段筛选。
- 表格展示邮箱、标签、状态、创建时间和操作。
- 邮箱提供复制图标按钮和成功反馈。
- 创建对话框只输入可选标签。
- 删除显示邮箱二次确认；停用和恢复提交后刷新列表。
- 不在页面展示或要求用户操作 `anonymousId`。

### 7.4 收件箱

- 筛选项：别名、时间范围 `1/3/7/14/30` 天、数量 `10/20/50`。
- 显示实际读取方式：`IMAP` 或 `Web API`。
- 列表展示发件人、主题、收件地址、时间和受控长度预览。
- 选择邮件后在侧栏或移动端独立区域显示现有摘要信息。
- 首期不承诺完整正文和附件；需要完整正文时另行设计后端端点。
- 手动刷新不打乱筛选条件。

### 7.5 凭据管理

- 只展示“已配置/未配置”和最近校验状态，不回显凭据。
- Cookie 输入支持 Header、JSON map、浏览器 Cookie 数组。
- App Password 表单包含 iCloud 邮箱和密码。
- Apple 登录先提交密码，需要时再打开 OTP 对话框。
- 密码字段提交后立即清空，不写入 URL、localStorage 或持久查询缓存。

### 7.6 系统设置

- 展示后端健康状态和版本。
- 展示脱敏后的数据目录位置。
- 提供配置重载命令按钮。
- API Token 使用全局会话配置入口；仅在页面内存保存，验证后重取活动查询，`api_token_invalid` 与 iCloud Cookie 会话失效分别处理。

## 8. API 集成规范

所有接口统一解析：

```ts
type ApiResponse<T> = {
  success: boolean;
  code?: string;
  data?: T;
  message?: string;
};
```

### 8.1 当前接口与页面映射

| 后端接口 | 前端用途 |
| ------------------------------------- | ------------------------- |
| `GET /api/accounts` | 账户列表和状态带 |
| `POST /api/accounts` | 添加账户 |
| `DELETE /api/accounts/:id` | 删除账户 |
| `POST /api/accounts/:id/password` | 设置并验证 App Password |
| `PUT /api/accounts/:id/cookies` | 更新并验证 Cookie |
| `POST /api/accounts/:id/login/start` | 提交 Apple 密码并启动登录 |
| `POST /api/accounts/:id/login/verify` | 提交一次性 OTP challenge |
| `POST /api/create` | 创建别名 |
| `GET /api/aliases` | 别名列表 |
| `POST /api/aliases/:id/deactivate` | 停用别名 |
| `POST /api/aliases/:id/reactivate` | 恢复别名 |
| `DELETE /api/aliases/:id` | 删除别名 |
| `GET /api/inbox` | 邮件列表 |
| `GET /api/health` | 服务版本和配置可用状态 |
| `POST /api/reload` | 重载账户配置 |

### 8.2 Query Key

```text
['health']
['accounts']
['account', accountId]
['aliases', accountId]
['inbox', accountId, alias, limit, days]
```

### 8.3 请求规则

- GET 请求可以对网络错误重试 1 次，401/403/4xx 不重试。
- 创建、删除、登录、停用和恢复操作不自动重试。
- 页面卸载或筛选条件改变时通过 `AbortSignal` 取消旧请求。
- 401/403 映射为“会话过期”，提供更新 Cookie 操作。
- 502 映射为 Apple 服务错误，保留经过清理的后端错误摘要。
- 表单校验错误不使用全局通知，直接显示在对应字段旁。
- 所有 mutation 成功后精确失效相关 Query Key。

## 9. 视觉与交互规范

- 中性浅色背景，蓝色主操作，绿色成功，琥珀色警告，红色危险。
- 不使用单一色调铺满页面，不使用装饰性渐变球或大面积插画。
- 表格、筛选栏和标签页保持稳定尺寸，加载状态不能引发布局跳动。
- 工具操作优先使用 Lucide 图标；陌生图标必须提供 tooltip 和无障碍名称。
- 二元设置使用开关或复选框；模式选择使用分段控件；固定范围使用菜单。
- 危险操作使用红色语义和明确目标名称，不依赖颜色作为唯一提示。
- 不在页面放置大段“如何使用”说明，必要信息通过字段标签、校验和状态反馈表达。
- 文本不得溢出按钮、表格单元格、对话框或移动端容器。

### 9.1 响应式基线

- 桌面：`1440 x 900`
- 小型桌面/平板：`1024 x 768`
- 移动端：`390 x 844`

所有核心页面必须通过 Playwright 截图检查，确认没有重叠、横向溢出和不可操作控件。

### 9.2 无障碍要求

- 完整键盘导航和可见焦点。
- 表单字段具有显式 label 和错误关联。
- 对话框正确管理焦点并支持 Escape 关闭，危险提交期间除外。
- 状态图标同时提供文本。
- 正文和交互控件满足 WCAG AA 对比度。
- 动态通知通过合适的 live region 呈现。

## 10. 实施阶段

### Phase 0 - 后端契约与安全整改（2-4 人日）

- [x] BE-001 收紧监听地址和远程访问策略
- [x] BE-002 实现安全账户 DTO
- [x] BE-003 统一账户配置 schema
- [x] BE-004 修复并发 Cookie 和持久化
- [x] BE-005 增加输入边界
- [x] BE-006 修复 IMAP 关键逻辑
- [x] BE-007 修复 Web 邮件回退
- [x] BE-008 补全账户邮箱字段
- [x] BE-009 完成两阶段登录
- [x] BE-010 增加健康检查
- [x] 更新 README 和 API.md
- [x] 增加后端单元测试
- [x] 在具备 `gcc` 的环境运行 `go test -race ./...`

完成门槛：安全 DTO 和接口契约确定；测试通过；真实账户完成 Cookie、IMAP、Web 回退和 2FA 冒烟验证。

### Phase 1 - 前端基础工程（1-2 人日）

- [x] FE-001 创建 `web/` Vite + React + TypeScript 工程
- [x] FE-002 配置 lint、format、Vitest 和 Playwright
- [x] FE-003 建立设计令牌、全局样式和响应式应用框架
- [x] FE-004 建立路由、错误边界和 Not Found 页面
- [x] FE-005 实现类型化 API 客户端和 Zod 响应校验
- [x] FE-006 建立 TanStack Query 和 MSW fixtures
- [x] FE-007 实现通知、确认对话框、加载和空状态组件

完成门槛：所有计划路由可访问；API mock 可切换成功、空数据和错误状态；三种基线视口无布局问题。

### Phase 2 - 账户工作台（2-3 人日）

- [x] FE-101 实现账户状态带和账户列表
- [x] FE-102 实现账户状态、认证能力和错误展示
- [x] FE-103 实现添加账户表单
- [x] FE-104 实现删除账户确认流程
- [x] FE-105 实现账户上下文和详情标签导航
- [x] FE-106 覆盖账户空状态、离线和后端错误

完成门槛：用户可以完成账户查看、添加、打开和删除；敏感字段不进入前端响应。

### Phase 3 - 凭据与登录（包含在账户阶段估算内）

- [x] FE-201 实现 Cookie 多格式解析和更新（已完成）
- [x] FE-202 实现 App Password 设置和连接校验状态（已完成）
- [x] FE-203 实现 Apple 密码登录（已完成）
- [x] FE-204 实现 OTP challenge 对话框和过期处理（已完成）
- [x] FE-205 实现会话过期到凭据页面的恢复入口（已完成）
- [x] FE-206 验证密码和 Cookie 不被日志或持久缓存保存（已完成）

完成门槛：Cookie、App Password、无 2FA 登录和有 2FA 登录均通过真实账户验证。

### Phase 4 - 别名管理（1-2 人日）

- [x] FE-301 实现别名列表、搜索和状态筛选
- [x] FE-302 实现创建别名
- [x] FE-303 实现邮箱复制反馈
- [x] FE-304 实现停用和恢复
- [x] FE-305 实现删除二次确认
- [x] FE-306 实现会话失效和 Apple 服务错误恢复

完成门槛：所有 HME 操作可完成；操作后状态与 Apple 服务端一致；创建操作不会因前端重试产生重复别名。

### Phase 5 - 收件箱（1-2 人日）

- [x] FE-401 实现账户和别名筛选
- [x] FE-402 实现日期和数量选项
- [x] FE-403 实现邮件摘要列表和预览区域
- [x] FE-404 展示 IMAP/Web API 实际读取方式
- [x] FE-405 实现刷新、空状态、超时和回退错误
- [x] FE-406 验证超长主题、发件人和正文预览布局

完成门槛：IMAP 和 Web API 两条路径均能正确展示；别名筛选有效；移动端无横向溢出。

### Phase 6 - 系统与生产集成（2-3 人日）

- [x] FE-501 实现健康状态和配置重载
- [x] FE-502 配置 Vite 开发代理
- [x] FE-503 实现 `go:embed` 静态资源服务
- [x] FE-504 更新 Dockerfile 为 Node + Go 多阶段构建
- [x] FE-505 更新本地构建脚本和 README
- [x] FE-506 配置 SPA fallback、缓存和安全响应头
- [x] FE-507 完成生产二进制冒烟测试

完成门槛：单个 Go 二进制可以打开 UI 并完成所有流程；Docker 挂载数据目录后行为一致。

### Phase 7 - 测试、可访问性与发布验收

- [x] QA-001 单元测试覆盖 Cookie 解析、DTO schema、错误映射
- [x] QA-002 组件测试覆盖核心表单和危险确认
- [x] QA-003 E2E 覆盖账户、凭据、别名和收件箱主流程
- [x] QA-004 E2E 覆盖 401、403、502、超时和空数据
- [x] QA-005 Playwright 三视口截图检查
- [x] QA-006 键盘导航和焦点检查
- [x] QA-007 敏感数据响应、缓存和日志审计
- [x] QA-008 真实 iCloud 账户手工冒烟测试（线上受控账户已配置并稳定使用一段时间；仅记录脱敏结论）

完成门槛：满足第 13 节 Definition of Done，且没有 P0/P1 缺陷。

### Phase 8 - 操作审计、移动端适配与错误体验（进行中）

- [x] AUD-001 扩展所有操作记录的请求与响应审计。为 API 操作和定时任务统一记录操作时间、操作类型、关联请求 ID、请求参数快照、返回响应快照、HTTP 状态、业务错误码、耗时和重试次数；请求参数与响应按字段白名单和脱敏规则持久化，Cookie、Apple ID 密码、App Password、API Token、OTP、代理认证信息、完整别名地址、邮件正文及上游原始响应体不得写入操作日志。需要定义日志 schema、保留周期、查询 API/UI 和迁移策略，并补充脱敏、失败路径和并发写入测试。
- [x] FE-601 完成全平台移动端浏览器适配。覆盖账户、凭据、别名、收件箱、设置和操作日志页面，以及添加账户、更新 Cookie、创建/删除别名、查看审计记录等关键流程；窄屏表格切换为标签/值布局，收件箱在移动端以独立详情页呈现，导航使用可收起菜单。对话框使用安全区域与 `visualViewport` 高度约束，并可在内部滚动以保证软键盘出现时提交操作仍可触达；四个定义视口均通过无横向溢出和关键操作可达性回归。
- [x] ERR-001 细化 Apple 上游接口错误契约与页面提示。服务端按操作阶段和可验证证据将错误映射为稳定、脱敏的业务错误码及 `retryable`/建议动作，不得仅因 HTTP `403` 就判定 Cookie 会话失效；至少区分已确认的会话失效、账号权限或 iCloud+ 资格不足、别名总量/每日额度限制、频率或风控限制、设备信任/二次验证、请求参数或状态冲突、Apple 服务暂时不可用/超时，以及无法安全定性的上游拒绝。前端按业务错误码显示不同的精细说明、下一步操作和重试策略；不展示 Apple 原始响应体、Cookie、Token 或未经验证的归因。覆盖别名、凭据登录、收件箱 Web API 等涉及 Apple 服务的页面，更新 API 文档、Mock 场景和错误映射测试。
- [x] QA-009 为 AUD-001 建立审计契约和安全回归：覆盖成功、参数校验失败、Apple 上游拒绝、超时和定时任务；断言允许字段可追溯、敏感字段不会进入数据文件、API 响应、浏览器缓存、日志、截图或测试报告。
- [ ] QA-010（进行中）为 FE-601 建立移动端验收：已在 `web/e2e-mock/mobile-acceptance.spec.ts` 为 `375 x 667`、`390 x 844`、`768 x 1024` 和桌面基线建立 Playwright 检查，覆盖账户添加、别名创建、凭据更新、收件箱筛选和设置日志入口的横向溢出、可达性及对话框键盘焦点恢复；仍需在 iOS Safari 与 Android Chrome 真机或云设备完成关键流程手工冒烟，确认焦点/软键盘行为和关键提交操作可见可用。
- [x] QA-011 为 ERR-001 建立跨层错误矩阵回归：对每类业务错误码分别验证后端响应、前端提示、可执行恢复动作、自动重试边界和敏感信息脱敏；特别覆盖“别名列表可读但创建 `generate` 返回 `403`”时页面不得提示 Cookie 已失效。

完成门槛：每条可审计操作均有结构化、可查询且不泄露敏感信息的记录；核心管理流程在定义的移动端浏览器和视口下可完整完成；Apple 上游错误具有可验证、可恢复且不误导用户的差异化提示。

## 11. 测试策略

| 层级 | 工具 | 覆盖重点 |
| ------------ | --------------------- | ------------------------------------------ |
| Go 单元测试 | `go test` | 配置、脱敏、参数边界、邮件解析、登录状态机 |
| Go 竞态测试 | `go test -race` | Manager、Cookie 刷新、并发 API 请求 |
| 前端单元测试 | Vitest | schema、格式化、Cookie 输入转换、错误映射 |
| 组件测试 | Testing Library + MSW | 表单、表格、对话框、加载和错误状态 |
| E2E | Playwright | 用户主流程、路由、响应式和可访问性 |
| 真实服务验证 | 手工受控测试 | Apple 私有 API、IMAP、2FA、Cookie 轮换 |

真实 Apple 凭据不得进入仓库、fixture、CI 日志或截图。CI 使用 MSW/后端 stub；真实服务验证在本地受控环境完成。

## 12. 风险登记

| 风险 | 影响 | 应对 |
| ----------------------------------- | -------------------- | ---------------------------------------------------------------------------------- |
| Apple 私有 API 或 build number 变化 | HME/Web 邮件功能失效 | 将端点和构建号集中管理；增加明确错误和真实账户冒烟流程 |
| Cookie 有效期短 | 用户频繁遇到 401/403 | 统一会话过期状态和快速更新 Cookie 流程 |
| SRP/2FA 协议变化 | 密码登录不可用 | Cookie 登录保持可用；登录状态机独立测试 |
| Web API 缺少收件人字段 | 别名过滤不可靠 | 仅解析明确收件人并做精确匹配；缺少字段时返回显式错误；真实账户冒烟继续确认响应字段 |
| 邮件正文过大或 MIME 复杂 | 响应慢、界面卡顿 | 服务端限制预览、分页和正文大小；前端限制渲染长度 |
| 敏感信息泄漏 | 账户和邮件安全事故 | 安全 DTO、同源访问、日志审计、浏览器不持久化凭据 |
| 缺少真实 Apple CI | 回归发现较晚 | 保留 mock 契约测试和发布前手工冒烟清单 |

## 13. Definition of Done

版本可以交付必须同时满足：

- 当前后端所有计划内能力都可以从 UI 完成。
- 默认仅本机访问，远程模式有明确访问控制。
- 账户和登录响应不包含 Cookie、App Password 或 Apple 密码。
- 密码和 Cookie 不进入 URL、localStorage、日志、截图或测试 fixture。
- 所有页面具备加载、空数据、错误和会话过期状态。
- 非幂等操作不会被前端自动重试。
- Go 单元测试和竞态测试通过。
- 前端类型检查、构建、单元测试和 E2E 通过。
- `1440x900`、`1024x768`、`390x844` 无重叠和横向溢出。
- 键盘操作、焦点管理、字段标签和颜色对比满足要求。
- 单 Go 二进制和 Docker 两种部署方式完成冒烟验证。
- README、API.md 和本文档与实际实现一致。

## 14. 进度记录

状态枚举：`未开始`、`进行中`、`已完成`、`阻塞`。

| 阶段 | 状态 | 开始日期 | 完成日期 | 验证/备注 |
| ------------------ | ------ | ---------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Phase 0 后端整改 | 进行中 | 2026-07-31 | - | BE-001 至 BE-010 已完成；GitHub Actions Run 25 已通过 Linux `go test -race ./...`，真实 Apple Cookie/IMAP/Web/2FA 冒烟仍待 Phase 0 验收 |
| Phase 1 前端基础 | 已完成 | 2026-08-01 | 2026-08-01 | FE-001 至 FE-007 已完成；计划路由、API 契约、Query/MSW 与通用状态组件均通过自动化验证 |
| Phase 2 账户工作台 | 已完成 | 2026-08-01 | 2026-08-01 | FE-101 至 FE-106 已完成；账户查看、添加、打开、删除、详情导航及空数据/离线/后端错误均有自动化覆盖 |
| Phase 3 凭据与登录 | 进行中 | 2026-08-01 | - | FE-201 至 FE-206 已完成并通过自动化安全审计；受控真实账户的 Cookie、App Password、无 2FA 与 2FA 登录仍待验收 |
| Phase 4 别名管理 | 已完成 | 2026-08-01 | 2026-08-01 | FE-301 至 FE-306 已完成；别名页会话失效与 Apple 服务错误恢复已补齐并通过自动化验证 |
| Phase 5 收件箱 | 已完成 | 2026-08-01 | 2026-08-01 | FE-401 至 FE-406 已完成；筛选、摘要预览、读取方式、恢复状态和长文本响应式布局均有自动化覆盖 |
| Phase 6 生产集成 | 进行中 | 2026-08-01 | - | FE-501 至 FE-507 已完成；Windows `build.ps1` 已生成 Linux amd64 嵌入式二进制并校验资源镜像，`scripts/windows-release-smoke.ps1` 已验证原生临时二进制的嵌入资源、鉴权、缓存、安全头、SPA fallback 与重启持久化；README 中英文已补充 Linux/macOS/Windows、Docker/Compose、反向代理、备份升级和安全部署方案；GitHub Actions Run 25 已通过 Bash 构建、ELF 校验、独立 Linux 二进制运行烟测和 Docker 挂载数据目录烟测，本地 Docker 验收仍待完成 |
| Phase 7 发布验收 | 已完成 | 2026-08-01 | 2026-08-14 | QA-001 至 QA-008 已完成并保留自动化及三视口视觉证据；QA-008 已在线上受控账户环境完成，未记录账号、凭据或原始响应 |
| Phase 8 操作审计、移动端适配与错误体验 | 进行中 | 2026-08-14 | - | `AUD-001`、`QA-009`、`ERR-001`、`QA-011` 与 `FE-601` 已完成。移动端已覆盖账户、凭据、别名、收件箱、设置和操作日志页面，关键对话框依据安全区域和 `visualViewport` 内部滚动；四个视口的 Playwright 回归均验证无横向溢出和关键操作可达。iOS Safari 与 Android Chrome 真机或云设备冒烟仍待由 `QA-010` 完成；GitHub Actions Run 48 已通过 Linux 竞态、前端回归、发布二进制与 Docker 烟测。 |

当前开发断点（2026-08-15）：`BE-001` 至 `BE-010` 已完成；Phase 1 的 `FE-001` 至 `FE-007`、Phase 2 的 `FE-101` 至 `FE-106`、Phase 3 的 `FE-201` 至 `FE-206`、Phase 4 的 `FE-301` 至 `FE-306`、Phase 5 的 `FE-401` 至 `FE-406` 和 Phase 6 的 `FE-501` 至 `FE-507` 已完成；Phase 7 的 QA-001 至 QA-008 已完成；Phase 8 的 `AUD-001`、`QA-009`、`ERR-001`、`QA-011` 与 `FE-601` 已完成，剩余 `QA-010` 真机验收。GitHub Actions Run 25 已通过 Linux `go test -race`、前端回归、Linux amd64 ELF 构建、独立 Linux 二进制运行烟测和 Docker 挂载重启烟测；Windows 原生发布烟测已经验证嵌入资源、鉴权、SPA fallback、缓存、安全头、404 和重启持久化。QA-008 已在线上受控账户环境完成，且未向仓库记录账号、凭据或原始响应。下次继续完成本地 Docker 发布运行时与受控真实 iCloud 账户下的 Phase 0/3 冒烟；具体步骤见 `RELEASE_SMOKE_CHECKLIST.md`；真实账号验收继续禁止删除、停用或恢复别名等破坏性操作。

## 15. 决策与变更记录

| 日期 | 变更 | 原因 |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-08-14 | 完成 `ERR-001` 与 `QA-011`：新增隐私安全的 Apple 上游错误分类和稳定业务错误契约，按已确认会话失效、资格、别名额度、频率/风控、设备信任、参数/状态冲突、服务不可用和未知拒绝分别返回 `code`、`action` 与 `retryable`；别名、收件箱、自动化和密码登录均接入，前端只会对 `icloud_session_expired` 打开 Cookie 恢复入口。新增 Mock、单元和浏览器回归，覆盖“列表可读但创建 `generate` 返回普通 `403`”不误报 Cookie 失效。验证通过：`npm run lint`、`npm run typecheck`、`npm run test`（174/174）、`npm run test:e2e:mock`（41/41）、`npm run build`、相关 Prettier 检查、`git diff --check`，以及临时 Go `1.26.6` 的 `gofmt`、`go vet ./...`、`go test ./...` 和 `go build ./...`；GitHub Actions Run 48 已通过 Linux `go test -race ./...`、前端/浏览器回归、发布二进制与 Docker 烟测及 `main` 镜像发布。 | 将 Apple 上游拒绝从基于状态码的猜测改为可验证的错误边界，避免错误引导用户反复更新 Cookie，同时保留可恢复的服务异常重试策略。 |
| 2026-08-14 | 推进 `QA-010`：新增 `web/e2e-mock/mobile-acceptance.spec.ts`，在 `375 x 667`、`390 x 844`、`768 x 1024` 与 `1440 x 900` 依次覆盖账户添加、别名创建、凭据更新、收件箱筛选和设置操作日志入口；断言页面无横向溢出、关键控件可滚动至完整可视区域，且添加账户与创建别名对话框可由键盘打开、Escape 关闭并恢复焦点。验证通过：`npx prettier --check e2e-mock/mobile-acceptance.spec.ts`、`npm run typecheck`、`npm run lint`、`npm run test:e2e:mock`（38/38）。iOS Safari 与 Android Chrome 真机或云设备手工冒烟仍待完成。 | 将已有零散的 `390 x 844` 回归扩展为统一、可重复的四视口移动端验收基线，同时保留只能由真实浏览器验证的软键盘和平台焦点行为。 |
| 2026-08-14 | 完成 `QA-008`：线上受控 iCloud 账户已配置并稳定使用一段时间，真实环境手工冒烟结论已记录；未将账号、Cookie、密码、验证码、原始响应或邮件内容写入仓库。 | 以线上受控环境的持续使用结果完成真实账户验收，同时保持敏感数据不落库、不入日志和不进入版本控制。 |
| 2026-08-14 | 新增 `ERR-001` 与 `QA-011`：以操作阶段和可验证证据映射 Apple 上游错误码，前端按错误码显示差异化提示、恢复动作和重试边界；明确禁止将单独的 HTTP `403` 一律解释为 Cookie 会话失效。 | Apple 接口的权限、额度、风控、设备信任、会话和服务可用性问题需要区分呈现，避免用户在非会话问题上反复更新 Cookie。 |
| 2026-08-14 | 新增 Phase 8 待开发任务：`AUD-001` 要求为所有 API 操作与定时任务保存经过字段级脱敏的请求参数和返回响应审计快照；`FE-601` 要求全平台移动端浏览器适配，并补充 `QA-009`、`QA-010` 作为安全与移动端验收门槛。 | 账号权限问题的排查需要可追溯的操作证据；现有桌面优先界面需要形成完整的移动端浏览器使用保障。 |
| 2026-08-04 | 完成 README 跨平台部署文档：补充 Linux 原生二进制与 systemd、macOS launchd、Windows PowerShell/任务计划、Docker/Compose、Caddy/Nginx HTTPS、首次管理员认证、已有数据目录复用、备份恢复、升级和破坏性操作警告；同步修正 GitHub Releases、源码仓库和 GHCR 地址。提交 `4e8ab02` 已推送到 `main`。README Prettier、代码围栏、相对链接和 `git diff --check` 均通过；该提交触发的 GitHub Actions Run 26 截至记录时仍在排队 | 将部署文档从快速启动扩展为可直接执行的多平台生产部署与维护指南，降低错误使用空数据目录、暴露 HTTP 端口或误执行真实删除操作的风险 |
| 2026-08-04 | GitHub Actions Run 24（提交 `6f4e366`）已通过 Linux `go test -race ./...`、前端回归、Linux amd64 ELF 构建、Docker 挂载数据目录重启烟测，以及新增 `scripts/ci-linux-binary-smoke.sh` 覆盖的独立 Linux 二进制根页面、SPA 深链接、哈希资源缓存、健康检查、鉴权边界和 404 验证 | 对齐实际远程验收结果，并补齐 Docker 之外的独立 Linux 发布二进制运行契约 |
| 2026-08-01 | FE-306 远程 API Token 会话补强：服务端在 Bearer 校验失败时返回机器可识别的 `code: "api_token_invalid"`；前端 API 客户端解析该 code、仅从页面内存 Token provider 写入 `Authorization`，并排除其进入 iCloud Cookie 会话恢复分支。应用顶栏新增全局钥匙入口和密码型令牌对话框，验证时仅用临时 ref 调用 `/api/health`，成功后写入模块内存并重取活动查询；输入在提交后立即清空，令牌不写入 URL、Web Storage、Cache Storage、IndexedDB、Query/Mutation 缓存、DOM 或通知。新增单元与 Playwright 回归，覆盖初始拒绝、Bearer 重试、无持久化、刷新重新输入、移动端无横向溢出及 iCloud 401/403 既有恢复语义。验证通过：`go test ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（133/133）、`npm run test:e2e`（7/7）、`npm run test:e2e:mock`（30/30）、`npm run build` 和 `scripts/windows-release-smoke.ps1 -Version fe306-api-token-smoke -SkipNpmCi -Port 18083`。 | 让 Docker 或非回环部署的内置浏览器 UI 能安全完成 API 鉴权，同时不把部署令牌误判为 iCloud Cookie 失效，也不扩大浏览器端令牌持久化暴露面 |
| 2026-08-01 | 路由级前端拆包：`web/src/app/router.tsx` 将账户工作台、账户详情、别名、收件箱、凭据和设置页改为 React Router 原生 `route.lazy`，保留根级错误边界并提供初始模块加载 fallback。应用壳层依据路由导航状态在首次加载时显示可访问状态、在页面切换时保留当前页面，避免空白内容区；常规 Playwright 回归会延迟设置页模块响应，断言当前账户页仍可见且内容区设置 `aria-busy`，再确认模块返回后完成导航；另模拟模块响应 503，验证根级错误边界可返回账户页或重新加载页面获取更新资源，错误标题会自动获得焦点，且错误页在移动基线视口无横向溢出。生产入口应用代码由 246.20 kB（gzip 74.66 kB）降至 195.71 kB（gzip 62.33 kB）；账户工作台、详情、别名、收件箱、凭据和设置页分别产出 10.03、2.65、11.24、6.73、15.12、3.52 kB 的按需 chunk，Vite 无体积告警。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（129/129）、`npm run build`、`npm run test:e2e`（6/6）、`npm run test:e2e:mock`（30/30）、`go test ./...`，以及 `build.ps1 -Version fe306-route-error-focus-smoke -SkipNpmCi -SkipUpx`。发布二进制为 26.93 MiB，`web/dist` 与 `internal/webui/dist` 的 21 个 Vite 产物 SHA-256 一致（排除受版本控制的占位文件）。 | 继续降低首次进入应用的解析与下载成本，将低频页面代码延后到真正访问时加载，同时维持可访问加载反馈、会话恢复和嵌入式发布的完整性 |
| 2026-08-01 | Docker 嵌入资源隔离：`.dockerignore` 排除主机生成的 `internal/webui/dist/*`；Docker 构建器在复制前端阶段产物前显式清空并重建该目录，确保镜像只嵌入当前 Vite 构建的哈希资源。CI 在构建 Docker 镜像前写入一个旧资源哨兵，`scripts/ci-docker-smoke.sh` 断言该路径仍为 404，从实际镜像行为防止旧资源泄漏。验证通过：CI YAML 的 Prettier 检查和 `go test ./...`。当前 Windows 环境没有 Docker 或 Bash，镜像构建与哨兵断言待首个远程 CI 执行。 | 避免开发机残留的被忽略前端产物随 `COPY . .` 混入发布镜像，减少镜像冗余并保证发布二进制只包含当前入口引用的资源 |
| 2026-08-01 | 前端生产拆包：`web/vite.config.ts` 为 React、表单校验、TanStack Query 和 UI 原语配置稳定的 Rollup `manualChunks`。生产构建入口包从约 531.13 kB（gzip 164.59 kB）降至 246.20 kB（gzip 74.66 kB）；新增 `form-vendor` 97.01 kB（gzip 28.70 kB）、`react-vendor` 94.41 kB（gzip 31.95 kB）、`ui-vendor` 47.38 kB（gzip 16.26 kB）和 `query-vendor` 44.56 kB（gzip 13.70 kB），CSS 为 31.72 kB（gzip 5.61 kB）。Vite 默认 500 kB chunk 告警已消失。验证通过：`npm run build`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（129/129）、`npm run test:e2e`（3/3）、`npm run test:e2e:mock`（30/30）、`go test ./...`，以及 `build.ps1 -Version fe306-chunks-smoke -SkipNpmCi -SkipUpx`。该发布构建生成 26.92 MiB Linux amd64 二进制，`web/dist` 与 `internal/webui/dist` 的 8 个 Vite 产物 SHA-256 完全一致（排除受版本控制的占位文件）。 | 将长期稳定、跨路由复用的依赖从应用代码中分离，缩小首次应用代码更新的下载量，并消除构建期体积告警，同时用完整前端与嵌入式发布回归确认加载、交互和交付链未受影响 |
| 2026-08-01 | CI 与 tag 发布：新增 `.github/workflows/ci.yml`。PR、`main` 和 `v*` tag 都会运行 Go 静态分析与竞态测试、前端格式/Lint/类型/单元/两套 Playwright 回归、Bash 嵌入式 Linux 发布构建；随后构建 Docker 镜像，并通过 `scripts/ci-docker-smoke.sh` 验证 API Token 的 401/200 边界、SPA fallback、哈希资源缓存、安全响应头、未知路径 404 和挂载数据目录重启持久化。`v*` tag 在前置检查完成后构建 Linux/Darwin/Windows 二进制、推送 GHCR 版本镜像及稳定版 `latest`，并创建或更新 GitHub Release。失败时保留 Playwright 诊断和 Linux 二进制 artifact；临时 Token 由 CI 运行时生成并遮蔽，测试账户不含 Cookie、密码或 App Password。README 中英文同步 CI 契约。工作流 YAML 已通过 Prettier 解析；当前环境没有 Bash、Docker、`gcc`、WSL 发行版或 `actionlint`，尚未执行远程工作流 | 将原先仅在文档中承诺的自动发布变为可审计的流水线，并把本机无法执行的 Linux、Docker 和竞态验收交由无真实账户数据的 CI 覆盖 |
| 2026-08-01 | 发布构建可移植性：新增 `build.ps1`，在 Windows PowerShell 中执行与 `build.sh` 等价的前端构建、嵌入资源同步和 Linux amd64 交叉编译；Bash、PowerShell 与 Docker 构建均显式使用 `-buildvcs=false`，避免源码包缺失 `.git` 元数据时 Go VCS stamping 使发布构建失败。新增 `RELEASE_SMOKE_CHECKLIST.md`，定义 Docker、发布二进制、Cookie、IMAP、Web 回退、无 2FA/2FA 和浏览器敏感数据审计的受控验收步骤；README 中英文补充 Windows 构建与清单入口。验证通过：`./build.ps1 -Version qa-win-smoke -SkipNpmCi -SkipUpx`，生成 26.92 MiB 的 ELF Linux amd64 `build/icloud-hme`；构建元数据显示 `CGO_ENABLED=0`、`GOOS=linux`、`GOARCH=amd64`，`web/dist` 与 `internal/webui/dist` 逐文件 SHA-256 一致；`go test ./...`、`go vet ./...`、PowerShell 语法解析和文档 Prettier 均通过。当前 Windows 环境仍无 Docker、Bash、`gcc` 和真实账户配置，未执行 Docker/Bash、竞态或真实 iCloud 验收 | 在不要求 Windows 安装 Bash 的前提下提供可验证的发布构建路径，并把剩余外部验收转化为不泄露凭据的可重复清单 |
| 2026-08-01 | QA-001 至 QA-007：补充 `alias-forbidden` 的 403 会话恢复 E2E；新增 `web/artifacts/qa005/capture.cjs`，为长文本收件箱和设置页生成 `1440x900`、`1024x768`、`390x844` 六张截图并断言无横向溢出。视觉检查发现设置页健康信息最后一项只占半行，现改为跨越完整网格行并以 Mock E2E 固化。新增浏览器级键盘回归：别名删除确认可通过 Escape 关闭且焦点返回触发按钮，OTP 对话框自动聚焦验证码输入框并在 Escape 后返回登录按钮；通用确认框和账户删除也使用显式焦点恢复。敏感数据审计继续覆盖 DOM、URL、Web Storage、Cache、IndexedDB、资源 URL、Query/mutation 缓存和控制台。验证通过：`gofmt -l`、`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（129/129）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（30/30）；生产主包 531.13 kB（gzip 164.59 kB），仍有 Vite 默认 500 kB chunk 阈值警告 | 完成可重复的发布前自动化、视觉和键盘可访问性证据，修复受控对话框关闭后焦点丢失及设置页网格残缺 |
| 2026-08-01 | FE-502 至 FE-507：Vite 开发服务器新增可由 `VITE_API_PROXY_TARGET` 覆盖的 `/api`→`127.0.0.1:8081` 代理，Mock 模式保持 MSW；新增 `internal/webui` 嵌入资源 FS，Go 服务支持根页面、HTML 导航的 SPA fallback 与 `/assets/*` 静态资源，未知 API/静态文件仍为 404。HTML 使用 `no-cache`，哈希资产使用一年不可变缓存，所有响应添加 CSP、反嵌入、禁 MIME 嗅探、Referrer 与 Permissions 安全头。Dockerfile 改为 Node 22.12 + Go 多阶段构建，`build.sh` 和 README 同步为先构建前端再嵌入 Go。生产二进制烟测以临时 `web/dist` 编译版本 `fe507-smoke`，验证 `/`、深链接、哈希资产和 `/api/health` 均为 200。验证通过：`gofmt -l`、`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（129/129）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（27/27）；Docker 与 Bash 在当前 Windows 环境不可用，镜像构建和 `build.sh` 运行待外部环境验收；前端生产构建主包约 530.81 kB（gzip 164.43 kB），仍有 Vite 默认 500 kB chunk 阈值警告 | 使发布二进制可同源提供前端与 API，并在开发、生产、缓存与浏览器安全边界上建立可验证的完整交付链 |
| 2026-08-01 | FE-501：将 `/settings` 从占位页替换为服务设置页，接入 `GET /api/health` 展示服务、版本、健康状态、配置可用性和不暴露真实路径的配置位置；接入 `POST /api/reload`，成功后失效健康和账户查询并通知用户，失败保留可重试操作。组件测试覆盖健康成功/失败、重载成功刷新和重载错误；普通与 Mock E2E 覆盖设置路由、错误恢复和 `1440x900`、`1024x768`、`390x844` 无横向溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（129/129）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（27/27）；生产构建 CSS 约 31.67 kB，主包约 530.81 kB（gzip 164.43 kB），仍有 Vite 默认 500 kB chunk 阈值警告 | 让服务状态和配置重载具备清晰、可恢复且不暴露服务端敏感路径的前端入口 |
| 2026-08-01 | FE-406：新增 `inbox-long` Mock 场景，覆盖无空格的超长主题、发件人、收件人和多行正文预览；收件箱网格子项补充最小宽度约束，确保长文本可截断、换行或在预览正文内滚动。Mock E2E 覆盖 `1440x900`、`1024x768` 和 `390x844`，验证页面、列表和预览面板无横向溢出且正文仍可滚动。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（125/125）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（25/25）；生产构建主包约 527.83 kB（gzip 163.79 kB），仍有 Vite 默认 500 kB chunk 阈值警告 | 让邮件中的极端长文本不会破坏列表、详情预览或移动端页面布局，并确认正文预览的滚动边界 |
| 2026-08-01 | FE-405：收件箱标题新增固定 32px 刷新图标按钮，刷新期间保持当前结果和筛选参数、禁用重复触发并通过既有 Query key 重读服务端。空结果改为具名 `EmptyState`；API 客户端支持可选超时信号，收件箱请求固定为 15 秒，超时与 504 均展示“读取邮件超时”且不自动重试，路由切换的父 `AbortSignal` 仍按正常取消处理。Mock 新增仅收件箱 `inbox-error`（502 回退失败）和 `inbox-timeout`（504）场景，错误状态保留账户、别名、日期和数量筛选，并可手动重试恢复。组件、API 客户端、场景与 Mock E2E 覆盖刷新 URL 不变、空数据、客户端超时映射、502/504 错误、重试恢复和三种视口。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（125/125）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（24/24）；生产构建 CSS 约 29.92 kB，主包约 527.83 kB（gzip 163.79 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 让读取暂时失败具备明确、可恢复且不会丢失筛选上下文的用户路径，并避免无界请求一直占用界面 |
| 2026-08-01 | FE-404：收件箱标题区域基于接口响应的 `method` 直接显示实际读取方式，`imap` 显示带 Server 图标的“IMAP”，`web_api` 显示带 Globe 图标的“Web API”；加载和错误状态不保留旧方式，避免依据凭据能力猜测当前实际链路。Mock 新增独立 `web-api` 场景，仅改变收件箱响应方法并保持账户、别名与邮件数据可用；组件、场景和 Mock E2E 覆盖两条路径与响应式布局。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（121/121）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（23/23）；生产构建 CSS 约 29.77 kB，主包约 526.83 kB（gzip 163.45 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 让用户清楚看到本次邮件结果来自 IMAP 还是 Web API，而不将配置状态误当作实际读取路径 |
| 2026-08-01 | FE-403：收件箱筛选结果替换为可选择的邮件摘要列表和详情预览。列表展示发件人、主题、收件地址、时间及两行受控预览；点击行只更新组件内选中状态，预览区展示同一封邮件的发件人、收件地址、时间和完整服务端预览，不请求正文或附件。查询结果变化后选中邮件不存在时自动回退为首封，桌面使用稳定双栏布局，平板收紧列宽，移动端按列表后预览的顺序单列展示。Mock 夹具增加三封不同主题/收件人的邮件；组件与 Mock E2E 覆盖默认选中、切换、筛选组合和 `1440x900`、`1024x768`、`390x844` 无横向溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（120/120）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（22/22）；生产构建 CSS 约 29.31 kB，主包约 525.83 kB（gzip 163.13 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 在不暗示已支持完整邮件正文或附件的前提下，提供可扫描的多邮件摘要和可访问的选中预览工作流 |
| 2026-08-01 | FE-402：收件箱新增固定范围的时间与数量下拉选项，时间支持 `1/3/7/14/30` 天，数量支持 `10/20/50` 封。URL 中的 `days`、`limit` 只接受白名单值，非法值回退到 `7/20`；非默认值写入 URL，恢复默认值会移除参数。筛选变更复用 `['inbox', accountId, alias, limit, days]` Query key 和现有请求取消链，账户切换保留日期/数量并只清理旧别名。组件与 Mock E2E 覆盖请求参数、缓存命中回退、URL 保留、账户切换和 `1440x900`、`1024x768`、`390x844` 无横向溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（119/119）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（22/22）；生产构建 CSS 约 26.55 kB，主包约 523.50 kB（gzip 162.54 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 将后端 `1..365` 天和 `1..100` 封的宽范围契约收敛为首期明确选项，保证筛选可分享、可恢复且不会把非法参数传给服务端 |
| 2026-08-01 | FE-401：以账户详情路由作为当前账户筛选，收件箱页提供账户下拉切换和别名下拉筛选；切换账户会导航到目标账户的收件箱并清除旧别名，选择别名只更新 `alias` URL 参数且保留 `mock`、`source` 等无关参数。页面复用 `accounts`、`aliases` 和 `inbox` Query，查询 key 保持 `accountId/alias/limit/days`，由现有 `AbortSignal` 请求链取消失效筛选；已删除别名的深链接仍如实显示并继续按 URL 值查询。补齐普通 smoke 的 inbox 夹具和断言，新增组件测试与 Mock E2E，覆盖账户切换、别名请求参数、URL 保留和 `1440x900`、`1024x768`、`390x844` 无横向溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（119/119）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（22/22）；生产构建 CSS 约 26.49 kB，主包约 522.69 kB（gzip 162.32 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 将收件箱的账户与别名范围固定为可分享、可恢复且可取消旧请求的筛选基础，为日期、数量和邮件列表后续迭代提供稳定查询边界 |
| 2026-08-01 | FE-306：别名页在 `GET /api/aliases` 遇到 401/403 时复用 `AccountRequestErrorState` 的会话恢复入口，进入凭据页后返回原别名 URL；在 502 Apple 服务错误时保留当前账户上下文、显示清理后的错误摘要和“重新加载”按钮，并在服务恢复后通过 `mock=alias-error`→`mock=success` 的 Mock E2E 路径验证列表恢复。补充 `alias-error` 场景以保持账户接口成功、别名接口返回 502，新增组件/场景单测与 Mock E2E 覆盖，重新生成 `web/artifacts/fe306/` 三视口 expired/recovered 截图。验证通过：`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（117/117）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（21/21）；生产构建 CSS 约 25.77 kB，主包约 520.24 kB（gzip 161.72 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 让别名页在会话失效与 Apple 服务暂时错误两类恢复场景下都能保持账户上下文、可重试和服务端重读一致性 |
| 2026-08-01 | FE-305：别名列表操作列扩展为固定 32px 的停用/恢复与删除双图标按钮组，删除使用 Lucide Trash2 图标和包含目标邮箱的可访问名称。点击删除先打开 Radix Alert Dialog 二次确认，文案明确展示目标邮箱和不可恢复结果；提交期间锁定取消、Escape 关闭和重复提交，确认按钮在原尺寸内显示“处理中”。删除调用现有 `DELETE /api/aliases/:id`，请求体携带 `account_id`，mutation 显式禁用自动重试；成功后发送包含目标邮箱的通知并失效当前账户别名查询，从服务端重读后移除行，失败时保留对话框和别名行并展示清理后的错误。MSW 别名存储新增按账户持久化删除，fixture 和新建别名删除后都不会再出现在 `GET /api/aliases`，测试间可重置。组件测试覆盖确认文案、取消不删除、成功服务端刷新、pending 锁定、防重复提交、失败保留和单次请求；API 客户端测试覆盖删除路径编码与账户请求体；Mock E2E 覆盖确认、取消、成功删除、URL 上下文和三种视口无横向溢出。`1440x900`、`1024x768`、`390x844` 的确认态和删除后状态截图保存在 `web/artifacts/fe305/`，视觉抽查无重叠，移动端操作列保持两个 32px 图标按钮。验证通过：`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（115/115）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（19/19）；生产构建 CSS 约 25.77 kB，主包约 519.92 kB（gzip 161.63 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 让不可恢复的别名删除具备明确目标、可访问确认、失败可恢复和服务端重读确认，避免误删、重复写入或本地乐观状态与 Apple 侧最终状态不一致 |
| 2026-08-01 | FE-304：别名列表新增“操作”列，每行根据服务端状态展示固定 32px 的 Lucide PauseCircle/PlayCircle 图标按钮和包含目标邮箱的可访问名称；停用与恢复分别调用现有状态接口，mutation 显式禁用自动重试，提交期间禁用按钮并在原尺寸内显示加载图标。成功后失效当前账户别名查询并等待服务端重读，再发送包含目标邮箱的成功通知；失败时保留原状态、恢复操作能力并展示清理后的错误。MSW 别名存储按账户持久化状态变更并可在测试间重置。组件测试覆盖双向状态切换、服务端刷新、成功通知、失败保留和单次请求，API 客户端测试覆盖动态路径编码及账户请求体；Mock E2E 覆盖停用、恢复、URL 上下文、按钮边界不变和三种视口无横向溢出。`1440x900`、`1024x768`、`390x844` 的停用/恢复截图保存在 `web/artifacts/fe304/`，按钮实测均为 32x32px 且无布局重叠。验证通过：`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm test`（109/109）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（18/18）；生产构建 CSS 约 25.55 kB，主包约 518.79 kB（gzip 161.45 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 让别名生命周期操作具备明确、可访问且不会重复写入的行级反馈，并以服务端重读而非本地乐观推测确认 Apple 侧最终状态，同时保持桌面与移动布局稳定 |
| 2026-08-01 | FE-303：每个别名邮箱新增固定 32px 的 Lucide Copy 图标按钮和明确 tooltip；点击后使用标准 Clipboard API 写入完整邮箱，异步期间按钮禁用并显示加载图标，成功后在相同尺寸内切换为绿色 Check 图标两秒，同时通过 live region 通知“邮箱已复制”及目标地址；定时器在重复操作和组件卸载时清理。浏览器拒绝或不支持剪贴板写入时恢复 Copy 状态并发送权限错误通知，不伪报成功。桌面表格与移动端标签/值布局均为邮箱文本保留可收缩空间，长地址可换行且图标不会挤出单元格。组件测试覆盖 pending 锁定、准确写入、成功反馈和拒绝恢复；Mock E2E 授予 Chromium 实际 `clipboard-read`/`clipboard-write` 权限，写入后读回目标邮箱，并验证 Copy/Check 状态前后按钮边界完全一致、URL 不变及三种视口无横向溢出；`1440x900`、`1024x768`、`390x844` 实际成功态截图无重叠。验证通过：`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（106/106）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（17/17）；生产构建 CSS 约 25.44 kB，主包约 517.58 kB（gzip 161.21 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 让高频复制操作具备可访问、可验证且不造成表格位移的即时反馈，同时在浏览器权限不足时保持错误可见而不误导用户 |
| 2026-08-01 | FE-302：别名列表与空状态新增基于 Radix Dialog、React Hook Form 和 Zod 的创建入口；标签可选、提交前去除首尾空白，并按后端 `utf8.RuneCountInString` 契约限制为 200 个 Unicode code point。创建 mutation 显式禁用自动重试，提交期间锁定关闭和重复操作；成功后立即关闭并清空表单、发送包含新邮箱的成功通知、清除可能隐藏新别名的搜索/状态参数但保留其他 URL 参数，再失效当前账户别名查询从服务端重新读取，不做乐观更新；失败时保留标签和对话框供手动重试。Mock 层新增按账户隔离且可重置的别名存储，创建响应后的 `GET /api/aliases` 会返回带匿名 ID 的新行，空列表同样可创建首个别名。schema、API 客户端和组件测试覆盖 Unicode 边界、请求序列化、筛选清理、空数据创建、服务端刷新、失败保留与单次请求；Mock E2E 覆盖完整创建流程及三种视口对话框边界，`1440x900`、`1024x768`、`390x844` 实际截图无重叠或横向溢出。验证通过：`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（104/104）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（16/16）；生产构建 CSS 约 25.12 kB，主包约 516.06 kB（gzip 160.70 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 在不重复提交非幂等 Apple 写操作的前提下完成创建工作流，并确保成功结果只通过服务端重读进入列表，避免本地推测匿名 ID 或远端状态 |
| 2026-08-01 | FE-301：账户别名页改为通过 TanStack Query 读取 `GET /api/aliases`，提供邮箱、匿名 ID、标签、启用状态和创建时间的语义化列表；搜索覆盖邮箱、标签与匿名 ID，全部/使用中/已停用分段筛选展示实时数量，搜索词与状态分别同步到 URL 的 `q`、`status` 参数并保留 `mock` 等无关参数。加载、空数据、无匹配结果和可重试 API 错误均有独立状态；桌面和平板使用固定列宽表格，移动端转为标签/值布局。组件、Query 和 Mock E2E 覆盖渲染、组合筛选、URL 同步、空数据、失败重试及账户上下文保持；`1440x900`、`1024x768`、`390x844` 截图检查无重叠或横向溢出。验证通过：`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（98/98）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（15/15）；生产构建 CSS 约 25.05 kB，主包约 513.38 kB（gzip 160.24 kB），仍有 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6 | 将别名读取、搜索和状态过滤固定为可分享、可恢复且响应式稳定的工作流，为创建、复制和状态写操作建立统一列表基础 |
| 2026-08-01 | FE-206：新增账户提交改用请求期间的组件 `ref`，带用户名和密码的代理 URL 不再进入 TanStack mutation variables，成功或失败后均清空临时引用，失败时仍只在可编辑表单中保留输入；API fetch 统一使用 `cache: 'no-store'`，后端所有 `/api` 响应统一发送 `Cache-Control: no-store`。HME verbose 日志不再输出 Cookie 名称/值、请求头值、URL 查询串、账户标识或上游错误正文，只保留请求方法、无查询参数端点、Cookie 数量与 header 名称；mock worker 启用 quiet 模式，避免 MSW 将完整凭据请求正文写入浏览器控制台。Go 测试使用明显伪值验证 Cookie、Apple 密码、代理密码、URL 参数、客户端标识与上游正文均不进入日志；组件测试验证认证代理不进入 query/mutation cache；新增浏览器审计完整执行 Cookie、App Password、Apple 直登、OTP 和认证代理流程，并检查 DOM、URL、控制台、Local/Session Storage、Cache Storage、IndexedDB 及资源 URL 无凭据残留。验证通过：`go test ./...`、`go vet ./...`、`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（94/94）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（14/14）；生产构建仍有 Vite 默认 500 kB chunk 阈值警告（主包约 506.96 kB），真实凭据流程仍按 Phase 3 完成门槛在受控账户环境验收 | 在进入别名管理前封闭浏览器缓存、开发日志和 HME 调试日志中的凭据扩散路径，并用跨层回归测试固定安全边界 |
| 2026-08-01 | FE-205：新增统一的账户请求错误状态，`401/403` 显示“Cookie 会话已过期”和“更新 Cookie”操作，其他错误保留清理后的摘要与手动重试；账户列表会识别安全 DTO 中的会话失效、Cookie `401/403` 摘要并提供明确恢复入口。恢复链接只通过 React Router 内存 state 携带经校验的站内来源路径，不写入 URL 或存储；凭据页展示恢复告警并自动聚焦 Cookie 输入，Cookie 更新、Apple 密码直登或 OTP 验证成功后返回来源页，失败时仍停留在凭据页。浏览器 mock 场景在 SPA 导航后保持，支持完整恢复 E2E。测试覆盖安全/外部返回路径、持久错误识别、动态 `401/403`、非会话 `502` 重试、实际账户跳转、Cookie 恢复、Apple 登录返回及凭据不残留；`1440x900`、`1024x768`、`390x844` 截图无重叠或横向溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（94/94）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（13/13）；生产构建仍有 Vite 默认 500 kB chunk 阈值警告（主包约 506.74 kB），真实 Cookie 会话过期恢复仍按 Phase 3 完成门槛在受控账户环境验收 | 让会话失效从仅有错误文案变成可返回原工作流的恢复操作，并为后续别名和收件箱页面提供统一且不会误吞非认证错误的 `401/403` 状态组件 |
| 2026-08-01 | FE-204：Apple 登录的 OTP 分支改为 Radix 模态对话框，验证码输入自动聚焦并仅接受 6 位 ASCII 数字；根据后端 `expires_in` 展示倒计时，本地到期或用户取消后关闭 challenge 并返回密码登录。验证码提交成功仅失效 `['accounts']`，`401`、`410`、网络或 Apple 验证错误均按服务端单次消费语义关闭对话框并要求重新提交密码。密码与 OTP 只在请求期间保存在组件 `ref`，challenge ID 只在组件内存中流转；启动登录 mutation 返回脱敏状态，三者均不进入 mutation 变量、Query/mutation 缓存、URL、存储或截图。MSW account store 新增账户绑定、同账户替换、单次消费和删除/重置清理的 challenge 生命周期，成功验证会更新目标 pending 账户。schema、store 和组件测试覆盖焦点、格式校验、成功、`401`、`410`、本地过期、取消与缓存脱敏，mock E2E 覆盖完整 OTP 流程及三种视口边界；`1440x900`、`1024x768`、`390x844` 截图无重叠或横向溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（72/72）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（13/13）；生产构建仍有 Vite 默认 500 kB chunk 阈值警告（主包约 505.37 kB），真实 2FA Apple 登录仍按 Phase 3 完成门槛在受控账户环境验收 | 完成交互式双重认证状态机，同时按照后端五分钟、账户绑定和一次性消费契约处理过期与失败，并阻止临时 challenge 和用户凭据扩散到前端持久状态 |
| 2026-08-01 | FE-203：凭据页新增 Apple 登录区段，通过账户上下文确定登录邮箱，只提交 Apple ID 密码；密码默认遮蔽且保留原始字符，合法提交后立即清空并恢复遮蔽，仅在请求期间存在于组件 `ref`，mutation 不携带敏感变量。无 2FA 响应会刷新 `['accounts']` 并展示 Cookie 已配置状态；OTP 联合响应进入持久的“需要验证码验证”页面状态并预留独立 `otp` Mock 场景；登录接口的 `401/403` 保留后端清理后的认证错误，不再误报为 Cookie 会话过期。组件测试覆盖校验、直接登录、OTP 分支、错误密码和缓存脱敏，mock E2E 覆盖完整无 2FA 登录；`1440x900`、`1024x768`、`390x844` 完整页面截图无重叠或横向溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（58/58）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（12/12）；生产构建存在 Vite 默认 500 kB chunk 阈值警告，路由级拆包留待 Phase 6，真实无 2FA Apple 登录仍按 Phase 3 完成门槛在受控账户环境验收 | 完成不需要验证码的 Apple 密码登录路径，并固定密码输入、错误语义和 OTP 分支之间的安全边界，为下一步只处理 challenge 对话框与过期状态提供稳定基础 |
| 2026-08-01 | FE-202：凭据页新增 App 专用密码区段，从安全账户 DTO 预填 iCloud 邮箱并展示明确的已配置/未配置状态；邮箱与密码经过提交前校验，密码默认遮蔽，合法提交后立即清空并恢复遮蔽，凭据只在组件 `ref` 中保留到请求结束，mutation 不携带变量。服务端 IMAP 连接或收件箱探测失败时保留原能力状态并展示清理后的错误，成功时只失效 `['accounts']`；MSW 仅更新邮箱和 `has_app_password`，不保存密码。组件测试覆盖预填、校验、成功、失败和缓存脱敏，mock E2E 覆盖完整设置流程；`1440x900`、`1024x768`、`390x844` 截图检查无重叠或横向溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（52/52）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（11/11）；真实 iCloud IMAP 连接仍按 Phase 3 完成门槛在受控账户环境验收 | 将 App Password 的保存结果绑定到后端真实连接校验，同时让请求失败可恢复且不把密码写入 URL、DOM 残留、fixture 或 TanStack Query 持久状态 |
| 2026-08-01 | FE-201：凭据页新增 Cookie 更新表单，支持 Cookie Header、JSON 字符串 Map 和浏览器导出数组三种输入，限制最多 128 项并拒绝非法名称、换行注入、非字符串值及空输入；输入默认遮蔽，合法提交后在请求发出前立即清空，解析后的 Cookie 仅在组件 `ref` 中存活且 mutation 不携带变量，失败时同样不回填，查询与 mutation 缓存均不保留敏感值。MSW 成功更新账户能力与校验状态，组件和 mock E2E 覆盖解析、成功、失败、缓存脱敏及移动端无溢出。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（47/47）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（10/10） | 允许用户直接粘贴常见 Cookie 导出格式，同时确保原始凭据不会进入 URL、DOM 残留、通知或 TanStack Query 持久状态，并与后端 Cookie 数量边界保持一致 |
| 2026-08-01 | FE-106：MSW 新增独立 `offline` 场景，使用网络错误模拟本地服务不可达，并将离线行为统一应用到账户、凭据、别名、收件箱和健康检查端点；账户页分别展示空数据、502 Apple 服务错误和本地服务离线消息，离线恢复后可手动重新加载。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（33/33）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（9/9） | 固定三类容易混淆的账户工作台状态，确保网络断开不会被误报为 Apple 业务错误，恢复入口与查询重试策略保持一致 |
| 2026-08-01 | FE-105：新增账户详情父路由，通过共享 `['accounts']` 查询解析当前账户，展示账户名称、邮箱、ID、状态和清理后的异常摘要；账户列表名称成为明确的打开入口，详情页共享 `别名 / 收件箱 / 凭据` 标签导航和返回账户链接；未知账户自动返回列表并发送错误通知，查询失败保留重试入口。组件测试覆盖打开、标签切换和未知账户回退，mock E2E 覆盖 `1440x900`、`1024x768`、`390x844` 三种视口。验证通过当前 33 个前端测试、生产构建及两套 E2E | 在没有单账户 GET 接口的前提下复用脱敏账户列表缓存，建立后续凭据、别名和收件箱页面的稳定账户上下文，避免各子页面重复实现账户选择与失效处理 |
| 2026-08-01 | FE-104：账户列表新增删除图标操作和带目标账户名称的危险确认；删除期间锁定关闭与重复提交，成功后只失效 `['accounts']`、刷新状态汇总并发送账户名称通知，失败时保留账户行和对话框。MSW 内存 store 会过滤已删除 fixture、移除新建账户并在测试间重置；组件测试覆盖确认、成功和失败，mock E2E 覆盖完整删除流程。验证通过当前 33 个前端测试、生产构建及两套 E2E | 让账户删除具备明确目标、不可恢复提示、可靠服务端刷新和失败恢复，同时避免通知展示内部账户 ID 或依赖乐观更新 |
| 2026-08-01 | FE-103：新增 Radix Dialog + React Hook Form + Zod 的添加账户表单，字段包含名称、iCloud/`me.com`/`mac.com` 邮箱、区域和可选 HTTP/HTTPS/SOCKS5 代理；邮箱作为无 Cookie pending 登录流程的必要输入；提交成功后只失效 `['accounts']`，关闭并清空表单并发送通知，失败时保留输入并显示清理后的错误；空账户页只保留一个主操作按钮。MSW 增加创建账户内存 store，覆盖成功刷新和错误场景。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（27/27）、`npm run build`、`npm run test:e2e`（3/3）、`npm run test:e2e:mock`（6/6）以及三种基线视口对话框截图检查 | 让 pending 账户可以直接进入后续密码登录流程，同时避免表单错误丢失、重复提交或把敏感凭据混入账户创建流程 |
| 2026-08-01 | FE-102：账户列表为 `active`、`pending`、`error` 提供文本与语义色状态；Cookie 和 App 密码分别显示“已配置/未配置”，不依赖空白或颜色；待登录与异常账户提供直达凭据页的设置/更新入口，异常行显示后端已清理的 `last_error` 摘要；MSW 增加接口成功但含异常账户的 `mixed` 场景。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（23/23）、`npm run build`、`npm run test:e2e`（3/3）、`npm run test:e2e:mock`（4/4）及三种基线视口混合状态截图检查 | 将账户级业务异常与 API 传输错误分开呈现，让用户能明确看到缺失的认证能力和对应恢复入口，同时继续只消费脱敏 DTO |
| 2026-08-01 | FE-101：账户工作台改为通过 TanStack Query 读取 `GET /api/accounts`；新增账户总数、正常、待登录和异常状态带，使用安全 DTO 展示名称、邮箱、别名统计、认证能力和最后校验；加载、空数据、网络/后端错误均有独立状态和重试入口；桌面使用语义化表格，移动端改为标签和值的单列行布局。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（22/22）、`npm run build`、`npm run test:e2e`（3/3）、`npm run test:e2e:mock`（3/3）及 `1440x900`、`1024x768`、`390x844` 成功数据截图检查 | 将账户页从静态骨架推进为可读取、可恢复且在三种基线视口稳定的工作台基础，为添加、删除和详情导航保留清晰行级边界 |
| 2026-08-01 | FE-007：新增通知 Provider、异步确认对话框、加载、空数据和错误状态组件；通知通过 live region 播报并支持手动/自动关闭，危险确认基于 Radix Alert Dialog 管理焦点和 Escape，异步操作失败时保持对话框打开；统一按钮、图标按钮、对话框和通知样式，并用通用空状态替换账户页重复标记。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（18/18）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（3/3） | 固定跨页面的加载、错误反馈和危险操作行为，避免账户、别名和凭据页面各自实现不一致的状态与焦点管理 |
| 2026-08-01 | FE-006：新增 TanStack Query 5 Provider、默认查询策略（GET 网络错误最多重试一次，mutation 禁止自动重试）、标准 query keys/options，以及 MSW 2 fixtures；Node 测试 server 与浏览器 Service Worker 共用 handlers，支持 `success`、`empty`、`error` 场景，新增 `dev:mock` 和 `test:e2e:mock`。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（14/14）、`npm run build`、`npm run test:e2e`（3/3）和 `npm run test:e2e:mock`（3/3） | 让页面可以在真实 API、空数据和 Apple 服务错误之间切换而不改业务代码，并把请求取消、缓存和重试策略固定在统一基础层 |
| 2026-08-01 | FE-005：新增 `web/src/api` 类型化客户端和 Zod 4 契约，覆盖账户、登录 challenge、别名、收件箱和健康检查等全部现有端点；统一处理网络中断、HTTP 业务错误、非法 JSON 与响应契约漂移，401/403 映射为会话过期，502 保留后端清理后的摘要；动态路径逐段编码，查询请求支持 `AbortSignal`，非幂等操作不重试，响应中的契约外字段会在进入 UI 状态前剥离。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（9/9）和 `npm run build` | 将后端 JSON 边界集中为唯一可测试入口，阻止契约漂移和意外敏感字段扩散到页面，并为 Query 与业务模块提供稳定类型和错误语义 |
| 2026-08-01 | FE-004：引入 React Router，根路径重定向到账户工作台，建立账户、别名、收件箱、凭据和设置路由；应用壳层使用 `Outlet` 与 `NavLink`，按当前路由显示页面标题，并增加路由错误边界、应用内 404 和账户详情占位页。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（3/3）、`npm run build` 和 `npm run test:e2e`（3/3，覆盖全部计划路由、404 及三种基线视口） | 固定 SPA 导航和错误恢复边界，使后续功能页可按模块独立替换占位内容且未知路径不会脱离应用框架 |
| 2026-08-01 | FE-003：将前端样式拆为 `tokens.css`、全局 reset 和 `app-shell.css`；集中管理字体、颜色、间距、圆角、焦点环、布局宽度和控件高度，补充成功/警告/危险语义色；应用壳层增加 1024px 平板压缩布局、760px 移动端上下结构和 `prefers-reduced-motion` 支持，表格列使用 `minmax(0, ...)` 防止内容撑破容器。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（2/2）、`npm run test:e2e`（2/2，含 `1440x900`、`1024x768`、`390x844` 无横向溢出检查）和 `npm run build` | 建立可复用的视觉和布局基础，保证后续业务页面在三种基线视口下保持稳定且不把颜色、尺寸和焦点行为散落在组件中 |
| 2026-08-01 | FE-002：配置 ESLint 10 flat config、Prettier、Vitest 4 + jsdom + Testing Library，以及 Playwright 1.62；新增格式/lint/typecheck/test/test:e2e 脚本、DOM cleanup setup、账户工作台单元测试和浏览器冒烟测试；Playwright 使用独立 Vite 端口 `4173`，本地首次运行需安装 Chromium。验证通过：`npm run format:check`、`npm run lint`、`npm run typecheck`、`npm run test`（2/2）、`npm run test:e2e`（1/1）和 `npm run build` | 固定前端代码质量门槛和可重复的浏览器验证入口，避免后续页面开发只依赖手工检查 |
| 2026-08-01 | FE-001：新增独立 `web/` Vite + React + TypeScript 工程，提供 `dev`、`typecheck`、`build` 和 `preview` 脚本；应用入口使用 StrictMode，基础账户工作台骨架使用 Lucide 图标、可访问主导航、语义化账户表格和响应式侧栏；构建产物、依赖目录和 TypeScript 增量缓存加入忽略规则，README 补充前端启动与构建命令。验证通过：`npm run typecheck`、`npm run build`；Vite 的 `/` 与 `/accounts` 返回 HTTP 200，并通过实际 CSS 视口检查确认 `390x844` 下导航、顶部栏和工作区不重叠 | 建立可独立运行、严格类型检查且与后续 React SPA 目录和视觉方向一致的基础工程，避免后续业务功能在临时脚手架上叠加 |
| 2026-08-01 | BE-010：新增受统一 Bearer 鉴权保护的 `GET /api/health`，固定返回服务名、构建版本、`ok`/`degraded` 状态和配置可用性；缺少 `accounts.json` 视为有效空配置，损坏或不可读取的磁盘配置降级但仍返回 HTTP 200；响应设置 `Cache-Control: no-store` 且不暴露路径、账户、凭据或内部错误；`build.sh` 和 Docker 构建支持注入 `main.version`，README 与 API 契约同步，并新增配置与路由测试。验证通过：`go test -count=1 ./...`、`go vet ./...`、带版本注入的生产二进制构建及健康接口冒烟；`go test -race ./...` 因当前 Windows 环境缺少 `gcc` 未执行 | 为前端和部署探针提供稳定、可鉴权且不泄露敏感信息的服务状态契约，并让发布产物能报告实际版本 |
| 2026-08-01 | BE-009：SRP `signin/init` 返回的服务端 `c` 挑战现在原样提交，并补齐 Apple 要求的认证/OAuth 请求头；密码登录拆为 `POST /api/accounts/:id/login/start` 与 `/login/verify`，OTP 前通过认证选项刷新 `scnt`；challenge 仅在服务端内存保存 5 分钟、绑定账户且只能消费一次；成功 Cookie 原子写入并更新账户状态，响应只返回脱敏 DTO；新增协议、账户、路由和过期/重放测试。验证通过：`go test -count=1 ./...`、`go vet ./...`、生产二进制构建；`go test -race ./...` 因当前 Windows 环境缺少 `gcc` 未执行 | 修复原实现把 OAuth client ID 当作 SRP challenge 的登录错误，支持前端交互式 2FA，并避免密码、OTP 和临时认证状态进入持久化或 API 响应 |
| 2026-07-31 | BE-008：`POST /api/accounts` 新增可选 `icloud_email`，仅接受精确的 `@icloud.com`、`@me.com` 或 `@mac.com` 地址并规范化域名；无 Cookie 账户将邮箱随 `pending` 状态原子持久化，后续密码登录优先使用该字段；有效 Cookie 会话仅在用户未显式提供时自动推导邮箱；新增 Manager/API 的持久化、登录邮箱选择、域名边界和脱敏 DTO 测试，并同步 README 与 API 文档。验证通过：`go test ./...`、`go test -race ./...`、`go vet ./...`、生产二进制构建 | 补上创建 pending 账户到发起密码登录之间缺失的邮箱输入与存储契约，同时阻止显示名、第三方域名和伪造域名后缀进入账户配置 |
| 2026-07-31 | BE-007：validate 返回的动态 `mccgateway` 必须属于账户对应的 iCloud 域，规范化端口后向实际网关 Cookie jar 注入账户 Cookie；Web 摘要固定映射 `id/from/to/subject/date/preview`，按时间倒序，日期统一 UTC RFC3339，预览限制 4 KiB；解析明确 `To/CC/BCC` 收件人并精确匹配别名，缺少可验证收件人时返回 `ErrWebRecipientUnavailable`，不再以主题或发件人猜测；新增网关安全、Cookie、消息映射和过滤测试并同步文档 | 修复固定 `p217` Cookie 导致动态网关 403 的问题，避免 Web/IMAP 字段格式漂移，并以显式降级替代可能泄露其他邮件的错误别名匹配 |
| 2026-07-31 | BE-006：IMAP 收件箱通过 `UID SEARCH SINCE` 应用 `days` 后再按 UID 限量；摘要按邮件时间和 UID 确定性倒序；使用 `BODY.PEEK` 且每封最多拉取前 64 KiB，返回预览限制为 4 KiB 有效 UTF-8；登录失败立即 `Terminate`，登出失败也强制终止连接；新增可注入会话及协议级单元测试，README 和 API 文档同步更新 | 让时间筛选在限量前真正生效，稳定前端列表顺序，避免摘要读取下载无界正文或改变已读状态，并消除认证失败时的 IMAP 连接泄漏 |
| 2026-07-31 | BE-005：统一限制 `/api` 请求体为 1 MiB；收件箱 `limit` 为 `1..100`、`days` 为 `1..365`；别名标签最多 200 个 Unicode 字符；单账户最多 128 个 Cookie。所有 JSON 路由使用统一绑定器，Manager 在加载、解析、刷新、保存和提交 Cookie 时均执行数量校验；README、API 文档和边界测试同步更新 | 在进入前端联调前固定输入契约，阻止超大或无界输入消耗服务资源，并确保 HTTP 层与账户持久化层执行相同的 Cookie 约束 |
| 2026-07-31 | BE-004：Manager 使用读写锁和账户深拷贝快照，HME/Web 邮件客户端复制 Cookie；配置通过同目录临时文件、同步、关闭和原子替换保存；写入失败回滚内存；所有 Cookie 刷新保存错误显式返回，HTTP 持久化失败统一为 500 | 消除客户端与 Manager 共享 Cookie map 导致的数据竞争，避免配置半写入，并防止远端操作后本地 token 保存失败被误报为成功 |
| 2026-07-31 | BE-003：将 `accounts.json` 规范为 `accounts` 数组；账户字段统一使用 `cookies` 字符串 Map、单个 `app_password` 和 `icloud_email` 等 `Account` 字段；写出按 ID 排序并校验 ID/name/status；兼容旧版按 ID 对象格式并在保存时迁移 | 修复代码、模板、README 和 API 文档之间的数组/对象、Cookie 数组/Map、`app_passwords`/`app_password` 多套 schema，建立前端联调可依赖的唯一磁盘契约 |
| 2026-07-31 | BE-002：账户列表、新增、Cookie 更新、App Password 设置及密码登录成功响应统一使用安全 `AccountDTO`；仅返回 `has_cookies`、`has_app_password`、`proxy_configured` 能力标记 | 在前端联调前建立固定的敏感数据响应边界，杜绝 Cookie、密码和带凭据代理 URL 被序列化到 API 响应 |
| 2026-07-31 | BE-001：服务默认监听 `127.0.0.1:8081`；非回环监听必须设置至少 32 字符 `ICLOUD_HME_API_TOKEN`；`/api` 统一 Bearer 鉴权 | 降低本地工具误暴露风险，并为远程部署提供明确访问边界 |
| 2026-07-31 | 创建完整前端开发计划 | 后续按阶段逐步实现并统一验收标准 |
