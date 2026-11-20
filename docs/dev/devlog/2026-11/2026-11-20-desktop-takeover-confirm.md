# 2026-11-20（Agent 运行中接管云电脑先确认）

- 日期：2026-11-20
- 范围：`web/src/components/run/NovncPreviewPanel.vue`、`web/src/components/run/NovncPreviewPanel.test.ts`、`web/src/locales/zh-CN/pages.json`、`web/src/locales/en/pages.json`
- 做了什么：
  - 「接管」按钮（`toggleControl`）和点击画面接管（`onScreenPointerDown`）在 `desktop.agentBusy`（桌面状态 `agentActive`，Agent 处于回合中）为真时，先打开 `AppModal` 确认框；确认后才把 `control` 置为 true 并聚焦远程画面，取消则保持仅观看。交还不需要确认。
  - 点击画面触发确认时，这次按下的后续事件仍按原逻辑吞掉（拆出 `swallowTakeoverGesture`），不会传到 noVNC。
  - 新增文案 `pages.appPreview.desktop.takeoverConfirmTitle` / `takeoverConfirmBody` / `takeoverConfirmOk`（中英文）。
- 为什么：接管期间桌面 MCP 会拒绝 Agent 的键鼠工具，Agent 还在输出时误点画面就会打断它当前的操作，原来只有悬停提示。
- 如何验证：`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`（569 个文件全部通过，Lines 91.82%）；新增用例覆盖按钮和点击画面两种入口的确认、取消，以及 Agent 空闲时不弹窗。
- 已知限制：`agentActive` 只在云电脑（desktop 镜像）上报，浏览器镜像和公开分享链接没有该状态，接管不弹窗，与原来一致。状态每 3 秒轮询一次，Agent 刚开始或刚结束回合的几秒内判断可能滞后。
