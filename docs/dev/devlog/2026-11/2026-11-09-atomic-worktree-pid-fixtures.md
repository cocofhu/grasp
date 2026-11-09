# 2026-11-09 原子发布本地停机测试的 PID 就绪文件

- 日期：2026-11-09
- 范围：`server/internal/sandbox/worktree_stop_linux_test.go`
- 做了什么：两个 Python 进程夹具先将完整 PID 写入临时文件并关闭，再通过 `os.replace` 原子发布就绪文件；Go 等待逻辑仅在成功解析正 PID 后继续。保留原有三秒等待上限、独立后代停机证明、脱离会话的进程保护、相同工作目录的无关进程保护和原文件保留断言，不修改生产代码。
- 为什么：原夹具先创建空文件，Go 读取成功就忽略整数解析错误并退出等待，导致 CI 在 0.17 秒内误报独立进程未启动，未实际等待既有三秒上限。
- 如何验证：私有 Go overlay 在创建文件与写入 PID 之间固定停留 0.2 秒，原夹具稳定重现误报；修复夹具在同一窗口下两个原生进程测试均通过。`go test -race ./internal/sandbox -run 'TestWorktreeStop|TestLocalStopKnownDetachedWriter' -count=20` 和 `go vet ./internal/sandbox` 通过；没有扩大等待时间或放宽停机权限。
