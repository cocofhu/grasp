# 2026-11-11（首页「全部会话」改为弹窗，修复点击白屏）

- 日期：2026-11-11
- 范围：`web/src/components/solo/HomeSoloPanel.vue`、`web/src/components/solo/SoloSessionList.vue`（新增）、`web/src/views/SoloView.vue` 及对应测试。
- 做了什么：首页 Solo「全部会话」改为在当前页打开弹窗，列出全部 Solo 会话，分「运行中 / 全部」两个标签，显示沙箱占用；点某个会话关闭弹窗并进入该会话。`/solo`（不带会话 id）不再自动跳转，改为显示同一份列表页，带「新建会话」入口。
- 为什么：#920 移除 Solo 页内嵌列表后，「全部会话」仍指向 `/solo`，名称与行为不符；之后 `/solo` 只在 watcher 里重定向，而会话列表是模块级共享状态，从首页进入时 `loaded` 已为 true，watcher 不触发，页面停在 `/solo` 一片空白。
- 如何验证：新增 `HomeSoloPanel.test.ts`（弹窗打开、地址不变、点会话后跳转并关闭）与 `SoloView.test.ts`（`/solo` 显示列表与标签切换、`/solo/:id` 打开会话）；本地用 SQLite 启动 server 并写入 5 条 mock 会话，在浏览器中复现修复前白屏，确认修复后弹窗与列表页行为；`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`、`npm run build` 通过。
