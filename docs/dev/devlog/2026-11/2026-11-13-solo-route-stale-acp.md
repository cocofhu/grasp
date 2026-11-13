# 2026-11-13（Solo 模型切换：失败后不再保留已关闭的 ACP 客户端）

- 日期：2026-11-13
- 范围：`server/internal/services/sandbox_solo_route.go`、`server/internal/services/sandbox_solo_route_test.go`。
- 做了什么：`AlignSoloModelRoute` 在重放工具前关闭旧 ACP 客户端时，改为先在锁内把 `ls.acp` 置空再关闭。此前在无回滚可用的两条失败路径（切换失败、保存切换状态失败）上，`ls.acp` 仍指向已关闭的客户端；有回滚的路径会被 `restored` 覆盖，不受影响。
- 为什么：#1220 review 发现。已关闭客户端的 `IsConnected()` 为 false，`ensureConnected` 仍会重连，所以目前没有可观察的错误，但 `ls.acp` 的语义（非 nil 即为当前客户端）被破坏，后续直接使用 `ls.acp` 的代码可能踩到；同时原先在锁外读取 `ls.acp`，现与其他写入一致改在 `s.mu` 内。无用户可见变化，不改 CHANGELOG。
- 如何验证：`TestSoloRouteFailureStagesKeepTheirOutcome` 的断言从「回滚失败」扩展到所有未恢复的路径（`live.acp == nil`）；旧实现上无回滚的两个用例失败，修复后通过，`TestSoloRoute*` `-race -count=3` 通过。`server/` 下 golangci-lint、`go vet ./...`、gen-configdoc 检查、`go test ./...`、`cover-check-server.sh 90` 通过。
- 未做：本地未跑完整 Agent E2E（`ci-agent-e2e`），由 CI 执行。
