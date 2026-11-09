# 2026-11-09（稳定验证释放任务跨租期续租）

- 日期：2026-11-09
- 范围：`sandbox-gateway/gateway/internal/service/environment_release_test.go`
- 做了什么：长释放任务回归改用 2 秒初始租期、6 秒整体期限和 2.5 秒阻塞的原生操作，每 100 毫秒续租至 1.9 秒后，每 40 毫秒检查 fence。校验至少五次续租、操作确实跨过原租期、最终租期已延长、每次响应及最终 SQL 的整体期限精确不变，并保留只派发一次和原释放收据的断言。启动和结束等待均有界。
- 为什么：原测试 100 毫秒租期只续至 90 毫秒后，在并发 CI 和 race 检查负载下可真实过期，正确的失权校验因而使测试失败。扩大测试的调度余量，同时加强期限和跨租期证据；生产续租、过期拒绝及 45 秒默认租期均未改变。
- 如何验证：目标用例 `go test ./internal/service -run '^TestEnvironmentReleaseLongOperationRenewsWithoutExtendingOverallDeadline$' -race -count=10` 通过；gateway 全包 `go vet`、`golangci-lint` 通过，既有 `cover-check.sh gateway 90` 门槛通过，覆盖率 90.4%。
