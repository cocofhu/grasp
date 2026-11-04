# 2026-11-05（设备码登录上游断连错误分类）

- 日期：2026-11-05
- 范围：`server/internal/codexoauth/client{,_test}.go`、`server/internal/handlers/codex_device{,_test}.go`、`CHANGELOG.md`。
- 做了什么：认证客户端为请求失败和响应读取失败统一添加 `ErrTransport` 标记，保留底层错误原因并继续去掉请求 URL。设备码路由按该标记返回 502／`upstream_error`；续期仍把这些错误当作可重试故障，设备码轮询仍保留待登录会话。新增直接断开连接和截断响应的 HTTP 路由回归，以及 EOF、取消、响应截断的客户端分类和令牌不外露检查。
- 为什么：PR #1108 的 server CI 在服务关闭后复用连接时得到 EOF；EOF 不实现 `net.Error`，旧代码误报内部错误 500，而连接被拒绝时返回 502，导致错误行为和测试依赖断连时机。
- 如何验证：Go 1.26.10 下服务端 golangci-lint v2.12、go vet、配置文档检查、全量单测和核心覆盖率门禁通过（90.6%，门槛 90%）；设备码路由断连回归重复 50 次通过，客户端和设备码路由竞态回归重复 10 次通过。actionlint、错误级 shellcheck、文档构建和官方 npm registry 审计通过。不使用真实账号或供应商服务。
