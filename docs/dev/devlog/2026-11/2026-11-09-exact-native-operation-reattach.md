# 2026-11-09 工作流重启后只续接原生原操作

- 日期：2026-11-09
- 范围：`server/internal/{engine,runtime,sandbox,services,models}`、`server/cmd/server/main.go`、`sandbox-gateway/sandbox/internal/{service,handler}` 的原生执行与续接路径。
- 做了什么：在节点准备事务中分配原生 operation ID，在发送前以当前 Run/StateRun/provider attempt 栅栏原子封存资源、原生会话、工作区与授权摘要。启动恢复只采用这份确切收据对应的存活资源；专用 `attach_operation` 不调用 EnsureAgent、模型重置或提示词重发。客户端校验原生专用确认，以原 ID 等待并读取不超过 32 MiB、100000 事件的完整原回合；旧行、缺失/混入/重复边界、变更后的会话/工作区、缺少结果仍明确保留 unknown，不能透明新建或重跑。
- 为什么：原生产配置对所有执行中 Agent 无条件转 unknown，阻断了仍活着的原生进程；旧通用 reconnect 又可能建新会话或用第一轮日志冒充当前 visit。另为 one-shot CLI 引入后台实例与 Agent generation 构成的稳定 nativeSessionId，避免 vendor thread ID 从 pending 变成真实 ID 时误判。重新解析当前账号/所有权、原 WorkspaceActorID、模型/Git/MCP 凭据后，短 SQL 事务只检查元数据；OriginSolo 的独立环境关闭/聊天模型切换不撤回独立工作流，真正撤权或换主则拒绝续接。读取和后续合法补充回合不复用原提示词的 ID。
- 如何验证：新增确切 visit/attempt/资源、已取消 visit、当前凭据与父级权限、原生根/会话/日志边界的正反用例。原生 Bridge 与真实 one-shot 子进程传输使用本地确定性 CLI wire peer：断开第一个客户端后续接仍在执行的原任务、取得原结果、只记录一次子进程调用；同时覆盖 vendor thread ID 变化。该测试不声称 vendor 原生 CLI 推理，真实 OpenCode/容器重启验收由最终组合源码另记。运行 server 相关包与 native service/handler 全包、定向 race 重复、vet/lint，结果与冻结源码清单放任务验证产物。
