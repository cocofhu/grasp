# 2026-11-24（E2B 启动失败时在错误里带上启动日志末尾）

- 日期：2026-11-24
- 范围：`sandbox-gateway/gateway/internal/driver/e2b/driver.go`、`driver_test.go`、`observe_test.go`
- 做了什么：E2B 驱动发现 guest 的 `bootstrap.exit` 非 0 时（`waitHealthy` 与 `observe`），经 relay `/_bridge/logs` 读取 `bootstrap.log`，脱敏后把末尾 20 行附加到 `E2B bootstrap exited with code N` 之后；读不到或日志为空时保持原错误。读取与脱敏抽成 `guestLog`，`Logs` 复用。脱敏只替换长度 ≥ 8 的值。
- 为什么：创建失败后驱动会立即清理 guest，日志随之消失，Solo 错误卡片只剩退出码，看不出是 `git clone` 权限不足等具体原因。原脱敏会把 `PREVIEW_DIRECT=1`、`SKIP_INNER_DOCKER=0` 这类短值也当密钥替换，日志里的每个 `1`/`0` 都变成 `[redacted]`，带进错误后无法阅读。
- 取舍：日志读取最多 3 秒且不脱离调用方的 context。`observe` 在 entry 锁内运行，预算仍受 `observeTimeout` 约束，不会拖住状态读取。长度 < 8 的环境变量值不再脱敏；驱动生成的密钥和 Grasp 下发的令牌、密码都远长于此。
- 如何验证：新增用例覆盖日志末尾带入且密码被脱敏、`1` 不被替换；日志为空时错误不变；已发布 guest 报告启动失败且日志接口挂起时，`Status` 仍在 `observeTimeout` 内返回。`golangci-lint`、`go vet`、`scripts/cover-check.sh gateway 90`（90.2%）通过。
