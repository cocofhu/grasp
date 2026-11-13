# 2026-11-13（移除账号后已登录会话随即失效）

- 日期：2026-11-13
- 范围：`server/internal/auth/service.go`、`server/internal/auth/http_test.go`
- 做了什么：`ValidateSession` 在过期检查之后用当前配置确认会话用户名仍是有效账号，不在配置中就删除这条会话并拒绝。API 中间件、沙箱跳转和 WebSocket 都经过这里。
- 为什么：此前只看会话记录和到期时间，账号移除后新登录被拒，但旧会话直到到期仍可访问（#1134）。
- 如何验证：新增 `TestRemovedUserSessionRejected`，覆盖三个会话入口、保留账号不受影响、复用会话库重建服务后仍拒绝；未改 `service.go` 时该测试失败（HTTP 200）。server 全量测试、vet、lint、配置文档检查和覆盖率门槛通过。
