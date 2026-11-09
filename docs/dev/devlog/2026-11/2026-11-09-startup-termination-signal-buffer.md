# 2026-11-09 在启动恢复前接管终止信号

- 日期：2026-11-09
- 范围：`server/cmd/server/main.go`
- 做了什么：在 main 初始化日志后立即注册带单项缓冲的 SIGTERM / SIGINT 通道，并保留 `signal.Stop` 清理。早到信号等待启动恢复返回后进入原停机流程，不提前宣称 drain 完成；信号消费、HTTP / 工作区等待上限及运行登记清理顺序不变。
- 为什么：原代码先启动 HTTP 并同步恢复工作流，最后才注册终止信号。恢复较慢时健康检查已经可达，此时 SIGTERM 会走默认退出，跳过 drain 和 SQL 运行登记清理。
- 如何验证：从固定源码构建真实 main 二进制，私有 overlay 仅在工作流恢复入口设置可释放暂停。旧版在 health 200 后收到 SIGTERM，实际退出码为 -15、没有 signal/drain 日志并留下一个运行登记；修复版在暂停期间保留信号，释放后按原流程退出 0，运行登记为 0。无 overlay 的修复版重复 20 次健康后立即 SIGTERM，均记录原 signal/drain 流程、退出 0 并清理登记。`go test -race ./cmd/server -count=2`、`go vet ./cmd/server` 和该包 golangci-lint 均通过；所有原生验证使用独立空数据库，无模型请求和稳定资源变更。
