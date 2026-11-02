# 2026-11-02（清理待办页控制台警告，#815）

- 日期：2026-11-02。
- 范围：`web/src/locales/{zh-CN,en}/pages.json`、`web/src/locales/locales.compile.test.ts`、`web/src/components/canvas/inspector/VariablesField{.vue,.test.ts}`、`web/src/views/GatesInboxView.vue`、`web/vite-plugins/stripNovncSecureContext.ts`、`web/src/lib/shared/stripNovncSecureContext.test.ts`。
- 做了什么：
  - `[intlify] Detected HTML`：issue 里的 `gatesInbox.subtitleHtml` 早在 #819 就不再渲染，删掉这个废弃键，同时删掉同样没人用的 `inspector.actions.help`、`inspector.cases.help`。仍在用的 `inspector.variables.help` 改成 `<I18nT>` 插槽渲染 `<code>` / `<b>`，文案里不再带 HTML。vue-i18n 把任何 `<word>` 都当 HTML，所以英文的 `<name>` / `<model>` 占位改成 `NAME` / `MODEL`，中文两处 `<name>` 改成 `<仓库名>`（与其他中文 `<名字>` 一致）。`locales.compile.test.ts` 新增一条：所有语言包都不能命中 vue-i18n 的 HTML 检测。`processedBanner` 本来就是纯文本，`v-html` 改成插值。
  - `Extraneous non-props attributes (class)`（`ClarifyChat`）：10-22 已加 `inheritAttrs: false` 并把 `$attrs` 绑到根节点，当前代码不再复现，本次未改。
  - `noVNC requires a secure context (TLS)`：`stripNovncSecureContext` 只在构建产物里生效。开发模式下 noVNC 走 esbuild 预构建，模块 id 是 `.vite/deps/@novnc_novnc_lib_rfb__js.js`，注释也被去掉，id 判断和正则都对不上。现在 id 同时匹配两种路径，正则里注释改为可选。
- 为什么：审批页每次渲染都刷一串警告，掩盖真正的问题。
- 如何验证：本地 `vite` 开发服务器取 `/node_modules/.vite/deps/@novnc_novnc_lib_rfb__js.js`，修改前第 14803 行有该 `Log.Error`，修改后已无 `isSecureContext`。web `npm run lint`（0 error）、`npx vue-tsc --noEmit`、`vitest --coverage`（565 个文件、6140 个用例，行覆盖 92.06%）、`npm run build`（含两个 assert 脚本）通过。新增 `VariablesField.test.ts` 断言帮助文字、`<code>` / `<b>` 结构和无 HTML 警告。
