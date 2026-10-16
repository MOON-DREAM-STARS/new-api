# API 密钥创建向导：模型家族、按 Key 服务档位与看板「模型-档位」（2026-10-16）

## 目的与范围

- 创建 API 密钥由右侧窄抽屉改为屏幕居中的小窗口，分 4 页：① 模型家族（默认全选，与「渠道聚合」的模型家族同源）② 服务档位（只作用于该 Key）③ 其他设置（名称、过期时间、数量、额度、IP 白名单）④ 显示 API Key（红字警示「请勿泄露此 API Key！！！」，可复制）。
- 默认模型家族从 4 个扩到 8 个：Claude、GPT、Gemini、DeepSeek、Qwen、GLM、Kimi、Grok。
- Key 记住所选家族，请求时按家族**当前**成员动态放行；Key 可以有自己的档位设置，未改动则跟随账户默认。
- 数据看板「分流」图：普通用户 = 用户 → 令牌 → 模型家族 → 模型 → 模型档位；管理员/超管 = 用户 → 节点 → 令牌 → 分组 → 模型家族 → 模型 → 模型档位 → 渠道。档位节点显示为「模型名-档位名」，未走聚合的请求显示「模型名-未分档」。普通用户的接口响应里不再包含节点、分组、渠道。
- 不改：使用日志页、模型调用分析图、渠道聚合管理页的现有功能。

## 变更前实时状态

- 仓库 `D:\github\api\new-api`，`dev` HEAD `643c704ea`。它与生产 Release 对应提交 `05146349640a` 相比只多一个路由兼容修复提交，`model/` 目录无任何差异。开发在其上新建的分支 `feat/api-key-wizard-model-families` 进行，`dev` 本身未动。
- 用户已有的本地修改与未跟踪文件（`AGENTS.md`、`2026/10/15/10-15-2026-channel-aggregation-service-tiers.md`、`2026/10/14/10-14-2026-agents-branch-architecture.md`、`browser-agent/`、`outputs/image2ui/*`）保持原样，未触碰。
- 本次只做本机开发与本机测试：没有连接生产，没有发布，也没有取得生产实时基线。开发期间没有 stage / commit / push；开发完成后按用户的明确要求做了一次选择性提交，见文末「提交」一节。

## 精确变更

后端（Go）：

- `model/service_tier.go`：默认家族扩到 8 个（含各自的匹配词）。新库一次性预置；老库在 `migrateServiceTierTables` 中按 options 表内部标记 `ServiceTierDefaultFamiliesVersion=2`（只读、不进 OptionMap，管理员不能改）一次性补齐缺失的 claude / gpt / gemini / grok，排序接在现有最大值之后，同一事务内写标记，管理员之后删掉的家族不会被重建。新增 Key 级档位行读写（`SaveTokenServiceTierSetting`；「跟随账户默认」用同一行 `preference=''` 作标记而不是删行，保证版本单调）、`deleteTokenServiceTierSettings`、`ExistingModelFamilyIDs`。`model/option.go` 对该标记做只读校验。
- `model/token.go`、`model/token_cache.go`：`tokens` 新增列 `model_families`（text，家族 id 的 JSON 数组；`0` 表示未归类模型；空表示不限制）。`Update()` 的 Select 与 Redis 缓存 HSET 脚本带上该字段；`InsertWithServiceTier` 让建 Key 与它的档位设置同事务；删除 Key（单个、批量）同事务清理该 Key 自己的档位行。
- `service/service_tier_family.go`（新增）：模型 → 家族解析（与 `TokenModelLimitAllows` 同口径：精确名 → 格式化名 → 路由匹配名）；`CheckTokenModelFamily` / `TokenModelFamilyFilter`（家族配置读不到、或 Key 里的家族数据损坏时一律拒绝，不放行）；`NormalizeTokenModelFamilies`（去重、排序、校验存在、至多 64 个）；`GetServiceTierUserFamilies`（用户接口数据）；`AnnotateFlowModelFamilies`。
- `service/service_tier.go`、`service/service_tier_route.go`：按 Key 读取档位覆盖（`GetTokenServiceTierSettings`，Redis 键 `service_tier:token:<id>`，沿用按版本写入缓存的脚本；没有覆盖则回退账户默认）；选路改用该 Key 的设置；把请求实际承接的档位写入请求上下文，供计费日志与看板使用。
- `middleware/auth.go`、`middleware/distributor.go`、`controller/model.go`、`relay/responses_websocket.go`：家族限制与原有模型白名单一起生效于普通请求、任务模型名回填、`/v1/models` 列表过滤、Responses WebSocket。
- `controller/token.go`：`POST` / `PUT /api/token/` 增加 `model_families`、`service_tier`（缺省 = 不改，`null` = 清除 / 跟随账户默认，有值 = 保存；先校验再写入）；`POST /api/token/` 成功时返回脱敏后的新 Key（含 id），完整密钥仍走带审计的 `POST /api/token/:id/key`、`/batch/keys`，`api_key:write` 与 `api_key:reveal` 权限保持分离；`GET /api/token/:id` 附带 `service_tier`；审计 `changed_fields` 增加 `model_families`。
- `controller/channel_aggregation.go`、`router/api-router.go`、`middleware/access_token_routes.go`：新增 `GET /api/user/model_families`（PAT 权限 `profile:read`），只返回用户可用分组里的启用模型及其家族归属，不泄露不可用的模型名。
- 看板数据：`model/usedata.go` 的 `quota_data` 新增列 `service_tier`（varchar(16)，默认空串），并纳入内存缓存键与合并条件；`model/log.go` 记录档位；`model/usedata_flow.go` 按角色返回维度（普通用户只有 user / token / model / tier，管理员与超管为完整维度）；`controller/usedata.go` 补 `model_family_id`、`model_family`。家族取查询时的归属，档位取请求当时的记录。
- 数据库只新增：`tokens.model_families`、`quota_data.service_tier` 两列、一条内部 option、老库补 4 个家族。没有删除、修改既有列或数据。

前端（web/）：

- 新向导 `web/src/features/keys/components/api-key-wizard/`：`api-key-wizard-dialog.tsx`（用 `@/components/dialog`，创建与编辑共用；每次打开都是全新的向导，切页时滚动回页首）、`wizard-steps.tsx`、`family-step.tsx`、`tier-step.tsx`、`options-step.tsx`、`expiration-field.tsx`、`key-result-step.tsx`；配套 `hooks/use-api-key-wizard.ts`、`hooks/use-user-model-families.ts`，`lib/model-families.ts`、`lib/key-service-tier.ts`、`lib/create-api-keys.ts`；`lib/api-key-form.ts`、`api.ts`、`types.ts` 随之调整。
- 删除旧抽屉及只被它使用的组件和测试：`api-keys-mutate-drawer.tsx`、`api-key-group-combobox.tsx`、`auto-group-order-editor.tsx`，对应 3 个组件测试与 `lib/__tests__/auto-group-form.test.ts`。`api-keys-dialogs.tsx` 改用新向导。
- 列表「模型」列：Key 有家族限制时显示「N 个模型家族」，不再显示成「所有模型」。
- 服务档位：把页面主体抽成共享组件 `ServiceTierSettingsEditor`（`web/src/features/service-tiers/components/service-tier-settings-editor.tsx`），「服务档位」页与向导第 2 页共用；页面文案改为「未单独设置档位的 API 密钥使用此默认值」。
- 数据看板：`features/dashboard` 的类型、`lib/flow.ts`、`components/flow/flow-charts.tsx` 增加家族、档位阶段，按角色决定阶段列表；家族、档位不属于敏感列。
- 翻译：7 个语言各新增 40 条（38 条界面文案 + 2 条后端错误消息），按原顺序追加在文件末尾，未重排既有词条；只通过 `add-missing-keys.mjs` 加 `i18n:sync` 写入（临时脚本已删除）；`src/i18n/static-keys.ts` 登记 5 个运行时传入的看板标签。

## 行为规则

- 向导第 1 页：家族卡片默认全选；全选时提交 `model_families: []`（不限制，之后新增的家族也自动可用），取消勾选则提交所选 id（未归类模型为 `0`，显示为「其他模型」卡片）；一个都不选不能进入下一页。
- 向导第 2 页：用账户的档位设置预填，没改动就不提交 `service_tier`（该 Key 跟随账户默认）；改了才保存为该 Key 自己的设置；编辑已有自定义档位的 Key 时可点「跟随账户默认」恢复。多次保存以最后一次为准（向导不暴露乐观锁）。
- 新建 Key 的分组沿用原有默认逻辑：`default_use_auto_group` 开启且后端有 `auto` 分组时为 `auto` 并开启跨组重试，否则为空（账户分组）；不设模型白名单。向导里不再出现「分组」「模型限制」；编辑老 Key 时这些原值原样回传，不发送 `auto_groups`。
- 批量创建：向导限制一次最多 100 个（新增的前端限制）；遇到第一个失败就停止，已创建的 Key 仍然展示并提示失败原因；完整密钥分批（每批 100 个）通过带审计的接口取回。取回失败时（例如触发限流）第 4 页提示「密钥已创建，请重试，或稍后在 API 密钥列表中复制」，并提供重试按钮。
- 请求放行：模型必须同时通过原有模型白名单和家族限制；家族按请求当时的成员判断，所以管理员之后把模型加进已选家族，Key 自动可用；`/v1/models` 只列放行的模型；家族限制拒绝时返回 403 `access_denied`。
- 档位：Key 自己的设置优先于账户默认；计费与使用日志按实际承接档位。

## 测试变更

后端（Go）：

- `model/service_tier_test.go`：`TestServiceTierStorageDatabaseMatrix` 扩到 8 个家族、老库补齐只补一次、删除后不重建、标记只读；`model/usedata_flow_test.go` 重写为各角色维度。
- `controller/token_test.go`：`runTokenMigrationCompatibilityTest` 增加 `model_families` 列类型、旧 Key 不受限、二次迁移零 DDL 的断言；新增 `TestAddTokenStoresModelFamiliesAndOwnServiceTier`、`TestAddTokenRejectsInvalidModelFamiliesAndServiceTier`、`TestUpdateTokenModelFamiliesAndServiceTierTriState`、`TestDeleteTokenRemovesOwnServiceTierSettingsOnly`，并把其中三个场景抽成可在任意数据库上运行的函数，新增三库矩阵 `TestTokenModelFamilyAndServiceTierDatabaseMatrix`。
- `controller/usedata_flow_test.go`：重写角色维度测试；新增 `TestQuotaDataServiceTierMigration`（SQLite）、`...MySQL`、`...Postgres`（旧 `quota_data` 加列、旧行为未分档、二次迁移零 DDL、新旧流量并存、两种角色的看板查询）。
- `controller/channel_aggregation_test.go`、`service/service_tier_cache_test.go`、`relay/responses_websocket_test.go`：Key 档位覆盖与缓存版本顺序、家族限制、用户家族接口、WebSocket 家族限制。
- `relay/relay_task_test.go`：测试辅助函数补迁移档位相关表。这同时修复了基线提交上已有的 Responses WebSocket 测试失败（缺 `model_families` 表导致 503），属于测试辅助函数问题，不是生产代码缺陷。

前端：

- 新增 `features/keys/components/__tests__/api-key-wizard.test.tsx`、`model-limits-cell.test.tsx`，`features/keys/lib/__tests__/{model-families,key-service-tier,create-api-keys}.test.ts`，`features/service-tiers/__tests__/service-tier-settings-editor.test.tsx`；更新 `api-key-form.test.ts`、`tiers.test.ts`、`features/dashboard/lib/flow.test.ts`。

## 验证（命令与结果）

环境：Go 1.26.9（`golang:1.26` 容器，离线模块缓存；补下载了 `godotenv`、`json-iterator` 两个模块到该缓存，`go.mod` / `go.sum` 未改）；Node v22.23.1，vitest 4.1.10；SQLite 3.50.4、MySQL 8.4.11（严格模式，含 `ONLY_FULL_GROUP_BY`）、PostgreSQL 15.19，均为本机临时容器（同一网络命名空间内的回环地址），测试后已删除。

Go：

- `go build`（根包与除 `relaykit`、`tokenkit` 两个独立子模块外的全部包；这两个子模块没有改动）退出码 0；`go vet` 同范围退出码 0，`./e2e` 单独 `go vet` 退出码 0（该包只有一个文档解析测试，没有改动、没有运行）；28 个改动 Go 文件 `gofmt -l` 无输出 — VERIFIED。
- `go test -count=1` 同范围全部 ok（controller 约 271 秒）。`pkg/jsplugin` 首轮因离线缓存缺模块无法构建，补模块后单独重跑 ok — VERIFIED。

三库（SQLite / MySQL 8.4.11 / PostgreSQL 15.19）：

- `TestServiceTierStorageDatabaseMatrix`、`TestTokenMigrationFromChar48ToVarchar128`（含 MySQL、Postgres 变体，已加 `model_families` 与二次迁移零 DDL 断言）、`TestQuotaDataServiceTierMigration`（含 MySQL、Postgres 变体）、`TestTokenModelFamilyAndServiceTierDatabaseMatrix`、`TestAuditDatabaseMatrix`、`TestAPITokenAuditDatabaseMatrix` 全部通过 — VERIFIED。
- 升级演练：用生产 Release `05146349640a` 的代码（临时 worktree）在三个库里建库并写入样例（4 个家族、改名的 Qwen、DeepSeek 成员、用户、Key、`quota_data` 行、账户档位设置），再用新代码启动三次。首次启动后两列已加；4 个新家族只补一次且排在原家族之后；管理员改动保留；旧 Key 仍不受限；旧流量为未分档。第二次启动的 schema 快照（全部表、列、索引）与首次一致，家族数不变；删除 grok 后第三次启动不再重建、schema 不变 — VERIFIED（三库）。
- 全新库演练：同样启动三次，结果相同 — VERIFIED（三库）。演练用的临时测试文件通过 Go `-overlay` 注入，没有写进仓库。

前端：

- `tsgo -b` 退出码 0（另做过一次 `--force` 全量重建）；改动的 39 个 ts / tsx 文件 `oxlint` 无告警、`oxfmt --check` 通过；7 个语言 JSON 可解析 — VERIFIED。
- `vitest run` 全量：199 个文件、2353 项通过；`rsbuild build` 退出码 0 — VERIFIED。
- 仓库自带的 `format:check` 脚本在本机无法运行（脚本按名字启动 `oxfmt`，本机 PATH 里没有）；改用 `oxfmt --check` 对改动文件检查，并核对过被重新格式化的文件版权头完好（18 行）。
- 与本次无关、未处理的既有问题：未改动文件里的 lint 错误（如 `src/assets/logo.tsx`），以及 3 个既有的缺失词条（「Expand all」「Length」「Select preset」）。

浏览器端到端（内置浏览器）：

- 环境：从工作树构建 Linux 二进制，容器内用临时 SQLite 和一个本地假的 OpenAI 兼容上游运行，只绑定 `127.0.0.1`。管理员、普通用户两个测试账号只存在于这个临时库，用完即删；3 个假渠道，GPT、DeepSeek 两个家族配了三档渠道池。
- 创建与编辑：第 1 至 4 页走通。默认 9 张卡片全选；取消 5 项后计数为 4/9；可展开模型列表；第 2 页改偏好后提示使用自定义档位并出现「跟随账户默认」；第 4 页为 `role="alert"` 的红字警示（暗色 `rgb(255,101,104)`、亮色 `rgb(228,0,20)`）加密钥与复制按钮；列表「模型」列显示「4 个模型家族」；编辑向导回显已存的家族与档位，保存后用接口回读一致。
- 请求放行：家族内模型 200，家族外 403 `access_denied`，`/v1/models` 只列家族内模型。全选且未改档位的 Key 保存为 `model_families=[]` 且没有档位覆盖，所有模型可用。使用日志中，偏好 performance 的 Key 走 performance 档（倍率 1），跟随账户默认（低价优先）的 Key 走 economy 档（倍率 0.2），没有策略的模型无档位。
- 看板：普通用户的接口响应只有 `count`、`model_family(_id)`、`model_name`、`quota`、`service_tier`、`token_id`、`token_name`、`token_used`、`user_id`、`username`，**没有** `node_name`、`use_group`、`channel_id`、`channel_name`；管理员响应含全部维度。图上普通用户为 用户 › 令牌 › 模型家族 › 模型 › 模型档位，管理员为 8 个阶段；档位节点显示「模型名-档位名」，如「deepseek-v3-高性能档」「claude-opus-4-未分档」。
- 窗口：1280×720 暗色、390×844 暗色（卡片单列，无横向滚动，对话框居中）、1920×1080 亮色，为抽样检查，没有做完整的「尺寸 × 主题」矩阵。
- 失败路径：取回密钥被限流（429）时，第 4 页显示失败提示与「重试」，密钥已创建且可在列表里取回。
- 端到端中发现并修复：在第 1 页滚动后点「下一步」，第 2 页沿用原滚动位置，看不到步骤条。现在每页打开时回到页首，并补了测试。

## NOT RUN

- MySQL 5.7、PostgreSQL 9.6（本机没有镜像）；MariaDB 等其他兼容库。
- 生产部署；用生产**数据**做的升级演练（演练用的是生产 Release 的代码加临时库）；真实上游渠道与真实计费数据联调。
- Safari、Firefox 等其他浏览器；2560 宽及全部「尺寸 × 主题」组合；Responses WebSocket 的浏览器端到端（只有 Go 单测）。
- push、Release、生产部署：按仓库规则，等明确要求再做（commit 已按用户要求完成，见文末「提交」）。

## 回滚

- 代码：撤销本次文件改动（`git checkout -- <修改文件>` 并删除新增文件），或直接弃用分支 `feat/api-key-wizard-model-families`。
- 数据库：新增的两列、一条内部 option、4 个家族行都不被旧代码读取，可保留；彻底清理需手工删除（破坏性，需人工确认）。
- 注意：旧代码不认识 `tokens.model_families`，回滚后已设置家族限制的 Key 会变成不限制；Key 自己的档位设置（`user_service_tier_settings` 里 `token_id > 0` 的行）会被忽略，回到账户默认。回滚前需评估这一点。

## 风险与已知边界

- 上线后管理员需要在「渠道聚合」页给新增的 Claude / GPT / Gemini / Grok 家族分配模型（可用「添加 N 个建议」）；分配前这些模型在向导里归入「其他模型」。
- 滚动发布期间新旧节点并存：旧节点不认识家族限制（放行），全部节点更新后才完整生效。
- 看板的「模型家族」取查询时的归属，档位取请求当时的记录；改过家族成员后，历史流量的家族标签会随之变化。`quota_data.service_tier` 的老数据为空，显示为未分档。
- 取回完整密钥走 critical 限流（每个 IP 每 20 分钟 20 次，登录等也计入）：连续新建很多 Key 时，第 4 页可能取回失败，页面会提示并可重试，也可稍后在列表里查看。
- 对已有大表加列：MySQL 8.4 与 PG 15 在测试库上瞬间完成；MySQL 5.7 等老版本可能整表重建，生产 `quota_data` 很大时应选低峰期 — 未验证。
- 向导不再提供「分组」「模型限制」；仍需要它们的用户只能通过 API，或编辑老 Key（原值保留）。新 Key 的分组按默认逻辑。
- Key 的档位覆盖以最后一次保存为准。
- 没有涉及真实密钥或生产凭据；测试账号、密钥和数据库口令只存在于临时容器与会话临时目录，已销毁。没有修改 `docs/`。

## 提交（2026-10-16）

- 用户明确要求后，只选择性 stage 了本次功能文件和本记录，在分支 `feat/api-key-wizard-model-families`（基于 `dev` 的 `643c704ea`）上创建一个提交；提交号以 `git log -1` 为准。未 push，未发布，也不构成生产部署授权。
- 没有 stage、保持原样的文件：`AGENTS.md`、`2026/10/15/10-15-2026-channel-aggregation-service-tiers.md`（用户已有的未提交修改），`2026/10/14/10-14-2026-agents-branch-architecture.md`、`browser-agent/`、`outputs/`（用户已有的未跟踪文件）。
- 提交前用 `git diff --cached --check` 检查空白错误，并核对暂存区文件清单与上文「精确变更」一致。
