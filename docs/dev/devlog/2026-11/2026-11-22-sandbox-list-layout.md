# 2026-11-22 沙箱列表页管理台布局

- 日期：2026-11-22
- 范围：`web/src/views/SandboxListView.vue`、`web/src/locales/zh-CN/pages.json`、`web/src/locales/en/pages.json`、`web/vite.e2e.config.ts`
- 做了什么：为沙箱列表增加运行概览卡片、状态筛选和更清晰的表格层级，保留详情弹窗、控制台、停止、销毁和空闲回收行为，并补充中英文文案。复核亮色主题后移除页面级阴影，让卡片回到项目的边框优先风格。E2E 控制面新增 `fixture=rich` 入口，提供 9 条包含运行中、对话中、创建中、已停止和启动失败的 mock 沙箱数据，并支持预览内的停止、销毁和回收操作。
- 为什么：原页面将所有信息压在单一表格中，状态和操作的优先级不够清晰，移动端也缺少快速筛选入口。
- 如何验证：`npx vitest run src/views/SandboxListView.test.ts`、`npx vue-tsc --noEmit`、`npm run lint -- --quiet`、`npm run build`、`E2E_PORT=5175 npx playwright test e2e/sandbox-list-isolation.spec.ts --project=desktop`。
