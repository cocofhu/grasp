# 2026-11-22 聊天活动状态图标优化

- 日期：2026-11-22
- 范围：`web/src/components/run/ThoughtSummaryStatus.vue`、`web/src/components/run/AgentToolGroup.vue`、`web/src/lib/icons/definitions.json`、`web/src/styles/icon-motion.css` 及对应测试；`npm run build:live-overlay` 刷新的图标运行时生成副本
- 做了什么：参考 OpenCraft 的活动时间线视觉语言，把折叠思考行的静态图标换成脑部线稿，把工具组完成状态换成圆环勾选，并为两种状态补充图标回归断言。
- 为什么：截图中的小尺寸星芒和单独勾线在聊天时间线里辨识度不足，状态语义也不够统一。
- 如何验证：`src/components/run`、`src/lib/icons` 与 `Icon.test.ts` 共 1319 条 Vitest 通过，`npm run lint`、`npx vue-tsc --noEmit` 通过；server `internal/handlers` 与 sandbox `internal/previewinject` 的 Go 测试通过；并在本地真实 GRASP 栈接入 DeepSeek Harness 跑通对话，确认思考行与工具完成行的实际渲染。
