# 2026-11-09 执行身份匹配后仍核对离线维护

- 日期：2026-11-09
- 范围：`server/internal/executionlineage/observation*`、`server/cmd/storage-admin/execution_status_test.go`、`server/STORAGE.md`
- 做了什么：只读检查在控制对象与 SQL 身份匹配后，继续读取既有维护元数据。维护未解除返回 inactive 和 maintenanceActive，不清理维护 owner；缺少必需元数据返回 unavailable。
- 为什么：离线激活成功但解除维护前崩溃，身份已经匹配，服务却仍受启动屏障约束。部署检查须保留该区别，让原票据流程显式收尾。
- 如何验证：新增真实 SQLite 维护进入、只读核对、owner 保留及显式解除后的状态回归；连同执行身份和对象存储并发测试执行 race 两轮。完整服务端与仓库检查在固定源码副本核对。
