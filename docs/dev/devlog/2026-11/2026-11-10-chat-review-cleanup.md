# 聊天输入法回车统一

- **日期**：2026-11-10
- **范围**：`web/src/lib/chat/composerKeys*`、`web/src/components/agent/AgentChatTester.vue`、`web/src/components/pm/PmLeaderChat.vue`、`web/src/components/run/CommentPinInspectCard*`、`web/src/lib/inbox/useClarifyChat.ts`、`web/src/lib/solo/useReferenceComposer.ts`、`web/LIB_DOMAIN_MAP.json`。
- **做了什么**：新增 `composerKeys`，统一“组字中按键不发送”的判断（`isComposing`、Safari 组字结束后的 keyCode 229、组件自身的 composing 状态）。Agent 测试对话、PM 对话和评论钉输入框原先用 `@keydown.enter.exact`，没有这项判断，现改用 `isPlainEnter`；澄清对话和 `@` 引用改用同一函数。
- **为什么**：共享输入框、首页和澄清对话已处理输入法回车误发，但各自复制了判断，其余三个聊天输入仍会在 Safari 和部分输入法确认候选词时发出半截消息。
- **如何验证**：`composerKeys` 单测覆盖组字、229、修饰键；评论钉组件回归覆盖组字和 Shift+Enter 不提交。
- **来源**：#1157 中与输入法相关的部分；该 PR 的 Solo 清理针对已回退的 v3.3 代码，未移植。
