# Web Workspace cherry-pick 来源清单（2026-10-14）

按源提交的时间顺序 cherry-pick，使用 `-x` 保存来源 SHA。共 53 个专属提交；没有 cherry-pick upstream merge、首页、性能监控、Fork Release 或 Responses 提交。混合 merge 中的工作区 session-only PAT 规则另按 hunk 提取。

| 来源 SHA | 原始提交主题 |
| --- | --- |
| `32a1e9190e5cdd5a2167f741ca2764e5be9e8a80` | docs: initialize web workspace architecture |
| `febbd5196d54660f997924de684b19a27d76df52` | feat(web-workspace): phase 1 core models, entitlement and ownership |
| `c3f3e5495b25934b2d02a4031b37e33660b3befd` | feat(web-workspace): phase 2 browser agent and stream broker |
| `6acb92625c91e6353f7c0d4d35f5f9bcf8a0383b` | feat(web-workspace): phase 3 network and browser guard |
| `2d2bdbe996367624b640698c05d73769df98f6f1` | feat(web-workspace): phase 4 chatgpt provider adapter |
| `adf7739d44609777983994c1624b8b3ae87fa86f` | feat(web-workspace): phase 5 frontend |
| `12511ef0f01e6fd026a7ec9041e2aef2816c324b` | feat(web-workspace): phase 6 security verification |
| `f5106ec7509673100d7e5377d034830686b034dc` | feat(web-workspace): start the runtime as the provider app window |
| `584e256f95ff370fa05a5fd5f1d203031e7f4651` | feat(web-workspace): workspace-first layout with provider presentation crop |
| `8934bfcb534daef7515377d74322f6396fa3ba1f` | fix(web-workspace): scale the remote framebuffer onto the presentation stage |
| `dd4bc5a18edf5d6c73ed33d8ef3355a883bcf7a4` | feat(web-workspace): show the real no-project empty state in the viewport |
| `5dd300cfab44119ee6ea5e23b18be0428a78b9b7` | feat(web-workspace): real remote navigation channel |
| `944b442a999a8ab5993fe86edf325b29af2cec18` | feat(web-workspace): expose the runtime mode and lock after sign-in |
| `e5520a48a376e7e2ddbccbb42092270f5b9be70d` | feat(web-workspace): size the remote screen to the workspace frame |
| `87de0bffce2081e9e1e8e3f09d052acfc886d991` | feat(web-workspace): account for the display stream and pause hidden tabs |
| `8524ae7dd3c7a4a9a6efec2026d9f0b0ee4e14df` | fix(web-workspace): keep the browser guard event queue unbounded |
| `466848df6e5cee5279b9ef3298cf652a11550b61` | revert(web-workspace): drop the unproven x11vnc update flag |
| `6d1824f1118e4aff9832c574bdecc5a67d4ba454` | feat(web-workspace): surface the remote page health and retry a failed load |
| `db5ba6797397eff4dd83632034e94e861d8879dd` | feat(web-workspace): containerize the browser agent |
| `2cab15b86cde82cbd2c9d904ff3b4e39fecd3689` | fix(web-workspace): retry a blank remote page before reporting ready |
| `aae81ef1d06c334fa81d3eb504f90e3ef5d0e90b` | feat(web-workspace): create projects from the workspace without the provider sidebar |
| `f28ae524ea69fef1c563b126e725f7b410dd76a7` | fix(web-workspace): persist the Chromium cookie profile across restarts |
| `9c28362d44d448584f74090f46b4c6c1c6632b01` | fix(web-workspace): harden provider guard and project operations |
| `9931efa6e147df3d707b3d50fe768cc30b2bdfa4` | feat(web-workspace): expose project lifecycle in the browser agent |
| `128ea7c81b46953a776a5bb13628e22192aab83c` | feat(web-workspace): support project lifecycle in the control plane |
| `9cb566b01d0bed1030b918fb241cbcbcf18161b6` | feat(web-workspace): manage project lifecycle in the workspace UI |
| `103c71b7209e1789750b48190258d6fe52c8af8f` | fix(web-workspace): include CJK fonts in the runtime image |
| `82a6a1c7e452447672833d11d5fc7dadd048f438` | perf(web-workspace): reduce two-core stream cost |
| `8e64cf25ed2c38d58254624f2ad7b6f35e416ae3` | fix(web-workspace): crop provider top chrome and remove manual fallback |
| `694ca038b3b4605f66745e2397e70e3b6ffcd63f` | fix(web-workspace): remove renderer process limit |
| `cce73feb2db605f707b6dd5e239487be8005ac10` | feat(web-workspace): guard the last registered project |
| `f31e733f1adecced67cdc199d3c3d3e5a4c95343` | feat(web-workspace): add validated project lifecycle UI |
| `cb4b3bbeaaef7383bded61b08270bac1e761b5b2` | feat(web-workspace): dock project tabs in the workspace header |
| `ffc52c5e1a49adb5e04b67eed6c12e13fc6855cc` | feat(web-workspace): add browser agent bridges and KasmVNC runtime |
| `0e851a0e19e241cfc4bc6ac98a667535ee242748` | feat(web-workspace): expose bridges and KasmVNC in the control plane |
| `0b6c08d264cb02a3aead5f67d42554f5914d9c2a` | feat(web-workspace): wire bridges and KasmVNC into the web client |
| `a65edd21e12f1ac0046ff0022ecdf47a503e287e` | fix(web-workspace): auto-start an idle-stopped session |
| `e03cab26ed9202e4c8d9193881c6da2c62ccee4d` | test(web-workspace): cover bounded auto-start behavior |
| `c64b3a97693e558938719ece2fa9031c9ff7bc91` | fix(web-workspace): bridge clipboard shortcuts inside KasmVNC |
| `9c99e1f3312d86f6dd375e50ce3c96852718b445` | test: isolate vitest files for stable web workspace runs |
| `734cee238771ff471b50af6e093b69dcb324226f` | fix(web-workspace): force-disable KasmVNC clipboard |
| `498727e1e89fbc4104f1579cc7e93ea91e78e7bd` | test(web-workspace): lock KasmVNC client URL contract |
| `df4f605d6c3a7140471709a1e160d3270b592394` | test(web-workspace): pin last-project read in page test |
| `5e5a237ce5c853089b4092d84b4ad6d2af510a46` | fix(web-workspace): treat an empty remote clipboard as a no-op |
| `04fdde6fdf8e3097ba7c3f968ee57614fb2ff87c` | fix(web-workspace): classify file chooser failures honestly |
| `511a408290828c9041401612a6e8b28d1c99369e` | fix(web-workspace): accept dotted file names in the chooser bridge |
| `2a059661b53358e6692fc11e726a6ba2b8974dec` | fix(web-workspace): keep the stream attached through a reconnect |
| `400d6e9392a7a346d9cec5f34f51f075f79a7acf` | fix(web-workspace): label the input toggle with the active mode |
| `e2569c9fe248c04da6ac4270640ab3f222ffedb1` | fix(web-workspace): keep local input focused across an iframe reload |
| `461cd9bf88b8a14064074c439bc6a23fb549d63d` | perf(web-workspace): switch runtime to native KasmVNC websocket |
| `cf0254135c19a2f789f6ccd04e92300415ac143b` | fix(web-workspace): align API and UI with native KasmVNC transport |
| `b7ddfc67bcc56fbe37973290ee8f0dfc8dd23aac` | fix(web-workspace): match the provider shell in both locales |
| `6fe7abeb398230a071061b77d899c1bd53b35724` | fix(web-workspace): click the creation entry only where it is hittable |
