# 2026-11-14（Solo 默认可读取全部产物）

- 日期：2026-11-14
- 范围：`server/internal/services/onboarding.go`、`server/internal/services/onboarding_solo_test.go`、`CHANGELOG.md`。
- 做了什么：初始化创建 Solo Agent 时，能力里加上 `reads: ["*"]`，与内置团队 Agent 一致。
- 为什么：`list_artifacts` / `read_artifact` 按 `AgentCapabilities.CanRead` 放行，只认 `Reads` 里列出的产物。此前 Solo 创建时 `Reads` 为空，读不到任何产物，需要用户到 Agent 能力页手动勾选「全部产物」。用户可见，已记 CHANGELOG。
- 如何验证：`TestOnboardingCreatesEmptySoloOutsideWorkflow` 断言 Solo 的 `Reads` 为 `["*"]`；`server/` 下 `go vet`、`go test ./...` 通过。本地未装 golangci-lint，由 CI 执行。
- 未做：初始化重入时保留已有 Solo Agent，不迁移旧数据；此前创建的 Solo 仍需手动勾选。
