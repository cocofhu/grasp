# 2026-11-15（工作流引擎：失败报告汇总改为注入窄接口）

- 日期：2026-11-15
- 范围：`server/internal/engine/engine.go`、`server/internal/engine/finish.go`、`server/cmd/server/main.go`、`server/internal/engine/run_failure_observability_test.go`。
- 做了什么：#1206 第 3 项。`persistRunErrorArtifact` 不再在运行中自行 `services.NewRunService(e.db)`，改为通过新的 `RunFailureAggregator` 窄接口（只含 `AggregateRunFailure`）获取失败汇总；启动装配处 `eng.SetRunFailureAggregator(runSvc)` 注入。未注入时与 `IssueResolver` 相同，回退到基于 `e.db` 的短生命周期 `RunService`，测试和旧装配行为不变。需求草稿生成与反馈台账已分别在 #1248、#1210 随功能删除，引擎内不再有其他运行中创建的具体业务服务。
- 为什么：引擎主流程只负责执行、等待、恢复和结束，外围业务由装配处提供，便于替换和单测。纯结构调整，`run_error.json` 写入时机（AbortRun 之前）、内容、失败时仅记日志的 fail-open 行为都不变，不改 CHANGELOG。
- 如何验证：新增 `TestRunErrorArtifactUsesInjectedAggregator`，确认 `run_error.json` 来自注入的汇总器且每次失败只调用一次；原有 `TestResearchEarlyFailureThreeChannelsNonEmpty` 等失败可观测性测试不变通过。`server/` 下 golangci-lint、`go vet ./...`、gen-configdoc 检查、`go test ./...`、`cover-check-server.sh 90` 通过。
