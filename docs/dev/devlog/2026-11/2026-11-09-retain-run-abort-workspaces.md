# 2026-11-09 工作流取消和失败保留普通 Agent 的原工作区

- 日期：2026-11-09
- 范围：`server/internal/runtime/acp_run_agent.go`、`run_abort_retention_test.go`。
- 做了什么：普通 Agent 的 Run 取消路径发送原生取消请求、撤销驱动连接，再统一通过已有 `retireRunSandbox` 和生产 `RunSandboxRetirer` 保留原沙箱、文件及配置目录。成功、失败、取消使用同一工作区保留边界。取消与原生完成同时发生时，已取消的 Run 不返回成功，且不会因连接关闭再启动一个新尝试。保留 shared/parked 会话撤销及预览租约的既有行为。
- 为什么：普通 in-flight Agent 在没有预览保活时绕过 Retirer，直接注销并 Destroy，用户取消或任务失败可能删除唯一未提交/未跟踪文件副本。取消任务不等于用户确认释放工作区。
- 如何验证：使用真实 gateway-backed Sandbox handle 和真实 ACP WebSocket 测试桥，观察 DELETE 并维护唯一文件副本；普通并发 Abort、取消与自然完成竞争、成功/失败对称路径均断言零 DELETE、零注销、原目录和原文件字节保留、原生取消到达、驱动状态清理且不创建替代沙箱。另覆盖未知启动、共享 re-prime 失败、预览保活、取消期间 setup/takeover 的既有回归，使用 `-race -count=3`；runtime 全套、golangci-lint、vet 另行校验。
- 停机边界：Retirer 使用现有保留租约及 sweeper 停机策略；原生取消是请求，关闭连接和进入保留期都不声明计算已停止，不提前释放未确认的计算占用。
