# 2026-11-16（Solo 提问卡片复用澄清卡片）

- 日期：2026-11-16
- 范围：`web/src/lib/inbox/{clarifyAnswers,useClarifyChat}.ts`、`web/src/components/run/{ClarifyQuestionDeck,ClarifyFormCard,ClarifyChat,ClarifyMessageThread}.vue`、`web/src/components/solo/SoloSessionPane.vue`（删除 `SoloAskCard.vue`）、`web/src/styles/chat.css`、`web/src/locales/{zh-CN,en}/pages.json`，以及对应测试。
- 做了什么：把 `useClarifyChat` 里 ask_question 的选择状态（单题翻页、推荐预选、「其他」勾选、回复文本拼接）和 ask_form 的填写状态抽成 `useQuestionDeck` / `useFormDraft`；把 `ClarifyChat` 内联的单题卡片和表单卡片抽成 `ClarifyQuestionDeck`、`ClarifyFormCard`。工作流对话和 Solo 都用这两个组件，删除 Solo 自己的 `SoloAskCard`。`ClarifyMessageThread` 的只读表单也改用 `ClarifyFormCard`；`.clarify-request` 两处重复的 scoped 样式移到 `chat.css`。顺带删掉无用逻辑：`pick`（`toggle` 的别名）、`otherAnswered`（并入 `answered`）、`submitRecommended`（组件内先 `applyRecommended` 再提交）、`formNotice` 及文案 `formEmptyNotice`（提交按钮在未填完时已禁用，提示永远不会出现）。`useFormDraft` 的默认值改为挂载时即填入。
- 为什么：#1213 给 Solo 另写了一套简陋的提问卡片（所有题平铺、小标签、每题一个输入框），与工作流的卡片设计不一致，也要维护两份逻辑。
- 如何验证：`clarifyAnswers.test.ts`（回复格式、「其他」替换单选、跳过未答题、按题目 id 重置、表单默认值与完整性）、`useClarifyChat.actions.test.ts`、`useClarifyChat.artifactGuard.test.ts`、`SoloSessionPane.test.ts`；`vue-tsc --noEmit`、eslint、全量 vitest。
- 未做：历史轮次的只读选择题在 `ClarifyChat`（只列带 demo 的选项）和 `ClarifyMessageThread`（列全部选项并打勾）展示方式不同，未合并。
