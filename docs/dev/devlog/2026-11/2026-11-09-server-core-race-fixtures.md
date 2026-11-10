# 2026-11-09 修正 Server 竞态验收的独立数据库夹具

- 日期：2026-11-09
- 范围：`server/internal/services/sandbox_operation_test.go`、`server/internal/services/pm_coverage_more_test.go`
- 做了什么：续租故障测试的 GORM 回调保留到其独立数据库结束，不再在最后一次后台续租退出时并发修改回调注册表。PM 启动、完成及取消用例将共享内存 SQLite 的连接池限定为一个连接，与正式 SQLite 配置一致，避免状态轮询使启动租约 CAS 遇到共享缓存表锁。
- 为什么：远端 `server-core` 竞态检查分别复现了回调移除与后台 UPDATE 的数据竞态，以及测试数据库无限连接导致的 `SQLITE_LOCKED`。二者都属于测试环境和清理顺序问题，不能通过重跑原失败、增加等待时间或放松状态断言处理。
- 如何验证：两项原用例 `go test -race ... -count=20` 通过（178.053 秒）；邻近 `TestPmTurnRunner|TestSandboxOperation` 竞态测试重复三次通过（29.222 秒）。保留原运行、回复、完成和取消断言及原超时，未增加 sleep，未改产品代码和完整 Agent 矩阵脚本。原 CI 失败日志独立保留，新组合按精确源码一致性复用完整 84 项与 56 个有序切换的验收结果。
