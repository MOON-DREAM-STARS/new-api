# 模型价格对比页与 models.dev 定价导入（2026-10-16）

## 目的与范围

运营者提出两项要求，本次一并在 `D:\github\api\new-api` 的分支 `feat/price-comparison-models-dev-pricing`（从 `dev` 新建）实现：

1. **价格对比页**：所有登录用户（含管理员、root）都能看。按模型家族直白展示管理员挑选的热门模型的**最低价**（性价比档位的价格），并以 `deepseek-v4.1-flash` 的价格为基准线，显示“0.40X”或“40%”，后面跟美元或人民币价格。峰价 / 谷价、币种、倍数格式、基准模型和每个家族展示哪些模型，由管理员（admin 与 root）在页面里设置；普通用户没有权限。
2. **models.dev 自动导入定价**：仿照 CC Switch，每次 Release 导入一次；开启前需确认；已有价格默认锁定；优先官方厂商；同名自动匹配加手动映射；可撤销；上级 `README.md` 写明要求和每次同步的操作。

不在范围：不改旧的“上游价格同步”工具（`controller/ratio_sync.go` 的 models.dev 预设仍按原行为只写倍率表）；不 stage / commit / push，不发布，不部署。

## 变更前实时状态

- 工作区在 `dev`；运营者原有的未提交内容（`AGENTS.md`、`2026/10/15/10-15-2026-channel-aggregation-service-tiers.md` 的修改、`2026/10/14/10-14-2026-agents-branch-architecture.md`、`browser-agent/`、`outputs/image2ui/*`）保持原样，未触碰。
- 生产的 `deepseek-v4.1-flash` 是峰谷计费表达式（高峰输入 $0.2857 / 输出 $1.1429 每百万 token，即 ¥2 / ¥8；空闲减半），站点汇率为 7。服务档位的倍率**替代**分组倍率计费（`relay/helper/price.go`）。
- 上一版（API Key 向导与模型家族）已由受控 Release 部署；本次与它互不影响。

## 精确变更

### 价格对比页

后端（Go）：
- `model/managed_option.go`：把一个 options 键当作带校验的 JSON 配置读写（`GetOptionFromDB` / `SaveOption`），供价格对比设置和 models.dev 配置使用。
- `service/price_comparison.go`、`controller/price_comparison.go`：`GET /api/price_comparison`（登录即可）返回设置、汇率、基准模型价格、按管理员挑选顺序排列且**当前用户可用**的模型及其适用倍率——有适用的聚合策略时取最便宜的已提供档位倍率，否则取用户可用分组里的最低分组倍率（倍率 ≤ 0 的分组视为没有价格，用标价）。不下发分组名、渠道。`GET / PUT /api/price_comparison/settings` 仅管理员；保存时校验枚举、家族存在、模型属于该家族、去重和每家族上限 10，写审计。
- 路由、令牌规则（页面 GET 任意令牌；设置读 `model:read`、写 `model:write`）、审计模板。

前端（`web/src/features/price-comparison/`，全部新增）：
- `lib/prices.ts`：读取标价——倍率模式 `输入 = model_ratio × 2`、`输出 = 输入 × completion_ratio`；表达式去掉请求规则后，有时间分支时每个分支取第一档，混合价最高的为峰价、最低的为谷价，否则取第一档；按次计费、`fixed()`、免费或无法识别的表达式显示“无法对比”。混合价 = (3 × 输入 + 输出) / 4；倍数 = 模型混合价 ÷ 基准混合价（基准取标价，不打任何档位或分组折扣）。
- 页面：基准卡片、按家族分组的模型卡片（档位徽章、大号倍数、混合价、输入 · 输出、峰谷模型的另一时段提示）；管理员的“页面设置”对话框；空状态区分普通用户与管理员。路由 `/price-comparison`，侧边栏“常规”组，侧栏模块开关与默认配置同步更新。

### models.dev 定价导入

后端（Go）：
- `service/models_dev_catalog.go`：下载并展开 `https://models.dev/api.json`（20 秒超时、32 MB 上限、内存缓存 1 小时；`MODELS_DEV_API_URL` 仅供测试或镜像）。只保留有按 token 价格的文本模型；跳过已弃用、非文本（音频 / 嵌入 / 图像 / 审核 / 实时 / 转写 / TTS / 视频）和输入输出都为 0 的条目；ID 规范化沿用 CC Switch（取最后一个 `/` 之后、去掉 `:变体`、`@` 换 `-`、转小写、去掉 `[1m]`）；常用模型按家族规则各取官方供应商最新 6 个；官方判定包含“第一方供应商转售别家厂商的模型不算官方”。
- `service/models_dev_expression.go`：把价格写成自带真实美元单价的计费表达式 `tier("base", p * 输入 + c * 输出 [+ cr * 缓存读] [+ cc * 缓存写])`；名称含 `claude` 时加 `cc1h`（缓存写 × 1.6）；有上下文分档时生成 `len <= 上限 ? … : …` 链，各分支补齐缓存项；不写 1.25 倍兜底；免费条目拒绝。
- `service/models_dev_pricing.go`：配置（`ModelsDevPricingConfig`）、状态（`ModelsDevPricingState`）、计划（预览）、导入、撤销、概览、目录分页、单条表达式。写入走 `model.UpdateModelPricing`（版本校验、表达式冒烟测试、事务）。锁定原因：`explicit`（手动锁定）、`modified`（导入后被改）、`configured`（原本就有价格）、`default`（只是内置默认倍率）。需人工确认（`Review`）：`reseller`（非官方供应商的价格）和 `large_change`（与上次导入相比涨跌超过 10 倍）；自动同步一律跳过。预览带数据哈希，数据或价格变了会拒绝并要求重新预览；同一时刻只有一个导入在跑。
- 系统任务 `models_dev_pricing`（`controller/system_task_handlers.go`、`model/system_task.go`）：开关已开且当前版本尚未导入时每小时检查，导入一次；失败每个版本最多重试 6 次；撤销会把当前版本记为已处理。
- `controller/models_dev_pricing.go`：7 个接口 `/api/option/models_dev/{state,config,catalog,expression,preview,sync,undo}`（仅 root；PAT 读 `option:read`、写 `option:write`），审计 `models_dev.config_update` / `models_dev.sync` / `models_dev.undo`。`GET /api/option/` 不再返回 models.dev 的状态键（含可撤销批次）。

前端（`web/src/features/system-settings/models/models-dev-sync/`，全部新增）：
- “models.dev 同步”页签（系统设置 → 计费 → 模型定价）：自动同步开关（开启前弹确认，并显示下一次将写入的新价格 / 更新 / 锁定 / 待确认数量）、上次导入状态与错误、本站同名 / 常用模型开关、“选择模型”对话框（搜索、供应商、仅热门、仅官方、全选 / 清除当前列表）、“立即导入”预览对话框（变更 / 已锁定 / 已是最新 / 问题四个筛选，锁定原因，待确认条目默认不勾选并需勾选才导入，逐个“允许导入 / 不再导入”，为未匹配的本站模型“映射”到 models.dev 条目，刷新数据）、“更多操作 → 撤销上次导入”。
- 编辑模型价格的表达式页签新增“从 models.dev 导入”，只把表达式填进草稿，仍走原来的保存与版本校验。
- 导入或撤销后，若模型价格编辑器没有未保存的修改，则重新加载价格；有未保存修改时只提示。
- 使用日志的审计文案、系统任务名称、7 种语言共 119 个词条（按原顺序追加在文件末尾）。

## 行为规则（服务端强制）

- 已有价格的模型默认锁定，导入不会覆盖；只有“由导入写入、之后没人改过”的价格会被后续导入更新。显式解锁后才可覆盖。
- 同一模型多家报价：官方厂商优先；没有官方报价才取最便宜的转售商，并且需要人工确认。
- 同一个模型名被多处命中：映射 > 手选 > 本站同名 > 常用。
- 价格对比页只显示当前用户能用的模型；基准始终以标价比较。
- 只写 options 表，不新增表和列。

## 端到端中发现并修复的问题

用本机一次性实例走完整流程（Docker、临时 SQLite、版本号 `v-e2e-1` / `v-e2e-2`；models.dev 数据是一份只读下载的真实快照，由本机 fixture 服务提供，应用通过 `MODELS_DEV_API_URL` 读取）时发现并修复：

1. **中文界面下预览对话框崩溃**：界面语言标签是 `zhCN`，`Intl` 抛 `RangeError: Invalid language tag`。金额格式化改为先经 `toIntlLocale` 转换；加了中文界面的回归测试。
2. **LongCat 官方报价被判成转售商**：models.dev 的供应商 ID 是 `longcat`，规范厂商命名空间是 `meituan`；补上供应商映射，后端测试覆盖。
3. **目录搜索把转售商排在官方前面**：现在多个词同时匹配、点和横线视为相同，按“完全匹配 > 前缀 > 官方 > 常用 > 最新”排序；后端测试覆盖。
4. **新版本的首次导入被 1 小时间隔挡住**：系统任务调度器只在“距上次任务 ≥ 间隔”时才新建任务，新版本若在上次导入后一小时内启动，要多等到一小时后才导入。现在 `Interval()` 对“这个版本还没尝试过”返回 0（`service.ModelsDevAutoSyncFirstTry`），失败重试仍按每小时。同时 `RunModelsDevAutoSync` 在这个版本已经导入过时直接返回，避免多个节点的选项缓存滞后时重复导入、覆盖上一次的摘要。控制器测试对这两点都加了断言。README 和开启确认文案同步改为“开启后不久 / 下一次调度（通常半分钟内）”。
5. **手机宽度下模型定价页签被标题栏裁掉**：标题栏会截断内容，5 个页签只露出 3 个（原来 4 个页签时第 4 个也已被裁）。宽度小于 640px 时，页签改放在内容上方并可横向滚动；桌面布局不变。
6. **预览行补“转售商”徽章**：非官方供应商的条目在来源旁标出，与“官方”徽章对应。

## 验证（命令与结果）

后端（Docker `golang:1.26`，`GOPROXY=off`，只在显式目录运行，不在仓库根目录 `go build ./...`）：
- `gofmt -l`（本次改动和新增的 Go 文件）：无输出。
- `go test -p 1 -count=1 ./controller/... ./service/... ./model/... ./router/... ./middleware/...`：全部通过——controller 281.6 秒、service 2.0 秒、service/authz 0.2 秒、model 13.2 秒、router 3.0 秒、middleware 0.8 秒（`service/passkey` 没有测试文件）。
- 这一轮开始后又加了“这个版本已导入就直接返回”的守卫和对应断言，所以用最终代码重跑了受影响的测试：`go test -count=1 ./service/... ./model/ ./controller/ -run 'TestModelsDevPricing|TestPriceComparison|TestAccessTokenRouteRules|…'` 通过（controller 25.9 秒）；`gofmt -l` 对 controller、service、model、middleware、router 无输出。

前端（`web/`，直接用 Node 调各工具）：
- `tsgo -b`：退出码 0，无输出。
- `oxlint -c .oxlintrc.json`（本次改动和新增的文件）：0 警告、0 错误。按整个 `system-settings/models` 目录检查会报出 `utils.ts`、`upstream-ratio-sync-helpers.ts`、`upstream-price-cells.tsx` 三个原有文件的问题，本次没有改它们。
- `oxfmt --check`（32 个改动或新增的 ts / tsx，保留许可证头）：全部符合。整个仓库的 `format:check` 有约 24 个与本次无关的原有失败，没有动。
- `vitest run` 全量：201 个文件、2400 个测试全部通过（157 秒）；其中 models.dev 同步 22 个测试。
- `rsbuild build`：退出码 0（含重新生成的 `routeTree.gen.ts`）。
- i18n：新增和改动的词条在 7 个语言文件里齐全，缺失 0。

本机端到端（一次性 Docker 实例，localhost 测试账号，内置浏览器；不碰生产）：
- **价格对比页**：基准 `deepseek-v4.1-flash` 峰价输入 ¥2.09、输出 ¥8.34、混合 ¥3.65（谷价 ¥1.83）；`claude-haiku-4-5` 4.00X ¥14.60、`claude-sonnet-4-5` 12X ¥43.80（混合价 (3×3+15)/4 = $6，对 $0.50）、`gpt-5` 6.88X ¥25.09、`qwen3-coder` 0.45X ¥1.62、`glm-5` 3.10X ¥11.32，与手算一致。管理员在“页面设置”对话框里添加了模型（其余家族用管理接口一次性配置并保存）；普通用户能看到同样的页面，没有“页面设置”按钮，设置读写和 models.dev 的状态、同步接口都返回 403，侧边栏“价格对比”在“服务档位”之后。375px 宽度单列、无横向溢出；深色和浅色都看过。
- **models.dev 页签**：开启开关弹确认（显示将写入 55 个新价格、0 个更新、3 个保持锁定、1 个等待确认）；取消后保持关闭，确认后开启，调度器在半分钟内为 `v-e2e-1` 自动导入（触发方式 `release`，写入 54 个，1 个转售商价格因待确认被跳过）。预览对话框里转售商条目带“转售商”徽章和“勾选后才会导入”提示，勾选后导入成功；把本站的 `my-private-model` 映射到 `anthropic/claude-haiku-4-5` 后导入，写出 `tier("base", p * 1 + c * 5 + cr * 0.1 + cc * 1.25)`（名称不含 claude，不加 `cc1h`）；手工设置的 DeepSeek 峰谷表达式全程没被改动。
- **下一个 Release（`v-e2e-2`，同一数据卷）**：先把 fixture 改成 Sonnet 输入价 3 → 2.5、Haiku 输入价 1 → 40、新增一个热门模型 `glm-5.4-flash`，并手工改了 `glm-5` 的价格。重启后第一次调度就自动导入（触发 `release`）：新增 1、更新 1、未变 51、锁定 4、待确认跳过 2。Sonnet 更新为 `p * 2.5`；Haiku 和映射到它的 `my-private-model` 因涨幅 15.6 倍被跳过；`glm-5` 因导入后被改过而锁定（原因 `modified`）；`glm-5.4-flash` 新增。随后在界面里“撤销上次导入”：恢复 2 个模型（Sonnet 回到 `p * 3`，`glm-5.4-flash` 价格移除），手改的 `glm-5` 保持不变，本版本不会再次自动导入。
- **手机宽度（390px）**：模型定价页签放在内容上方，可横向滚动到第 5 个页签；1100px 宽度时页签仍在标题栏。

## NOT RUN（没有做，不要当作已验证）

- **生产**：没有部署，没有在生产后台开启自动同步，没有在生产导入任何价格。生产状态仍是 `NOT RUN`，要等受控 Release 部署之后。
- **MySQL / PostgreSQL 迁移矩阵**：本次只写 options 表，不新增表和列，没有跑。
- **应用在线拉取 models.dev**：端到端里应用读的是本机 fixture（真实快照的副本和改过价格的副本）；只在开发机上一次性只读下载过快照用来核对解析，应用自己的在线拉取没有在端到端里跑。
- **“性价比档”徽章**：端到端实例没有配置聚合策略，页面上只看到“标准价格”徽章；档位倍率与分组倍率的取值由 `controller/price_comparison_test.go` 覆盖，没有在浏览器里目测。
- **多节点租约去重**：沿用系统任务的既有机制，没有起多个实例实测。
- **真机和其他浏览器**：只用内置浏览器（Chromium）模拟 375–390px，没有真机、Safari、Firefox。
- **其他语言界面**：只检查了词条齐全（缺失 0）和中文界面实测，英文 / 繁体 / 法 / 日 / 俄 / 越南语界面没有逐个目测。

## 上线后要做

1. 受控 Release 部署后（由 Codex 或运营者执行），管理员在价格对比页的“页面设置”里为各家族挑选要展示的模型。
2. root 在“系统设置 → 计费 → 模型定价 → models.dev 同步”先点“立即导入”看预览（DeepSeek 峰谷、GPT 272k 应保持锁定），确认后再打开“每个新版本导入一次”。
3. 想交给 models.dev 维护的已有价格模型，在预览里逐个“允许导入”。
4. 之后每次 Release 按上级 `README.md` 的“每次 Release 的模型定价同步”做，并把结果写进当次发布记录。

## 已知不足

- models.dev 没有 DeepSeek 的人民币峰谷价，这类模型保持锁定，继续手工维护；音频、推理单价不导入。
- models.dev 是美元标价，人民币价格按站点汇率换算显示。
- 手机宽度下页签条的处理只做在“模型定价”这一张卡片上；设置页标题栏本身会截断内容的行为没有改（共享布局），其他页面如果也有放在标题栏里的页签条，窄屏下仍可能被裁。
