# 2026-11-18（Solo 斜杠命令）

- 日期：2026-11-18
- 范围：`server/internal/models/`、`server/internal/services/`、`web/src/`、`web/e2e/`、`CHANGELOG.md`
- 做了什么：Solo 输入 `/` 时可选 `/new`、`/compact` 和当前 Agent 可用的 skill。`/new chat` 等同于 `/new`。两条上下文命令随消息队列持久化，在下一条消息时重开当前沙箱内的 Agent 会话。`/new` 为历史回放和 `get_history` 划定新的上下文范围。`/compact` 带入有长度上限的关键要求和近期记录。`/skill <名称> [任务]` 使用 Agent 或共享 Agent 注入的 `SKILL.md`。
- 为什么：会话重开、历史回放和技能注入由平台统一提供，键盘和菜单入口可在不同编码工具上通用。
- 如何验证：集成测试通过模拟 ACP 桥接验证队列顺序、原沙箱复用、压缩、恢复后的历史隔离和 skill 指令。组件测试覆盖发送和系统消息回显，Chromium 在窄屏视口验证键盘和鼠标选择。
