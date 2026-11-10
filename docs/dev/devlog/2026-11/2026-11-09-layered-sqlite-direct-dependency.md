# 2026-11-09 分层服务端明确 SQLite 直接依赖

- 日期：2026-11-09
- 范围：`server/go.mod`。
- 做了什么：将已有固定版本 `github.com/mattn/go-sqlite3 v1.14.22` 从间接依赖移到直接依赖；未改变版本或校验和。
- 为什么：持久队列和迁移代码直接使用 SQLite 驱动的错误类型，分层前置版本也必须独立满足 Go module tidy 门槛，不能依赖顶层应用合并后才修正。
- 如何验证：在 storage foundation 和 sandbox platform 两个独立前缀运行 Go 1.26.10 `go mod tidy -diff`，修正后均无差异；运行各前缀的持久队列、历史迁移、文件存储相关包测试。
