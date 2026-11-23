# 2026-11-23（修复 Cursor 重开 Agent 后的 active run 卡死）

- 日期：2026-11-23
- 范围：`sandbox-gateway/sandbox/internal/service/bridge_connect.go`、`sandbox-gateway/sandbox/internal/service/bridge_lifecycle_test.go`、`sandbox-gateway/sandbox/internal/provider/cursorsdk/bridge.mjs`、`sandbox-gateway/scripts/agent-platform-wire-fixture.mjs`、`sandbox-gateway/scripts/agent-platform-wire-fixture.test.mjs`、`CHANGELOG.md`
- 做了什么：`Bridge.connect` 重开 Agent 时先在 `b.mu` 外关闭旧会话，再取消其进程 context；Cursor 桥接发送遇到 `already has active run` 时以 `local.force` 重发一次。Go 生命周期测试覆盖关闭顺序与关闭期事件不死锁，Cursor 桥接 fixture 模拟 SDK 的活动运行锁。
- 为什么：旧顺序先 `agentCancel()`，`exec.CommandContext` 立即 SIGKILL Cursor 进程组，`bridge.mjs` 来不及取消运行和 `await agent.close()`（4.0.0 的修复因此在重开路径上不生效）。Cursor SDK 把 `activeRunId` 存在本地 SQLite，未结束的运行会让同一 `agentId` 的后续 `send` 一直被拒。直接对调顺序也不行：关闭期间的事件经 `Broadcast` 需要 `b.mu`，持锁关闭会卡满 3 秒宽限后仍被强杀。`local.force` 是 SDK 为崩溃残留运行提供的恢复入口；桥接进程内同时只有一个回合，发送时出现的活动运行必然来自已死的旧进程。
- 如何验证：`go test ./...`（sandbox，`-race` 覆盖 handler/service/cursorsdk）；`node --test scripts/agent-platform-wire-fixture.test.mjs`。新测试在旧代码上分别以死锁超时和 `already has active run` 失败。
