# 2026-11-05（移动端浏览器缩放合约）

- 日期：2026-11-05
- 范围：`web/src/styles/global.css`、`web/src/styles/mobileFormZoom.test.ts`、`web/e2e/mobile-browser-contract.spec.ts`、`web/e2e/workspace-main.ts`、`web/playwright.config.ts`、`web/package.json`、`.github/workflows/ci-web.yml`、`CONTRIBUTING.md`
- 做了什么：窄屏下将可编辑的 `input`、`textarea`、`select` 提升到至少 16px，并固定移动端文本尺寸调整为 100%，保留用户手势缩放；Playwright 增加 Pixel/Chromium 与 iPhone/WebKit 项目，覆盖路由表中的工作区页面、动态页面代表路径和公共审批壳，检查加载时及聚焦控件时的页面缩放、横向溢出和脚本错误；将 WebKit 依赖安装纳入 Web CI，并提供移动交互矩阵命令。
- 为什么：iOS Safari 会在聚焦字体小于 16px 的表单控件时自动放大页面。全局 `body` 默认 14px，页面内还有大量 11–14px 的控件，导致多个页面出现看似随机的放大。
- 如何验证：`vitest run src/styles/mobileFormZoom.test.ts`、`npm run lint`、`npx vue-tsc --noEmit`；移动路由合约 Chromium/WebKit 58 个用例通过；移动关键交互矩阵 Chromium/WebKit 168 个用例通过。
