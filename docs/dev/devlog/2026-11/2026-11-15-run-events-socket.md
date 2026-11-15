# 2026-11-15（前端：运行事件 socket 生命周期收拢到 openRunEventsSocket）

- 日期：2026-11-15
- 范围：新增 `web/src/lib/run/runEventsSocket.ts` 及测试，`web/src/lib/inbox/useGatesInbox.ts`（主会话 socket、跟随 socket），`web/src/lib/run/useRunDetailWs.ts`，`web/src/lib/run/useRunLive.ts`，`web/LIB_DOMAIN_MAP.json`。
- 做了什么：#1206 第 7 项第二部分。收件箱主会话、收件箱跟随、运行详情、Live 这四处 `WS /runs/:id/events` 原先各写一遍同样的生命周期。现在统一由 `openRunEventsSocket` 负责：
  - 创建前 `markConnecting`；创建失败时调用页面清理并 `onClose` 安排重试。
  - 先交给页面保存，再启动心跳。
  - open、message、close 都先判断页面是否仍持有这个 socket；Live 页还要同时判断仍是同一次访问。
  - open 时 `markOpened` 并刷新心跳；替换断线 socket 时补读一次 Live。
  - 每帧刷新心跳，丢弃无法解析的帧。
  - close 时先停心跳，再调用页面清理，最后安排重连。

  各页的帧处理（dialogue、live、snapshot、acp、review 等分支）原样留在页面，只是移进 `onFrame`。
- 为什么：四处重复的连接代码改一处容易漏改其余三处，例如过期 socket 守卫、重连后补读。收拢后连接生命周期只有一份实现。
- 保持不变：
  - 心跳、重连控制器、各页的重连条件和超时处理不变。
  - 帧处理逻辑逐字不变，包括首个快照的会话重置、会话版本判断、`dialogueRevision` 失效、`deliverOrBufferGateAcp` 缓冲交付、重连后的 `projectDialogueAfterLoad` / `seed(true)`。
  - Live 页原先连帧处理抛出的异常也一起忽略，现在在 `onFrame` 内保留同样的 try/catch。
  - 跟随 socket 的 `_runId` 标记、关闭时的 `followStep` 补拉保留在页面回调里。
  - 没有新增恢复循环，源码守卫测试依赖的分支文本仍在原文件。纯结构调整，不改 CHANGELOG。
- 如何验证：新增 `runEventsSocket.test.ts`，覆盖调用顺序、只有替换 socket 才补读、过期 socket 的事件全部忽略、创建失败后重试。原有四处页面的重连、快照、缓冲测试不变通过。`web/` 下 `npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`、`npm run build`、`npm run test:e2e:ci` 通过。
