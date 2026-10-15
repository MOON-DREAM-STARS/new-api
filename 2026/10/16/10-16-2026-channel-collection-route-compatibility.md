# 渠道集合路由末尾斜杠兼容（2026-10-16）

## 目的与范围

修复管理员打开渠道页面时，列表请求 `GET /api/channel` 返回 `404 Invalid URL` 的问题。只扩展渠道集合路由对带斜杠和无斜杠路径的兼容，不调整前端请求、全局路由重定向、权限模型、数据结构或计费行为。

## 变更前状态

- 前端 `web/src/features/channels/api.ts` 的 `getChannels()` 和 `createChannel()` 分别请求 `GET /api/channel` 与 `POST /api/channel`；更新渠道使用带斜杠的 `PUT /api/channel/`。
- `router/channel-router.go` 只注册 `GET/POST/PUT /api/channel/`。生产只读复现显示，无斜杠 GET 返回 `404 Invalid URL`，带斜杠 GET 能进入鉴权并返回 `401`。
- 本任务开始时生产应用运行 `dreamstars-20261015-072048-05146349640a`，容器 healthy，宿主只绑定 `127.0.0.1:3001`；发布时间、服务器备份及部署后证据见父工作区 2026-10-16 渠道路由发布记录。

## 实现

- 在现有 `channelPermissionRoutes` 中为 GET/POST/PUT 根路径各增加一条空相对路径路由，与原带斜杠路由共用处理器、权限和 `handlePermissionRoute` 中间件链。
- 新增路由集成回归，覆盖两种地址的未登录拒绝、授权管理员的同一渠道列表且不泄露 Key、无渠道读取权限的拒绝、缺少个人访问令牌 scope 的拒绝，以及无副作用的无效创建/更新载荷。
- 不涉及数据库迁移。前端与全局 HTTP 客户端保持原样。

## 验证

- Go 1.26 路由/权限回归：`go test -p 1 -mod=readonly ./router -run 'TestChannel|TestAccessTokenRouteRulesCoverEveryDashboardRoute|TestAuditRoutesEnforceProductionAuthorization' -count=1` — **VERIFIED**。
- `gofmt` 完成；`git diff --check` 通过。
- 浏览器与生产发布状态见父工作区 `2026/10/16/10-16-2026-channel-collection-route-deployment.md`，相邻备份清单记录根路径的实时回读和回滚信息。

## 数据与回滚

- 无数据库结构或数据更改；没有创建、更新或删除生产渠道。
- 代码回滚由父工作区发布记录中列出的上一受控 Release 执行；本次生产发布成功后是否实际回滚，以该记录为准。
