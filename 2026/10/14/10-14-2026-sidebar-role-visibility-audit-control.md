# 角色侧栏可见性与普通用户审计访问限制（2026-10-14）

## 目的与范围

在 NewAPI 日常开发分支 `dev` 增加按角色配置侧栏模块的能力，并禁止普通用户查看审计页面或调用本人审计 API。工作限于 NewAPI 管理台与 `/api/audit/self` 授权；不涉及数据库 schema、服务 Compose 或生产拓扑。

## 变更前实时状态

- NewAPI `dev` 基线：`dcd1be88e19abc4bedfd53cf23f096833273d553`；独立 `main` worktree 位于同一基线。分支流程以同日 `10-14-2026-branch-topology-web-workspace-pause.md` 为准。
- 实施前工作树有 1 个用户修改文件 `AGENTS.md` 和 45 个用户未跟踪路径（含分支规则记录、Browser Agent 实验文件及截图）；均未覆盖、清理或纳入本次变更。
- 本次没有连接或修改生产 VPS。线上配置、容器和端点状态未重读；本记录只证明本地代码与测试结果。

## 精确变更目标

- 扩展 `SidebarModulesAdmin` JSON，新增可选 `roles` 规则，涵盖 Common（1）、Admin（10）、Root（100）；旧配置没有 `roles` 时保留原显示行为。全局设置为总开关，角色规则可进一步隐藏，个人偏好仍可进一步隐藏。
- 设置页可分别编辑全局与三类角色；推荐规则为 Common 隐藏审计、绘制和任务日志，保留使用日志，Admin 与 Root 保持可见。只有保存后才启用角色规则。
- 普通用户不能通过直接路由打开 `/usage-logs/audit`，也不能调用 `/api/audit/self`。后端该路由改为 `AdminAuth`；`/api/audit` 仍受 `AdminAuth` 与 `AuditRead` 保护。普通用户安全页面中的访问令牌审计入口同时隐藏。
- 更新访问令牌 `usage:read` 文案及七种 locale。i18n 脚本按规范同步排序了已有 locale key；每种 locale 仅新增 4 个 key，没有删除 key 或改变既有翻译值。

## 备份与回滚

- 回滚基线是 `dev` 的 `dcd1be88e19abc4bedfd53cf23f096833273d553`，文件清单见相邻 manifest。
- 未创建生产数据备份，因为本次没有生产配置或数据库写入。代码回滚通过精确 revert 本次 dev 实现提交及后续本地 `main` 集成提交完成；不得 reset、clean 或覆盖未跟踪用户产物。
- OWASP 授权、认证及会话管理指引已阅读；本次没有声称完成 ASVS 合规审计。

## 实际验证

- 回归先于后端修复运行并失败：普通用户 session/PAT 对 `/api/audit/self` 得到 HTTP 200，角色降级后的旧凭据也仍能访问；修复后相关路由测试通过。
- 使用本地已缓存 Docker 镜像 `golang:1.26`，未拉取镜像：真实 `SetApiRouter` 测试及三个相关路由/令牌 scope 测试通过（`go test ./router -run "^(TestAuditRoutesEnforceProductionAuthorization|TestAccessTokenRouteRulesCoverEveryDashboardRoute|TestSensitiveDashboardRoutesRequireBrowserSession|TestAccessTokenCatalogCoversEveryPermission)$" -count=1`）。临时 SQLite 测试输出 `ok .../router 0.068s`。
- Web TypeScript 检查 `node node_modules/@typescript/native-preview/bin/tsgo -b` 通过。
- 侧栏 hook 与访问令牌卡片测试：2 suites、35 tests 通过；角色设置和审计路由测试：2 suites、4 tests 通过。Tabs 修正后角色设置测试再次通过：1 suite、1 test。
- 修改 TS/TSX 文件的 oxlint 通过，0 warning、0 error；`git diff --check` 通过。
- locale JSON 回读对比确认每种语言新增 4 个 key，删除 0 个 key，既有值变更 0 个。Bun 在此环境 `NOT AVAILABLE`；使用 Node 执行 `web/package.json` 中 `i18n:sync` 对应的 `node scripts/sync-i18n.mjs`。

## 结果与剩余事项

- 本地代码验证：`VERIFIED`。生产角色配置尚未通过管理界面保存；普通用户的线上页面/API限制要在代码从 `main` 经受控 Release 部署后生效。
- 全量 Go/Web 测试、真实浏览器验收、生产配置保存、受控 Release、VPS 部署及部署后回读：`NOT RUN`。
- 没有更改 `AGENTS.md`、上层架构 README、Compose、Docker、Caddy、Cloudflare、DNS、防火墙或 upstream 引用。
