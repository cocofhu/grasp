# 2026-11-09 本地 Solo 保留工作树停机

- 日期：2026-11-09
- 范围：`server/internal/sandbox/worktree_create.go`、`worktree_stop*.go`；`server/internal/services/solo_environment.go`、`solo_container_start.go`、`sandbox_solo_recovery.go`、`solo_local_environment*.go`
- 做了什么：新本地后端启动在独立 Linux session，保存原 PID/start 与 pidfd 私有收据。停止时先冻结并核对受控进程的线程和 session 成员，覆盖独立进程组及同 session 的 reparent 子进程，然后逐一确认退出；不会按 cwd 或同名进程推断权限。StopLocal 不调用 Destroy/Release/End，不删除工作树或配置 home。Solo 只有在停止证明确认、原物理租约与生命周期栅栏仍有效时，才发布 stopped、完成 close_cleanup 和释放计算名额。超时保留原 handle/名额，重试核对原停止操作。相同应用进程内可用原 handle 在同根目录重新启动，重建模型上下文；未就绪本地创建也保存其私有 handle，失败的重新配置候选进程不会丢失停止收据。
- 为什么：原本 worktree 始终返回 stop_unsupported，使每次关闭永久占用 MaxSolo 配额；直接复用旧 Destroy 又会删除唯一工作树。等待后端父进程或一个进程组退出也不足以证明受控子任务退出。
- 如何验证：Linux 实际进程回归覆盖独立子组/reparent、同 cwd 无关进程不被停止、已知 setsid 脱离的 writer 保持 pending；服务回归覆盖停止超时保留名额、禁止未知停止时 Resume、同收据重试确认后释放名额、同根重新打开和再次关闭后可创建新 Solo。定向 race 重复三次及 sandbox/services 完整测试通过，最终整合日志由主代理记录。

能力边界：这里只停止平台创建并证明归属的本地 Agent/runtime 进程。worktree 不是容器，不承诺回收任意事前自行 setsid 脱离的外部服务。已知脱离成员、pidfd/进程信息不可用、旧启动没有收据或应用重启丢失私有 handle 时保持 unknown/pending，不伪造进程归属、停止证明或内存恢复。
