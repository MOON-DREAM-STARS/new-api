# Phase 4 冻结接口（ChatGPT Provider Adapter）

状态：Main 于 2026-09-18 冻结。Phase 4 的三个并行工作包（A：provider+guard；B：agent API+`.guard` 文件；C：控制面）必须严格按本文件实现。
任何一方不得单方面修改本文件；发现缺陷回报 Main。Phase 5（前端）与 Phase 6（安全验证）不在本文件范围。

## 0. 范围

Phase 4 交付：ChatGPT URL 语法解析、Project/Conversation 发现与观察、unknown resource deny、
短时单次 creation permit、adapter 失败 → deny，以及对应的控制面登记/重同步。

## 1. ChatGPT URL 语法（A 实现，唯一解析权威；C 不解析 URL）

- host：`chatgpt.com`（策略表已放行的同一域集与子域）。
- 分类前先规范化：忽略 query/fragment，允许且仅允许一个尾斜杠，大小写敏感。
- 形态：
  1. `/` → `shell`（非资源）
  2. `/g/g-p-<id>[-<slug>]` 或 `/g/g-p-<id>[-<slug>]/project` → `project`
     `<id>` = `g-p-` 之后、第一个后续 `-` 之前的片段，必须匹配 `^[0-9a-f]{32}$`；不匹配 → `unknown`
  3. `/g/g-p-<id>[-<slug>]/c/<conversationId>` → `conversation`（含 project）
     `<conversationId>` 必须匹配 `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
  4. `/c/<conversationId>` → `conversation_no_project`（始终 DENY）
  5. path 以 `/g/` 或 `/c/` 开头但不符合上述形态 → `unknown`
  6. 其它路径（`/auth/...`、`/backend-api/...`、静态资源等）→ `other`
- `unknown` 的判定必须 fail closed（DENY），不得猜测 ID。

## 2. `.guard` 状态文件（容器内 `/workspace/.guard`）

agent 在 workspace 准备时创建该目录并 chown 10001:10001（B 负责）；guard 可写（A 负责）。

- `ownership.json`（B 写，A 只读）：
  `{"generation":<int>,"projects":["g-p-<32hex>",...],"conversations":["<id>",...],"updated_at":<unix>}`
- `permit.json`（B 写，A 只读）：
  `{"permit_id":"<8..64>","kind":"project_create","issued_at":<unix>,"expires_at":<unix>}`
- `permit.consumed`（A 写）：`{"permit_id":"<string>","consumed_at":<unix>}`
- `observations.jsonl`（A 追加，B 读）：每行一个对象，见 §4
- `observations.offset`（B 维护，A 不写）

## 3. Guard 语义（A 实现）

- 启动及每 2s 轮询 `ownership.json`；缺失/解析失败/generation 非法 → 视为“无登记资源”并对所有 provider 资源导航 DENY（fail closed），日志 `ownership_state_unavailable`（Info + reason）。
- 对 page session 的 Fetch Request stage：先按 Phase 3 host 策略（scheme/port/host allowlist），再对 provider document 请求按 §1 分类：
  - `project`：`projects` 含该 id → ALLOW；否则若存在有效 permit（未过期、`kind=project_create`、`permit.consumed.permit_id` 不同于当前 permit_id）→ ALLOW，写 `permit.consumed`，产生 `project_created` 观察；否则 DENY(`project_not_registered`)
  - `conversation`：project 已登记 → ALLOW（并按去重规则产生 `conversation_created`）；否则 DENY(`project_not_registered`)
  - `conversation_no_project` → DENY(`conversation_without_project`)
  - `unknown` → DENY(`unknown_resource_shape`)
  - `shell`/`other` 且 host 策略通过 → ALLOW
- popup/新 target 初始 URL 检查（Phase 3 已有）应用同一分类；不通过 → `Target.closeTarget` + `target_deny`。
- 观察写 `observations.jsonl`（单行 JSON，含 `observed_at` unix；内存去重，重启可重复，控制面必须幂等）：
  - `{"event":"project_created","permit_id":"..","external_project_id":"g-p-..","slug":"..","observed_at":..}`
  - `{"event":"project_renamed","external_project_id":"g-p-..","slug":"..","observed_at":..}`（同一 id 观察到不同 slug）
  - `{"event":"conversation_created","external_project_id":"g-p-..","external_conversation_id":"..","observed_at":..}`
  - `{"event":"project_not_found","external_project_id":"g-p-..","observed_at":..}`
- `project_not_found` 来源：`Network.enable`（异步非致命，失败仅 Warn）+ `Network.responseReceived` 中 type=Document、status ∈ {404,410}、URL 属已登记 project。
- 任何解析失败、状态读取失败 → DENY（adapter failure → deny）。
- deny 审计字段（与 Phase 3 一致）：`component=guard`、`event=policy_deny|target_deny`、`mode`、`host`、`resource_type`、`reason`；禁止 path/query。

## 4. Agent HTTP API（B 实现；C 调用；沿用现有 bearer 鉴权）

- `PUT /internal/v1/runtimes/{workspace_id}/ownership`
  body `{"generation":<int>,"projects":[...],"conversations":[...]}`
  校验：generation ≥ 0；projects 匹配 `^g-p-[0-9a-f]{32}$`；conversations 匹配 `^[0-9A-Za-z-]{8,64}$`；否则 400 `invalid_request`。
  行为：确保 `.guard`（0700，10001:10001）→ 原子写 `ownership.json`。响应 200 `{"generation":<int>,"projects":<n>,"conversations":<n>}`。
- `POST /internal/v1/runtimes/{workspace_id}/permits`
  body `{"permit_id":"<8..64>","kind":"project_create","ttl_seconds":<1..3600>}`
  要求 runtime 存在且 RUNNING|IDLE（否则 404 `runtime_not_found` / 409 `runtime_not_running`）。
  行为：写 `permit.json`（issued_at=now, expires_at=now+ttl），删除旧 `permit.consumed`。
  响应 200 `{"permit_id":"..","expires_at":<unix>}`。
- `GET /internal/v1/runtimes/{workspace_id}/observations`
  行为：自 `observations.offset` 读取完整行（忽略不完整尾行）；文件不存在 → 空。
  响应 200 `{"observations":[<原始对象>...],"next_offset":<int>}`。
- `POST /internal/v1/runtimes/{workspace_id}/observations/ack`
  body `{"offset":<int>}` → 写 `observations.offset`；200 `{"offset":<int>}`。
- 端点不得回传客户端；DTO 不含 permit_id/外部 ID 之外的新内部信息（外部 ID 本来也不在 DTO）。

## 5. 控制面流程（C 实现，root module）

- `StartSession` 成功（runtime RUNNING）后：`PutOwnership(workspace.Id, generation, projects)`；
  generation = 该 workspace 现有 project 行的 max(updated_at)（无行 = 0）。
- `GET /api/web-workspace/projects`：先 `PullObservations` → 幂等应用（§6）→ 成功后 `Ack(next_offset)` → 若有变更 `PutOwnership` → 返回本地列表。
- 新增 `POST /api/web-workspace/projects`：
  entitlement + 当前用户 workspace + 活跃 session（否则 409 `WEB_WORKSPACE_SESSION_REQUIRED`）
  + `settings.MaxProjects > 0` 且已登记数 ≥ 上限（否则 409 `WEB_WORKSPACE_PROJECT_LIMIT`）
  → 生成随机 permit_id（32 字节随机 token）→ `POST permits`（TTL 300s）
  → 200 `{"permit_id":..,"expires_at":..}`；**不创建本地行**。
- `PATCH /projects/:id`：本地改名（现状）+ `PutOwnership`。
- `DELETE /projects/:id`：本地删除（现状）+ `PutOwnership`。
- Agent 不可达/被拒 → 503 `WEB_WORKSPACE_AGENT_UNAVAILABLE`，fail closed，不得静默成功。
- `setting/system_setting.WebWorkspaceSettings` 增加 `MaxProjects int`（json `max_projects`，默认 0 = 不限制）。

## 6. 观察应用（C；幂等 + 事务）

- `project_created`：upsert `web_projects`（provider=chatgpt，external_project_id，name=slug 或 external id，workspace_id=当前用户 workspace）；audit `permit_matched` / `permit_unmatched`。
- `project_renamed`：本地存在该 external id → 更新 name=slug；否则忽略 + audit。
- `conversation_created`：按 external_project_id 找本地 project；不存在 → audit `orphan_conversation_skipped`（不建孤儿）；存在 → upsert `web_conversations`（title=external_conversation_id）。
- `project_not_found`：事务删除本地 project 行 + 其 conversations，audit `project_not_found_removed`（fail closed，可经新 permit 重新登记）。
- 应用失败 → 不 ack（下次重试）。

## 7. 审计事件名（跨包一致）

`policy_deny`、`target_deny`、`ownership_state_unavailable`、`permit_issued`、`permit_matched`、`permit_unmatched`、
`project_registered`、`project_removed`、`project_renamed`、`conversation_registered`、`orphan_conversation_skipped`、
`ownership_pushed`、`observations_applied`。日志中禁止 path/query/cookie/token/外部地址。

## 8. 测试要求

- A：`internal/provider/chatgpt/testdata/url_cases.json` fixture 驱动（≥25 例：合法/畸形/大小写/尾斜杠/query/percent-encoding/未知 `/g/`、`/c/` 形态/其它 provider 路径），合法解析与 unknown→DENY 全覆盖；guard 单测覆盖 ownership 放行/拒绝、permit 单次消费、conversation 无 project 拒绝、popup 拒绝、观察写入、状态文件缺失 → 全 deny、Network 404 → 观察。
- B：`.guard` 目录与文件原子写、offset 解析（含不完整尾行）、permit 生命周期（写入/覆盖/消费清理）、GET observations 幂等、并发 PUT ownership。
- C：幂等应用（重复观察不重复建行）、permit 匹配/不匹配、无活跃 session 拒绝签发、max_projects 上限、agent 不可达 fail closed、`project_not_found` 级联删除、ownership 推送内容与 generation。

## 9. 明确不做（Phase 4 范围外）

- 不 reverse-engineer ChatGPT private API；不抓 DOM；不解析 provider 响应体（仅 status code）。
- 不实现共享 upstream account 的响应过滤（模式 B 仍为 NOT strong isolation）。
- 不实现 Phase 5 前端与 Phase 6 安全验证；不改 Phase 3 的 host/egress 策略语义。