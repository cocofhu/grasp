# 2026-11-05（E2B Review P2 修复）

- 日期：2026-11-05。
- 范围：`server/internal/mcp/preview.go`、`server/internal/mcp/preview_switch_test.go`、`sandbox-gateway/gateway/internal/driver/e2b/driver_test.go`、`CHANGELOG.md`。
- 做了什么：切换共享预览 injector 前，先为已有 injector 预览发布并探测其应用端口；只有端口可达才切换，注册更新时再次确认旧端口有独立 upstream。新增未预先发布旧端口的回归测试。修正 E2B 40ms deadline 测试，让清理断言按实际创建数量判断，避免 provider 尚未接受创建请求时误报 guest 泄漏。
- 为什么：修复 PR Review 提出的旧预览在 injector 切换后不可访问，以及慢磁盘/调度下测试的时序误报；生产默认 Docker/Kubernetes 路径和 E2B 生命周期语义保持不变。
- 如何验证：预览切换相关 server 测试 100 次通过，新增用例 race 20 次通过；server 全量 `go test ./...`、vet、golangci-lint、配置文档和核心覆盖率 90.4% 通过。gateway 全量 `go test ./...`、vet、golangci-lint、E2B deadline race 20 次和 gateway 覆盖率 91.7% 通过；E2B 包覆盖率 92.6%。
