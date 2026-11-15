# 2026-11-15（修复 TestRunScopeFollowsRunStatus 偶发失败）

- 日期：2026-11-15
- 范围：`server/internal/engine/mcp_scope_test.go`
- 做了什么：`TestRunScopeFollowsRunStatus` 在建好引擎后先 `eng.Halt()`，停止调度器认领新运行。
- 为什么：测试直接插入一条没有图的 queued 运行，再逐个改状态检查 MCP 授权。调度器在后台轮询时可能先认领这条运行，因"工作流没有可执行节点"把它标成失败，导致 `status running: authorized=false want true`。main 上 `4baa142f` 的 go-race (server-core) 就是因此变红，本地复现率约 4%。
- 如何验证：`go test ./internal/engine/ -run TestRunScopeFollowsRunStatus -count=1000` 跑两轮均无失败（修复前约 4% 失败）；`go test -race ./internal/engine/` 通过。
