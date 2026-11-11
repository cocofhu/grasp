# 2026-11-11（状态栏「执行中」不再统计空闲 Solo）

- 日期：2026-11-11
- 范围：`server/internal/services/platform_status.go`、`server/internal/services/platform_status_test.go`、`CHANGELOG.md`
- 做了什么：`PlatformStatus` 的执行中计数里，Solo 只统计「启动中」以及「运行中且对话队列有活动回合（`chat_queues.active_turn_id` 非空）」的会话；环境仍开着但空闲的 Solo 不再计入。
- 为什么：Solo 的 `running` 只表示沙箱环境存活。线上状态栏显示 5 个执行中，实际只有 1 个工作流在跑，另外 4 个是空闲十多个小时、环境未停止的 Solo 会话。会话列表的 `busy` 字段同样来自 `chat_queues.active_turn_id`，两处口径现在一致。
- 如何验证：更新 `TestPlatformStatus_activeSoloCountsStayFresh`，覆盖空闲 running、忙碌 running、已关闭会话残留的活动回合以及其他 surface 的同名队列；`go test ./internal/services/ ./internal/handlers/`、`go vet`、`golangci-lint` 通过。
