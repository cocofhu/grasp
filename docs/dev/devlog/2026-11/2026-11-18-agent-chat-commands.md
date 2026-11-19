# 2026-11-18（Agent 对话通用斜杠命令）

- 日期：2026-11-18
- 范围：`server/internal/chatcmd/`、`server/internal/engine/`、`server/internal/runtime/`、`server/internal/sandbox/`、`server/internal/services/`、`sandbox-gateway/sandbox/`、`web/src/`、`web/e2e/`
- 做了什么：在工作流澄清、复审和审批预览、Agent Studio、沙箱控制台及独立访客对话接入 `/new`、`/compact` 和 `/skill`。接入现有工作流队列，用持久化的命令消息恢复上下文范围。重开 Agent 时重新装入节点要求，并在桥接层保留可见历史。补齐 Kimi Code 的工作流注册。
- 为什么：平台通过 ACP 桥接打开新的 Agent 会话。面向长连接的适配器使用 `session/new`，一次性进程从空的恢复标识启动。技能从各 Agent 的配置目录加载，工作流继续使用当前节点的交付要求。
- 如何验证：工作流队列和模拟 ACP 集成测试覆盖节点要求、历史隔离及原沙箱复用。桥接队列测试覆盖命令排序、技能路径及保留历史。浏览器测试覆盖工作流与 Solo 输入框的菜单选择。
