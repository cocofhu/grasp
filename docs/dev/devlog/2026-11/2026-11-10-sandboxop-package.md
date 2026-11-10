# 沙箱操作租约下沉到 internal/sandboxop

- 日期：2026-11-10
- 范围：新增 `server/internal/sandboxop`；`server/internal/services` 的 `sandbox_operation.go`、`sandbox.go`、`solo_journal.go`、`workspace_import.go`、`workspace_release_worker.go`、`workspace_snapshot_job.go`

## 做了什么

- workspace 拆包第 1 阶段：租约的获取、续期、释放、提交围栏和更新移到 `sandboxop.Manager`。执行谱系检查、事务、release 守卫和续期间隔通过 `Hooks` 注入，`SQL` 与原实现逐行一致。
- `SandboxService` 用 `sync.Once` 懒创建 Manager，原有 `acquireSandboxOperation` / `commitSandboxOperation*` / `updateSandboxOperation` 保留为薄封装，调用点不变；测试中直接构造的 `&SandboxService{}` 仍然可用。
- 四处手写的 context 链遍历（找当前持有的 owner / generation）改为 `Held` / `Current`；进程内锁表从 `soloRecoveryLocks` 移入 Manager。
- `errSandboxOperationLost`、`sandboxOperationLeaseTTL`、`publicationContext` 改为指向新包的别名，`errors.Is` 判断不受影响。

## 为什么

工作区核心依赖最多的就是租约（`acquire` 9 处、`commitPrepared` 8 处、`errSandboxOperationLost` 10 处）。先把它下沉成独立包，后续 `internal/workspace` 才能不依赖 `services` 直接使用。

## 如何验证

- `go build ./...`、`go vet`、`go test -race ./internal/services/` 全部通过。
- 首轮测试发现 `soloChat.emit` 在没有沙箱服务的测试里会空指针：原实现只做指针比较，现在补了 nil 判断，`heldSandboxOperation` 对 nil 接收者同样返回未持有。
