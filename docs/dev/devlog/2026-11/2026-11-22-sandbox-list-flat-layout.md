# 2026-11-22 沙箱列表页连续布局

- 日期：2026-11-22
- 范围：`web/src/views/SandboxListView.vue`
- 做了什么：移除统计卡片的独立边框、背景和装饰圆环，改成带垂直分隔线的指标带。状态筛选并入列表标题行，数据表格改为连续的上下边界，保留行内操作、移动端结构和状态颜色。
- 为什么：实际页面同时使用统计卡片、筛选卡片和表格卡片，形成多个嵌套容器，内容被切得过碎，和项目的边框优先风格不一致。
- 如何验证：通过亮色和暗色预览检查真实空列表与 9 条 Mock 数据列表，运行 `npx vitest run src/views/SandboxListView.test.ts`、`npx vue-tsc --noEmit`、`npm run lint -- --quiet` 和 `npm run build`。
