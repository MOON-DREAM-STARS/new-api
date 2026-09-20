# Web Workspace KasmVNC 隔离测量 Spike

## 结论摘要

- 该 spike 仅用于研究，不替换生产 runtime。
- 在同一容器限制 `--cpus=1 --memory=1g --pids-limit=256`、无 GPU、无宿主端口发布的内网中完成真实客户端连接测量。
- KasmVNC 1.5.0 的 idle 与 active-scroll 带宽均显著低于 x11vnc + websockify/noVNC baseline：
  - 1280x720 idle：3580 B -> 300 B，降低 91.62%。
  - 2048x900 idle：3538 B -> 216 B，降低 93.89%。
  - 1280x720 active scroll：24,023,346 B -> 11,363,043 B，降低 52.70%。
  - 2048x900 active scroll：23,194,243 B -> 16,875,208 B，降低 27.24%。
- KasmVNC 在所有测量场景均未出现 1 vCPU 饱和；最高采样 CPU 峰值为 44.06%（1280x720 active scroll），低于 1 vCPU 上限。
- 键盘（ASCII）、鼠标/滚轮/页面导航和 file chooser 的截图诊断均已完成。中文 IME 与 clipboard 尚未执行，因此不能对完整 decision gate 给出 PASS。
- 目视检查 8–12 px 英文与中文样本未发现明显清晰度回退；但该判断来自客户端 canvas 截图的人工观察，不是独立的图像质量验收。

## 文件

- `Dockerfile.server`：Debian bookworm-slim，安装 Chromium、Xvfb、x11vnc、noVNC/websockify 与 KasmVNC v1.5.0 bookworm amd64 `.deb`。
- `Dockerfile.client`：Puppeteer-core 客户端镜像，用于以真实 Chromium/noVNC/KasmVNC web client 访问内网 spike server。
- `scripts/entrypoint-baseline.sh`：x11vnc + websockify + noVNC baseline 的 throwaway 定义，复制当前 runtime 的 x11vnc 参数。
- `scripts/entrypoint-kasmvnc.sh`：启动 KasmVNC、独立 Chromium 和本地 provider-free 页面。
- `page/index.html`、`page/second.html`：本地静态测试页，含小字号文本、长列表、textarea、file input 与导航目标页。
- `measure.js`：真实 web client 连接、三组 workload、客户端 RX 字节计数和 canvas 截图。
- `diagnose.js`：独立的键盘、textarea、file chooser 诊断。
- `run-spike.ps1`：构建、内网、容器限制、采样与汇总编排脚本。
- `artifacts/`：原始 JSONL、合并 JSON、客户端日志、截图和运行日志。

## 方法与边界

- baseline 不是直接复用生产 Alpine 镜像，而是在同一 Debian 基底上使用当前 `browser-agent/runtime/entrypoint.sh` 的 x11vnc 参数重放：`-forever -shared -nopw -nolookup -deferupdate 50 -wait 30 -quiet`，并用 `websockify + noVNC` 提供浏览器入口。
- KasmVNC 使用 Debian bookworm amd64 1.5.0 官方 release；未使用桌面镜像。
- 客户端为 headless Chromium 中的真实 noVNC/KasmVNC web client。`serverToClientBytes` 取自客户端容器 `eth0` 的 `rx_bytes` 增量；full run 时同一时间只运行一个 server。
- CPU 取自 server 容器 cgroup v2 `cpu.stat usage_usec`，每约 1 秒采样；RSS 同时记录 `memory.current` 与 `anon` 峰值。
- 不发布任何宿主端口，不修改 DNS/Caddy/Cloudflare/UFW/防火墙，不写入生产 runtime 文件。

## 原始结果

| renderer | size | workload | duration s | server->client bytes | CPU peak % | CPU avg % | RSS peak MiB | anon peak MiB |
|---|---:|---|---:|---:|---:|---:|---:|---:|
| baseline | 1280x720 | idle | 127.163 | 3,580 | 32.85 | 2.40 | 286.76 | 235.22 |
| kasmvnc | 1280x720 | idle | 127.068 | 300 | 13.70 | 1.97 | 267.11 | 217.43 |
| baseline | 1280x720 | active scroll | 60.102 | 24,023,346 | 46.12 | 21.49 | 274.12 | 220.71 |
| kasmvnc | 1280x720 | active scroll | 60.135 | 11,363,043 | 44.06 | 10.75 | 257.88 | 202.64 |
| baseline | 1280x720 | load + navigation | 12.002 | 63,078 | 10.87 | 3.41 | 296.02 | 237.89 |
| kasmvnc | 1280x720 | load + navigation | 12.002 | 51,624 | 17.95 | 5.90 | 267.48 | 216.08 |
| baseline | 2048x900 | idle | 127.177 | 3,538 | 6.51 | 1.74 | 321.92 | 243.25 |
| kasmvnc | 2048x900 | idle | 127.167 | 216 | 12.87 | 1.98 | 296.39 | 229.59 |
| baseline | 2048x900 | active scroll | 60.097 | 23,194,243 | 35.69 | 25.77 | 334.37 | 241.57 |
| kasmvnc | 2048x900 | active scroll | 60.102 | 16,875,208 | 22.87 | 14.87 | 303.97 | 228.09 |
| baseline | 2048x900 | load + navigation | 12.002 | 52,074 | 11.68 | 3.72 | 335.95 | 242.79 |
| kasmvnc | 2048x900 | load + navigation | 12.002 | 40,899 | 21.20 | 4.81 | 305.70 | 229.15 |

所有场景 `cpuIntervalTicksAtOrAbove95Percent = 0`。

## 功能诊断

- Mouse/滚轮/导航：active-scroll workload 有真实字节增长；`*-navigation.png` 显示 page 2 的 `Navigation destination reached`，baseline 与 KasmVNC 均成立。
- ASCII keyboard：`diagnose-*-keyboard.png` 显示 textarea 中注入的 `ASCII-keyboard-probe`，baseline 与 KasmVNC 均成立。
- File chooser：`diagnose-*-filechooser.png` 显示远端 GTK 文件选择器，baseline 与 KasmVNC 均成立。
- Chinese IME：`NOT RUN`。当前镜像没有配置 IME，不能从静态中文渲染截图推断输入法回退。
- Clipboard：`NOT RUN`。baseline 当前 x11vnc 明确未开 VNC clipboard；KasmVNC 原生 clipboard 未做双向验证。
- 真实 provider 页面、生产 runtime image、生产网络路径与 2-core target server：`NOT RUN`。

## 推荐

不执行生产替换。该 spike 对 KasmVNC 的带宽和 CPU 结果支持继续做一次隔离的 functional parity/PoC 阶段，至少补齐：

1. 中文 IME 输入与组合文本提交。
2. Clipboard 双向文本/HTML/图片能力与权限边界。
3. 真实 provider 页面、长连接、idle timeout 和 600s 生命周期。
4. 2-core no-GPU target server 上的独立复测。
5. 更严格的主观/客观文本清晰度验收。

在以上完成前，decision gate 不能判定为 PASS；当前结论为 `CONDITIONAL_GO for follow-up spike`、`NO-GO for production replacement`。