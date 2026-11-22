# 2026-11-22（状态栏执行中只统计实际工作的 Solo）

- 日期：2026-11-22
- 范围：`server/internal/services/platform_status.go`、`server/internal/services/platform_status_test.go`
- 做了什么：状态栏的执行中计数只把启动中的 Solo，或带有 Solo 活动回合的运行中会话计入；环境保持运行但没有 Agent 回合输出的 Solo 不再计入。
- 为什么：状态栏的“执行中”需要表示 Agent 正在输出，或任务仍在创建、流转，而不是把沙箱存活误当作执行。
- 如何验证：`go test ./internal/services ./internal/handlers`、`git diff --check`
