# 2026-11-13（Solo 模型切换：统一两类失败的提示文案）

- 日期：2026-11-13
- 范围：`server/internal/services/sandbox_solo_route.go`、`server/internal/services/sandbox_solo_route_test.go`、`CHANGELOG.md`。
- 做了什么：#1206 跟进（#1220 review 提出）。`soloRouteSaveFailed` 的两条文案与 `soloRouteSwitchFailed` 对齐：`rollbackFailed` 改为「保存切换状态失败，恢复原配置也失败」；`noRollback` 改为「……工作区已保留，请重新选择模型重试」。
- 为什么：两类失败的处理流程已合并，但提示措辞不一致。「也」表明切换和恢复都失败；无回滚时原配置已失效、指纹已清空，单纯重试不会恢复，需要重新选择模型触发切换，故统一为「请重新选择模型重试」。用户可见，已记 CHANGELOG。
- 如何验证：`TestSoloRouteFailureStagesKeepTheirOutcome` 的期望文案同步更新（整句匹配，旧文案会失败）；`TestSoloRoute*` `-race -count=3` 通过。`server/` 下 golangci-lint、`go vet ./...`、gen-configdoc 检查、`go test ./...`、`cover-check-server.sh 90` 通过。前端与文档无引用这些文案的地方。
- 未做：本地未跑完整 Agent E2E（`ci-agent-e2e`），由 CI 执行。
