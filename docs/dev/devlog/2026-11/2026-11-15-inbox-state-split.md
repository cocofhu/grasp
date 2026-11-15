# 2026-11-15（收件箱：处理中状态、上下文请求、输入保护拆出 useGatesInbox）

- 日期：2026-11-15
- 范围：`web/src/lib/inbox/useGatesInbox.ts`，新增 `web/src/lib/inbox/inboxProcessing.ts`、`web/src/lib/inbox/inboxInputGuard.ts` 及对应测试，`web/LIB_DOMAIN_MAP.json`。
- 做了什么：#1206 第 7 项第一部分。从 `useGatesInbox` 中拆出三块原本散落在同一函数体里的状态：
  - `createInboxProcessingState`：负责已处理三元组、已确认不存在三元组、处理中意图集合、处理锁，以及列表刷新后的已处理对账。
  - `createInboxContextFlight`：负责收件箱上下文请求的 AbortController 管理（按三元组取消、全部取消），以及回合结束后的尾随刷新。尾随刷新是否执行，仍由 `useGatesInbox` 传入的回调按当前选中项判断。
  - `createInboxInputGuard`：负责聚焦输入框是否有草稿、最近是否在输入（`TYPING_IDLE_MS`），以及 focusin/focusout/input 监听的挂载和卸载。
  - `useGatesInbox` 通过解构保留原有局部名，调用点不变。`TYPING_IDLE_MS` 从原路径继续导出。`begin`/`rollback`/`endProcessingIntent` 依赖列表和选中态，留在原处。
- 为什么：`useGatesInbox` 同时管理列表、选中、会话、处理中、输入保护、实时连接，状态归属不清。拆出的模块各自有明确的状态所有者和入口，可以单独测试。
- 保持不变：只移动代码，不改逻辑，原有判定条件、取消顺序、微任务时机都保留。确认仪式、分享面板、澄清会话加载后投影等被源码守卫测试固定在 `useGatesInbox.ts` 中的代码，这次刻意不动。纯结构调整，不改 CHANGELOG。
- 如何验证：新增 `inboxProcessing.test.ts`、`inboxInputGuard.test.ts`，覆盖对账、处理中禁用、尾随刷新只执行一次、取消和监听卸载。原有收件箱单测和源码守卫测试不变通过。`web/` 下 `npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`、`npm run build` 通过。
