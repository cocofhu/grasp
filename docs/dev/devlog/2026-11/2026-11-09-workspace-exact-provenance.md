# 2026-11-09 工作区记录关联精确执行轮次

- 日期：2026-11-09
- 范围：`server/internal/engine/node_req.go`、`server/internal/runtime/{provider,acp_sandbox,acp_shared}.go`、`server/internal/models/models.go`、`server/internal/services/sandbox_run.go` 及对应测试。
- 做了什么：节点请求按 Run、节点、Iteration 和 Run Attempt 的完整组合解析唯一 StateRun ID，并通过普通与共享沙箱注册持久保存。创建占位行的接管也核验精确来源；沙箱就绪时间只更新指定执行轮次。缺失或重复历史来源保留未知值，不以最新执行替代。Provider Attempt 字段先保留为 0，等待真正跨进程的持久分配器，不使用会重置的本地重试计数。
- 为什么：完整工作区快照需要区分循环、Run 重试与共享环境的实际写入来源。单独记录节点名或查询最后一行可能把新执行文件当作旧执行成果；本次仅建立来源基础，不宣称已经创建或恢复文件快照。
- 如何验证：引擎验证相同节点和 Iteration 的不同 Run Attempt、缺失和重复来源；运行时验证创建、注册及共享路径原样传递；服务验证并发占位行不会接管另一 Attempt、共享来源更新及错误来源不会写入就绪时间。定向测试通过，并纳入隔离源码的完整后端检查与 race 回归。
