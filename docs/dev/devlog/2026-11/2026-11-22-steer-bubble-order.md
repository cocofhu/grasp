# 2026-11-22（修复引导气泡显示顺序）

- 日期：2026-11-22
- 范围：`web/src/components/run/AgentTimeline.vue`、`web/src/components/run/AgentTimeline.test.ts`、`CHANGELOG.md`
- 做了什么：已结束回合中，位于最终回复之前的引导气泡改为显示在耗时汇总之前，保留实时回合和最终回复之后引导的原有顺序。
- 为什么：引导是用户在回合中的追加消息，显示在耗时汇总下方会让它看起来属于 Agent 的处理信息。
- 如何验证：运行 AgentTimeline 定向单测、Web lint、vue-tsc 和生产构建，并通过浏览器截图核对 Solo 对话布局。
