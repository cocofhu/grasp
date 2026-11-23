# 2026-11-23（Agent 连上后立即推送 Steer 能力，连接中显示等待状态）

- 日期：2026-11-23
- 范围：`server/internal/runtime/`（`acp_sandbox.go`、`acp_provider.go`、`acp_react.go`、`acp_run_agent.go`、`acp_visitor.go`、`chat_route.go`、`registry.go`）、`server/internal/engine/`（`engine.go`、`sandbox_busy.go`、`dialogue_state.go`、`dialogue_public.go`、`review_session.go`）、`server/internal/handlers/react_sessions_dto.go`、`server/internal/services/solo_turn.go`、`web/src/components/run/PendingSendQueuePanel.vue` 及工作流 Chat、审批热修改、Solo 的接线、`web/src/lib/chat/__fixtures__/dialogue/`
- 做了什么：
  - runtime 新增 `SetBridgeLiveSink`：会话的 ACP 客户端能被 `SessionBridgeState` / `VisitorSessionBridgeState` 读到时（park、切换路由后换客户端、新开访客 lane）通知 engine；engine 对该会话 `PublishQueueState`，没有会话时直接发布对话状态。
  - 对话状态新增 `steerPending`，`queue_state` 帧、刷新快照和 REST 新增 `sandboxSteerPending`：平台有轮次在跑但桥接会话还没连上时为 true，表示能力未知而不是不支持。
  - Solo 的 `phase` 帧带上 `steerable`，模型切换重连后再推一次；前端在 `preparing` 阶段显示等待状态。
  - `PendingSendQueuePanel` 新增 `steerPending`，在 Steer 位置显示禁用的加载按钮。
- 为什么：issue #1352。`ACPClient.steerable` 在收到 `connected` 帧时就已更新，但没有任何代码在这时发布对话状态，前端要等 Agent 第一条内容输出才拿到 `steerable: true`；同时布尔值把「尚未握手」和「不支持」混成一个 false。Solo 运行阶段原本完全不推 `steerable`，可能整轮都不显示 Steer。
- 如何验证：`go test ./internal/engine/ ./internal/services/ ./internal/handlers/ ./internal/runtime/ ./internal/chatsession/`；新增 `TestParkReactSessionNotifiesBridgeLive`、`TestAttachSandboxStateMarksSteerPendingUntilBridgeLive`；`go test ./internal/engine -run TestDialogueContractFixtures -update-dialogue-fixtures` 重新生成契约 fixture；`vue-tsc --noEmit`；`vitest run` 覆盖 `src/lib/chat`、`src/components/run`、`src/components/solo`、`src/lib/inbox`、`src/lib/solo`、`src/locales`。未在真实沙箱中复现。
