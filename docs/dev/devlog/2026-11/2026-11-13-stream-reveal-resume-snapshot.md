# 2026-11-13（刷新回到进行中的长回复不再从头打字）

- 日期：2026-11-13
- 范围：`web/src/lib/run/streamTextReveal.ts`、`web/src/components/run/AgentTimeline.vue`、`web/src/components/run/GateReactStreamPanel.vue` 及对应测试
- 做了什么：
  - `AgentTimeline` / `GateReactStreamPanel` 挂载时已有的文本（刷新、重连后服务端重放的整轮快照）直接完整显示，只对挂载之后到达的增量做打字机追赶。
  - `createStreamTextReveal` 新增 `maxLag`（默认 `STREAM_REVEAL_MAX_LAG = 500`）：落后超过上限时直接跳到只剩末尾 500 字再追赶。
- 为什么：刷新后 WebSocket 不带游标订阅，服务端重放当前整轮，前端拿到的是完整长文本；打字机从空串开始、每 tick 最多 8 字（约 200 字/秒），且每 tick 对整段已显示文本重新跑 markdown lexer，超长一轮要追几分钟且越来越卡。
- 如何验证：新增 `streamTextReveal`、`AgentTimeline`、`GateReactStreamPanel` 非同步模式单测（挂载即完整显示、后续增量逐步显示、大落差被截到上限），撤掉修复时这 4 个用例失败；`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`、`npm run build` 通过。
