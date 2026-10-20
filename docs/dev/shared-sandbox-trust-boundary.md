# 共享运行沙箱的信任边界

适用于同一个工作流运行里、指纹相同的 Agent 节点共用一个容器（scope=run）的情况。完整的复用与恢复设计在单独的设计稿里，这里只记录当前代码的隔离范围。代码注释见 `server/internal/runtime/shared_sandbox_env.go`。

同一 run 的节点在同一个容器里、以同一个系统用户运行。节点之间的隔离只有平台按 bridge chat 注入的那一层：

- **按节点**：ACP 会话 env overlay（AI key、Agent env、key 不属于容器级的凭据绑定）只设在该节点的 chat 进程上，chat 关闭时清掉。它挡不住后面的节点：节点写到磁盘上的东西、留下的进程，下一个节点都看得到。
- **按容器**（建容器时确定，计入兼容性指纹）：git clone token、SSH key / known_hosts、ConfigHome 里的 Codex `auth.json`，以及被归为容器 env 的其他凭据绑定 key。只要这些凭据的 id 不同，节点就不会共用容器，而是回退到节点私有容器。
- **不隔离**：写在容器 home 或 ConfigRoot 下的 CLI 登录缓存（例如刷新后的 Codex `auth.json`、其他 CLI 的 token 缓存）、工作区，以及节点留下的进程（用 setsid 启动、由 keepalive 租约保活的 preview 进程，保留的是启动时的 env）。

结论：不能互相看到凭据的节点，不要放在同一个 run 里。
