# 2026-11-11 已确认关闭的 Solo 会话不再因缺失环境记录占用名额

- 日期：2026-11-11
- 范围：`server/internal/services/sandbox_solo.go`、`server/internal/services/solo_lifecycle_test.go`、`CHANGELOG.md`
- 做了什么：`countSoloComputeReserved` 统计“会话绑定的环境记录不存在”的未知占用时，排除 `status = closed` 且 `lifecycle_action = ''` 的会话。关闭中（`close_cleanup`）、错误、启动中和运行中的缺失绑定仍计入名额。
- 为什么：线上上限设为 32，实际只有 5 个 Solo 环境运行，却提示已达上限。另外 27 个是 10-23 至 11-04 的旧会话，其中 26 个已关闭、1 个错误，环境行已被旧版本删除但仍保留 `sandbox_id`，被未知占用规则计入。完成的 Close 已经确认计算停止，与已关闭且环境行为 stopped 的会话一样不应占用计算名额。这些会话仍不能直接删除或释放，绑定保持原样，本次不处理。
- 如何验证：新增 `TestSoloCompletedCloseWithMissingEnvironmentRowHoldsNoQuota`，上限为 1 时，已关闭的缺失绑定不阻止新建；关闭中和错误状态的缺失绑定仍计 2。未修复时该测试失败。server 全量测试、golangci-lint、go vet 与覆盖率门槛见 PR。
