# 2026-11-09 本地工具切换先确认原日志持有进程退出

- 日期：2026-11-09
- 范围：`server/internal/sandbox/worktree_create.go`、`worktree_reprime_test.go`、`worktree_reprime_order_linux_test.go`
- 做了什么：本地工具切换先使用既有进程监督的原 stop receipt 确认旧 backend 及已记录子进程退出，再启动替代 backend。停止未知时不派发新进程、不替换原关联、不删除工作区。单环境切换通过可取消的串行 gate 排队；Close 或绑定代次变化时拒绝发布候选并保留其精确清理句柄。新进程启动失败后如实保留旧进程已停止的状态，由现有上层回滚协议恢复工具配置。
- 为什么：CI 的 Cursor→Kimi Code 在发出任何模型请求之前超时。原生 backend 诊断证明替代进程先启动，尝试打开旧 backend 仍独占的持久工作区日志并立即退出，错误为 `workspace operation journal is already open`。延长超时不能解决两个进程竞争同一日志的问题。
- 如何验证：实际内核独占文件锁回归、停止未知零派发与原收据重试、排队期限、原有本地进程组停止和 Git 授权/文件保留回归在 `-race -count=3` 下通过。只以旧顺序覆盖生产文件时，新增日志锁回归立即失败。实际服务端和同源 runtime 的 Cursor/Kimi Code 11 项调试子集通过，覆盖双向切换、MCP 历史、原文件和推理档位；两工具均明确是平台 CLI wire fixture，未声称真实厂商推理或完整八工具矩阵，付费调用为零。sandbox 包 lint/vet 通过。
