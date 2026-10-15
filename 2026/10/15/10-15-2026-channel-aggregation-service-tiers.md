# 渠道聚合与服务档位（多模型渠道聚合与智能故障接力）（2026-10-15）

## 目的与范围

按交接文档实现「多模型渠道聚合与智能故障接力系统」：管理员按模型家族（DeepSeek/Qwen/GLM/Kimi，可自建）为同一外部模型配置性价比/均衡/高性能三档渠道池与倍率；用户在「服务档位」页开启档位、选择默认偏好、决定是否允许向更高档位自动接力。外部模型名、API Key、Base URL 均不变。

## 变更前实时状态

- `D:\github\api\new-api` 位于 dev，HEAD `f8a6c56b7`（与 origin/dev 一致）。用户已有的 `AGENTS.md` 本地修改与 untracked 产物（截图、browser-agent、10-14 记录）保持原样，未触碰。
- 开发在云端隔离副本（同一 commit 克隆）完成，验证通过后把最终文件写回本工作区；未 stage、commit、push。

## 精确变更

后端（Go）：
- 新增 `model/service_tier.go`：4 张新表 `model_families`、`model_family_members`、`aggregation_policies`、`user_service_tier_settings`（均带 version 乐观锁与唯一索引）；首次建表时写入 4 个家族与一条**默认关闭**的 DeepSeek 示例规则（0.15/0.20/0.40）。`model/main.go` 在 migrateDB 中调用。只新增表，不修改既有表。
- 新增 `service/service_tier.go`（配置快照缓存、规则校验、用户设置读写 + Redis 缓存）、`service/service_tier_route.go`（档位链、选路、重试/接力决策、严格预扣、日志）、`service/service_tier_admin.go`（管理视图、路由模拟、用户预览）。
- `middleware/distributor.go`、`controller/relay.go`：聚合模型走档位选路与接力；未聚合模型行为不变。
- `relay/helper/price.go`：聚合请求以实际承接档位倍率替代分组倍率，所有计费路径（按量、按次、缓存、tiered_expr）统一生效。
- `service/billing_session.go`：新增 `ReserveStrict`，接力到更贵档位前按新价格严格预扣，不走信任额度绕过；`service/funding_source.go` 修复钱包多次预扣时累计金额，退款可完整返还。
- `relay/responses_websocket.go`：聚合模型拒绝 Responses WebSocket（fail-closed）。
- `controller/channel_aggregation.go` + 路由：`/api/channel_aggregation/*`（渠道读/写权限，写操作记审计），`/api/user/service_tier`（GET/PUT）与 `/preview`（POST）；access token 路由规则、审计模板、后端 i18n（en/zh-CN/zh-TW）。
- `service/log_info_generate.go`：用户可见日志写 `service_tier`（档位、倍率、是否接力）；管理员日志写 `service_tier_route`（每次尝试的档位、渠道、结果）。

前端（web/）：
- 新页面 `/channel-aggregation`（管理员，侧栏「管理员」组）与 `/service-tiers`（用户，侧栏「常规」组），侧栏模块开关与默认配置同步更新。
- 使用日志详情新增「服务档位」区块（管理员可见尝试轨迹）。
- 7 种语言新增 237 个词条（按原顺序追加，未重排既有内容）。
- 单测：`features/service-tiers/__tests__`、`features/channel-aggregation/__tests__`。

## 行为规则（服务端强制）

- 档位链：从偏好对应档位开始，同档位内按优先级/权重换渠道；仅当用户允许接力时才向上走，且不超过用户设定的最高档位；只升不降；关闭的档位任何情况下都不会被使用；偏好档位无可用渠道且未允许接力时直接报错。
- 不接力的情况：已开始向客户端输出（流式已开始）、客户端断开、请求类 4xx、本地拒绝、504/524（全局不重试规则）、指定渠道请求、超出尝试次数/总耗时。
- 仅支持 chat/completions、completions、messages、responses(compact)、Gemini generateContent；embeddings/图像/音频/realtime/WebSocket/任务类接口对聚合模型返回明确错误。
- 用户必须本身就能在其分组内使用该模型，聚合不扩大模型权限。
- 计费：只按实际承接档位倍率扣一次费；失败尝试不收费；余额不足以按更高档位预扣时不接力，并返还已预扣额度。

## 备份

只新增文件与表，既有表结构未改。修改文件的变更前版本即 HEAD `f8a6c56b7`。

## 验证（命令与结果）

环境：Go 1.26（本机工具链源码构建）、SQLite（glebarez）、MySQL 8.0.46、PostgreSQL 16.15（本地测试实例，非生产）。

- `go test -p 1 ./...`（含 MySQL/PostgreSQL DSN）：本功能相关包 model/controller/service/router/middleware/relay 全部 ok — VERIFIED。
  - 首轮运行中 PostgreSQL 矩阵用例因测试 DSN 写成 key=value 格式失败（既有测试要求 URL 格式），改 URL DSN 后全部通过；根包首轮因 web/dist 为空无法 embed，构建前端后通过。
- `TestServiceTierStorageDatabaseMatrix`：三库建表、二次迁移零 DDL、唯一索引、版本冲突、级联删除 — VERIFIED（SQLite/MySQL 8/PG 16）。
- 端到端用例（真实 InitDB 迁移 + 假上游）：低价档计费、同档换渠道、跳过关闭档位接力并按 0.40 计费、tiered_expr 计费、余额不足阻止接力且退回、流式已开始不切换、400 不接力、上限档位、不降档、空档位报错、embeddings 拒绝、尝试上限、并发 6 请求计费 — VERIFIED。
- `bun run typecheck`、对全部改动前端文件的 oxlint 与 oxfmt 检查 — VERIFIED。（仓库整体 `format:check` 有 25 个既有文件不符合，均非本次改动。）
- 前端新单测 21 项，以及 service-tiers/channel-aggregation/usage-logs 相关 28 个测试文件 462 项 — VERIFIED。全量 `bunx vitest run`：196 个文件 2302 项全部通过 — VERIFIED。
- `bun run build` — VERIFIED。
- 本地启动构建产物（临时 SQLite、假渠道）用 Chromium 检查两页面：桌面浅色/深色、390px 移动端无横向滚动、无页面错误；渠道池抽屉与路由测试交互正常 — VERIFIED。

## NOT RUN

- MySQL 5.7、PostgreSQL 9.6 兼容性（容器无法拉取镜像）— NOT RUN。
- 旧版本二进制升级到本版本的迁移演练 — NOT RUN（新表为纯新增，既有表无改动）。
- 真实上游渠道联调、生产部署 — NOT RUN（按要求不执行）。
- 初始功能写回时 stage / commit / push — NOT RUN；后续复核与发布准备的实际状态见文末补充。

## 回滚

- 代码：撤销本次文件改动（`git checkout -- <修改文件>` 并删除新增文件）即可恢复。
- 运行时：在「渠道聚合」页关闭家族规则即停止聚合路由，请求回到普通分组选路，无需回滚代码。
- 数据库：4 张新表不被旧代码读取，可保留；如需彻底清理再手动删除（破坏性操作，需人工确认）。

## 风险与已知边界

- 渠道健康/冷却状态为各节点内存态，多节点部署时不共享。
- 用户设置表预留 `token_id` 维度（按 Key 覆盖），本次只开放账户级设置。
- 档位倍率替代分组倍率；分组特殊倍率对聚合请求不叠加（按交接文档“按实际承接档位计费”实现）。
- 未涉及任何真实密钥或生产凭据；测试数据库口令仅为本地临时实例。

## 发布复核补充（2026-10-15）

独立代码审查发现并修复以下问题：

- 初始档位预扣会取整，升档时此前用该整数反推基数；小额低价档可能把更高档位的严格预扣算成 0。现保留档位倍率前、取整前的预扣基数，升档时据此重新计算。
- 第一次读取聚合配置失败会被缓存成空快照并回退普通选路。现首次读取失败向聚合解析返回配置不可用错误，不写入空快照；已成功加载过配置时则保留最后一个快照。
- 管理员配置刷新与请求 TTL 刷新共用同一互斥锁，避免旧快照覆盖刚保存的规则。
- 用户设置 Redis 缓存按持久化版本原子写入，旧版本读请求不能覆盖较新设置。

本次新增的回归检查结果：

- `service`：首次配置读取失败不生成空快照；Redis 旧版本不覆盖新版本 — VERIFIED。
- `controller`：低档倍率 `0.0001` 初始预扣取整为 0 时，升到 `0.40` 档仍须成功预扣 400；余额仅 10 时拒绝接力且不调用高档渠道 — VERIFIED。
- 以上检查由本机缓存的 Go 1.26 Docker 镜像执行；只运行了这三个聚焦用例，没有重跑完整 Go 矩阵或前端套件。
- `git diff --check` 无空白错误；仅保留既有 `AGENTS.md` 行尾提示。

本次生产只读基线状态为 `BLOCKED_EXTERNAL`：`Connect-DreamstarsHost.ps1` 的 Access 请求超时，按脚本内建 Direct 严格主机密钥路径连接时被拒绝，公网状态接口也超时。未获得实时容器、Compose、持久化和公网行为完整基线；未运行受控 Release、未备份或改动服务器。记录本段时 stage / commit / push / 生产部署均为 `NOT RUN`，随后 stage 状态见下文。

本次随后仅选择性 stage 了 72 个功能文件和本记录，并在 `dev` 创建了功能提交。用户的 `AGENTS.md` 修改、homepage 截图、`browser-agent/` 产物及 10-14 日期记录均未 stage。commit `VERIFIED`；push / 生产部署仍为 `NOT RUN`。
