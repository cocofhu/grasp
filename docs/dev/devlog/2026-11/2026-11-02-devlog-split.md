# 2026-11-02（开发日志按条拆成文件，#814）

- 日期：2026-11-02。
- 范围：`docs/dev/DEVLOG.md`（删除）、`docs/dev/devlog/`、`AGENTS.md`、`docs/agent/README.md`、`docs/README.md`、`docs/dev/workflow-chat-state.md`。
- 做了什么：原来所有 PR 都往 `DEVLOG.md` 顶部插条目，并行 PR 几乎必然冲突。现在每条记录一个文件 `docs/dev/devlog/YYYY-MM/YYYY-MM-DD-<slug>.md`，规则与模板放在 `docs/dev/devlog/README.md`。185 条历史记录用一次性脚本迁移，按标题日期归档，同一天按原文件中的先后编号（`-01` 最早），正文原样保留，`###` 标题改为一级标题。只按天或按月拆仍会在同一天/同一月的 PR 之间冲突，所以按条拆。根 `AGENTS.md` 与 `docs/agent/README.md` 的日志规则同步改为「新建自己的文件，不改别人的」。
- 为什么：减少开发日志的合并冲突。
- 如何验证：把迁移后的全部文件与原 `DEVLOG.md`「记录」一节去掉标题行和空行后排序比对，md5 一致。`docs/` 下 `npm run build` 通过，`dev/` 不进入 `public/`。
