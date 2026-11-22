# 2026-11-22（移动端对话输入框不再被导航球遮挡，操作按钮不再换行）

- 日期：2026-11-22
- 范围：`web/src/lib/shared/mobileNav.ts`、`web/src/components/shell/AppShell.vue`、`web/src/components/shell/FloatingNavBall.vue`、`web/src/components/chat/ConversationWorkspace.vue`、`web/src/components/chat/ChatComposer.vue`、`web/src/components/solo/SoloSessionPane.vue`、`web/e2e/conversation-workspace.spec.ts`
- 做了什么：
  - 移动端（≤767px）的对话工作区标题栏最左侧显示导航按钮，点击打开 AppShell 的移动端抽屉；标题栏在场时右下浮动导航球隐藏。没有 AppShell 的宿主（公开分享、嵌入）不显示该按钮，导航球行为不变。
  - `ChatComposer` 工具栏左侧组改为 `flex-1 min-w-0`，内容过宽时收缩、模型名截断，发送 / 停止按钮保持在同一行。
  - Solo 窄面板（容器 ≤480px）的「停止环境」「恢复环境」只显示图标，保留 `title` 和 `aria-label`。
  - 移动端（≤767px）输入框不显示「停止环境」；停止入口保留在导航抽屉 Solo 会话的长按菜单。「恢复环境」仍显示，沙箱暂停时需要它。
- 为什么：浮动导航球固定在左下角，压在所有对话页输入框的左下方；工具栏 `flex-wrap` 在模型名和「停止环境」放不下时把停止 / 发送挤到第二行，输入框变高且按钮位置跳动。
- 如何验证：`vitest`（`src/components/shell`、`src/components/chat`、`src/components/solo`，新增导航球让位与标题栏按钮用例）、`vue-tsc --noEmit`、`eslint`；新增 e2e「390px Solo keeps composer actions on one row and moves navigation into the header」通过，并截图确认。同文件既有用例「mobile workflow links reopen chat…」在未改动的 main 上同样失败，与本次无关。
