# 2026-11-15（设置式页面内层滚动区恢复 4px 滚动条）

- 日期：2026-11-15
- 范围：`web/src/styles/global.css`；`web/src/styles/globalScrollbar.test.ts`。
- 做了什么：在 `@supports selector(::-webkit-scrollbar)` 块里给 `.settings-shell-content`（`AppShell.vue` 设置式路由的内容容器）加上 `scrollbar-width: initial` 和 `scrollbar-color: initial`，与已有的 `.board-workbench` 规则并列；`AppShell.vue` 的 `.settings-main-scroll` 不动。
- 为什么：`.settings-main-scroll` 声明的 `scrollbar-color` 会被内层滚动区（如 `ArtifactList` 的 `.scroll-area`）继承，Chromium 121 及以上版本随之忽略 `::-webkit-scrollbar` 规则，内层滚动区画出带箭头的原生宽滚动条。在内容容器上切断继承后，内层滚动区回到全局 4px、无箭头、悬停才显示的规则。
- 如何验证：
  - `web/`：`npx vitest run --maxWorkers=2 src/styles/globalScrollbar.test.ts src/components/shell/AppShell.test.ts`（26 passed）。
  - Chromium 149 里打开 `/artifacts`、`/notifications`、`/agents`、`/sandboxes`、`/settings`，浅色和深色主题下在内容容器里插入可滚动的 `.scroll-area` 测量：滚动条宽 4px；去掉重置（让容器继承 `scrollbar-color`）时为 15px 原生滚动条。
