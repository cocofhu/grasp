# 2026-11-23（对齐移动端对话时间线与输入提示行高）

- 日期：2026-11-23
- 范围：`web/src/styles/chat.css`、`web/src/components/run/AgentTimeline.vue`、`web/src/components/run/agentToolGroup.css`、`web/src/components/run/collapsedReplySpacing.test.ts`
- 做了什么：移动端对话输入框在字号提升到 16px 时使用 1.5 倍行高，让两行 placeholder 保持紧凑；工具折叠行和思考行在触屏设备上继续使用桌面的 20px 行高，避免移动端出现额外纵向空隙；补充样式回归断言。
- 为什么：移动端防止输入框聚焦缩放的字号规则会把原有 1.75 倍输入行高放大到 28px，触屏点击规则还把时间线行高放大到 44px，导致截图中的提示和时间线都显得过松。
- 如何验证：`npm test -- --run src/components/run/collapsedReplySpacing.test.ts`；在真实 Vue E2E 页面中以 390×844、DPR 3、触摸设备模拟查看移动端预览，输入框计算样式为 16px / 24px，时间线折叠行与桌面统一为 20px。
