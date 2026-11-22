# 2026-11-22（优化立即引导图标）

- 日期：2026-11-22
- 范围：`web/src/components/run/PendingSendQueuePanel.vue`、`web/src/components/agent/AgentChatTester.vue`、`web/src/lib/icons/definitions.json`、`web/src/styles/icon-motion.css` 及生成的运行时图标副本
- 做了什么：排队消息的「立即引导」操作从闪电图标改为转向箭头图标。新增路径描绘和箭头前移的单次动效，复用 Vue、沙箱和预览注入页的共享图标运行时。
- 为什么：闪电更容易被理解为加速或优先级，转向箭头更直接表达把排队消息导入正在进行的回合。
- 如何验证：`npm run build:live-overlay`、`npm test -- --run src/components/ui/Icon.test.ts`、`npm test -- --run src/components/run/PendingSendQueuePanel.test.ts`、`npm run lint`、`npx vue-tsc --noEmit`。浏览器检查确认新图标悬停时进入 `once` 状态并有两个活动动画。
