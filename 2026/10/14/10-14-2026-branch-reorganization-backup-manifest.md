# 分支重组 Git 备份清单（2026-10-14）

备份目录：`D:\github\api\outputs\branch-reorganization-20261014`。本清单不含凭据。

| 产物 | 用途与验证 |
| --- | --- |
| `source-refs.bundle` | 完整 Git 历史与四条迁移前分支；`git bundle verify` 通过 |
| `source-refs.txt` | 迁移前完整 SHA 与分支映射 |
| `restore-check.git/`、`restored-refs.txt` | 向新 bare repository 实际 fetch bundle，四条恢复 ref 与原清单相同：VERIFIED |
| `untracked-paths-before.txt` | 用户原有未跟踪截图与 KasmVNC 产物路径清单 |
| `cherry-pick-source-list.txt`、`cherry-pick-progress.log` | 提取来源与冲突处理过程 |
| `removed-workspace-locale-keys.json` | 活动线剥离的专属翻译键 |
| `browser-agent-filesystem-test.txt` | Linux 测试夹具修正的验证输出 |
| `final-refs.txt` | 发布与清理完成后的远端/本地引用回读（操作完成时保存） |

迁移前引用：

- `main`：`3733f0508f885f8e12996fe4f9820c14a09a86ec`
- `feature/dreamstars-homepage`：`75915ef2e9c039176412b1834210c3d9c874ea17`
- `master-main`：`9a0be8750a6d736d9692535ed2cd68f8eec46529`
- `upstream-sync`：`63cecab824bad1a421d19950004c309bb2054c09`

恢复使用新 recovery 分支，不覆盖现有分支：

```powershell
git fetch D:\github\api\outputs\branch-reorganization-20261014\source-refs.bundle refs/heads/main:refs/heads/recovery-pre-workspace-pause
```

原 `main` 历史、历史 tags、发布镜像和 VPS 回滚材料均保留；未执行 VPS 恢复或发布。
