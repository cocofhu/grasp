# 2026-11-16（自动过程回复的统计信息补齐 Token、Agent、编码工具与模型）

- 日期：2026-11-16
- 范围：`server/internal/models/{chat_execution,models}.go`、`server/internal/runtime/{acp_run_agent,chat_execution,provider}.go`、`server/internal/engine/{nodes,node_agent,outcome,finish_node,review,node_req,chat_metadata,dialogue_state,takeover}.go`、`server/internal/services/conversation_chain.go`、`server/internal/handlers/dto.go`、`web/src/lib/inbox/runConversationChain.ts`、`web/src/lib/run/useRunLive.ts`、`web/src/lib/shared/types.ts`、`web/src/components/run/{ClarifyChat,ClarifyMessageThread}.vue`，及对应测试。
- 做了什么：
  - 新增 `models.ReplyMetadata`（执行路由、本条用量、按模型用量、用时），`ReactMessage` 和执行链的 `ChainTurn` 内嵌它，`ChainSegment` 内嵌其指针；JSON 字段名不变。`StateRun`、`runtime.NodeResult`、`runtime.ReactTurn` 各有 `ReplyMetadata()` 产出它，`applyReplyMetadata` 改为复用。
  - 自动执行的 `RunAgent` 在 `NodeResult.Execution` 带回本次尝试的 ACP 执行路由；引擎像 `usage` 一样经 `nodeOutcome.execution` 传到 `saveState`，存进新增的 `StateRun.Execution`（JSON 列，AutoMigrate 自动加列）。接管也保存它。
  - 无人接管的自动过程对话（`buildObserved`）结束后，最后一条回复带 `sr.ReplyMetadata()`；执行链对已结束的 agent 段同样返回，前端作为历史自动段回复的 `metadata`。
  - 执行链的历史对话 turn（澄清、复审、接管）过去只带文本，现在带各自的 `ReplyMetadata`。
  - 接管的开场消息带自动阶段的 `NodeResult.ReplyMetadata()`（自动阶段自己的用量与路由，不含接管后的轮次）。
  - 运行详情接口的 `nodeExecutions` 带 `execution`；`useRunLive` 按 prompt 拆出的回复继续用各自 `turn_end` 的用量，并都标上该次运行的路由（不带运行开始时间，各回复保留自己的时间）。
- 为什么：#1080（`5a4c5754`）把对话改由服务端下发后，自动过程回复由 `buildObserved` 从事件流现拼，没有任何统计字段；前端原来从 `turn_end` 读 Token 的路径在有服务端状态时被跳过，Token 变成「未提供」。执行路由在自动执行路径上从未保存，所以 Agent、编码工具、模型一直缺失。执行链的历史 turn 也只复制了文本。一次无人接管的执行只渲染成一条回复，执行级汇总就是这条回复的数据；有对话的段仍按每条回复各自统计。
- 如何验证：`server/` 下 `go test ./...` 全部通过，新增/扩展 `TestRunAgentReportsExecution`、`TestDialogueObservesAutomaticVisit`、`TestTakeoverRunningAutoNodeBecomesManual`、`TestConversationChainAutoReplyInfo`；`web/` 下相关 vitest（164 个文件）、`npx vue-tsc --noEmit`、`npx eslint` 通过。未在真实沙箱上手动验证。改动前已结束的执行没有执行路由记录，Agent/工具/模型仍显示「未提供」。
