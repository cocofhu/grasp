# 2026-11-09 保留普通 Agent 与测试沙箱启动失败时的原工作区

- 日期：2026-11-09
- 范围：`server/internal/services/sandbox_agent.go`、`sandbox_test_pool.go`、`sandbox_startup_failure.go` 及对应测试。
- 做了什么：非 Solo 启动使用物理操作租约，在 ACP 就绪前记录原 provider handle。Create 返回 handle 和错误、ACP 失败、取消或最终发布失败时，保留原行、工作区和配置目录，记录错误并清除删除期限。后续相同会话的复用返回原错误回执，不隐式另建环境。失败状态继续占用未确认计算配额。启动巡检保留非运行资源及唯一关联的原 provider ID，歧义关联不猜测。清理器在同一物理租约内重读当前状态，避免旧的 running 扫描删掉后来失败的环境；已受理的 Run 停机仍只重试原 Stop 并核验停止结果。
- 为什么：启动超时和 ACP 连接失败不代表用户文件不存在，也不授权立即删除或在两分钟后由 sweeper 删除。晚到的旧代回调和重启巡检不得覆盖新的停止、替换或来源状态。
- 如何验证：真实 Manager 部分 Create 回执和真实 ACP WebSocket 错误握手覆盖 Agent/test 两条入口，断言零 DELETE/自动 Stop、原文件与目录保留、原 provider ID 可追踪；取消后保留、显式 Stop 失败后重试及再次 Close、代次/名称/来源变更 CAS、执行授权回收、旧 NULL 来源、过期删除期限、旧扫描竞争、唯一/歧义关联和 unknown 不重发回归通过。受影响回归使用 `-race -count=3`，services 全套、`go vet`、golangci-lint 另行校验。全模块和集成门禁由统一冻结批次执行。
