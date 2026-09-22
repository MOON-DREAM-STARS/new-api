# Web Workspace 架构设计（Design Baseline）

> **状态：设计基线 / 尚未实现（Design Baseline, NOT IMPLEMENTED）。**
> 本文档只定义 New API Fork（`MOON-DREAM-STARS/new-api`）中 Web Workspace 功能的目标、边界与设计决策，不包含任何已交付业务代码。
> 归档日期：2026-09-18 · 归档分支：`feature/dreamstars-homepage`
>
> 关联文档：
>
> - [Web Workspace 实施 Checklist](./implementation-checklist.md)（执行清单、实施阶段与验收标准）
> - [用户鉴权与登录会话](../authentication.md)（现有面板鉴权模型）

## 1. Goals / Non-goals

### 1.1 Goals

Web Workspace 是 New API 的一级功能：经过授权的 New API 用户，通过 New API Web UI 操作运行在服务器端 Linux 环境中的受控 Chromium。

- **New API 是 control plane / auth gateway**：负责身份认证、功能授权（entitlement）、资源所有权（ownership）、浏览器会话编排与流票据签发。
- **Browser Agent 是 execution plane**：负责在服务器端启动、隔离并守护 Chromium 运行时。
- **本地 PC 只是 Thin Client**：只接收远程显示流、发送键盘/鼠标/触摸输入，并展示当前用户有权看到的 Workspace 元数据。
- Chromium、Browser Profile、Cookie、LocalStorage、IndexedDB、缓存、上传/下载目录全部位于服务器端。
- 采用单一所有权模型 `User → Web Workspace → Project → Conversation`（见第 3 节），第一版不引入通用资源 ACL。
- 功能授权（feature entitlement）与资源归属（resource ownership）完全分离（见第 4 节）。
- 先面向 ChatGPT Web，但架构不写死 ChatGPT：Provider 差异收敛在 Provider Adapter 内，未来可扩展 Claude、Gemini 等 Web AI。
- 任何安全相关判定 fail closed；未知资源默认拒绝。

### 1.2 Non-goals

在没有单独设计并获批准之前，第一版明确不做：

- 不把 ChatGPT Web 直接 reverse proxy 成 New API 的子路径。
- 不向客户端复制或导出 upstream Cookie、Chromium profile、LocalStorage/IndexedDB、缓存。
- 不在客户端暴露 CDP（Chrome DevTools Protocol）endpoint 或 DevTools WebSocket URL。
- 不在公网暴露 Chromium remote debugging、VNC/KasmVNC 或 Browser Agent 端口。
- 不给 New API 容器挂载 Docker socket；不让 New API 主进程长期直接管理 Chromium 子进程。
- 不用前端菜单隐藏替代权限校验；不用 URL 正则作为 Conversation ownership 的唯一判断；不用 DOM selector 作为安全边界的唯一实现。
- 第一版不实现 Project 多 Owner、共享 ACL、跨用户 Project 分享（明确不重新引入上一版多用户共享 Project 的 ACL 设计）。
- 不把 New API 的 Admin role 当作 Web Workspace 套餐等级。
- 不在完成 response/DOM/data filtering 之前，把多个不同用户共享同一 upstream 账号的模式宣称为强隔离多租户（见第 15 节）。

## 2. 现状基线与集成点

设计建立在仓库现有结构上，而不是假设一个全新系统。2026-09-18 核对到的相关基线：

| 领域 | 仓库现状 | 对本设计的意义 |
| --- | --- | --- |
| 鉴权 | 面板鉴权使用短期 Access Token（15 分钟 JWT，仅存浏览器内存）+ HttpOnly Refresh Cookie + 服务端登录会话控制面（见 `docs/authentication.md`）；PAT 与面板会话是不同契约 | Web Workspace 不新建平行登录体系；API 与 stream 挂载复用现有用户身份与 Session 撤销语义 |
| 角色 | `model/user.go` 的 `User.Role`（int，默认 1）与 `web/src/lib/roles.ts` 的 `ROLE`（GUEST=0、USER=1、ADMIN=10、SUPER_ADMIN=100） | `role` 是管理权限，只是 entitlement 的一个输入，不是套餐等级 |
| 用户组 | `User.Group`（varchar，默认 `default`） | 可作为 entitlement 的 allowed_groups 输入 |
| 分层 | Router → Controller → Service → Model（Gin + GORM v2） | Web Workspace API 必须沿用该分层，路由挂到 `router/api-router.go` 的 `/api` 分组 |
| 前端 | React 19 + TanStack Router；`web/src/routes/_authenticated/route.tsx` 已提供登录守卫；功能代码位于 `web/src/features/*` | 未来页面放 `web/src/routes/_authenticated/web-workspace/`，功能代码放 `web/src/features/web-workspace/` |
| Sidebar | 根 Sidebar 由 `web/src/hooks/use-sidebar-data.ts` 组装，支持 `requiredRole`；后端另有默认边栏配置（`model/user.go`）与 `HeaderNavModuleAuth` 一类中间件 | Web Workspace 入口需要与 sidebar module / entitlement 集成；菜单可见性只是 UX，不是安全边界 |
| 部署 | `docker-compose.yml`：`new-api` + `redis` + `postgres`，共享 `new-api-network`，`./data:/data` | 未来的 Browser Agent 是独立组件；New API 容器不因本设计获得额外特权（不加 Docker socket） |
| 数据库 | 必须同时兼容 SQLite、MySQL ≥ 5.7.8、PostgreSQL ≥ 9.6 | 数据模型只使用三方言都支持的 GORM 能力；migration 在实施阶段单独验证 |

## 3. Ownership Model

采用单一所有权模型（单一所有权链），而不是多对多 ACL：

```text
User A
└── Workspace A
    ├── Project A0
    ├── Project A1
    └── Project A2

User B
└── Workspace B
    ├── Project B0
    └── Project B1
```

不变量：

```text
1 New API User   = 0 or 1 Web Workspace
1 Web Workspace  = 1 Browser Profile = N Projects
1 Project        = exactly 1 Web Workspace
1 Conversation   = exactly 1 Project
```

Owner 只能通过关系链推导：

```text
Project      → Workspace → User
Conversation → Project   → Workspace → User
```

判定原则：

```text
CanAccessProject(currentUser, project)
  = project.workspace.user_id == currentUser.id

CanAccessConversation(currentUser, conversation)
  = conversation.project.workspace.user_id == currentUser.id
```

第一版明确禁止：

- resource ACL table；
- project-user join table / per-project permission matrix；
- `owner_user_id` 与 `workspace_id` 双重冗余所有权；
- 任何 Project 属于多个 User 或 Workspace 的设计。

理由：Project 互斥所有权天然成立，所有权可从关系链唯一推导；单一来源避免了 ACL 与所有权多份状态分歧。未来若要支持共享语义，必须作为独立设计评审，而不是顺手扩展第一版模型。

## 4. Entitlement（功能授权）与 Ownership（资源归属）

两类问题必须分开，不能混成一套 ACL：

| 问题 | 概念 | 判定依据 |
| --- | --- | --- |
| 这个用户能使用 Web Workspace 吗？ | Feature entitlement | 全局开关、user.status、role、group、可选 user override、未来配额 |
| 这个 Project / Conversation 属于当前用户吗？ | Resource ownership | `project.workspace.user_id == current_user.id`（Conversation 沿链推导） |

### 4.1 Entitlement 求值模型

设计目标语义（伪代码）：

```text
entitled =
    web_workspace.global_enabled
AND user.status == enabled
AND (user override != deny)
AND (user override == allow OR (role >= minimum_role AND group in allowed_groups))
```

说明：

- `user.status` 与全局开关是硬门槛，任何 user override 都不能绕过。
- allow override 的语义：绕过 role/group 检查；若产品要求 allow 可绕过全局开关，必须单独评审并写入实施文档，默认不允许。
- deny override 优先于一切允许条件。
- 求值结果不得由前端计算；后端在 API、session 创建、stream attach 每个入口重新校验。

### 4.2 配置字段（设计草案）

```text
global_enabled
minimum_role
allowed_groups
optional_user_override   # allow / deny / none
```

未来能力限制（可以晚于首个 schema 实现，但字段设计要预留）：

```text
max_projects
max_concurrent_sessions
idle_timeout_seconds
storage_limit_bytes
allow_upload
allow_download
allow_clipboard_in
allow_clipboard_out
allow_audio
allow_popups
```

明确：`role` 是管理权限。Admin 用户是否能用 Web Workspace，仍由 entitlement 判定，不能写成"ADMIN 及以上自动开通"。

### 4.3 双重 enforcement

- Sidebar：无权限时不显示入口（UX）。
- 前端 route guard：阻止直接导航（UX / 防御纵深）。
- 后端 API：强制 entitlement + ownership 校验（安全边界）。
- Browser session 创建或重启：再次校验 entitlement。
- Stream attach（WSS upgrade）：再次校验用户、Workspace 与 stream ticket。

前端隐藏从来不是安全边界。
## 5. 总体架构（Architecture）

推荐拓扑：

```text
┌─────────────────────────────────────────────┐
│ Local PC / Browser（Thin Client）           │
│  只接收：remote display stream、可见元数据   │
│  只发送：keyboard / mouse / touch input     │
└──────────────────────┬──────────────────────┘
                       │ HTTPS / WSS
                       ▼
┌─────────────────────────────────────────────┐
│ New API（control plane / auth gateway）     │
│  Authentication · Feature Entitlement       │
│  Workspace Ownership Resolver               │
│  Session Broker · One-time Stream Ticket    │
│  WebSocket Stream Gateway                   │
│  /api/web-workspace REST API                │
└──────────────────────┬──────────────────────┘
                       │ private network（不公开）
                       ▼
┌─────────────────────────────────────────────┐
│ Browser Agent（execution plane）            │
│  Workspace A runtime（isolated Chromium）   │
│  Workspace B runtime（isolated Chromium）   │
│  Browser Guard · Display Transport          │
└──────────────────────┬──────────────────────┘
                       │ controlled egress（default deny）
                       ▼
                  Internet / Provider Web
```

边界规则：

- 客户端与 Browser Agent 之间不存在直接连接路径；客户端一律连接 New API。
- Browser Agent 只在私有网络可见，不 publish 端口到公网；服务间通信需要服务认证（短期 service token 或请求签名），不接受仅凭来源 IP 的信任。
- New API 容器不挂载 Docker socket，不以特权方式管理宿主。
- CDP 只允许监听 Workspace runtime 内部 loopback；不 publish 到宿主；不把 CDP WebSocket URL 返回给客户端；Browser Guard 是唯一 CDP consumer。
- 显示协议（KasmVNC 原生 HTTP/WebSocket）在 Browser Agent 内部封装为 Kasm proxy 边界，核心业务不直接持有 runtime 地址或端口。

### 5.1 组件职责

| 组件 | 职责 | 明确不做 |
| --- | --- | --- |
| New API | 认证、entitlement、ownership、会话编排、stream broker、ticket 签发、审计 | 不直接长期托管 Chromium 进程；不持有 Docker socket |
| Browser Agent | 管理 workspace runtime、Chromium、虚拟显示、display transport、Browser Guard、provider 观察 | 不公开监听；不信任客户端输入；不接受任意 URL / 路径 / 用户 ID |
| Browser Guard | navigation、network/egress、target/popup、download、clipboard 控制与 ownership 强制 | 不是普通 UI helper；不得 fail open |
| Provider Adapter | URL / 资源解析、Project 与 Conversation 识别与观察 | 不能把 provider 细节扩散进核心 ownership 模型 |

## 6. 数据模型（Data Model）

本轮不创建任何 migration；以下为实施阶段要落地的设计。

### 6.1 web_workspaces

```text
id                   内部主键
user_id              UNIQUE（保证一个 User 至多一个 Workspace）
provider
status
created_at
updated_at
last_active_at
```

- `user_id` 唯一约束是"一个 User 最多一个 Workspace"的数据库级保证。
- 不把服务器 profile 绝对路径作为 API 可见字段；内部如需 storage key，使用逻辑 ID。
- Workspace 删除策略（软删除、硬删除、profile 清理）必须在实施阶段明确；profile 属于用户数据，不得随普通删除静默丢失（见第 7 节）。

### 6.2 web_projects

```text
id
workspace_id
provider
external_project_id
name
created_at
updated_at
```

约束：

```text
UNIQUE(provider, external_project_id)
INDEX(workspace_id)
```

- 不保存冗余 `owner_user_id`；owner 一律从 Workspace 推导。
- `external_project_id` 是 provider 侧标识，只用于内部映射与 Browser Guard 判定，默认不返回给普通客户端。

### 6.3 web_conversations

```text
id
project_id
provider
external_conversation_id
title
created_at
updated_at
```

约束：

```text
UNIQUE(provider, external_conversation_id)
INDEX(project_id)
```

设计选择：保留 `provider` 列并采用 `UNIQUE(provider, external_conversation_id)`。理由：conversation ID 的命名空间归 provider 所有，未来多 provider 共存时不同 provider 的 ID 空间可能重叠；把 namespace 显式写进唯一约束，比假设全局唯一更安全。若实施阶段证实某 provider 的 ID 全局唯一，这个选择仍然兼容。

### 6.4 Browser Session（运行时）

浏览器会话是临时状态，优先放内存或 Redis；只有出现明确审计需求时才考虑持久化：

```text
session_id
workspace_id
user_id
runtime_id
status
created_at
last_seen_at
expires_at
```

敏感连接凭证（display password、CDP endpoint、service token）不得明文持久化。

### 6.5 数据库兼容

所有表必须同时满足 SQLite、MySQL ≥ 5.7.8、PostgreSQL ≥ 9.6：

- 使用 GORM 常规能力（Create / Find / Where / Updates、唯一索引、普通索引）；
- 不使用方言特有 SQL，不直写 AUTO_INCREMENT / SERIAL；
- 唯一约束与索引必须在三方言上语义等价；
- migration（fresh、upgrade、幂等）属于实施阶段验收项（checklist Phase 1）；本轮明确 NOT RUN。

## 7. 文件系统隔离（Filesystem Isolation）

服务器端目录模型：

```text
/data/web-workspaces/
└── workspace-<id>/
    ├── profile/
    ├── uploads/
    ├── downloads/
    ├── cache/
    └── tmp/
```

- `<id>` 是数据库 Workspace ID；不以 `user-<id>` 路径承担所有权判断，所有关联都必须经数据库解析。
- 每个 runtime 只允许挂载自己 Workspace 的目录，例如 `/data/web-workspaces/workspace-A`；不得挂载整个 `/data/web-workspaces` 根。
- Linux 运行时隔离要求：
  - 专用非特权 UID/GID；
  - 非 privileged 容器；
  - 尽可能只读 rootfs；
  - 可写挂载仅限本 Workspace 的目录；
  - 丢弃不必要的 Linux capabilities；
  - 不使用 host network / host PID / host IPC；
  - 不挂 Docker socket；
  - CPU/RAM/PID 资源限制；
  - 每 Workspace 独立 tmp，不跨 Workspace 共享 profile 或临时目录。

### 7.1 Profile 生命周期

明确区分：**stop browser ≠ delete profile**。

| 事件 | 默认行为 |
| --- | --- |
| 首次创建 | 创建空 profile |
| 浏览器重启 / Server 重启 | 复用现有 profile |
| 用户登出 New API | 不删除 profile；结束 runtime / session |
| upstream（Provider）登出 | 保留 profile 文件；状态由 Provider 页面决定 |
| Workspace reset（显式操作） | 由用户或管理员显式触发清理，需要独立确认 |
| User 删除 / Admin 禁用 | 按保留策略处理，不得静默、不可恢复地删除用户数据 |
| 存储清理 | 只清理明确归属且允许清理的 Workspace 目录；不触碰其他 Workspace |

Chromium 在 runtime 中使用 `--password-store=basic`：Provider cookie、localStorage 及其加密密钥随 `profile/` 持久化，容器重建或 runtime 重启后不会因此要求重新登录。New API 的登录 cookie 属于操作者本机浏览器，不在该 profile 中；显式 Provider 登出、会话过期或 Provider 主动失效仍会结束登录态。

## 8. 浏览器运行时（Browser Runtime）

- Chromium 运行在服务器端 Linux；本地 PC 不运行、也不控制 Chromium。
- 主交互环境使用正常 GUI Chromium + KasmVNC Xvnc 虚拟显示，不以真正的 `chromium --headless` 作为主要交互形态。
- 显示传输实现：KasmVNC 1.5.0 原生 HTTP/WebSocket，运行时仅监听私有 network 内的 6901；通过 Agent 的 Kasm reverse proxy 暴露给 New API。
- 不再保留传统 RFB 5900、websockify 或 raw RFB DisplayTransport；Kasm ticket 与 New API session ownership 是唯一流式授权路径。
- 显示传输仍由 Browser Agent 的 Kasm proxy 边界封装，核心业务不直接持有 runtime 地址或端口。

### 8.1 生命周期与状态

```text
Workspace   持久
Profile     持久
Runtime     临时（ephemeral）
Session     临时（ephemeral）
```

第一版调度假设：每个 Workspace 最多一个活动 runtime。

状态建议：

```text
STOPPED → STARTING → RUNNING → IDLE → STOPPING → STOPPED
                       └──────── FAILED ────────┘
```

必须处理：重复启动请求、并发请求、Chromium 崩溃、Browser Agent 崩溃、孤儿 runtime、New API 重启、Browser Agent 重启、过期 stream ticket、session 重连。

### 8.2 Provider Adapter

核心 Workspace Engine 不得写死 ChatGPT URL 规则。建议抽象（草案）：

```text
ProviderAdapter
  ParseNavigation(url)
  ClassifyResource(url)
  ResolveProject(...)
  ResolveConversation(...)
  ObserveProjectCreated(...)
  ObserveProjectRenamed(...)
  ObserveProjectDeleted(...)
  ObserveConversationCreated(...)
  AllowedHosts(mode)
```

首个实现：`providers/chatgpt`；未来可加 `providers/claude`、`providers/gemini`。ChatGPT 特有的 URL 与网络接口只存在于 adapter / guard 层，不应扩散到核心 ownership 模型。
## 9. Browser Guard

Browser Guard 是安全组件，不是普通 UI helper。职责至少包括：

| 职责 | 说明 |
| --- | --- |
| Navigation Guard | 拦截顶层导航、redirect、history 恢复等进入 Provider 页面的路径 |
| Network / Egress Guard | 按域名与地址策略放行或拒绝请求（第 10 节） |
| Project / Conversation ownership enforcement | 通过数据库关系链判定资源是否属于当前 Workspace（第 11 节） |
| Target / Popup Guard | 未知 popup / 新 target 默认阻止，不允许绕过导航策略 |
| Download control | 默认关闭；开启时限定目录与大小，防止数据外发 |
| Clipboard control | clipboard_in / clipboard_out 独立开关，默认关闭 |
| Provider Observer | 观察 Project / Conversation 的创建、重命名、删除等事件并回报登记 |

### 9.1 CDP（Chrome DevTools Protocol）约束

如果使用 CDP：

- 只监听 Workspace runtime 内 loopback；
- 禁止 publish 到宿主或公网；
- 禁止把 endpoint 或 DevTools WebSocket URL 返回给客户端；
- Browser Guard 是唯一 CDP consumer；
- New API 不把 CDP 当作客户端协议。

### 9.2 不以 DOM / URL 正则作为安全边界

DOM filtering 可以用于减少信息展示，但不能成为 ownership enforcement 的唯一手段；Provider 前端结构变化不得导致安全边界静默 fail open。所有权判定必须以数据库映射为主，URL / 资源解析只是输入而不是结论。

## 10. 域名与网络策略（Domain / Egress Policy）

原则：**default deny / fail closed**。域名 allowlist 是第一层网络边界，但不是 Project 隔离机制。

### 10.1 必须阻止的目标（至少）

```text
127.0.0.0/8       10.0.0.0/8        172.16.0.0/12     192.168.0.0/16
169.254.0.0/16    ::1               fc00::/7          fe80::/10
localhost         host.docker.internal
cloud metadata endpoints（如 169.254.169.254）
内部 DNS 域（按部署环境补充）
```

目标：防 SSRF、防扫描宿主与内网、防访问 New API 内部服务（数据库、Redis、Agent 控制面）、防访问 Docker / metadata endpoint。

### 10.2 Domain ACL ≠ Project ACL

- Domain ACL 回答："这个网络请求允许发到哪个域名 / 地址？" 它防的是 SSRF、内网横向移动与无关公网访问。
- Project ACL 回答："这个 Provider 资源属于当前用户吗？" 它由数据库关系链（`web_projects → web_workspaces → user`）判定。
- 两者互补且不可互相替代：域名放行不等于资源有权访问；资源有权访问也不代表可以放开域名策略。
- 需要处理 DNS rebinding：allowlist 判定与实际连接必须基于一致的解析结果，禁止"按域名放行后连接到私网 IP"。

### 10.3 Login Mode / Locked Mode

OAuth 登录临时需要访问身份提供方（OpenAI auth host、Google / Microsoft identity 等），因此区分：

| 模式 | 行为 |
| --- | --- |
| LOGIN_MODE | 在有限时间内扩展 identity host allowlist；仍拒绝私网地址；由用户或管理员显式启动 |
| LOCKED_MODE | 只允许 Provider 运行所需域名；继续拒绝私网；登录完成后必须回到此模式 |

模式切换必须可审计（进入 / 退出记录）。

## 11. Project / Conversation 隔离（所有权强制）

### 11.1 Project

访问 Provider Project（`/g/...` 或等价路由）时：

```text
external_project_id
    → web_projects
    → workspace_id
    → web_workspaces.user_id
    → current_user_id
```

只有最终相等才允许。

### 11.2 Conversation

访问 `/c/...` 或等价路由时不得只按 URL pattern 放行：

```text
external_conversation_id
    → web_conversations
    → project_id
    → workspace_id
    → user_id
```

### 11.3 Unknown resource

```text
unknown project      → DENY
unknown conversation → DENY
```

唯一例外：明确的 create permit 流程（第 12 节）中的新建动作。

## 12. Project 生命周期

### 12.1 创建

```text
用户触发 New Project
  → New API 校验：entitlement / workspace ownership / max_projects / active browser session
  → 签发短时 creation permit（绑定 user + workspace，单用途、单次、可审计）
  → Browser Agent 允许 create 动作
  → Provider 返回新的 external project ID
  → Agent 登记到当前 Workspace（UNIQUE(provider, external_project_id)）
  → permit 消费
```

新建 Project 登记后天然归当前 Workspace 所有；不需要创建任何 ACL 行。

### 12.2 重命名与删除

统一经 `GetOwnedProject(current_user, internal_project_id)`；只有 Owner 可以 open / rename / delete / create conversation。

### 12.3 Provider 与本地 DB 不同步时的 fail-safe

- Provider 删除成功、本地 DB 更新失败：必须可恢复与可重同步，并记录异常；不允许本地继续把资源当作可访问。
- 本地删除成功、Provider 删除失败（反向失败）同理：不得静默漂移，必须留下可审计状态并支持重试。
- 并发 create / rename / delete：permit 单次消费 + 唯一约束 + 事务幂等；竞争失败 fail closed。
- 状态不确定期间，宁可 deny 并提示重试或修复，不 fail open。

## 13. Thin Client 原则

本地 PC 被允许接触：

- New API 登录 Session（现有面板鉴权）；
- Web Workspace 页面（元数据、状态、控制按钮）；
- 当前用户有权看到的 Workspace / Project 展示信息；
- 远程显示流（像素 / 画面）；
- 键盘、鼠标、触摸输入通道；
- 短时、单次使用的 New API stream ticket。

本地 PC 不得接触：

- Chromium profile、ChatGPT/OpenAI Cookie、upstream session token；
- LocalStorage / IndexedDB / browser cache；
- CDP endpoint、`remote-debugging-port`；
- VNC / KasmVNC 原始密码；
- Browser Agent 地址、runtime / container 内网 IP；
- Host Docker socket、服务器真实文件系统路径；
- 其他用户的 Project / Conversation 标识与 upstream ID。

一句话：

```text
authenticated thin client = display stream + input
而不是 remote Chrome credential holder
```

## 14. Kasm Ticket 与同源 WebSocket 网关

Kasm ticket 定义：

```text
绑定：user_id、workspace_id、browser_session_id
属性：short TTL、single use、Kasm WebSocket upgrade 后失效、不可跨用户 / Workspace 复用
```

客户端连接形式（示意）：

```text
POST /api/web-workspace/session/{session_id}/kasm-ticket
GET/POST /api/web-workspace/session/{session_id}/kasm/t/{ticket}/...
```

- ticket 由已认证的 HTTPS API 调用签发；
- 客户端始终通过 New API 同源 Kasm iframe/WebSocket 路径连接，不连接 Browser Agent；
- Agent 仅以私有 service token 代理到 runtime 6901；
- ticket 校验失败、重放或过期 → 直接关闭连接，不降级、不重试放行。
## 15. Shared Upstream Account 风险（重要）

必须明确区分：

```text
navigation isolation ≠ display isolation ≠ upstream identity isolation
```

- 模式 A（强隔离推荐）：

```text
User A → Profile A → Upstream Account A
User B → Profile B → Upstream Account B
```

- 模式 B（多个 New API 用户共享同一个 upstream account/session）：即使 Project navigation 被阻止，Provider 自身的 sidebar、搜索、recent chats、notifications 或 API payload 仍可能包含其他 Project 的元数据。

结论：

- 模式 B 目前属于 **NOT a strong isolation mode**，不得在文档、UI 或对外说明中描述为安全的多租户隔离。
- 如果未来确实要支持共享 upstream 账号，必须独立设计 response filtering、sidebar / data filtering、metadata leakage controls，并配套 provider 版本兼容性测试；在这些机制完成前保持 fail closed。

## 16. API 原则（草案）

Namespace：`/api/web-workspace`，挂在现有 `/api` 分组下，沿用 `middleware.UserAuth()` 等现有鉴权中间件。建议文件边界（实施阶段按仓库当时规范确认）：`router/web-workspace-router.go`、`controller/web-workspace.go`、`service/webworkspace/`、`model/web_*.go`、`dto/web_workspace.go`。

接口草案（本轮不实现）：

```text
GET    /api/web-workspace/status
GET    /api/web-workspace/config

POST   /api/web-workspace/session
DELETE /api/web-workspace/session/:id
POST   /api/web-workspace/session/:id/restart

POST   /api/web-workspace/session/:id/kasm-ticket
GET    /api/web-workspace/session/:id/kasm/*
POST   /api/web-workspace/session/:id/kasm/*

GET    /api/web-workspace/projects
POST   /api/web-workspace/projects
PATCH  /api/web-workspace/projects/:id
DELETE /api/web-workspace/projects/:id

GET    /api/web-workspace/projects/:id/conversations
POST   /api/web-workspace/projects/:id/open

POST   /api/web-workspace/profile/reset
```

原则：

- 普通客户端不要自行选择任意 `workspace_id`；Workspace 一律从当前 authenticated user 推导，`workspace_id` 不是可信输入。
- Project / Conversation API 使用 New API internal ID。
- 外部 Provider ID 默认不返回给客户端（内部映射由服务端持有）。
- 所有 API 后端再次校验 entitlement + ownership，不信任前端状态。
- 拒绝路径不泄露资源存在性（unknown 资源与无权限资源使用一致的 deny 语义，避免枚举）。
- 错误信息不得包含 profile 路径、Agent 地址、CDP endpoint、内部 IP 或 upstream 凭证。

## 17. 前端（Frontend）

结合现有 TanStack Router 结构，未来入口预计位于：

```text
web/src/routes/_authenticated/web-workspace/
└── index.tsx

web/src/features/web-workspace/
├── index.tsx
├── components/
├── hooks/
└── types.ts
```

- 现有 `web/src/routes/_authenticated/route.tsx` 已提供登录守卫；Web Workspace 页面复用该守卫，并增加 entitlement route guard。
- 根 Sidebar 当前由 `web/src/hooks/use-sidebar-data.ts` 提供；入口名称使用 "Web Workspace" 或 "Web 工作区"，不写死 "GPT Pro" / "OpenAI Browser"。
- 与后端 sidebar module 配置（`model/user.go` 默认边栏配置、`HeaderNavModuleAuth` 一类中间件）的集成方式在实施阶段确定；菜单可见性永远不是安全边界。
- 建议页面结构：Provider / runtime 状态、Project 列表（含 New Project）、浏览器 surface、Session 控制。
- UI 不需要暴露浏览器地址栏；kiosk / app-like window 属于 UX 与防御纵深，不是安全边界。
- 文案必须走 i18n（`useTranslation()` + `t()`，flat JSON locale keys）。
- 移动端行为必须明确（支持或显式不支持），不能默认假设桌面体验可用。

## 18. 安全失败模式与审计

安全相关模块必须 fail closed：

```text
DB 查不到 Project                 → deny
Provider adapter 无法解析          → deny
Browser Guard 失联                 → pause / terminate session
Egress policy 加载失败             → 无外网
entitlement 服务或缓存异常          → 拒绝创建 session
stream ticket 不匹配                → close
未知 popup / 新 target             → block
未知 external resource             → block
```

审计事件至少包括：

```text
workspace created / browser started / browser stopped
login mode entered / exited
project created / renamed / deleted
profile reset
policy deny
stream attached / detached
admin entitlement change
```

日志中不得包含：upstream Cookie、Authorization header、session token、CDP WebSocket URL、profile 内容、明文密码、OAuth 凭证。Policy deny 日志建议字段：`user_id`、`workspace_id`、resource type、internal resource id（安全时）、reason、timestamp、request correlation id。

## 19. 实施阶段与验收

实施按 checklist 的六个阶段推进（细节与验收标准见 [implementation-checklist.md](./implementation-checklist.md)）：

1. Phase 1 核心模型与权限：global setting、entitlement、三张表、ownership service、三数据库 migration 验证。
2. Phase 2 Browser Agent MVP：runtime manager、虚拟显示、GUI Chromium、per-workspace profile、Kasm proxy、Kasm ticket、生命周期。
3. Phase 3 Network / Browser Guard：default-deny egress、私网与 metadata 拒绝、Login / Locked Mode、CDP loopback、navigation / popup / download / clipboard guard。
4. Phase 4 ChatGPT Provider Adapter：URL parser、资源发现与观察、unknown deny。
5. Phase 5 前端：sidebar 一级入口、route guard、Project 管理、remote surface、错误态、i18n 与可访问性。
6. Phase 6 安全验证：IDOR / 横向越权、ticket 重放、CDP / VNC 暴露、SSRF、文件系统穿越、跨 Workspace 访问、fail-closed 行为等。

当前状态（2026-09-18）：仅完成文档初始化；上述所有阶段均为 `NOT RUN`。

## 20. 待决事项（实施阶段必须先确认）

- 显示实现已选定 KasmVNC 原生 HTTP/WebSocket；后续只需保持 Kasm proxy/ticket 边界不泄漏 runtime 地址。
- entitlement 的存储位置（系统设置 / 用户组 / 独立表）与缓存策略。
- user override 是否进入首版。
- runtime 调度形态：每 Workspace 一容器，还是共享 agent 中的隔离进程（无论哪种都必须满足第 7 节隔离要求）。
- Workspace 删除与 profile 保留策略（软删除周期、用户数据导出与清理）。
- 审计事件落地位置（现有日志体系还是新表）。

## 21. 术语表

| 术语 | 含义 |
| --- | --- |
| Web Workspace | New API 的一级功能；一个用户的服务器端浏览器工作区 |
| Workspace | 用户的唯一工作区实体（`web_workspaces` 一行） |
| Project | Provider 侧项目在本地的映射（`web_projects` 一行） |
| Conversation | Provider 侧会话在本地的映射（`web_conversations` 一行） |
| Thin Client | 本地 PC；只接收画面、发送输入 |
| Browser Agent | 服务器端执行平面组件 |
| Browser Guard | Browser Agent 内的安全控制组件 |
| Stream Ticket | New API 签发的一次性 WSS 连接票据 |