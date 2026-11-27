# 2026-11-27（修复沙箱详情的 PostgreSQL 查询和 SSH 焦点）

- 日期：2026-11-27
- 范围：`server/internal/services/solo_task_info.go`、`web/src/components/sandbox/SandboxIdeMenu.vue`、相关测试和 `CHANGELOG.md`
- 做了什么：PostgreSQL 使用区分大小写的模型键分组。SSH 连接信息复用 `AppModal` 的焦点管理，关闭时将焦点还给本地 IDE 选择器。
- 为什么：解决合入 PostgreSQL 支持后任务详情接口返回 500，以及键盘焦点移到弹窗后面的控件的问题。
- 如何验证：在临时合并 PR #1376 的工作树中，用 PostgreSQL 18.6 复现旧接口的分组错误并通过修复后的接口用例。SQLite 接口用例和 Chromium 的 Tab、Shift+Tab、关闭后焦点恢复检查均通过。
