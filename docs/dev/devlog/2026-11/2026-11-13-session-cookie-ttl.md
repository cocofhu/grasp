# 2026-11-13（登录 Cookie 有效期跟随 auth.session_ttl）

- 日期：2026-11-13
- 范围：`server/internal/auth/cookie.go`、`server/internal/auth/handlers.go`、`server/internal/auth/service.go`、`server/internal/auth/http_test.go`
- 做了什么：`SetSessionCookie` 增加 `expiresAt` 参数，Cookie 的 `Max-Age` 取本次 Session 剩余秒数（至少 1，避免 0 或负数把 Cookie 当成删除）；删除写死 7 天的 `CookieMaxAge` 常量（#1138）。
- 为什么：数据库 Session 按 `auth.session_ttl` 过期，Cookie 却固定 7 天。配置大于 7 天时浏览器提前丢 Cookie，用户被提前登出；配置更短时 Cookie 比 Session 活得久（服务端仍会拒绝过期 Session）。
- 如何验证：新增 `TestLoginCookieFollowsSessionTTL`（720h / 168h / 2h / 未配置默认 7 天，同时核对与响应 `expires_at` 一致）与 `TestSessionCookieMaxAgeNeverDeletes`；临时改回固定 604800 时 720h、2h 两例失败。`server/` 下 `go test ./...`、`go vet`、golangci-lint、`cover-check-server.sh 90` 通过。
