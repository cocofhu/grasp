# 2026-11-24（Cursor 引导不再误打断回合，桥接进程不再被 SDK 异常带崩）

- 日期：2026-11-24
- 范围：`sandbox-gateway/sandbox/internal/provider/cursorsdk/`（`bridge.mjs`、`session.go`）、`internal/provider/{provider,turn}.go`、`internal/service/{bridge_steer,bridge_connect}.go`、`sandbox-gateway/scripts/agent-platform-wire-fixture.test.mjs`、`sandbox/docs/BACKEND.md`；`server/internal/chatsession/`、`sandbox/acp_{steer,turn}.go`、`runtime/steer.go`、`services/turn_steer.go`、`engine/{review_session,dialogue_state}.go`；`web/src/components/run/PendingSendQueuePanel.vue`、`components/agent/AgentChatTester.vue`、`components/solo/SoloSessionPane.vue`、`lib/solo/useSoloChat.ts`、`lib/chat/dialogueState.ts`、`lib/api/apiTypes.ts`、两份 `pages.json`
- 做了什么：
  - 桥接进程：`bridge.mjs` 注册 `unhandledRejection` / `uncaughtException`。SDK 自己没人 await 的三类 rejection（cancel 关掉输出流后 shell 再写最后一帧的 `WriteIterableClosedError`、`ERR_STREAM_WRITE_AFTER_END`、卡住检测的 `AbortError`、shell 启动失败）只记日志；其余仍退出，但先把错误名、消息和栈写到 stderr，不再让压缩源码行占满后端保留的 stderr 尾部。
  - 引导等待：Cursor 只在 run 的下一步并入引导，可能要等一个很长的工具调用。沙箱 `cursorsdk` 的 `Steer` 去掉 20 秒上限，等到并入或 run 结束（结束时 Cursor 退回，之后 10 秒无应答也按退回）；服务端 `ACPClient.Steer` 去掉 30 秒 ack 超时，改为等到被引导的回合结束后再宽限 15 秒；`chatsession` 去掉 30 秒投递上限，投递随当前回合结束（再宽限 20 秒）或取消而结束。
  - 退回不打断：沙箱 ack 新增 `declined`（Agent 退回输入）。服务端 `SteerResult` 把 `declined` 和投递失败映射为新的 `SteerDeclined`：不中断当前回合，消息排到队首、带 `steer: "queued"`，本轮结束后发送，接口返回 `steer: "queued"`；只有 `unsupported` 仍中断后重发。投递期间队列条目带 `steer: "pending"`，queue_state 与审核会话的对话状态都会下发。
  - 前端：队列条目显示「引导中」（禁止再次引导和编辑，可取消）和「本轮结束后处理」（不再提供引导）；Solo 的引导请求若在服务端已接手后被代理切断，不再报错。
  - 进程中途退出：`watchAgentConn` 在结束 Agent 上下文前，先以 `provider.ErrAgentExited` 取消正在运行的回合；`EndInterrupted` 把它当失败（`failed` + 错误文本），不再是「已取消」的空回复，消息可直接重试。
- 为什么：线上 Cursor Solo 会话里，用户在 `take_screenshot` 等待期间发的引导两次超过 20 秒被判失败，平台随即打断当前回合（界面显示「已停止」）；打断后约 6 秒 SDK 清理后台 shell 时抛出无人接住的 rejection，`bridge.mjs` 以退出码 1 退出，刚开始的新回合以 `cancelled`、空内容结束，用户消息丢失，用量也没记上。t3code 同样基于 `@cursor/sdk` 1.4.2，引导不设超时、退回后排在当前回合之后，并兜住同几类 rejection。
- 如何验证：
  - Node：`agent-platform-wire-fixture.test.mjs` 用桩 SDK 驱动真实 `bridge.mjs`，三类已知 rejection 下进程存活且 run 正常结束，其他 rejection / 未捕获异常退出码 1 且 stderr 首行是错误；去掉修复后两条新用例失败。
  - 沙箱 Go（`-race`）：`cursorsdk` 引导在 run 忙时不返回、run 结束后退回或在宽限后放弃；`bridge_steer` 的退回映射为 `declined`；`watchAgentConn` 让中途退出的回合以 `failed` 结束并广播含原因的 `op:error`（去掉修复时为 `cancelled`）；`EndInterrupted` 新原因。
  - 服务端：`chatsession` 投递期间条目标 `pending`、退回后标 `queued` 且不中断、本轮结束后按序执行并清除标记、无应答在回合结束后宽限内结束且不中断；`ACPClient.Steer` 回合运行时一直等待、结束后宽限放弃，新增 `declined` 状态；`SteerResult` 映射；Solo 退回与失败均返回 `queued`、不取消当前回合、本轮结束后发送；对话状态的排队回合带标记。
  - 前端：队列面板的两种状态、Solo 解析标记与被切断请求不报错、对话状态透传标记。
  - 门禁：sandbox / server / web 全量测试，`go vet`、golangci-lint、`vue-tsc`、eslint、web build。
