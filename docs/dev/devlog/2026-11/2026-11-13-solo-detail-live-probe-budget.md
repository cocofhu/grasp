# 2026-11-13（Solo 详情不再被高负载沙箱的健康检查卡住）

- 日期：2026-11-13
- 范围：`server/internal/services/solo.go`、`sandbox-gateway/gateway/internal/driver/e2b/driver.go` 及对应测试
- 做了什么：
  - `SoloService.view(live=true)` 的全部沙箱实时探测（`liveReasoningEfforts`、`GetView`）共用 3 秒预算 `soloLiveViewBudget`；超时后返回持久化的会话和沙箱状态，`containerStatus` 为 `unknown`，并取消发往网关的请求。
  - E2B `Driver.refresh` 对已 running 且所有隧道已发布的沙箱不再进入 `convergeResumed` / `waitHealthy`，改为一次最长 2 秒的 relay 探测 `observe`：只校验 relay 可达、身份和 bootstrap 退出码，不等待 SSH、会话 backend、IDE 就绪，也不重试。尚未发布隧道（恢复、网关重启后）的路径仍走完整就绪等待。
- 为什么：#1223。沙箱内存耗尽时 IDE/relay 超时，详情接口经 `GetView → Status → refresh → waitHealthy` 持有 entry 锁一直等待（Grasp 侧网关客户端上限 60 秒），前端在详情返回前不建立会话界面，历史可读但页面持续转圈。长时间持锁还会让维护循环 `TryLock` 失败，跳过该沙箱的租约续期。
- 如何验证：新增 `TestSoloGetReturnsSavedSessionWhenLiveProbesHang`（去掉预算时等待 60 秒失败）、`TestDriverStatusOfPublishedGuestDoesNotWaitForServices`（旧逻辑等到 context 超时失败）、`TestDriverStatusOfHangingRelayFailsWithinObserveBudget`（旧逻辑无限等待）。`server` 的 `go test ./...`、`go vet`，网关 e2b 包测试、`cover-check.sh gateway 90`（91.3%），两个模块 golangci-lint v2.12 均通过。未在真实 E2B 高负载环境复现。
- 未覆盖：entry 锁的等待本身仍不可取消；前端仍等详情返回才建立页面（现在最多约 3 秒），也还没有「状态未知 / 重试」的专门展示。
