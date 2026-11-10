# 2026-11-09 启动结果未确认时保留原环境与创建收据

- 日期：2026-11-09
- 范围：`server/internal/sandbox/{manager,gateway}.go`、`server/internal/runtime/acp_*.go`、`provider.go`、`server/internal/services/solo_container_start.go`、`solo_environment.go`、`sandbox_run.go`、`workflow_provider_attempt.go` 及对应测试。
- 做了什么：创建已返回真实 provider 身份但后续就绪等待失败时，返回原 handle 与明确的结果未确认错误，不自动销毁环境或删除配置目录。Solo 在独立的短期元数据上下文中核对原创建租约、代次及执行来源，保留原身份；授权变化不发布 ready，也不允许旧回调改写后继环境。工作流与共享运行时保留原失败回执，不自动重建替代环境；已知失败的启动记录不会登记为可执行。停机请求未获实际证明时保持文件未确认和计算占用。
- 为什么：真实 Kubernetes 启动超时曾沿 Manager 错误清理路径销毁唯一工作区。超时、ACP 失败或响应丢失均不能证明原环境不存在，也不构成用户释放磁盘的授权。
- 如何验证：Manager 部分成功、网关 ID 回执、Solo 创建身份与迟到回调、授权与来源变更、ACP/共享运行时重连、工作流失败登记及显式停机的回归通过；受影响关键包使用 `-race -count=3`，services 和 runtime 全套、vet 和 lint 通过。真实 Kubernetes 与最终统一源版本门禁另行验收，定向结果不代表最终部署已完成。
