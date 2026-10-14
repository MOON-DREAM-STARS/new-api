# 角色侧栏与审计访问变更清单（2026-10-14）

- 仓库：`D:\github\api\new-api`；开发分支：`dev`。
- 回滚基线：`dcd1be88e19abc4bedfd53cf23f096833273d553`。
- 后端变更：`router/api-router.go`、`router/access_token_scope_test.go`、`service/access_token_scope.go`。
- 前端变更：侧栏配置 parser、角色设置组件及测试、导航可见性 hook 及测试、个人侧栏偏好、访问令牌卡片与测试、审计路由与测试、`static-keys.ts` 和七个 locale JSON。
- 受控代码验证：Docker `golang:1.26` router tests、Node TypeScript/Vitest/oxlint、locale JSON 对比；详细命令和状态见相邻变更记录。
- 生产备份载荷：`NOT APPLICABLE`，本次未执行任何服务器或持久配置写入；未收集或保存秘密。
- 回滚通过 revert 本次精确源码提交及本地分支集成提交；无数据库迁移，无镜像或容器变更。
- 本地 Git 用户文件 `AGENTS.md`、已有分支迁移记录、Browser Agent 产物及图片均不属于本变更清单。
