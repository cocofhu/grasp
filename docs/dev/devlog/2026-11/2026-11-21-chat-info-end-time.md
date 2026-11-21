# 2026-11-21（回复统计信息显示完成时间）

- 日期：2026-11-21
- 范围：`web/src/components/chat/ChatMessageMeta.vue`、`web/src/components/chat/ChatMessageMeta.test.ts`、`web/src/locales/{en,zh-CN}/pages.json`、`CHANGELOG.md`
- 做了什么：回复下方的时间和「统计信息」面板改为优先显示完成时间（`endedAt` → `createdAt` → `at`，没有时再回退到开始时间）。面板里「时间」改名为「完成时间」。用时达到 1 小时或跨天时，在上方多显示一行「开始时间」。只记录了开始时间的回复只显示「开始时间」，不会把开始时间标成完成时间。
- 为什么：原来优先取 `execution.startedAt`，长任务（例如跑了近 7 小时）显示的是开始时间，和回复实际出现的时间对不上，前后消息的时间顺序看起来也会错乱。完成时间加用时可以推出开始时间，只有长任务才需要单独列出。
- 如何验证：新增单测覆盖 Solo 回复（`createdAt` + `execution.startedAt`）显示完成时间和开始时间、工作流回复（`at`/`endedAt`）短时不显示开始时间、只有开始时间时只显示「开始时间」行。前端 lint、`vue-tsc` 和 chat/run/solo 目录单测通过。
