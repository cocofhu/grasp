# 2026-11-13（修改账号密码后已登录会话随即失效）

- 日期：2026-11-13
- 范围：`server/internal/models/models.go`、`server/internal/auth/service.go`、`server/internal/auth/export_test.go`、`server/internal/auth/http_test.go`
- 做了什么：`Session` 新增 `PasswordFingerprint`（登录时密码哈希的 SHA-256，不出现在 JSON 中），`ValidateSession` 用当前配置的密码哈希比对，不一致就删除会话并拒绝。升级前创建的会话没有指纹，一律失效。
- 为什么：此前修改 `auth.users` 里的 `password_hash` 后，用旧密码登录的会话仍可用到原定到期时间，改密码挡不住已经登录的人。
- 如何验证：新增 `TestPasswordChangeEndsSessions`，覆盖改密码后旧会话被拒、改回原密码也不恢复、新密码登录可用、其他账号不受影响、无指纹的旧会话被拒；不改 `service.go` 时该测试失败。另用旧表结构的 SQLite 和 MySQL 8.4 库验证 AutoMigrate 加列、旧行被拒、重复迁移不报错。server 全量测试、vet、lint、配置文档检查和覆盖率门槛通过。
