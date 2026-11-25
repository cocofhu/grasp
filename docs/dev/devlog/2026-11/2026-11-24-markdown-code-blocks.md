# 2026-11-24（Markdown 代码块高亮和复制）

- 日期：2026-11-24
- 范围：`web/src/lib/shared/markdown.ts`、`web/src/lib/shared/copyToClipboard.ts`、`web/src/styles/global.css`、`web/src/locales/`、`web/package.json`、`web/package-lock.json`、`web/THIRD_PARTY_LICENSES.md`、Markdown 单元测试和浏览器测试、`CHANGELOG.md`。
- 做了什么：共享 Markdown 渲染器按围栏里的语言标记应用 Highlight.js 高亮，颜色跟随应用主题。普通代码块的右上角提供复制按钮，长行沿代码区横向滚动。复制直接读取代码文本并保留缩进和换行。浏览器兼容复制路径会清理临时文本框并恢复原控件的焦点。没有语言标记、未识别语言和超长片段以纯文本显示，也可以复制。
- 为什么：聊天中的代码原先采用单色显示，复制时需要手动选中整段，长行尤其容易漏选。
- 如何验证：构建和 ESLint 通过。Web 全量单测 6195 项通过，行覆盖率 91.89%，本地测试超时设为 15 秒。Chromium 的五个 Markdown 用例核对了浅色和深色高亮、长行滚动、精确复制、键盘焦点恢复和 Mermaid 渲染。WebKit 核对了触屏复制。修复焦点前运行了 345 个关键浏览器用例，复跑后有 343 个通过，剩下两个会话用例在改动前的版本也失败。
