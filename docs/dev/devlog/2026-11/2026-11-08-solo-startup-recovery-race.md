# 2026-11-08 Solo 首条消息与定时恢复竞争

- 日期：2026-11-08
- 范围：`server/internal/services/{sandbox_solo_recovery.go,solo.go,solo_startup_test.go,solo_model_test.go}`、`CHANGELOG.md`。
- 做了什么：定时恢复跳过仍由首次启动流程负责的 `creating` / `pulling` 沙箱；启动对账仍将进程重启前未完成的创建标为待恢复。准备沙箱、读取会话或对齐模型失败时，同时保存关联本轮用户消息的 assistant 失败或停止记录，让现有聊天界面在刷新后仍显示原因。
- 为什么：网关尚未返回真实 ID 时，本地名称只是关联标记；定时恢复直接查询该名称收到 404，将正常创建中的沙箱写成 error，首条消息随即退出。稍后创建成功不会重新执行已经失败的消息。原先准备阶段只在用户消息保存错误，界面没有 assistant 记录可展示，表现为发送后一直没有回复。
- 如何验证：阻塞网关创建，分别在 `creating` / `pulling` 阶段触发恢复，检查状态不被改为 error，释放后首条消息正常得到回复；检查准备失败和取消均在历史中保留同轮 assistant 终态。新增回归在修复前均失败，修复后通过；与恢复期间关闭会话的回归一起在 race 下重复 20 次通过。Go 1.26.10 下 server lint、vet、配置文档检查、全量测试、核心覆盖率门禁（90.7%）及 runtime 指定回归通过；根目录 actionlint、shellcheck、品牌断言、离线发布回归及 docs 构建、官方 registry audit 通过。
