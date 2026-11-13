# 2026-11-13（产物 MCP token 改为跟随所属对象）

- 日期：2026-11-13
- 范围：`server/internal/mcp/`（含新包 `mcptest`）、`server/internal/engine/`、`server/internal/services/`、`server/internal/handlers/doctor.go`、`server/internal/models/handoff.go`、`server/cmd/server/mcp_wiring.go` 及相关测试；`CHANGELOG.md`。
- 做了什么：`mcp.Host` 不再保存 token，也不再提供 `RegisterRun`、`RestoreRun`、`UnregisterRun`。改为按顺序询问注册进来的 `ScopeSource`，每个命名空间由各自的 owner 判定是否有效：工作流运行由 engine 负责，读运行上持久化的 `mcp_token`，状态为 queued、running、waiting_human 时有效；Solo 和测试沙箱由 `services.SandboxScopes` 负责，读沙箱行上的 token，Solo 还要求 Agent 存在且会话仍指向该沙箱（会话状态为 starting、running 或 stopping）；doctor 诊断的 `doctor-` 命名空间由 doctor handler 负责。`ReleaseRun` 只释放内存状态，命名空间仍有效时会拒绝并记录警告。`node_complete` 的交接目标改为从运行图计算（`models.Graph.HandoffTargets`），不再依赖进程内存。删除了会话墓碑、`releaseRunMCP` 和 `runStillActive`。401 日志带原因：unknown_scope、scope_ended、token_mismatch。
- 为什么：#797 中，回收工作流里任意一个节点的沙箱会吊销整个运行的 token，导致正在执行的节点断开 MCP；重启和恢复路径也各自维护了一份 token 状态。由 owner 直接判定后，重试、重启、多副本都用同一个 token，不需要吊销和恢复逻辑；Solo 换沙箱后，旧沙箱立即失效（因此不做鉴权缓存）。运行结束后保留调试的沙箱不再能调用 MCP，这是有意的行为变化。
- 如何验证：server 全量 `go test ./...`、`go vet ./...`、golangci-lint v2.12.0（0 issues）、`gen-configdoc -check`、`TestRunAgent|TestReact` 子集通过，核心覆盖率 90.6%。新增测试覆盖：owner 判定授权与拒绝原因、仍有效时拒绝 `ReleaseRun`、运行状态决定有效性、从运行图计算交接目标、运行结束释放状态、`loadCtx` 使用持久化的 token、沙箱行决定 Solo/测试命名空间、销毁工作流沙箱不影响运行的 MCP、已删除的会话 ID 被重新接纳。
