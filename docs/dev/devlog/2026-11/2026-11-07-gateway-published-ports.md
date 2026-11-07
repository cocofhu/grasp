# 2026-11-07 网关连接保留动态发布端口

- 日期：2026-11-07
- 范围：`sandbox-gateway/gateway/internal/service/connection.go`、`connection_test.go`、`CHANGELOG.md`
- 做了什么：建立 TCP 通道前改用驱动的 `Status` 查询运行状态，再从当前 `Endpoints` 解析目标；增加使用真实 Kubernetes 驱动与 fake client 的动态预览端口回归。
- 为什么：Kubernetes 驱动的 `Get` 会按初始配置收敛 Service 端口。预览通过 `PublishPort` 增加端口后，连接通道调用 `Get` 会移除该端口，导致预览注册及访问失败。查询连接目标只需要读取状态和端点。
- 如何验证：新用例在修复前复现端口被移除，修复后连续建立连接仍保留 ClusterIP 与 LoadBalancer 的预览端口，且 CDP 端口不进入公网 Service；运行 gateway 的 lint、vet、覆盖率门禁与连接用例 race 重复检查，以及仓库通用门禁和 docs 构建、依赖审计。
