# Checklist：Web Workspace 项目自动创建与居中项目管理窗口（2026-09-19）

Main = 唯一调度/集成/验收 authority。本文件只由 Main 写入。

## 工作包

| 包 | 范围 | owner | 状态 |
| --- | --- | --- | --- |
| WP1 | New API 控制面（DTO / permit / session.project_creation / 409） | Main | IN_PROGRESS |
| WP2 | Browser Agent + Guard 自动创建与 fallback 状态 | Main | IN_PROGRESS |
| WP3 | 前端（居中 Dialog、名称表单、reveal crop、轮询） | Halley(subagent) | IN_PROGRESS |
| WP4 | 本地 diagnostic（Go + 前端 + runtime 镜像） | Main | PENDING |
| WP5 | 云端受控部署与留档 | Main | PENDING |

## 契约（冻结）

- `POST /api/web-workspace/projects` body `{"name":"..."}`，trim 后 1–64 Unicode 字符。
- 进行中创建返回 `409 WEB_WORKSPACE_PROJECT_CREATION_IN_PROGRESS`。
- Agent `permit.json` 新增 `display_name`（可空，兼容旧 permit）。
- Guard `project-creation.json` = `{permit_id,state,error,updated_at}`，state ∈ RUNNING/CREATED/FAILED。
- session DTO 新增可空 `project_creation:{permit_id,state,error,updated_at}`。
- 失败码：ERR_PROVIDER_LOGIN_REQUIRED / ERR_PROJECT_UI_NOT_FOUND / ERR_PROJECT_NAME_REJECTED / ERR_PROJECT_CREATE_TIMEOUT。
- presentation `revealProviderChrome=true` ⇒ cropLeft=0，坐标仍走同一 transform。

## 状态

- 2026-09-19：只读探索完成，冻结契约，进入实现。
