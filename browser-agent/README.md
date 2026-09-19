# Browser Agent（Web Workspace 执行平面）

独立 Go 模块：`github.com/QuantumNous/new-api/browser-agent`。它管理每个 Web Workspace 的
runtime 容器（Xvfb + x11vnc + GUI Chromium）、per-workspace profile 挂载、远程显示字节流代理、
session 生命周期（idle timeout / crash recovery / reconcile）与资源限制。

依赖仅 stdlib + `github.com/gorilla/websocket`（+ testify，仅测试）；Docker 交互使用 stdlib
`net/http` over unix socket，不引入 Docker SDK，也不 import 根模块。

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `WEB_WORKSPACE_AGENT_TOKEN` | 是 | — | 服务认证 token（Bearer）。为空时进程启动失败（fail closed）。 |
| `WEB_WORKSPACE_AGENT_LISTEN` | 否 | `0.0.0.0:8730` | 监听地址，必须是 `host:port`。仅接入私网，不得暴露公网。 |
| `WEB_WORKSPACE_DATA_ROOT` | 否 | `/data/web-workspaces` | **agent 进程内**的 workspace 数据根目录，必须是绝对路径。本地 `MkdirAll` / chown 只用它。 |
| `WEB_WORKSPACE_HOST_DATA_ROOT` | 否 | 等于 `WEB_WORKSPACE_DATA_ROOT` | **Docker daemon 可见**的宿主机路径前缀，必须是绝对路径。仅用于计算 runtime 容器的 bind 源：`<host-data-root>/workspace-<id>`。 |
| `DOCKER_HOST` | 否 | `unix:///var/run/docker.sock` | 仅支持 `unix://` 前缀的 unix socket，其他形式启动失败。 |
| `WEB_WORKSPACE_RUNTIME_IMAGE` | 否 | `newapi-web-workspace-runtime:local` | runtime 镜像（见 `runtime/README.md`）。 |
| `WEB_WORKSPACE_RUNTIME_NETWORK` | 否 | `newapi-workspace-runtimes` | runtime 容器加入的私有 network。启动时检查 `Internal=true`；不存在则创建 internal bridge，已存在但非 internal 时启动失败。 |
| `WEB_WORKSPACE_EGRESS_PROXY_LISTEN` | 否 | `0.0.0.0:8731` | Agent 内 forward proxy 的监听地址，必须是 `host:port`，只接入 runtime internal network。 |
| `WEB_WORKSPACE_EGRESS_PROXY_URL` | 是 | — | runtime 容器使用的 proxy URL，注入为 `WW_PROXY_SERVER`；必须是 `http(s)://host[:port]`，缺失或非法时启动失败（fail closed）。 |
| `WEB_WORKSPACE_RUNTIME_MEMORY_BYTES` | 否 | `1073741824` | 每 runtime 内存上限。 |
| `WEB_WORKSPACE_RUNTIME_CPUS` | 否 | `1.0` | 每 runtime CPU 上限（转成 NanoCPUs）。 |
| `WEB_WORKSPACE_RUNTIME_PIDS` | 否 | `256` | 每 runtime PIDs 上限。 |
| `WEB_WORKSPACE_IDLE_TIMEOUT_SECONDS` | 否 | `600` | runtime 无 stream/无 activity 后自动停止的秒数。 |
| `WEB_WORKSPACE_IDLE_SCAN_SECONDS` | 否 | `15` | 崩溃探测与 idle 扫描周期。 |

### `DATA_ROOT` 与 `HOST_DATA_ROOT` 的语义（部署关键）

`WEB_WORKSPACE_DATA_ROOT` 是 **agent 容器内**的路径，`WEB_WORKSPACE_HOST_DATA_ROOT` 是
**Docker daemon 在宿主/VM 上**解析的同一份数据目录。runtime 容器的 `Binds` 只能用后者派生，
否则 daemon 会绑定到宿主上不存在（或自动创建）的空目录，runtime 拿到空 `/workspace` 后立即退出。

```yaml
services:
  browser-agent:
    environment:
      WEB_WORKSPACE_AGENT_TOKEN: "<service-token>"
      WEB_WORKSPACE_DATA_ROOT: /data/web-workspaces
      # 宿主/VM 上真实存在、且由 Docker daemon 解析的同一目录：
      WEB_WORKSPACE_HOST_DATA_ROOT: /srv/dreamstars/web-workspaces
    volumes:
      - /srv/dreamstars/web-workspaces:/data/web-workspaces
      - /var/run/docker.sock:/var/run/docker.sock
```

- agent 在内部路径下创建 `workspace-<id>/{profile,uploads,downloads,cache,tmp}` 并把属主交给
  runtime 镜像的非 root 用户 `10001:10001`；chown 失败且目标不属于该 uid/gid 时启动 fail closed。
- runtime 容器只挂载 `<host-data-root>/workspace-<id>` → `/workspace`（`WW_WORKSPACE_DIR=/workspace`），
  不挂载数据根目录，也不挂载其他 workspace。
- 两者取值相同（agent 直接跑在宿主上，或挂载点路径与宿主路径一致）时行为不变。
- workspace 目录树与 `.guard` 状态（`ownership.json`、`permit.json`、`permit.consumed`、
  `observations.jsonl`、`observations.offset`）全部经 `os.Root` 的 no-follow 句柄读写：runtime 容器
  把 `.guard`、状态文件或子目录换成符号链接（含相对链接指向其他 workspace）时 agent 一律 fail closed
  （HTTP 500），既不会跟随链接写到 workspace 之外，也不会对被指向的目录执行 chown（Phase 6 验收）。

## 部署前提

- **仅私网可达**：不 publish agent 端口，不把 agent 放入任何可从公网访问的网络；客户端只能经
  New API 接入。
- **服务认证**：除 `GET /healthz` 外所有端点都要求 `Authorization: Bearer <token>`，失败返回 401；
  token 比较为常数时间。
- **数据目录**：宿主目录必须真实存在且可被 Docker daemon 解析；agent 需能对其创建目录并 chown
  到 `10001:10001`（以 root/CAP_CHOWN 运行，或以 10001 运行）。
- **私有 network**：Agent 启动时先检查 `WEB_WORKSPACE_RUNTIME_NETWORK`；不存在则创建 `Driver=bridge`、`Internal=true` 的 network，已存在但 `Internal != true` 时拒绝启动。runtime 不 publish 任何端口，也没有除 Agent proxy 外的出网通道。
- **不暴露 runtime 端口**：runtime 容器 `PortBindings` 为空、`NetworkMode` 为上述私有 network；
  VNC（5900）只在容器网络内可达，由 agent 直连。

## default-deny egress（Phase 3A）

- runtime 容器只加入 `Internal=true` 的私有 bridge network，不能直接访问宿主、Docker、metadata、其他 workspace 或公网。
- Agent 进程内的 forward proxy 是唯一出网路径。它只接受 `CONNECT host:port` 与 absolute-form `http://host[:port]/...` 的 GET/HEAD/POST/PUT/DELETE/OPTIONS/PATCH；其他方法返回 405，端口只允许 80/443。
- proxy 使用共享 `internal/policy`：域名 allowlist、解析后地址段检查、混合解析结果整体拒绝、DNS rebinding 防护；实际拨号使用已经校验的 IP，不会再次按域名解析。未知来源 IP、解析失败/空结果、任何地址被拒绝时都返回 403 并记录 deny 审计。
- Create 时给 runtime 注入 `WW_PROXY_SERVER=<WEB_WORKSPACE_EGRESS_PROXY_URL>` 与 `WW_GUARD_MODE=LOCKED|LOGIN`；缺省为 `LOCKED`。runtime/guard 消费这些变量，本工作包只负责注入与 agent-side proxy 边界。

## runtime 容器隔离与资源（固定，请求不可覆盖）

容器名 `newapi-ws-runtime-<id>`；labels `newapi.web-workspace=1`、
`newapi.web-workspace.workspace-id=<id>`；`ReadonlyRootfs=true`；tmpfs `/tmp:rw,size=256m,mode=1777`
与 `/run:rw,size=16m,mode=755`；`ShmSize=512MiB`；`CapDrop=["ALL"]`；
`SecurityOpt=["no-new-privileges:true"]`；`Privileged=false`；`AutoRemove=false`；
`RestartPolicy=none`；无端口发布；Memory/CPU/Pids 按上表限制；使用镜像默认非 root 用户。
Agent 自身不挂载、也不把 Docker socket 暴露给 runtime 容器。

## HTTP API（私网）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | `{"status":"ok"}`，无需认证。 |
| POST | `/internal/v1/runtimes` | 幂等启动；`{workspace_id,provider,width,height,mode}`，尺寸默认 1280x720（范围 640..3840 / 360..2160），`mode` 缺省 `LOCKED`，只接受 `LOCKED`/`LOGIN`。 |
| GET | `/internal/v1/runtimes/{id}` | 当前 runtime 状态，无则 404 `runtime_not_found`。 |
| POST | `/internal/v1/runtimes/{id}/stop` | 停止并删除容器，保留 profile。 |
| POST | `/internal/v1/runtimes/{id}/restart` | 停 + 启；body 可省略以复用上次 provider/尺寸/mode，也可用 `mode` 覆盖为 `LOCKED`/`LOGIN`。非法 mode 返回 400 `invalid_request`。 |
| POST | `/internal/v1/runtimes/{id}/activity` | 刷新 `last_activity_at` / idle deadline。 |
| GET | `/internal/v1/runtimes/{id}/stream` | WebSocket，代理容器内 VNC(5900) 的原始 RFB 字节；非 RUNNING/IDLE 返回 409 `runtime_not_running`。 |

运行时 JSON 只含 `runtime_id`、`workspace_id`、`state`、`created_at`、`last_activity_at`、
`idle_deadline_at`，不含容器 IP、端口、Docker 路径、profile 绝对路径或 token。

状态机：`STARTING → RUNNING → IDLE → STOPPING → STOPPED`，异常 `FAILED`。
`RUNNING` 只在“容器确认运行中 **且** display 探测连通”后才返回（探测间隔 250ms，上限 20s）；
容器启动后立即退出或 display 一直不可用时，容器会被移除、状态置 `FAILED` 并返回错误。

## 构建与测试

无本地 Go 时在容器内执行：

```sh
docker run --rm -v "$PWD/..:/src" -w /src/browser-agent golang:1.26.1-alpine \
  sh -c "gofmt -l . ; GOWORK=off go vet ./... && GOWORK=off go test ./... -count=1"
GOWORK=off go build ./cmd/browser-agent
```

容器构建（构建上下文为 browser-agent/）：

```sh
docker build -t <tag> .
```
