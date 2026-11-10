# 聊天输入法回车统一与 Solo 死代码清理

- **日期**：2026-11-10
- **范围**：`web/src/lib/chat/composerKeys*`、`web/src/components/agent/AgentChatTester.vue`、`web/src/components/pm/PmLeaderChat.vue`、`web/src/components/run/CommentPinInspectCard*`、`web/src/lib/inbox/useClarifyChat.ts`、`web/src/lib/solo/useReferenceComposer.ts`、`web/src/lib/solo/useSoloChat.ts`、`web/src/components/solo/SoloSessionPane.vue`、中英文文案、`web/LIB_DOMAIN_MAP.json`、`server/internal/services/solo.go`、`CHANGELOG.md`。
- **做了什么**：新增 `composerKeys`，统一“组字中按键不发送”的判断（`isComposing`、Safari 组字结束后的 keyCode 229、组件自身的 composing 状态）。Agent 测试对话、PM 对话和评论钉输入框原先用 `@keydown.enter.exact`，没有这项判断，现改用 `isPlainEnter`；澄清对话和 `@` 引用改用同一函数。删除 Solo 面板中自 #920 起已无入口的关闭、删除确认框、“更多”菜单及其文档监听，以及仅由它们使用的 `useSoloChat.close/rename` 与文案键；合并 `adoptLocalSend` 中重复的查找，并将单条消息的预览选取上限 8 收为一个常量。服务端移除上下文前言读取失败时在租约栅栏之外直接写用户消息状态的语句，失败状态仍由栅栏内的回复发布事务写入；“已停止”收为常量。
- **为什么**：共享输入框、首页和澄清对话已处理输入法回车误发，但各自复制了判断，其余三个聊天输入仍会在 Safari 和部分输入法确认候选词时发出半截消息。无入口的弹窗与方法让后续修改需要维护不会运行的路径；栅栏外写入与 `3703ec90` 引入的执行栅栏约束不一致，在租约丢失后仍可能改写消息。
- **如何验证**：`composerKeys` 单测覆盖组字、229、修饰键；评论钉组件回归覆盖组字和 Shift+Enter 不提交（修复前失败）。Web 全量 vitest、`vue-tsc`、eslint；服务端 `go build ./...`、`go vet ./internal/services/`，以及 `go test ./internal/services/ -run 'Solo|Preamble|Turn|Conversation'`。
