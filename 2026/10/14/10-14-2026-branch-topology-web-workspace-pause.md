# Web Workspace 搁置与三分支重组（2026-10-14）

## 目的与范围

按运营者明确授权：创建 `main`、`dev`、`web-workspace` 三条活动分支；把工作区从日常开发和发布线剥离，只在冻结分支保存。允许提交、推送和清理指定旧分支。本任务只涉及本地源码和 MOON-DREAM-STARS Fork Git 引用。

## 变更前实时基线

- 本地和 origin `main=3733f0508f885f8e12996fe4f9820c14a09a86ec`；feature `75915ef2e9c039176412b1834210c3d9c874ea17`；master-main `9a0be8750a6d736d9692535ed2cd68f8eec46529`。
- 执行前通过 `git ls-remote` 和限定 fetch 核对 `upstream/main=4013729f6a2a14b9b5988d3c1a529ea97d2b2d8d`。
- `new-api` 工作树没有 tracked 改动；有截图和 `browser-agent/spike/kasmvnc/artifacts/`。其它两个源 worktree clean。用户产物只记录路径并保留。
- Git 完整 bundle 备份和 bare repository 恢复回读：VERIFIED。见旁边备份清单。
- 本机 Docker 起初 Linux engine stopped、API 返回500，WSL distro stopped；启动本地 engine 后为29.6.1，初始没有运行中的本地容器。仅创建任务专属测试容器，不操作 VPS。

## 精确变更

- `web-workspace` 基于上述最新 upstream，按顺序 `git cherry-pick -x` 53 个专属提交。见旁边来源清单。保留 Browser Agent、控制面、KasmVNC、桥接、页面、路由/迁移/侧栏接入和 noVNC；排除首页、sharp、性能监控、Fork Release 与 Responses 自定义改动。
- 早期文档/model/layout/locale/lock冲突保留新 upstream 实现并加入工作区 hunk；布局同时保留 `stackActionsOnMobile`。Bun1.3.2重新规范化 lockfile，最终相对 upstream 只增加 noVNC 依赖。
- 从源 merge `83b67bd1b` 提取工作区 session-only PAT 路由规则与对应回归；不复制 release 路由及 legacy PAT 生命周期修复。保留当前 main 的两项工作区后续修正和最后版本设计清单。
- `main` 移除工作区独有文件和注册、侧栏/生成路由/布局/CSS/翻译/noVNC。保留 UserAccessToken 迁移、非master只读 deadline 修复、release session-only 规则、首页、性能与 Responses。
- `dev` 从完成清理的 `main` 建立；日常开发 `dev → main`，上游同步 `upstream/main → main → dev`，使用 merge 保留 Fork 历史。
- workflow 和服务端 manifest allowlist 只接受 `main`；明确拒绝 dev、web-workspace、旧feature。GitHub workflow仍为手动，不自动部署。
- 独立审查确认隔离路径与专属翻译删除范围；没有混入其它 Fork 功能。
- 仅修正冻结分支一项旧测试夹具：用 `os.Root.Stat` / `os.SameFile` 检查目录身份，代替错误的绝对 `os.Root.Name()` 断言；生产行为未变。

## 实际验证

| 检查 | 结果与证据边界 |
| --- | --- |
| main Go1.26.0 service/router/model，DreamstarsRelease/AccessToken/LegacyAccessToken/Migration相关测试 | VERIFIED |
| main Web typecheck、10文件69项重点测试、修改接入文件 lint | VERIFIED |
| main / web-workspace Web生产构建（Node22.23.1、Bun1.3.2、Rsbuild2.1.6） | VERIFIED |
| main / web-workspace 根模块 Windows可执行文件构建 | VERIFIED；不等同于VPS运行证明 |
| web-workspace Web typecheck、19文件130项测试 | VERIFIED；jsdom有既有scrollTo未实现提示 |
| Browser Agent Linux Go1.25.1 全模块 tests/build | VERIFIED |
| web-workspace 新冲突接入文件 lint | VERIFIED |
| 搁置源代码已有 lint | PARTIAL；原源代码与迁移分支同样有14个error/4个warning，不扩大搁置功能开发来清理既有债务 |
| Windows服务并发SQLite清理、Browser Agent Unix socket/权限测试 | FAILED 平台检查；保留失败记录，目标Linux验证另行通过 |
| 真实SQLite/MySQL/PostgreSQL迁移与保留数据检查 | VERIFIED：SQLite、MySQL8.4.11、PostgreSQL15.19真实实例；main新库/二次迁移无工作区表、旧schema数据与索引经两次升级保留；冻结分支模型/ownership/sync三方言均通过。详见备份目录main-clean-migration.log与parked-*.log。 |
| git diff --check、分支范围复核 | VERIFIED |
| 三条origin分支推送、tracking、指定旧refs清理 | VERIFIED：三条分支已原子推送并与本地SHA一致，tracking均指向origin；指定旧refs已在SHA租约保护下删除。最终文档提交后的回读写入final-refs.txt。 |

数据库和Browser Agent目标平台为Linux。本次读取并沿用OWASP Authentication与Session Management Cheat Sheet关于服务端会话授权、拒绝不适当认证方式和不泄漏session/token的控制；不改变cookie登录或token生命周期，不宣称完整ASVS合规审计。

- <https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html>
- <https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html>

## 清理、恢复和剩余边界

已重新确认 feature、master-main、upstream-sync 都是保留main历史的祖先；先原子推送和回读三条目标分支，再按精确SHA租约删除 origin/feature/dreamstars-homepage 与 origin/master-main，并删除本地三条旧分支。当前工作目录已切换到dev；旧upstream-sync worktree已转detached，保留目录。逐路径回读确认94个原有未跟踪文件全部保留、没有新增或缺失。其它远端依赖分支、upstream引用和历史tags保留。

已有数据库的三张工作区表与数据不会通过此次代码剥离主动删除。恢复Git分支可从完整bundle fetch到新的recovery分支；恢复功能需要单独重新整合，不能把冻结分支自动合入活动线。

VPS连接、发布workflow触发、镜像发布、部署、回滚、线上账号/Provider请求、真实远程浏览器验收：NOT RUN。线上事实没有重新探测，本任务不声称线上功能已停用。受控部署和新即时授权仍独立于Git分支重组。

## 完成回读

- 功能变更提交：main/dev `76fab7410e9c67062311ab53c451c4265faca4a3`；冻结功能与必要接入提交 `58b36e114aad5201d5eadba72c77e65b4d5c5542`。后续只有此同期记录的文档提交，最终分支SHA见备份目录final-refs.txt。
- 三条分支：main、dev、web-workspace；均跟踪origin同名分支。当前日常worktree为dev，main工作树保留用于发布/sync，web-workspace有独立冻结worktree。
- 临时数据库和Linux验证runner按任务标签审计后移除；保留既有Go缓存卷。没有留下本任务容器或新数据库卷。
- 所有代码与数据库验证已完成；真实Provider/Web UI、GitHub Release、VPS部署与回滚仍为NOT RUN。
