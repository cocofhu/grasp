# 2026-11-12（Solo 模型切换：提取共用回滚流程）

- 日期：2026-11-12
- 范围：`server/internal/services/sandbox_solo_route.go`、`server/internal/services/sandbox_solo_route_test.go`。
- 做了什么：#1206 第 4 项。`AlignSoloModelRoute` 中「切换失败」和「保存切换状态失败」两段重复的回滚（无回滚时失效指纹并释放目标配置；否则脱离调用方取消、3 分钟内重放旧配置、记录端点、释放目标配置，回滚失败再失效指纹）合并为函数内的 `recoverFrom` 闭包（直接复用已确定的目标/旧配置与 `apply`，不再额外定义多参数方法）；两类失败的用户文案由 `soloRouteSwitchFailed` / `soloRouteSaveFailed` 各自保留，原文不变。保存切换结果的事务提取为 `saveSoloRoute`。
- 为什么：两段流程逐行重复，后续修改容易只改一处。纯结构调整，无用户可见行为变化，不改 CHANGELOG。
- 如何验证：先在旧实现上新增 `TestSoloRouteFailureStagesKeepTheirOutcome` 并通过，覆盖此前没有测试的保存失败已恢复、保存失败回滚也失败、无回滚的切换失败、无回滚的保存失败四条路径（文案、调用次数、目标配置释放、指纹保留或失效、回滚失败时不保留 ACP 客户端、切换锁释放）；重构后同一测试与原有 `TestSoloRoute*` 不变通过，`-race -count=3` 通过。`server/` 下 golangci-lint、`go vet ./...`、gen-configdoc 检查、`go test ./...`、`cover-check-server.sh 90`（90.4%）通过。
- 未做：本地未跑完整 Agent E2E（`ci-agent-e2e`），由 CI 执行。两阶段文案措辞不一致（「也失败」/「失败」、「请重新选择模型重试」/「请重试」）属用户可见变化，按原文保留，另在 #1206 跟进。#1206 其余各项另起 PR。
