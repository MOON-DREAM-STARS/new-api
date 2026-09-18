# Web Workspace Runtime

默认镜像名：`newapi-web-workspace-runtime:local`。镜像基于 `alpine:3.20`，提供 Xvfb、x11vnc 与 GUI Chromium，并以非 root 用户 `webworkspace`（uid/gid `10001`）运行。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `WW_WORKSPACE_DIR` | `/workspace` | 单 workspace 的持久目录；仅挂载该目录，不挂载其他 workspace 或 `/data/web-workspaces` 根。 |
| `WW_DISPLAY` | `:99` | Xvfb 显示号。 |
| `WW_SCREEN_WIDTH` | `1280` | Xvfb 屏幕宽度。 |
| `WW_SCREEN_HEIGHT` | `720` | Xvfb 屏幕高度。 |
| `WW_VNC_PORT` | `5900` | x11vnc 监听端口。 |
| `WW_PROVIDER` | `unknown` | 仅用于启动日志的信息字段。 |

入口脚本将 `HOME` 设为 `WW_WORKSPACE_DIR`，并将 `XDG_CONFIG_HOME`、`XDG_CACHE_HOME`、`XDG_DATA_HOME`、`XDG_RUNTIME_DIR` 分别指向 `profile/config`、`cache`、`profile/data`、`tmp/runtime`，避免 Chromium 写入只读 rootfs。

## 启动与退出行为

入口脚本按以下顺序启动进程：

1. 启动 Xvfb，并等待对应 X11 socket 就绪；
2. 启动 `x11vnc`，参数包含 `-rfbport "$WW_VNC_PORT" -forever -shared -nopw -nolookup -noxdamage -quiet -bg`，并等待该端口就绪；
3. 启动 Chromium（后台进程，脚本等待其退出），使用 `$WW_WORKSPACE_DIR/profile` 作为 `--user-data-dir`，并保持容器存活。

`SIGTERM`/`SIGINT` 到达时，按 Chromium → x11vnc → Xvfb 顺序终止进程后再退出；Chromium 的退出状态由脚本转发。`docker stop` 不应留下该容器内的孤儿进程。

## 运行前提与安全取舍

- 必须使用私有 Docker network，并由 Browser Agent service auth 控制访问；不要把 `$WW_VNC_PORT` publish 到宿主或公网。镜像不含 VNC 密码，`-nopw` 意味着连接认证完全依赖网络隔离与 Agent 认证；因此绝不能把该端口暴露给不受信网络。
- Runtime manager 必须保证每个 workspace 最多只有一个活动 runtime。容器重启后 hostname/PID 会变化，入口脚本会清理 `profile/` 中 Chromium 遗留的 `SingletonLock`、`SingletonCookie`、`SingletonSocket`，但不会删除其他 profile 数据；若并发挂载同一 profile，这些锁不能替代 Agent 层的单实例调度。
- 宿主 workspace 目录必须可由 uid/gid `10001` 写入（或在创建容器前调整为 `10001:10001`）；否则入口脚本无法创建 `profile/`、`cache/`、`tmp/`，Chromium 无法启动。
- 推荐由 Agent 设置 `--read-only`、`--tmpfs /tmp:rw,size=256m,mode=1777`、`--tmpfs /run:rw,size=16m,mode=755`、`--shm-size=512m`、`--cap-drop ALL`、`--security-opt no-new-privileges`、Memory/CPU/Pids 限制，并且只将该 workspace 的目录 bind mount 到 `/workspace`。
- Chromium 使用 `--no-sandbox`：在丢弃 capabilities 的容器内，这避免依赖 setuid sandbox；安全边界因此主要由只读 rootfs、capability 丢弃、资源限制、私有网络和单 workspace 挂载承担。不得在缺少这些边界的宿主环境中直接运行。
- Chromium 需要可写的 `/dev/shm`；验收运行使用 `--shm-size=512m`。若部署环境无法提供足够大小，需要在后续镜像/启动配置中显式增加 `--disable-dev-shm-usage` 并重新验收；本镜像的默认 Chromium 命令不包含该 flag。
