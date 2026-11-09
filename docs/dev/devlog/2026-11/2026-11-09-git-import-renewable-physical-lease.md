# 2026-11-09 Git 导入续租与实际进程互斥

- 日期：2026-11-09
- 范围：`server/internal/sandbox/workspace_code*`、`server/internal/services/workspace_import*`、`sandbox-gateway/sandbox/internal/{handler,router,workspacegit,workspacejob}/`、`CHANGELOG.md`
- 做了什么：Git 导入和导出在原短租约内运行，服务端只转发已经提交的同代 SQL 租约；运行时校验并确认精确代次和到期时间。长传输上限为 30 分钟，仍由最后一次已确认的短租约约束。旧运行时缺少续租能力时拒绝导入。binding 模式共用处理器级执行槽，后继操作先取消前驱，并等待其 HTTP 请求及整个 Git 进程组退出。Git 与持久工作区任务共用父进程死亡监督逻辑。
- 为什么：初始租约固定截止会中断合法的大传输，而简单延长 HTTP 超时会失去停写保证；每次请求创建 Service 也不能提供跨请求互斥。运行时不可从过期租约、错误确认或不同代次恢复写入。
- 如何验证：服务端 SDK 与服务 Git 导入测试 `-race -count=3` 通过。运行时 handler、router、workspacegit、workspacejob 全包 `-race -count=3` 通过；真实 HTTP 路由、鉴权、Git bundle 和限速上传用例证明上传超过初始期限时须持续收到准确续租确认，不续租则中断且不发布完成收据。服务端确认测试拒绝不同代次、缩短/擅自延长及已过期确认，并验证过期请求零派发。最终全模块和预览整体验收另行执行。
