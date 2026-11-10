# 2026-11-09 逐个离线核对旧 Solo 环境并保盘重建授权

- 日期：2026-11-09
- 范围：`server/cmd/storage-admin/{main,solo_review,solo_review_test}.go`、`server/internal/executionlineage/solo_review*.go`、`server/internal/services/solo_environment_review_*.go`、`server/internal/services/sandbox_solo_recovery.go`、`server/UPGRADING.md`
- 做了什么：增加只针对单个原 Solo 沙箱的离线核对命令、持久停止回执、独立维护目录和显式 finalize。原提供方身份经过前后核对且正向停止后，才原子更新该条隔离状态及原会话/环境 stopped 状态；未知停止只查询原结果。新的手动恢复必须消费已 finalize 的同一来源收据，冻结当前授权及冷启动 nonce，并调用提供方启动前完整配置刷新协议，成功后再次通过当前执行、父会话、队列和物理租约 fence 发布新授权与运行状态。没有冷配置能力时拒绝，E2B 暂停不能先恢复旧 RAM 再补 hash。
- 为什么：真实旧库副本的 4+1 条隔离实际均是原运行中的 Solo 沙箱；新版 Sandbox guard 使原会话的 Send/Stop/Resume 永久受阻，且这些旧环境的完整授权指纹均为空。只证明新建合成会话可工作不足以证明旧会话升级成功，也不能直接为旧进程填充当前授权指纹。
- 如何验证：离线核对 CLI、执行控制及冷恢复服务的定向故障/竞争测试，覆盖原 nonce、失 ACK、SQL 发布失败、取消、身份/授权/生命周期/物理 owner/独立 Authority 改变、无关隔离和旧 unknown 不重放、显式 finalize、同一资源及非 Git 二进制保留；真实提供方启动前协议及原 Solo 新消息链路验证在组合验收中单独记录，不用服务假提供方代替原生证明。
