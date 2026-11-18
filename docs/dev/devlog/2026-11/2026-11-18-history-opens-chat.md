# 2026-11-18（历史执行点开进对话，右键可进工作流视图）

- 日期：2026-11-18
- 范围：`web/src/lib/run/recentRuns.ts`、`web/src/lib/run/useRunLive.ts`、`web/src/components/shell/SidebarWorkspaceLists.vue` 及对应测试、`web/e2e/workspace.spec.ts`
- 做了什么：侧栏「历史执行」里已结束的运行（已完成 / 失败 / 已取消）链接改到 `/runs/:id/live`。对话页打开已结束的运行时不再跳到运行详情，而是只读展示对话：定位到 `?node=` 指定的节点或最后一个节点，之前的节点折叠在上方，底部显示运行结束状态；没有任何对话的运行仍回到运行详情。工作流运行的行支持右键（触屏长按 500ms，移动超过 8px 取消），菜单只有「工作流视图」，打开 `/runs/:id?view=canvas` 并保留 `wf` / `tag`；长按结束时的那次点击不触发跳转。Solo 会话没有这个菜单。
- 为什么：用户希望点开历史记录看到的是对话，画布作为可选入口。
- 如何验证：`npx vitest run src/lib/run src/components/shell`、`npx vue-tsc --noEmit`；在浏览器中点开、右键、长按「历史执行」里已结束的运行。
