# 2026-11-22 仓库页面视觉刷新

- 日期：2026-11-22
- 范围：`web/src/views/ProjectsView.vue`、`web/src/components/settings/SettingsStatStrip.vue`、`web/src/locales/en/pages.json`、`web/src/locales/zh-CN/pages.json`
- 做了什么：重排仓库页面的头部、搜索、统计区和仓库卡片。卡片现在展示仓库来源、默认分支和最近使用状态，主操作使用贴合设置页的描边按钮。搜索无匹配时增加空结果提示，统计卡片改为自适应列数。
- 为什么：原页面在宽屏下留下大块空白，卡片的主要操作和仓库信息层级不够清晰。
- 如何验证：`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --run src/locales/locales.compile.test.ts src/components/settings/settingsKit.test.ts`、`npm run build`
