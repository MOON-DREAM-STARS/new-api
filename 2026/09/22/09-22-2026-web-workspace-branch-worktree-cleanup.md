# Web Workspace 分支与 Worktree 清理记录

日期：2026-09-22
仓库：`D:\github\api\new-api`

## 目的与范围

整理 Web Workspace 验收后的长期分支与 worktree 拓扑：保留
`feature/dreamstars-homepage`、建立纯 `upstream-sync` 同步线、保留 `main`
作为 fork 集成/部署线，提交已验收的本地实现，并删除已确认不再需要的 release
worktree 与本地 release branch。

本次明确不 push、不部署、不修改生产服务器、不删除远端 branch。

## 变更前状态

- 当前分支：`feature/dreamstars-homepage`
- 当前 HEAD：`e2569c9fe`
- 相对本地 `origin/feature/dreamstars-homepage`：ahead 28、behind 0
- 工作区：无 staged 文件；存在受保护的用户 dirty work
- 已注册 worktree：主 feature、detached release-registration、release/upstream-7510-7512、main/upstream-sync
- `main` HEAD：`edb263a5b`
- `main` 相对本地 `upstream/main` 的净差异：Dreamstars release workflow

## 保护范围

以下内容不参与本轮提交或删除：

- `README.md`
- `docs/web-workspace/implementation-checklist.md`
- `docs/web-workspace/checklist-project-auto-create.md`
- `outputs/image2ui/*.png`
- `browser-agent/spike/kasmvnc/artifacts/`

不得使用 `git clean -fdx`、`git reset --hard`、全量 stash 或覆盖上述内容。

## 执行记录

后续在本文件追加：

- 两笔实现 commit 的 SHA 与精确文件范围；
- fetch 与 merge 的结果；
- 分支/worktree 删除前的 SHA；
- 删除后 worktree、branch、dirty 状态回读；
- 实际运行的测试与未运行项；
- 回滚路径和剩余风险。

## 外部动作

- Push：NOT RUN
- Release：NOT RUN
- 生产部署：NOT RUN
- 远端 branch 删除：NOT RUN

## 已完成：feature 实现分组提交

- Commit 1：`461cd9bf8` `perf(web-workspace): switch runtime to native KasmVNC websocket`
- Commit 2：`cf0254135` `fix(web-workspace): align API and UI with native KasmVNC transport`
- 两笔 commit 均只包含显式 Browser Agent/Runtime 或 New API/Frontend 路径。
- 受保护的 README、checklist、image2ui 输出和 KasmVNC spike artifacts 未 stage、未提交。
- 提交后根工作区剩余内容仅为既有用户 dirty work 与本记录文件。

## Fetch 结果

- `upstream/main` 从 `0aec08fee` 更新到 `9310231b3`：`fix(logs): keep model badge text readable and compact (#7504)`。
- `origin/main` 未变化，仍为 `4cb0a4730`。
- `origin/feature/dreamstars-homepage` 未变化，仍为 `aae81ef1d`。
- 本次只 fetch 了指定的 upstream/main、origin/main 和 origin/feature 分支；未 push、未部署。

## Worktree 拓扑调整

- `D:\github\api\new-api-upstream-sync` 已切换为 `upstream-sync`，当前 HEAD=`9310231b3`，tracking=`upstream/main`。
- 新增 `D:\github\api\new-api-main-integration`，承载原 `main`，当前 HEAD=`edb263a5b`，tracking=`origin/main`。
- 现有 feature worktree、protected dirty work 和两个待删除 release worktree 暂未改变。
- `origin/feature/dreamstars-homepage` fetch 后未前进；feature 相对其仍 ahead 30、behind 0。

## Feature 与 main 同步结果

- `feature/dreamstars-homepage` 已通过普通 merge 合入 `upstream-sync`，产生 merge commit：`c59c76012`。
- README 与 `docs/web-workspace/implementation-checklist.md` 使用路径级 stash 保护后已恢复为 dirty；未使用全量 stash，未丢弃用户内容。
- 处理的冲突保留了 Web Workspace 的 Center slot、侧栏入口、主页 PublicHeader、Dreamstars release update UI，同时合入 upstream 的系统更新、Audit Logs、Security & Access 和其他上游导航变更。
- `main` 已先合入 `upstream-sync`：`2993ff0b8`，包含 upstream `9310231b3` 的日志 badge 修复。
- `main` 随后已合入 `feature/dreamstars-homepage`，当前 main 与 feature 文件树一致。

## Diagnostic 结果

- `npm run test -- src/features/web-workspace`：PASS，19 files / 130 tests。
- `npm run typecheck`：PASS。
- `npm run build`：PASS。
- `git diff --check`：PASS。
- Browser Agent Go test/vet：NOT RUN；当前 Windows Docker CLI 无法连接 `dockerDesktopLinuxEngine`，本机 PATH 也没有 Go。原始错误为 Docker API named pipe 不存在；未重启 Docker Desktop、未修改系统服务。

## Release staging 删除前检查

- `new-api-main-release-registration`：clean、detached、提交已被 main/origin/main 包含。
- `release/upstream-7510-7512`：clean，独有 SHA 为 `f817dc927`、`582cebde0`、`1a4b2bd94`。
- 该 release staging branch 的部分文件与当前 main 不完全相同，但差异来自当前 main 已继续前进到更新的 upstream relayconvert 版本；main 已包含 #7510/#7512 对应功能和更新后的上下文签名。删除前保留上述 SHA 记录。

## Release staging 清理结果

- 已删除 clean worktree：`D:\github\api\new-api-main-release-registration`。
- 已删除 clean worktree：`D:\github\api\new-api-release-7510-7512`。
- 已删除本地 branch：`release/upstream-7510-7512`，删除前 HEAD=`1a4b2bd94`。
- 两个物理目录均已不存在；未删除任何远端 branch。

## 最终状态

- `feature/dreamstars-homepage`：`c59c76012`，相对 origin feature ahead 191；仅保留既有用户 dirty work 与本记录。
- `main`：`fca4fbc44`，位于 `D:\github\api\new-api-main-integration`，相对 origin/main ahead 231，worktree clean。
- `upstream-sync`：`9310231b3`，位于 `D:\github\api\new-api-upstream-sync`，与 `upstream/main` 完全一致，worktree clean。
- feature 与 main 文件树一致；`git diff --check` 通过。
- Push：NOT RUN
- Release：NOT RUN
- 生产部署：NOT RUN
- Go test/vet：NOT RUN，原因是本机 Docker Linux engine named pipe 不可用且 PATH 无 Go；未重启 Docker 或修改系统服务。
