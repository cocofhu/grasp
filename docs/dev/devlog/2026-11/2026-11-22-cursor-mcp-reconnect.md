# 2026-11-22（修复 Cursor MCP 重连时的活动回合冲突）

- 日期：2026-11-22
- 范围：`sandbox-gateway/sandbox/internal/service/bridge_connect.go`、`sandbox-gateway/sandbox/internal/service/bridge_lifecycle_test.go`、`sandbox-gateway/sandbox/internal/provider/cursorsdk/bridge.mjs`、`CHANGELOG.md`
- 做了什么：Cursor 不再在 MCP 列表下发前由后台预热，关闭 SDK Agent 时等待异步释放完成，并加入 Cursor 预热回归测试。
- 为什么：后台空 MCP 会话在首次连接带上 `grasp-tool` 后需要重启并恢复同一个 Agent。旧桥接尚未释放时，Cursor SDK 会拒绝下一次发送并返回 `already has active run`。
- 如何验证：运行 sandbox Go 单测、Cursor bridge fixture 测试和 sandbox 相关静态检查，并使用指定 Grok 模型完成一次远端连接与对话验证。
