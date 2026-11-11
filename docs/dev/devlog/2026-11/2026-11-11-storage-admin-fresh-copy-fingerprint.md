# 2026-11-11 修复全新副本复制自拦截并收窄离线维护身份指纹

- 日期：2026-11-11
- 范围：`server/cmd/storage-admin/`、`server/internal/executionlineage/{authority,observation}.go`。
- 做了什么：`checkExecutionRelease` 改为接收调用方已加载的副本检查点，不再二次调用 `prepareCheckpoint`；全新 `copy`（不带 `--resume`）不再因自己刚写入的 `checkpoint.json` 报「replica checkpoint exists; use --resume」并残留维护锁。执行初始化与恢复意图的配置指纹改为只覆盖 SQL 身份（驱动、网络地址、库名或 SQLite 绝对路径）和业务存储物理位置指纹，不再包含 MySQL DSN 密码、S3 凭据和 `max_file_bytes` 等可调项。`ObserveSQL` 改为单个无行锁快照读取，诊断不再与运行时准入或离线维护争用 `FOR SHARE` 锁；运行时守卫仍保留原锁。
- 为什么：原实现会让每次全新副本复制必然失败且需要手工 `--resume` 或释放；凭据轮换会把进行中的恢复误判为另一配置并卡住，私有维护目录也不应保存密钥摘要；只读诊断与维护互相阻塞没有必要。
- 如何验证：新增 `TestFreshCopyWithoutResumeCompletesAndReleasesMaintenance`（内存 S3 线协议，覆盖未初始化与已初始化两种安装；临时恢复旧调用时稳定复现原错误）和 `TestExecutionConfigurationFingerprintTracksIdentityNotCredentials`。服务器 `go test ./...`、`go vet ./...`、golangci-lint v2.12、配置文档生成校验通过；核心覆盖率 90.2%（门槛 90%）。
