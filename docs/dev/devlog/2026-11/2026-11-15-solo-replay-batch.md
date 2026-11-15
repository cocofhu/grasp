# 2026-11-15（Solo 切回运行中的长回复不再逐帧回放）

- 日期：2026-11-15
- 范围：`web/src/lib/solo/useSoloChat.ts`、`web/src/lib/solo/acpTurn.ts`、`web/src/components/solo/SoloSessionPane.vue`、`web/src/components/run/AgentTimeline.vue` 及对应测试
- 做了什么：
  - 会话 WebSocket 收到忙碌的订阅快照后，把随后的回放帧（seq 不超过快照 seq）缓存，收到 seq 达到快照 seq 的帧或第一条实时帧时一次性应用。回放停在快照 seq 之前（末帧是不回放的 KeepNone 帧）或网络中途停顿时，静默 120ms 先显示已收到的部分，但仍保持批量模式，之后迟到的回放帧继续成批应用，直到第一条实时帧。连接关闭前先应用已收到的回放帧，确认游标照常推进。帧只解析一次。
  - 每应用一批回放帧，当前回合的 `replayBatches` 加一；`SoloSessionPane` 传给 `AgentTimeline`，计数变化的那次更新整段直接显示（包括批里新开的短段落），之后的实时增量照常打字。
- 为什么：`SoloView` 按 sessionId 重新挂载会话面板，切回会话会重新订阅，服务端重放整个运行中回合的每一帧。原先只在历史读取完成前缓存，之后到达的回放逐帧处理，每帧触发一次完整重渲染，`AgentTimeline` 还会把回放增量当新输出做打字动画（#1229 只处理了挂载时已有的文本），超长回复切回时卡顿并“重播”很久。
- 如何验证：新增 `useSoloChat` 用例（历史读取后到达的回放一次性应用并计一批；回放未到快照 seq 时静默后应用，迟到的回放帧仍成批应用）和 `AgentTimeline` 用例（回放批整段显示、之后实时增量仍有动画），未改源码时失败。`npx vitest run src/lib/solo src/components/solo src/views src/components/run src/lib/run` 全部通过，`eslint`、`vue-tsc --noEmit` 通过。另用独立演示页模拟 3 万字回放：逐帧处理约 3000 次刷新、31 秒、最低 8 帧/秒；批量处理约 0.3 秒显示完、最低 54 帧/秒。
