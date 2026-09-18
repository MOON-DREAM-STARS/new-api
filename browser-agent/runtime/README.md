# Web Workspace Runtime

默认镜像名：`newapi-web-workspace-runtime:local`。镜像基于 `alpine:3.20`，提供 Xvfb、x11vnc、GUI Chromium 与内置的 `workspace-guard`（Browser Guard），并以非 root 用户 `webworkspace`（uid/gid `10001`）运行。

## 构建

构建上下文是 `browser-agent/`（不是 `runtime/`）：

```sh
docker build -f browser-agent/runtime/Dockerfile -t newapi-web-workspace-runtime:local browser-agent/
```

Dockerfile 为多阶段构建：`golang:1.26.1-alpine` 阶段只复制 `go.mod`、`go.sum`、`internal/policy`、`internal/guard`、`cmd/workspace-guard`，以 `CGO_ENABLED=0` 静态编译 guard；最终阶段保留原有 alpine 包、用户、ENV 与 ENTRYPOINT，并额外复制 `/usr/local/bin/workspace-guard`（0755）。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `WW_PROXY_SERVER` | 无（必填） | egress proxy，格式 `http(s)://host[:port]`，作为 Chromium `--proxy-server`。缺失或为空时入口脚本报错并以 1 退出。 |
| `WW_GUARD_MODE` | `LOCKED` | `LOCKED` 或 `LOGIN`（大小写不敏感、忽略空白）；其他值入口脚本报错并以 1 退出，guard 侧再由 `policy.ParseMode` 复核。 |
| `WW_CDP_URL` | `http://127.0.0.1:9222` | guard 使用的 DevTools HTTP 端点。只允许 loopback（`127.0.0.1`、`::1`、`localhost`）且 `webSocketDebuggerUrl` 也必须落在 loopback，否则 guard 拒绝启动并退出 1。 |
| `WW_WORKSPACE_DIR` | `/workspace` | 单 workspace 的持久目录；仅挂载该目录，不挂载其他 workspace 或 `/data/web-workspaces` 根。 |
| `WW_DISPLAY` | `:99` | Xvfb 显示号。 |
| `WW_SCREEN_WIDTH` | `1280` | Xvfb 屏幕宽度。 |
| `WW_SCREEN_HEIGHT` | `720` | Xvfb 屏幕高度。 |
| `WW_VNC_PORT` | `5900` | x11vnc 监听端口。 |
| `WW_PROVIDER` | `unknown` | 信息字段，同时用于推导默认 `WW_START_URL`（`chatgpt` → `https://chatgpt.com/`）。 |
| `WW_START_URL` | 按 `WW_PROVIDER` 推导 | 运行时窗口的起始 URL：`chatgpt` → `https://chatgpt.com/`，其他 provider → `about:blank`；显式设置时优先使用该值。 |

入口脚本将 `HOME` 设为 `WW_WORKSPACE_DIR`，并将 `XDG_CONFIG_HOME`、`XDG_CACHE_HOME`、`XDG_DATA_HOME`、`XDG_RUNTIME_DIR` 分别指向 `profile/config`、`cache`、`profile/data`、`tmp/runtime`，避免 Chromium 写入只读 rootfs。

## Chromium 启动参数

入口脚本固定使用以下与安全相关的参数：

```text
--proxy-server="$WW_PROXY_SERVER"           全部 HTTP(S) 流量走 egress proxy
--proxy-bypass-list="<-loopback>"           取消 loopback 隐式绕过，内网请求同样交给策略层
--disable-quic                              禁止 QUIC 绕过 HTTP 代理策略
--webrtc-ip-handling-policy=disable_non_proxied_udp
--remote-debugging-port=9222                仅运行期内 CDP
--remote-debugging-address=127.0.0.1        CDP 只监听 loopback，禁止 publish
--deny-permission-prompts                   自动拒绝权限提示（clipboard 等）
--no-sandbox --test-type --user-data-dir=... --display=... --window-size=<W,H> --window-position=0,0 --app="$WW_START_URL"
```

运行时窗口是 Provider 的应用窗口（`--app`）：只显示页面内容，没有标签页、地址栏、书签栏和 `--no-sandbox` 警告条。`--start-maximized` 在没有窗口管理器的 Xvfb 中不会生效，因此窗口尺寸由 `--window-size="$WW_SCREEN_WIDTH,$WW_SCREEN_HEIGHT"` 显式对齐屏幕。`--test-type` 只抑制上述警告条，不改变网络与导航策略：起始 URL 之外的每次 navigation 与请求仍由 workspace-guard 与 egress proxy 按同一份策略表拦截。

## Browser Guard（`workspace-guard`）

`workspace-guard` 是运行期内唯一的 CDP consumer，连接 `WW_CDP_URL` 后：

- `Target.setDiscoverTargets` + `Target.setAutoAttach{autoAttach,waitForDebuggerOnStart,flatten}`，新 target 在运行任何脚本前先被纳管；
- 每个 page target 先 `Fetch.enable{patterns:[{urlPattern:"*"}]}` 与 `Page.enable`，再 `Runtime.runIfWaitingForDebugger`；
- `Fetch.requestPaused` 用与 agent 侧 egress proxy 相同的 `internal/policy` 表判定：未知域名、非 `http(s)` scheme、非 80/443 端口、IP 字面量一律 `Fetch.failRequest{errorReason:"AccessDenied"}`，并写 `event=policy_deny` 审计；
- 初始 URL 非空且不是 `about:blank` 的非白名单 page target 直接 `Target.closeTarget` 并写 `event=target_deny` 审计；`about:blank` popup 由首个 document 请求的 Fetch 拦截兜底；
- `Browser.setDownloadBehavior{behavior:"deny",eventsEnabled:true}`，收到 `Browser.downloadWillBegin` 写 `event=download_deny` 审计；
- 顶层 `Page.frameNavigated` 后对该 origin 把 `clipboard-read` 与 `clipboard-sanitized-write` 设为 `denied`；Chromium 不支持该命令时只记录 Debug 日志，`--deny-permission-prompts` 仍是兜底；
- 日志为 JSON（slog）写 stderr，只记录 host 级别信息，不写完整 URL、path、query、cookie 或 token，也不写 DevTools WebSocket URL。

### Fail closed

guard 与 CDP 断连、命令通道出错、启动 15s 预算内无法连接、模式或 `WW_CDP_URL` 非法时，guard 以非 0 退出。入口脚本的 watchdog 一旦发现 guard 退出，会记录日志、清理 Chromium → x11vnc → Xvfb 并以 1 退出，因此容器状态为 FAILED，浏览器不会在没有 guard 的情况下继续可用。

## 启动与退出行为

入口脚本按以下顺序启动进程：

1. 校验 `WW_PROXY_SERVER` 与 `WW_GUARD_MODE`，非法即退出 1；
2. 启动 Xvfb，并等待对应 X11 socket 就绪；
3. 启动 `x11vnc`（`-rfbport "$WW_VNC_PORT" -forever -shared -nopw -nolookup -noxdamage -quiet -bg`，不带 `-clip`，VNC 剪贴板保持关闭），并等待该端口就绪；
4. 启动 Chromium 应用窗口（后台，`--user-data-dir=$WW_WORKSPACE_DIR/profile`，起始 URL 为 `$WW_START_URL`）；
5. 启动 `workspace-guard`（后台），随后 watchdog 同时监视两者：guard 退出 → 清理并以 1 退出；Chromium 退出 → 按 Chromium 的退出码清理并退出。

`SIGTERM`/`SIGINT` 到达时，按 workspace-guard → Chromium → x11vnc → Xvfb 顺序终止进程后再退出。`docker stop` 不应留下该容器内的孤儿进程。

## 运行前提与安全取舍

- 必须使用私有 Docker network，并由 Browser Agent service auth 控制访问；不要把 `$WW_VNC_PORT` publish 到宿主或公网。镜像不含 VNC 密码，`-nopw` 意味着连接认证完全依赖网络隔离与 Agent 认证；因此绝不能把该端口暴露给不受信网络。
- CDP 只在容器 loopback 上监听（`--remote-debugging-address=127.0.0.1`），不得 publish，也不得把 endpoint 或 DevTools WebSocket URL 返回给客户端；guard 是唯一 CDP consumer。
- Runtime manager 必须保证每个 workspace 最多只有一个活动 runtime。容器重启后 hostname/PID 会变化，入口脚本会清理 `profile/` 中 Chromium 遗留的 `SingletonLock`、`SingletonCookie`、`SingletonSocket`，但不会删除其他 profile 数据；若并发挂载同一 profile，这些锁不能替代 Agent 层的单实例调度。
- 宿主 workspace 目录必须可由 uid/gid `10001` 写入（或在创建容器前调整为 `10001:10001`）；否则入口脚本无法创建 `profile/`、`cache/`、`tmp/`，Chromium 无法启动。
- 推荐由 Agent 设置 `--read-only`、`--tmpfs /tmp:rw,size=256m,mode=1777`、`--tmpfs /run:rw,size=16m,mode=755`、`--shm-size=512m`、`--cap-drop ALL`、`--security-opt no-new-privileges`、Memory/CPU/Pids 限制，并且只将该 workspace 的目录 bind mount 到 `/workspace`。
- Chromium 使用 `--no-sandbox`：在丢弃 capabilities 的容器内，这避免依赖 setuid sandbox；安全边界主要由只读 rootfs、capability 丢弃、资源限制、私有网络、guard 与 egress proxy 承担。不得在缺少这些边界的宿主环境中直接运行。
- Chromium 需要可写的 `/dev/shm`；验收运行使用 `--shm-size=512m`。若部署环境无法提供足够大小，需要在后续镜像/启动配置中显式增加 `--disable-dev-shm-usage` 并重新验收；本镜像的默认 Chromium 命令不包含该 flag。
- guard 与 egress proxy 共用同一份 `internal/policy` 白名单：浏览器层拦截 navigation/请求/弹窗/下载/剪贴板，网络层负责 DNS 解析与私网地址拒绝；两层都不能单独视为完整的 Project ownership 边界。
