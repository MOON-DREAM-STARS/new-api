# NewAPI Web Workspace 文档初始化与实施 Checklist

> Repository: `MOON-DREAM-STARS/new-api`  
> Target branch: `main`（执行 Agent 应先确认实际工作分支与工作区状态）  
> Recommended archive path: `docs/web-workspace/implementation-checklist.md`  
> Phase: **Documentation Initialization / Architecture Baseline**  
> Scope: 本清单首先用于建立架构文档、边界条件、数据模型、安全约束、实施阶段与验收标准。除非另有明确指令，本阶段**不实现 Browser Agent、Chromium、KasmVNC/noVNC 或 Web Workspace 业务代码**。
>
> **归档记录（2026-09-18）**：已按本清单完成第一轮文档初始化，归档于 `docs/web-workspace/implementation-checklist.md`，归档提交信息为 `docs: initialize web workspace architecture`，归档时工作分支为 `feature/dreamstars-homepage`。稳定设计基线见 [architecture.md](./architecture.md)；第 26 节起的全部实现阶段尚未开始（`NOT RUN`）。

---

## 0. 项目目标

在 NewAPI 中增加一个新的一级功能 **Web Workspace / Web 浏览器工作区**，用于让经过授权的 NewAPI 用户通过 NewAPI Web UI 操作运行在服务器端 Linux 环境中的受控 Chromium。

目标不是把 `chatgpt.com` 反向代理进 NewAPI，也不是把 Chrome、Cookie、Session Token 或 CDP 暴露给用户本地 PC。

目标架构应满足：

- 本地 PC 只接触 NewAPI 页面和远程画面/输入流。
- Chromium、Browser Profile、Cookie、LocalStorage、IndexedDB、缓存、上传/下载目录均位于服务器端。
- NewAPI 用户与 Web Workspace 一一对应。
- 每个 Project 只属于一个 Workspace，因此项目所有权天然互斥。
- Conversation 通过 Project 继承所有权，不单独维护 ACL。
- Browser Workspace 使用服务器侧网络策略、域名/地址限制、资源所有权校验和能力限制。
- 能按 NewAPI 用户等级、用户组或独立 entitlement 决定是否开放 Web Workspace。
- 架构首先面向 ChatGPT Web，但 Provider 适配层不能写死为 ChatGPT，未来应能扩展到 Claude、Gemini 等 Web AI。
- 不依赖客户端直接运行或控制 Chromium。

---

# 1. 核心不变量（必须写进设计文档）

## 1.1 User / Workspace / Project 所有权

采用单一所有权模型，而不是多对多 ACL：

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

必须满足：

```text
1 NewAPI User
=
0 or 1 Web Workspace

1 Web Workspace
=
1 Browser Profile
=
N Projects

1 Project
=
exactly 1 Workspace

1 Conversation
=
exactly 1 Project
```

禁止设计为：

```text
Project A0
├── User A
└── User B
```

也不要为第一版引入通用的：

- resource ACL table
- project-user join table
- per-project permission matrix
- owner_user_id + workspace_id 双重冗余所有权

项目 Owner 应通过关系链唯一推导：

```text
Project
  ↓
Workspace
  ↓
User
```

Conversation Owner 同样通过：

```text
Conversation
  ↓
Project
  ↓
Workspace
  ↓
User
```

---

## 1.2 权限只分两类

### Feature entitlement

回答：

> 这个 NewAPI User 能不能使用 Web Workspace？

示例因素：

- user.status
- user.role
- user.group
- Web Workspace global enabled
- Web Workspace group policy
- optional per-user override

### Resource ownership

回答：

> 这个 Project / Conversation 是不是属于当前用户的 Workspace？

统一判断原则：

```text
CanAccessProject(currentUser, project)
=
project.workspace.user_id == currentUser.id

CanAccessConversation(currentUser, conversation)
=
conversation.project.workspace.user_id == currentUser.id
```

不要把 Feature entitlement 和 Resource ownership 混成一套 ACL。

---

# 2. 推荐总体架构

设计文档必须至少包含下列拓扑：

```text
┌──────────────────────────────────────────┐
│ Local PC / Browser                       │
│                                          │
│ NewAPI UI                                │
│ /web-workspace                           │
│                                          │
│ Receives only:                           │
│ - remote display stream                  │
│ - workspace metadata allowed to user     │
│ Sends only:                              │
│ - keyboard/mouse/touch input             │
│ - explicitly permitted actions           │
└──────────────────────┬───────────────────┘
                       │ HTTPS / WSS
                       ▼
┌──────────────────────────────────────────┐
│ NewAPI                                   │
│                                          │
│ Authentication                           │
│ Feature Entitlement                      │
│ Workspace Ownership Resolver             │
│ Session Broker                           │
│ One-time Stream Token                    │
│ WebSocket Stream Gateway                 │
│ Web Workspace API                        │
└──────────────────────┬───────────────────┘
                       │ private network
                       ▼
┌──────────────────────────────────────────┐
│ Browser Workspace Manager / Agent        │
│                                          │
│ Workspace A runtime                      │
│ ├─ Chromium                              │
│ ├─ Virtual display                       │
│ ├─ Remote display server                 │
│ ├─ Browser Guard                         │
│ └─ Profile / files A only                │
│                                          │
│ Workspace B runtime                      │
│ ├─ Chromium                              │
│ ├─ Virtual display                       │
│ ├─ Remote display server                 │
│ ├─ Browser Guard                         │
│ └─ Profile / files B only                │
└──────────────────────┬───────────────────┘
                       │ controlled egress
                       ▼
                  Internet / Provider
```

设计原则：

- NewAPI 是 control plane + auth gateway。
- Browser Agent 是 execution plane。
- 不让 NewAPI 主进程直接管理 Chromium 子进程作为长期方案。
- 不给 NewAPI 容器挂 `/var/run/docker.sock`。
- Browser Agent 的内部端口不能直接 publish 到公网。
- CDP 只能位于 Workspace runtime 内部，不能暴露给浏览器客户端。
- 用户前端永远通过 NewAPI 建立受控 WSS stream。

---

# 3. 本地 PC 数据最小化要求

客户端必须被设计为 Thin Client。

## 3.1 本地 PC 可以接触

- NewAPI 登录 Session
- Web Workspace 页面
- 当前用户被允许看到的 Workspace/Project 展示信息
- 远程像素/显示流
- 键盘、鼠标、触摸输入
- 短时、单次使用的 NewAPI stream ticket

## 3.2 本地 PC 不得接触

- Chromium profile
- ChatGPT/OpenAI Cookie
- upstream session token
- LocalStorage
- IndexedDB
- browser cache
- CDP endpoint
- `remote-debugging-port`
- VNC/KasmVNC 原始密码
- Browser Agent 内网地址
- Browser container IP
- Host Docker socket
- 服务器文件系统真实路径
- 其他用户 Project ID
- 其他用户 Conversation ID

## 3.3 Stream ticket

文档应定义短期 stream ticket：

```text
bound to:
- user_id
- workspace_id
- browser_session_id

properties:
- single use
- short TTL
- invalid after WebSocket upgrade
- cannot be reused across users/workspaces
```

客户端连接形式建议：

```text
wss://newapi.example.com/api/web-workspace/sessions/{session_id}/stream
```

不要返回：

```text
ws://browser-agent:6080
password=...
```

---

# 4. 用户等级 / 用户组开放策略

现有 NewAPI Role：

```text
GUEST = 0
USER = 1
ADMIN = 10
SUPER_ADMIN = 100
```

Web Workspace 不应简单等价于 Admin 权限。

## 4.1 推荐 entitlement 模型

至少设计：

```text
global_enabled
minimum_role
allowed_groups
optional_user_override
```

建议语义：

```text
global enabled
AND user enabled
AND role >= minimum_role
AND group allowed
AND user override != deny
```

如果存在 allow override，应明确优先级。

## 4.2 推荐未来扩展字段

可以先文档化，不要求首个 schema 全部实现：

```text
enabled
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

## 4.3 UI / API 双重 enforcement

必须同时：

- Sidebar 隐藏无权限入口。
- Frontend route guard 阻止直接导航。
- Backend API 强制校验 entitlement。
- Browser session creation 强制再次校验。
- Stream attach 强制再次校验用户和 Workspace。

前端隐藏菜单从来不能作为安全边界。

---

# 5. 数据模型

数据库必须兼容：

- SQLite
- MySQL >= 5.7.8
- PostgreSQL >= 9.6

遵守项目 `AGENTS.md` 的 GORM 和跨数据库要求。

## 5.1 `web_workspaces`

建议字段：

```text
id
user_id                  UNIQUE
provider
status
created_at
updated_at
last_active_at
```

注意：

- `user_id` 唯一，保证一个 User 只有一个 Workspace。
- 不建议将真实服务器 profile 绝对路径直接作为 API 可见字段。
- 如果需要内部 storage key，应使用逻辑 ID，而不是暴露 filesystem path。
- Workspace 删除策略必须在设计文档中说明：软删除、硬删除、Profile 清理分别如何处理。

## 5.2 `web_projects`

建议字段：

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

不要增加冗余 `owner_user_id`，Owner 从 workspace 推导。

## 5.3 `web_conversations`

建议字段：

```text
id
project_id
external_conversation_id
title
created_at
updated_at
```

约束：

```text
UNIQUE(provider?, external_conversation_id)
INDEX(project_id)
```

是否需要 `provider` 取决于 conversation ID 是否在 provider namespace 中唯一，文档必须明确选择。

## 5.4 Browser session

运行时 session 可优先放：

- memory
- Redis

如有审计需求再持久化数据库。

需要记录的逻辑字段：

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

敏感连接凭证不得持久化明文。

---

# 6. 文件系统隔离

推荐目录模型：

```text
/data/web-workspaces/
└── workspace-<id>/
    ├── profile/
    ├── uploads/
    ├── downloads/
    ├── cache/
    └── tmp/
```

不要依赖：

```text
/data/web-workspaces/user-<id>/...
```

作为唯一关联依据；数据库 Workspace ID 应为主要内部标识，User 所有权通过数据库解析。

## 6.1 Runtime mount

Workspace A runtime 只能 mount：

```text
/data/web-workspaces/workspace-A
```

不得 mount：

```text
/data/web-workspaces
```

整个根目录。

## 6.2 Linux 隔离

设计文档应要求：

- dedicated unprivileged UID/GID
- no privileged container
- read-only root filesystem where feasible
- writable mount 仅限 workspace-specific directories
- drop unnecessary Linux capabilities
- no host network
- no Docker socket
- no host PID namespace
- no host IPC namespace
- resource limits for CPU/RAM/PIDs
- per-workspace temp directory
- no cross-workspace shared browser profile

## 6.3 Profile 生命周期

必须说明：

- 首次创建
- 浏览器重启
- Server 重启
- 用户 logout NewAPI
- Upstream logout
- Workspace reset
- User deletion
- Admin disable
- storage cleanup

尤其要区分：

```text
Stop browser
≠
Delete profile
```

---

# 7. 浏览器运行方式

不要采用真正的：

```text
chromium --headless
```

作为主要交互环境。

推荐：

```text
Virtual Display (Xvfb / equivalent)
    ↓
Chromium
    ↓
KasmVNC or VNC + web transport
```

Chromium 应运行在服务器端正常图形会话中。

## 7.1 第一阶段候选

允许 Agent 在设计文档中比较：

- KasmVNC
- noVNC + x11vnc + websockify

选择标准：

- WebSocket 支持
- 剪贴板控制
- 输入控制
- resize
- TLS termination compatibility
- embedding capability
- maintenance complexity
- attack surface

最终 Browser Agent 应通过抽象接口封装远程显示实现，避免业务层直接依赖某个 VNC 实现。

---

# 8. Provider Adapter

核心 Workspace Engine 不得写死 ChatGPT URL 规则。

建议抽象：

```text
ProviderAdapter

- ParseNavigation(url)
- ClassifyResource(url)
- ResolveProject(...)
- ResolveConversation(...)
- ObserveProjectCreated(...)
- ObserveProjectRenamed(...)
- ObserveProjectDeleted(...)
- ObserveConversationCreated(...)
- AllowedHosts(mode)
```

首个实现：

```text
providers/chatgpt
```

未来：

```text
providers/claude
providers/gemini
```

ChatGPT-specific URL 和网络接口只能存在于 adapter/guard 层，不应扩散到核心 ownership 模型。

---

# 9. 域名与网络限制

必须有 egress policy。

域名 allowlist 只是第一层，不是 Project 隔离机制。

## 9.1 默认网络策略

原则：

```text
DENY by default
ALLOW only provider-required hosts
```

至少明确阻止：

```text
127.0.0.0/8
10.0.0.0/8
172.16.0.0/12
192.168.0.0/16
169.254.0.0/16
::1
fc00::/7
fe80::/10
localhost
host.docker.internal
cloud metadata endpoints
internal DNS zones where applicable
```

目标：

- 防 SSRF
- 防扫描宿主机
- 防访问 NewAPI 内网服务
- 防访问数据库/Redis
- 防访问 Docker/metadata endpoint

## 9.2 Login Mode

OAuth 登录可能需要临时访问：

- OpenAI auth host
- Google identity
- Microsoft identity
- other explicit identity providers

因此区分：

```text
LOGIN_MODE
LOCKED_MODE
```

Login Mode：

- 时间有限
- 仅管理员或用户显式启动
- 扩展 identity allowlist
- 登录完成后回到 Locked Mode

Locked Mode：

- 仅 Provider 运行所需域名
- 继续拒绝私网

---

# 10. Project / Conversation 所有权隔离

## 10.1 Project

访问 `/g/...` 或等价 Provider Project 路由时：

```text
external_project_id
        ↓
web_projects
        ↓
workspace_id
        ↓
web_workspaces.user_id
        ↓
current_user_id
```

只有相等才允许。

## 10.2 Conversation

访问 `/c/...` 时不得只按 URL pattern allow。

必须：

```text
external_conversation_id
        ↓
web_conversations
        ↓
project_id
        ↓
workspace_id
        ↓
user_id
```

## 10.3 Unknown resource

默认：

```text
unknown project        → DENY
unknown conversation   → DENY
```

唯一例外是处于明确的 create permit 流程。

---

# 11. Project 创建

用户可为自己的 Workspace 创建新 Project。

推荐流程：

```text
User clicks New Project
    ↓
NewAPI checks:
- entitlement
- workspace ownership
- max_projects
- active browser session
    ↓
create short-lived creation permit
    ↓
Browser Agent permits create action
    ↓
Provider returns new external project ID
    ↓
Agent registers project to current workspace
    ↓
permit consumed
```

Creation permit 应：

- bound to user
- bound to workspace
- short TTL
- single purpose
- single use
- auditable

新建 Project 一旦登记，即天然归当前 Workspace 所有。

不需要创建 ACL rows。

---

# 12. Project 重命名 / 删除

统一：

```text
GetOwnedProject(current_user, internal_project_id)
```

只有 Owner 可：

- open
- rename
- delete
- create child conversation

删除必须定义同步策略：

```text
Provider delete succeeded
    ↓
local mark deleted / remove mapping
```

若 Provider delete 成功、本地 DB 更新失败：

- 必须可恢复/重同步
- 记录异常
- 不允许本地继续认为资源可访问而实际已不存在

反向失败同理。

---

# 13. Browser Guard

Browser Guard 是安全组件，不是普通 UI helper。

至少考虑：

```text
Navigation Guard
Network Guard
Target/Popup Guard
Download Guard
Clipboard Guard
Resource Ownership Resolver
Provider Observer
```

## 13.1 CDP

如果使用 Chromium DevTools Protocol：

- 仅监听 Workspace runtime 内 loopback。
- 禁止 publish 到 host。
- 禁止 NewAPI 前端获得 endpoint。
- 禁止返回 DevTools websocket URL。
- NewAPI 不应把 CDP 当作客户端协议。
- Browser Guard 是唯一 CDP consumer。

## 13.2 不依赖 UI DOM 作为唯一安全边界

DOM filtering 可以用于减少信息展示，但不能成为 ownership enforcement 的唯一手段。

Provider 前端变更不能导致：

```text
DOM selector changed
→ security boundary silently fails open
```

必须 fail closed。

---

# 14. ChatGPT 同账号多 Workspace 风险

设计文档必须明确区分：

## 模式 A：独立 upstream account/session

```text
User A → Profile A → Upstream Account A
User B → Profile B → Upstream Account B
```

这是强隔离推荐模式。

## 模式 B：多个 NewAPI User 共用一个 upstream account/session

即使 Project navigation 被阻止，Provider 自己的 sidebar、搜索、recent chats、notifications 或 API payload 仍可能包含其他 Project 的元数据。

因此：

```text
Navigation isolation
≠
Display isolation
≠
Upstream identity isolation
```

如果未来允许这种模式，必须单独设计：

- response filtering
- sidebar/data filtering
- metadata leakage controls
- provider-version compatibility tests

在这些机制完成前，文档应将其标记为：

```text
NOT a strong isolation mode
```

并避免把它描述成“安全的多租户隔离”。

---

# 15. NewAPI API 设计草案

建议 namespace：

```text
/api/web-workspace
```

候选 API：

```text
GET    /api/web-workspace/status
GET    /api/web-workspace/config

POST   /api/web-workspace/session
DELETE /api/web-workspace/session/:id
POST   /api/web-workspace/session/:id/restart

POST   /api/web-workspace/session/:id/stream-ticket
WS     /api/web-workspace/session/:id/stream

GET    /api/web-workspace/projects
POST   /api/web-workspace/projects
PATCH  /api/web-workspace/projects/:id
DELETE /api/web-workspace/projects/:id

GET    /api/web-workspace/projects/:id/conversations
POST   /api/web-workspace/projects/:id/open

POST   /api/web-workspace/profile/reset
```

文档化原则：

- API 使用内部 project ID。
- 外部 Provider project ID 默认不返回给普通客户端。
- 所有 project/conversation API 都通过当前 authenticated user 解析 ownership。
- 禁止 client 提交任意 `workspace_id` 访问别人的 Workspace。
- server 从 current user 推导 workspace。
- `workspace_id` 不应成为普通客户端的信任输入。

---

# 16. 前端路由与页面

当前 NewAPI 使用 TanStack Router，建议：

```text
web/src/routes/_authenticated/web-workspace/
└── index.tsx
```

Feature：

```text
web/src/features/web-workspace/
├── index.tsx
├── components/
├── hooks/
└── types.ts
```

根 Sidebar 当前由：

```text
web/src/hooks/use-sidebar-data.ts
```

提供。

## 16.1 一级入口

推荐：

```text
Web Workspace
```

或中文：

```text
Web 工作区
```

不要写死：

```text
GPT Pro
OpenAI Browser
```

## 16.2 页面结构

建议：

```text
Web Workspace
├── Provider / runtime status
├── Projects
│   ├── A0
│   ├── A1
│   └── + New Project
├── Browser surface
└── Session controls
```

浏览器地址栏建议不暴露。

可使用 kiosk/app-like window，但这只是 UX/defense-in-depth，不是安全边界。

---

# 17. Clipboard / 文件传输

安全默认值：

```text
clipboard local -> remote    OFF
clipboard remote -> local    OFF
upload                       OFF
download                     OFF
drag & drop                  OFF
printing                     OFF
devtools                     OFF
view-source                  OFF
```

以后由 capability policy 开启。

必须分别区分：

```text
clipboard_in
clipboard_out
upload
download
```

不要只用一个 `clipboard_enabled`。

---

# 18. Browser Agent 边界

Browser Agent 应作为独立组件。

建议逻辑：

```text
browser-agent/
├── manager
├── runtime
├── guard
├── display
├── providers/
└── storage
```

NewAPI 与 Agent 通信需要：

- private network only
- service authentication
- request signing or short-lived service token
- no public listener
- no trust based only on source IP

Browser Agent 不应该：

- 接受任意用户 ID 并自行信任
- 接受任意 filesystem path
- 接受任意 target URL
- 接受任意 CDP command from NewAPI client

---

# 19. Runtime 调度

第一版可以：

```text
1 active browser runtime per Workspace
```

但 Profile 是持久的，runtime 是临时的。

区分：

```text
Workspace
persistent

Profile
persistent

Browser Runtime
ephemeral

Browser Session
ephemeral
```

建议状态：

```text
STOPPED
STARTING
RUNNING
IDLE
STOPPING
FAILED
```

需要考虑：

- duplicate start
- concurrent requests
- crashed Chromium
- crashed Browser Agent
- orphan runtime
- NewAPI restart
- Browser Agent restart
- stale session ticket
- session reconnect

---

# 20. 审计与日志

至少记录：

```text
workspace created
browser started
browser stopped
login mode entered/exited
project created
project renamed
project deleted
profile reset
policy deny
stream attached
stream detached
admin entitlement change
```

日志中不得包含：

- upstream cookies
- authorization headers
- session tokens
- CDP websocket URLs
- browser profile content
- plaintext passwords
- OAuth credentials

Policy deny 日志应记录：

```text
user_id
workspace_id
resource type
internal resource id where safe
reason
timestamp
request correlation id
```

---

# 21. 安全失败模式

安全相关模块必须：

```text
fail closed
```

例如：

- DB 查不到 Project → deny
- Provider adapter 无法解析 → deny
- Browser Guard 失联 → pause/terminate session
- Egress policy 加载失败 → no Internet access
- entitlement service error → deny session creation
- stream ticket mismatch → close
- unknown popup target → block
- unknown external resource → block

不得：

```text
解析失败 → allow
```

---

# 22. 推荐代码边界（后续实现阶段）

NewAPI：

```text
router/
└── web-workspace-router.go

controller/
└── web-workspace.go

service/
└── webworkspace/
    ├── manager.go
    ├── entitlement.go
    ├── ownership.go
    ├── session.go
    ├── stream.go
    └── agent_client.go

model/
├── web_workspace.go
├── web_project.go
└── web_conversation.go

dto/
└── web_workspace.go
```

Frontend：

```text
web/src/routes/_authenticated/web-workspace/
web/src/features/web-workspace/
```

独立 Browser Agent：

```text
browser-agent/
```

最终具体目录需要由实施 Agent 根据仓库现有规范确认，不应机械创建不符合现有布局的结构。

---

# 23. README 初始化要求

文档初始化 commit 应在 README 的 Documentation 或合适位置添加简短入口。

README 不应复制完整架构。

推荐仅包含：

```text
Web Workspace (experimental/design)

Server-side isolated browser workspace design for controlled Web AI access.

Documentation:
- Architecture / implementation checklist
```

要求：

- 明确标记当前阶段（design / planned / experimental），不能写成已经完成。
- 不应改变或删除现有 New API / QuantumNous 归属、品牌、License、NOTICE。
- 不修改不相关 README 内容。
- 如仓库维护多语言 README，Agent 应检查现有同步约定，再决定是否同步；不得擅自制造翻译不一致而不说明。

---

# 24. 文档目录建议

本 checklist 建议归档：

```text
docs/web-workspace/implementation-checklist.md
```

同时建议生成：

```text
docs/web-workspace/architecture.md
```

职责：

### `architecture.md`

稳定的设计文档：

- goal
- architecture
- trust boundaries
- ownership model
- data model
- Browser Agent boundary
- provider adapter
- security model
- lifecycle
- API principles
- rollout phases

### `implementation-checklist.md`

执行清单：

- 尚未完成的工作
- decisions
- milestones
- acceptance checks
- validation status

不要把 checklist 放进 `.agents/`，除非仓库后续形成明确的 Agent 专用设计文档规范。

---

# 25. 文档初始化阶段 Agent 必做事项

- [ ] 读取根目录 `AGENTS.md`。
- [ ] 检查 `web/AGENTS.md` 是否存在；存在则读取与前端相关要求。
- [ ] 检查仓库当前 branch 和 working tree。
- [ ] 不覆盖或提交用户已有的无关修改。
- [ ] 阅读 README Documentation / Deployment / Security 相关章节。
- [ ] 阅读现有 auth/session 文档，确保 Web Workspace 不绕开现有 authentication。
- [ ] 检查现有用户 role/group/sidebar 配置实现。
- [ ] 检查 Gin Router → Controller → Service → Model 约定。
- [ ] 检查现有 Docker Compose / deployment structure。
- [ ] 将本 checklist 存档到 `docs/web-workspace/implementation-checklist.md`，如仓库结构强烈表明另一路径更合适，可调整并说明原因。
- [ ] 创建 `docs/web-workspace/architecture.md`。
- [ ] architecture 文档必须体现 User → Workspace → Project → Conversation 单一所有权链。
- [ ] 删除/避免上一版多用户共享 Project ACL 的设计。
- [ ] 文档中保留 entitlement 与 ownership 两层模型。
- [ ] 文档中明确 Domain/Egress ACL 与 Project Ownership Guard 是不同边界。
- [ ] 文档中明确 local PC 不接触 Chromium profile、Cookie、CDP、VNC credential。
- [ ] 文档中明确 Browser Agent 是独立 execution plane。
- [ ] 文档中明确不把 Docker socket mount 给 NewAPI。
- [ ] 文档中明确 Browser Agent/CDP/VNC internal endpoints 不公开。
- [ ] 文档中说明同 upstream account 多租户的 metadata leakage 风险。
- [ ] 文档中明确强隔离推荐独立 upstream profile/account。
- [ ] README 增加简短 Web Workspace 文档入口。
- [ ] README 明确功能仍处于 design/planned 状态。
- [ ] 不修改与此次文档初始化无关的代码。
- [ ] 不新增数据库 migration。
- [ ] 不新增 Browser Agent implementation。
- [ ] 不新增前端页面 implementation。
- [ ] 检查 Markdown links。
- [ ] 检查 git diff，确保只包含预期 docs/README 修改。
- [ ] 创建一个独立 commit。
- [ ] 建议 commit message：`docs: initialize web workspace architecture`
- [ ] 最终报告 commit SHA、变更文件、关键设计决策以及仍未实施的部分。

---

# 26. 实施 Phase 1：核心模型与权限

文档初始化之后再进入代码阶段。

- [x] 定义 Web Workspace global setting。
- [x] 定义 group/role entitlement。
- [x] 明确 user override 是否首版实现。
- [x] 建立 `web_workspaces`。
- [x] 建立 `web_projects`。
- [x] 建立 `web_conversations`。
- [x] 保证三数据库兼容。
- [x] fresh migration 验证。
- [x] upgrade migration 验证。
- [x] migration idempotency 验证。
- [x] ownership service。
- [x] entitlement service。
- [x] API authorization tests。
- [x] cross-user access tests。
- [x] unknown resource deny tests。

Acceptance：

```text
User A 无法通过 API 读取/修改 User B 的 Project/Conversation。
客户端提交 workspace_id/project_id 不能突破 current-user ownership。
```

---

# 27. 实施 Phase 2：Browser Agent MVP

- [x] 独立 Browser Agent。
- [x] Workspace runtime manager。
- [x] X virtual display。
- [x] Chromium GUI session。
- [x] per-workspace profile mount。
- [x] remote display transport。
- [x] Browser Agent service auth。
- [x] no public Browser Agent port。
- [x] NewAPI stream broker。
- [x] one-time stream ticket。
- [x] session lifecycle。
- [x] crash recovery。
- [x] idle timeout。
- [x] runtime resource limits。

Acceptance：

```text
客户端只能通过 NewAPI 连接。
本地客户端无法直连 Browser Agent。
Workspace A runtime 无法看到 Workspace B filesystem。
```

---

# 28. 实施 Phase 3：Network / Browser Guard

- [x] default-deny egress。
- [x] private IP ranges deny。
- [x] metadata endpoint deny。
- [x] Provider host allowlist。
- [x] Login Mode。
- [x] Locked Mode。
- [x] CDP loopback only。
- [x] navigation interception。
- [x] popup/target guard。
- [x] download guard。
- [x] clipboard controls。
- [x] fail-closed behavior。
- [x] deny audit logs。

Acceptance：

```text
Browser 无法访问内网服务。
Browser 无法访问任意公网域名。
未知 navigation 默认被阻止。
Guard 故障不会导致 unrestricted browser。
```

---

# 29. 实施 Phase 4：ChatGPT Provider Adapter

- [x] ChatGPT project URL parser。
- [x] ChatGPT conversation URL parser。
- [x] Project discovery。
- [x] Conversation discovery。
- [x] Project create observation。
- [x] Project rename observation。
- [x] Project delete observation。
- [x] Conversation create observation。
- [x] unknown resource handling。
- [x] provider API/UI change compatibility tests。
- [x] adapter failure → deny。

Acceptance：

```text
A 只能打开 A Workspace 已登记 Project。
A 不能通过手输 URL、history、popup、redirect 打开 B Project。
A 的 Conversation 必须可追溯到 A Project。
```

---

# 30. 实施 Phase 5：前端

- [ ] Sidebar 一级入口。
- [ ] route guard。
- [ ] entitlement disabled UI。
- [ ] project list。
- [ ] create project。
- [ ] rename project。
- [ ] delete project。
- [ ] remote browser surface。
- [ ] session state。
- [ ] reconnect。
- [ ] fullscreen。
- [ ] Browser Agent error states。
- [ ] policy denied state。
- [ ] no exposed upstream IDs where unnecessary。
- [ ] i18n。
- [ ] accessibility。
- [ ] mobile behavior 明确（支持或显式不支持）。

---

# 31. 实施 Phase 6：安全验证

必须覆盖：

- [ ] horizontal privilege escalation。
- [ ] IDOR。
- [ ] forged workspace ID。
- [ ] forged project ID。
- [ ] forged conversation ID。
- [ ] stale stream ticket。
- [ ] replayed stream ticket。
- [ ] browser-agent direct access。
- [ ] CDP exposure。
- [ ] VNC exposure。
- [ ] SSRF。
- [ ] DNS rebinding considerations。
- [ ] private IP access。
- [ ] host.docker.internal access。
- [ ] cloud metadata。
- [ ] cross-workspace filesystem access。
- [ ] symlink/path traversal。
- [ ] upload path traversal。
- [ ] download exfiltration。
- [ ] clipboard exfiltration。
- [ ] popup bypass。
- [ ] redirect bypass。
- [ ] service worker behavior。
- [ ] WebSocket/provider auxiliary hosts。
- [ ] browser crash。
- [ ] guard crash。
- [ ] DB unavailable。
- [ ] entitlement cache stale。
- [ ] concurrent project creation。
- [ ] delete/rename race。
- [ ] session fixation。
- [ ] logs do not contain secrets。

---

# 32. 不做的事情 / Non-goals

在没有单独设计之前，不要：

- 把 ChatGPT Web 直接 reverse proxy 成 NewAPI 子路径。
- 向客户端复制 upstream Cookie。
- 导出 Chromium profile 到客户端。
- 在客户端开放 CDP。
- 在公网开放 Chromium remote debugging。
- 在公网开放 VNC/KasmVNC。
- 给 NewAPI Docker socket。
- 用前端菜单隐藏作为权限控制。
- 用 URL 正则作为 Conversation ownership 的唯一判断。
- 用 DOM selector 作为唯一安全边界。
- 允许 unknown Provider resource fail-open。
- 第一版实现 Project 多 Owner。
- 第一版实现 Project 共享 ACL。
- 第一版实现跨 User Project 分享。
- 把 NewAPI Admin role 当作 Web Workspace 订阅等级。
- 未经设计就允许多个不同真人共享一个个人 upstream account 并宣称为强隔离多租户。

---

# 33. 文档初始化完成标准

本轮仅文档初始化，完成应满足：

- [ ] `docs/web-workspace/architecture.md` 存在。
- [ ] 本 checklist 已归档在仓库合理位置。
- [ ] README 有简洁入口。
- [ ] README 没有把未实现功能描述成已实现。
- [ ] architecture 与 checklist 均明确单一所有权模型。
- [ ] architecture 与 checklist 均明确客户端 Thin Client 原则。
- [ ] architecture 与 checklist 均明确 Domain/Egress Guard。
- [ ] architecture 与 checklist 均明确 Project/Conversation ownership。
- [ ] architecture 与 checklist 均明确 Browser Agent 隔离。
- [ ] architecture 与 checklist 均明确 upstream account/profile 隔离风险。
- [ ] 无业务代码实现混入本 commit。
- [ ] `git diff` 只包含预期文档变更。
- [ ] 已创建单独 commit。
- [ ] Agent 输出 commit SHA。
- [ ] Agent 列出下一阶段尚未实现事项。

---

# 34. 推荐文档初始化 Commit

建议：

```text
docs: initialize web workspace architecture
```

Commit 应只包含类似：

```text
README.md
docs/web-workspace/architecture.md
docs/web-workspace/implementation-checklist.md
```

如果根据仓库规范需要同步其他 README 语言版本，可包含，但 Agent 必须说明原因和范围。

---

# 35. 最终架构一句话

```text
NewAPI User
    ↓ entitlement
Web Workspace
    ↓ exclusive ownership
Projects
    ↓ exclusive ownership
Conversations
    ↓
Isolated server-side Browser Runtime
    ↓ egress + browser guard
Provider Web
```

客户端始终只是：

```text
authenticated thin client
=
display stream + input
```

而不是：

```text
remote Chrome credential holder
```


---

# Status & Evidence（Main 维护）

## Phase 1（核心模型与权限）— PASS

- 状态：**PASS**（本机冻结输入；2026-09-18，Asia/Singapore）
- 基线只读复核：`git status --porcelain=v1 -b`、`git remote -v`、`git rev-parse HEAD` 与 handoff 基线一致：`feature/dreamstars-homepage` @ `32a1e9190e5cdd5a2167f741ca2764e5be9e8a80`；origin=`MOON-DREAM-STARS/new-api`、upstream=`QuantumNous/new-api`。既有 dirty work（README 横幅、outputs/image2ui/*.png）原样保留且未 stage。
- 实现范围：`web_workspaces` / `web_projects` / `web_conversations` 模型与 AutoMigrate 注册；`web_workspace` 全局设置（enabled / minimum_role / allowed_groups）；entitlement service；ownership service；`/api/web-workspace` 最小认证 API（config、status、projects 读取、rename、delete、conversations 列表）；三方言迁移测试与 API 授权测试。
- 首版决策：user override 不实现；`allowed_groups` 为空数组表示不额外限制分组；Admin 不自动开通；Phase 1 的 PATCH/DELETE 仅更新本地映射，Provider 同步与 fail-safe 重同步属 Phase 4。
- 实际命令与结果（均在 `golang:1.26.1-alpine` Linux 容器内执行，挂载源码目录）：
  - `gofmt -l model service/webworkspace controller router setting/system_setting dto` → 本次改动文件无输出（`controller/channel_pin_retry_test.go` 为仓库既有未格式化文件，未被本次改动）。
  - `go vet ./model ./service/webworkspace ./controller ./router ./setting/system_setting` → VET_OK。
  - `go build ./...` → BUILD_OK。
  - `go test -p 1 ./model -run WebWorkspace -count=1`（TEST_MYSQL_DSN / TEST_POSTGRES_DSN 指向真实实例）→ PASS：SQLite、MySQL 5.7.44、PostgreSQL 9.6.24。
  - `go test -p 1 ./service/webworkspace ./router -run WebWorkspace -count=1` → PASS：ownership 隔离在 SQLite/MySQL/PostgreSQL 三方言各执行一次；router 层 6 个真实 HTTP + 真实 `middleware.UserAuth()` + 真实数据库测试（含 A 读/改/删 B 的项目、伪造 workspace_id 越权、entitlement 关闭拒绝、option 设置联动）。
  - 真实服务启动矩阵：fresh 与 “baseline 32a1e919 → 新二进制” upgrade 路径在 MySQL 5.7.44 / PostgreSQL 9.6.24 / SQLite 上各启动两次；每次日志 `database migration started` 1 次、`fatal|panic|migration failed` 0 次；升级后三张表与全部命名索引存在，升级前写入的 `options` marker 行保留。
- NOT RUN / 未覆盖（如实记录）：Phase 2 Browser Agent、Phase 3 Network/Browser Guard、Phase 4 ChatGPT Provider Adapter、Phase 5 前端、Phase 6 安全验证（均属后续 checklist 节）；Provider live 登录（需用户手动登录窗口）；云端部署（未开始，需用户单独确认连接方式与授权范围）。
- 提交：本阶段独立 commit（`feat(web-workspace): phase 1 core models, entitlement and ownership`），本地未 push；SHA 见阶段报告。

## Phase 2（Browser Agent MVP）— PASS

- 状态：**PASS**（本机冻结输入；2026-09-18，Asia/Singapore）
- 实现范围：
  - 独立 Go module `browser-agent/`：Docker Engine API 客户端（unix socket、仅 stdlib net/http）、runtime manager 状态机（STARTING→RUNNING→IDLE→STOPPING→STOPPED/FAILED）、per-workspace 串行化、idle timeout 扫描、容器消失检测、agent 重启 reconcile（adopt running / remove stopped）、RFB WebSocket 代理；HTTP API 全部要求 Bearer service token（常数时间比较），快照不含容器地址、端口或宿主路径。
  - runtime 镜像 `newapi-web-workspace-runtime:local`：alpine 3.20 + Chromium + Xvfb + x11vnc（uid/gid 10001），SIGTERM 清理链，重启时只清理 Chromium singleton 锁。
  - NewAPI 侧 session broker：`service/webworkspace/{agent_client,session,ticket}.go`、`controller/web_workspace.go`（session 控制面 + WSS gateway + 同源 Origin 校验）、`router/web-workspace-router.go`（控制面 UserAuth；stream 走一次性 ticket）、`dto/web_workspace.go`、`setting/system_setting/web_workspace.go`（AgentBaseURL；service token 仅读 env `WEB_WORKSPACE_AGENT_TOKEN`）。
- 实际命令与结果（均在 `golang:1.26.1-alpine` 容器内、对冻结源码执行）：
  - `gofmt -l model service/webworkspace controller router setting/system_setting dto` → 本次改动文件无输出（`controller/channel_pin_retry_test.go` 为仓库既有未格式化文件，未被本次改动）。
  - `go vet ./model ./service/webworkspace ./controller ./router ./setting/system_setting` → VET_OK。
  - `go test -p 1 ./model -run WebWorkspace -count=1`（TEST_MYSQL_DSN / TEST_POSTGRES_DSN 指向真实 MySQL 5.7.44 / PostgreSQL 9.6.24）→ PASS。
  - `go test -p 1 ./service/webworkspace ./router ./controller -run WebWorkspace -count=1` → PASS。
  - `go build ./...` → BUILD_OK。
  - `cd browser-agent && gofmt -l .`（无输出）、`GOWORK=off go vet ./...` → VET_OK、`GOWORK=off go test ./... -count=1` → 全部 ok、`GOWORK=off go build ./cmd/browser-agent` → BUILD_OK。
  - E2E（真实 agent 容器 + 真实 runtime 容器 + 真实 VNC）：`go test ./service/webworkspace -run WebWorkspaceAgentEndToEnd -count=1 -v` → PASS（1.82s，读得 RFB banner）；`go test ./router -run WebWorkspaceRouterAgentEndToEnd -count=1 -v` → PASS（1.86s，NewAPI WSS gateway → agent → VNC）。
  - 隔离与边界验收（真实 Docker，runtime 101/102 同时运行）：`docker inspect` 证实 `ReadonlyRootfs=true`、`CapDrop=["ALL"]`、`no-new-privileges:true`、Memory=1GiB、NanoCpus=1、PidsLimit=256、ShmSize=512MiB、`PortBindings={}`，仅私有网络 `newapi-workspace-runtimes`，仅挂载 `<host-data-root>/workspace-<id>:/workspace`；A/B marker 互不可见；容器内 `/data`、宿主 data root、其他 workspace 路径均不存在。
  - 无公网端口：`docker port` 对 agent 与 runtime 均为空；宿主 `127.0.0.1:8730` connection refused（目标计算机积极拒绝）。
  - crash recovery（真实 Docker）：`docker restart ws-agent` 后日志 `reconciled runtime containers adopted=2 removed=0`，私有网络内 `GET /internal/v1/runtimes/101` 返回 `state=RUNNING`。
  - 验收后已清理测试容器（runtime 101/102、agent）；测试网络与数据卷保留供后续阶段复用。
- NOT RUN / 未覆盖（如实记录）：真实 VNC 客户端的画面渲染与交互（属 Phase 5 前端验收）；Browser Guard / CDP 独占 / egress 策略（Phase 3）；长期 idle timeout 与多用户并发负载；镜像 CVE 扫描；云端部署（未开始，需用户单独确认连接方式与授权范围）。
- 提交：本阶段独立 commit（`feat(web-workspace): phase 2 browser agent and stream broker`），本地未 push；SHA 见阶段报告。

## Phase 3（Network / Browser Guard）— PASS

- 状态：**PASS**（本机冻结输入；2026-09-18，Asia/Singapore）
- 实现范围：
  - `browser-agent/internal/policy`（Main 冻结的共享策略表：LOCKED/LOGIN 域名 allowlist；仅 http/https 与 80/443；环回、RFC1918、链路本地（含 169.254.0.0/16 cloud metadata）、unique-local、multicast、CGNAT、文档/保留段、IPv4-mapped 全部拒绝；混合 A/AAAA 解析结果整体 deny）。
  - Agent 侧 egress：`internal/egress` forward proxy（CONNECT + absolute-form HTTP；来源 IP→workspace/mode 映射，未知来源 deny；解析一次并只拨已校验 IP；deny 审计只含 host/port/reason/workspace_id/mode，不含 path/query）。`internal/runtime/docker` 新增网络 ensure/verify（缺失则创建 `Internal=true`，已存在但不是 internal → 启动失败）；`cmd/browser-agent` 启动 egress listener 并随 ctx 优雅关闭；create/restart 接受可选 `mode`（缺省 LOCKED，非法 400）。
  - Runtime 侧 guard：`internal/guard` + `cmd/workspace-guard` 独占消费 loopback CDP（`WW_CDP_URL` 默认 `http://127.0.0.1:9222`，非 loopback 拒绝）：page target 先 `Fetch.enable`（fatal，先于任何脚本执行）→ 仅当 `waitingForDebugger=true` 时发送 5s 有界 resume（非致命）→ 异步 `Page.enable`（非致命）；`Fetch.requestPaused` 按共享策略 continue / `failRequest(AccessDenied)` + `policy_deny` 审计；未知初始 URL target `closeTarget` + `target_deny`；`Browser.setDownloadBehavior{deny,eventsEnabled}`；逐 origin 拒绝 clipboard 权限；传输断开、`Fetch.enable` 失败、popup close 失败仍 fatal。
  - Runtime 镜像/entrypoint：多阶段构建（`golang:1.26.1-alpine` 编译 guard → alpine 运行，build context 改为 `browser-agent/`）；Chromium 增加 `--proxy-server`（缺 `WW_PROXY_SERVER` 即 exit 1）、`--proxy-bypass-list=<-loopback>`、`--disable-quic`、`--webrtc-ip-handling-policy=disable_non_proxied_udp`、CDP `127.0.0.1:9222`、`--deny-permission-prompts`；watchdog 监视 guard，guard 退出 → 清理 chromium→x11vnc→Xvfb 并 exit 1。
- 实际命令与结果（均在 `golang:1.26.1-alpine` 容器内、对冻结源码执行）：
  - `gofmt -l .`（无输出）、`GOWORK=off go vet ./...` → VET_OK、`GOWORK=off go test ./... -count=1` → 全部 ok（policy/egress/guard/manager/httpapi/docker 聚焦用例）、`go build ./cmd/browser-agent` / `./cmd/workspace-guard` → OK。
  - 根模块未改动；E2E 回归：`go test ./service/webworkspace -run WebWorkspaceAgentEndToEnd -count=1 -v` → PASS（1.88s）；`go test ./router -run WebWorkspaceRouterAgentEndToEnd -count=1 -v` → PASS（1.90s）。
  - 真实 Docker 验收（冻结镜像 `newapi-web-workspace-runtime:local`，`sha256:35b1697275b2`）：
    - Agent fail-closed：缺 token／缺 `WEB_WORKSPACE_EGRESS_PROXY_URL`／runtime network 已存在但 `Internal=false` → 均 exit 1，错误信息明确；runtime 网络重建为 `Internal=true`。
    - runtime 501/502 持续 `Up` ≥2 分钟且 `state=RUNNING`、仅挂私有内网、`docker port` 为空。
    - 直接出网（无 proxy）：DNS 不可解析／网关 Connection refused；经 proxy：`example.com`、`169.254.169.254`、`10.0.0.1` 全部 `HTTP/1.1 403`，agent 审计完整（host/port/reason/workspace_id/mode）。
    - allow 路径：经 proxy 访问 `http://chatgpt.com/` 得到真实 Cloudflare `HTTP/1.1 301 Moved Permanently`（`Cf-Ray …-SIN`）→ allowlist 放行 + 已校验 IP 拨号生效。
    - Browser Guard（真实 Chromium 131，经 loopback CDP 探针）：未知域名 target `survived=false` + `target_deny(host not in LOCKED allowlist)`；内网服务 `ws-agent:8730` `survived=false` + `target_deny(port not allowed)`；`https://chatgpt.com/` `survived=true`（放行）。
    - CDP 暴露面：容器内 `127.0.0.1:9222/json/version` 可用（Chrome/131.0.6778.108）；同网另一 runtime 访问 `<container>:9222` → Connection refused。
    - fail-closed：`pkill -KILL workspace-guard` → 容器 `exited exit=1`，日志 `workspace-guard exited with status 137: failing closed` + 清理链完成；agent 随后将 runtime 置为 `FAILED`。
- NOT RUN / 未覆盖（如实记录）：真实 OAuth 登录流程与 LOGIN 模式端到端（需用户手动登录窗口，属 Phase 4）；真实剪贴板读写（单测 + 权限拒绝设计覆盖）；VNC 画面渲染与交互（Phase 5）；镜像 CVE 扫描；云端部署（未开始，需用户单独确认连接方式与授权范围）。
- 已知降级（记录，不隐藏）：本机对预先存在的 about:blank target 稳定出现 `Page.enable` 超时，已按非致命降级（`page_domain_unavailable` WARN；per-origin clipboard 拒绝退化为 `--deny-permission-prompts` + x11vnc 无 `-clip`，均为默认关闭）；`Runtime.runIfWaitingForDebugger` 仅在 `waitingForDebugger=true` 时发送，5s 有界且非致命（失败记 `resume_not_acknowledged`，目标保持暂停属 fail closed，不产生无守卫浏览）。
- 提交：本阶段独立 commit（`feat(web-workspace): phase 3 network and browser guard`），本地未 push；SHA 见阶段报告。

## Phase 4（ChatGPT Provider Adapter）— PASS（本机冻结验收；Provider live 登录为 NOT RUN）

- 状态：**PASS**（本机/离线冻结输入；2026-09-18，Asia/Singapore）。Provider live 登录与 LOGIN 模式端到端属外部依赖，`NOT RUN`。
- 冻结接口：`docs/web-workspace/phase4-contract.md`（URL 语法、`.guard` 状态文件、Agent API、控制面流程、观察语义、审计、测试要求）。
- 实现范围：
  - A（guard/provider）：`browser-agent/internal/provider/chatgpt`（唯一 URL 解析权威；shell/project/conversation/conversation_no_project/unknown/other 六类；unknown 一律 fail closed；55 例 fixture 驱动测试）；`internal/guard` 增加 `.guard` 状态读取（2s 轮询、缺失/损坏全 deny）、provider document 导航 ownership 判定、permit 单次消费（原子写 `permit.consumed`）、观察写入 `observations.jsonl`、`Network.responseReceived` → `project_not_found`。
  - B（agent）：`.guard` 目录纳入 workspace 准备（0700，10001:10001）；新增 `PUT /internal/v1/runtimes/{id}/ownership`、`POST .../permits`、`GET .../observations`、`POST .../observations/ack`（原子写、offset 与不完整尾行语义、状态码 400/404/409）。
  - C（控制面）：`service/webworkspace/sync.go`（观察幂等应用 + 事务、ownership 组装、permit 记录与审计）；`StartSession` 推送 ownership；`GET /projects` 先同步（pull→apply→ack→变更则 push）；新增 `POST /projects`（签发 permit，不建本地行；无 session → 409 `WEB_WORKSPACE_SESSION_REQUIRED`，超限 → 409 `WEB_WORKSPACE_PROJECT_LIMIT`）；PATCH/DELETE 后推送 ownership（失败 503）；`web_workspace.max_projects` 设置（默认 0=不限）。
- 实际命令与结果（冻结源码，容器内执行）：
  - browser-agent：`gofmt -l .`（无输出）、`GOWORK=off go vet ./...` → VET_OK、`GOWORK=off go test ./... -count=1` → 全部 ok（新增 provider 55 例、guard ownership/观察 19 例、manager 14 例、httpapi 8 例）、`go build ./cmd/browser-agent` / `./cmd/workspace-guard` → OK。
  - root：`gofmt -l service/webworkspace controller router setting/system_setting dto model`（仅仓库既有 `controller/channel_pin_retry_test.go`）、`go vet` → VET_OK、`go test -p 1 ./service/webworkspace ./router -run WebWorkspace -count=1`（真实 MySQL 5.7.44 / PostgreSQL 9.6.24）→ ok、`go build ./...` → BUILD_OK。
  - 真实 Docker 验收（镜像 `:local` sha256:e00734f85788…；601/602 两个 runtime 同时运行、各自不同 ownership）：
    - Agent API：`PUT ownership`（200，返回计数）、`POST permits`（200 + expires_at）、`GET observations`、`POST observations/ack`（ack 后重复 GET 为空且 `next_offset` 保持）。
    - 浏览器级（真实 Chromium 经 loopback CDP 新建 target）：自己登记的 project → `survived=true`（真实跳转 provider 登录页，逐跳重新判定）；602 的 project（手输 URL/新 target）→ `survived=false` + `target_deny reason=project_not_registered`；无 project 的 `/c/<uuid>` → `survived=false` + `conversation_without_project`；`/g/` 未知形态 → `survived=false` + `unknown_resource_shape`；自己 project 下的 conversation → `survived=true` 且产生 `conversation_created` 观察。
    - permit 单次消费：签发 permit → 新 project C `survived=true` 且 `project_created{permit_id,external_project_id,slug}` 观察；同一 permit 再开 project D → `survived=false`。
    - 运行时刷新：重新 PUT ownership 加入 project C → 3s 内经 2s 轮询再次打开 C `survived=true`（无需新 permit）。
  - E2E 回归（真实 agent + 新镜像）：`TestWebWorkspaceAgentEndToEnd` PASS（1.82s，日志 `ownership_pushed workspace_id=1 generation=0 projects=0`）；`TestWebWorkspaceRouterAgentEndToEnd` PASS（1.86s）。
  - 集成修复：runtime 镜像多阶段构建补 `COPY internal/provider`（guard 新依赖），修复后镜像重建成功。
- NOT RUN / 未覆盖（如实记录）：
  - **Provider live 登录与 LOGIN 模式端到端**（需用户手动登录窗口，Agent 不接触凭据）。
  - `project_not_found` 的真实 provider 404 触发（单测覆盖；真实触发需删除 provider 侧项目）。
  - 云端部署（未开始，需用户单独确认连接方式与授权范围）；Phase 5 前端；Phase 6 安全验证。
- 已知取舍（记录）：DELETE/PATCH 先本地提交再推送 ownership（推送失败 503，guard 保持旧文档直到下次推送，fail-closed 方向不放开未登记资源）；`project_not_found` 采用删除本地映射（fail closed，可经新 permit 重新登记，不做 schema 变更）；模式 B（共享 upstream account）仍为 NOT strong isolation。
- 提交：本阶段独立 commit（`feat(web-workspace): phase 4 chatgpt provider adapter`），本地未 push；SHA 见阶段报告。
