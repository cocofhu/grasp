# 2026-11-16（清理沙箱启动进度的无用代码）

- 日期：2026-11-16
- 范围：`web/src/components/run/ClarifyBootLoader.vue` 及调用方（`ClarifyChat.vue`、`ClarifyMessageThread.vue`、`ReactConnectingState.vue`）、`web/src/locales/*/pages.json` 及对应测试
- 做了什么：
  - `ClarifyBootLoader` 删除已没有调用方的居中样式（`variant="center"`）：节点排队中的 `pending` 提示、每 2.6 秒自动前进的 5 步假进度和进度圆点。组件只保留聊天内的两种状态：启动中的清单（`starting`）和折叠的「环境已就绪」行（`done`），`variant` 属性一并去掉。
  - 删除只在居中样式里用到的文案：`clarifyBootLoader.pending` / `pendingHint` / `stepRuntime` / `stepQuestions`，以及 `stepSandbox` / `stepAcp` 的 `hint`。
  - 删除只覆盖居中样式的 `ClarifyBootLoader.test.ts`。
- 为什么：工作流节点和 Solo 会话的沙箱启动进度已经统一用 `BootChecklist`，按真实状态推进；旧的计时器进度不再有入口，留着只会混淆。
- 如何验证：`npx vitest run src/components/run src/components/solo src/locales src/views`（156 个文件、2095 个用例）通过，`vue-tsc --noEmit`、`eslint` 通过。
