# 2026-11-09（离线升级安全引用 MySQL 字段名）

- 日期：2026-11-09
- 范围：`server/internal/filestore/migration.go`、`server/internal/filestore/migration_identifiers_test.go`、`server/internal/payloadstore/references.go`、`server/internal/payloadstore/mysql_identifiers_live_test.go`
- 做了什么：离线正文规范化和引用闭包扫描通过 GORM 方言引用模型字段名，排序和逐行游标使用结构化列条件。保留原始列名、扫描顺序、引用摘要以及 SQLite 原存储类型的比较并交换约束。新增保留字正文列、主键跨页和真实 MySQL 引用闭包回归；小型 `StateRun.Usage` JSON 元数据继续内联保存。
- 为什么：用真实 `v3.2.0` 模型生成的旧库在 MySQL 8.4 上升级时，闭包扫描未引用的 `usage` 列触发语法错误，导致规范化无法完成。字段来自模型 schema，但仍须按数据库方言引用；不能靠改变正文格式或忽略该列绕过校验。
- 如何验证：真实 MySQL 8.4.13 的 48 表旧库完成离线升级、原始 SQL 备份、同工作目录恢复、显式执行控制初始化及原收据重试。独立 S3 回读验证原正文、图片、附件、6000 条历史事件和 58 个正文对象、1 个附件的引用闭包；保留旧来源与 4 条隔离记录，未重放 Agent，模型请求为 0。MySQL 保留字回归单独在空数据库下通过 `-race`；`storage-admin`、`filestore`、`payloadstore` 包 `-race -count=3`、相关包 `go vet` 和 `golangci-lint` 通过。SQLite 回归覆盖两页保留字主键及原有 BLOB 类型 CAS。上述为隔离升级验证，不涉及实际部署或版本发布。
