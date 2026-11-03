# 2026-11-03（聊天消息导航刻度留白）

- 日期：2026-11-03
- 范围：`web/src/styles/chat.css`、`web/src/components/chat/ChatMessageRail.vue`、`web/e2e/conversation-workspace.spec.ts`。
- 做了什么：有消息导航时，相邻的正文滚动区为刻度的点击区域及其右侧的 14px 留白保留空间。
- 为什么：导航绝对定位，窄宽度下正文的边距可能比刻度的点击区域还小。
- 如何验证：浏览器用例量测 Solo 新标签预览浮层以及 390px、768px 主页聊天中刻度点击区域和正文之间的间距。
