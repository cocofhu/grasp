# 2026-11-15（修复 main 上已有的桌面 Playwright 失败，首页单行附件不再滚动）

- 日期：2026-11-15
- 范围：`web/e2e/`（13 个 spec / harness / mock）、`web/playwright.config.ts`、`web/src/views/DashboardView.vue`、`web/src/views/DashboardView.fill.test.ts`、`CHANGELOG.md`
- 做了什么：
  - 过期断言跟上已合入的产品改动：Agent 工作目录输入框（#944）改测「继承默认」开关和模型配置选择；侧栏「历史执行」链接改按页面查找；hover-ink 改用「看板」入口并补 `/board` 路由；运行列表分页改测 `nav.pagination`；工具栏高度测试从已无工具栏的 gates inbox（#819）改到运行列表，并支持 `?theme=`；控制台标签改断言 `data-active`；产物预览文件头改用 `artifact-preview-file-row`；新建工作流前 mock 四个引导 Agent（#942）；inbox 预算测试在 `apiPrimaryProducts` 无离线兜底（#747）后返回产物并先打开产物面板。
  - noVNC mock 只在 WebSocket open 之后才发 `connect`，`disconnect` 后不再发；此前提前发 `connect` 会让 `restoreViewport('connected')` 判定断线并无限重连。
  - Playwright 在未设置 `LANG`/`LC_ALL` 时给浏览器传 `LANG=C.UTF-8`，否则 Chromium 把中文下载文件名报成 `download`。
  - `blob-404-cache` 恢复阶段先等成功 UI，再允许重试 GET（带 `?_r=`）之外多一次普通 GET，消除 3 次 / 4 次的抖动。
  - 首页 #800 复发：c59ae80c 在附件缩略图下加了 `ChatAttachmentDetails`，单行附件高约 110.5px，超出原 72px 顶部余量。底部 40px（移动端 2.5rem）从 `padding-bottom` 改成可收缩的 `::after`（`flex: 0 1 40px; min-height: 0`），总余量 112px；无附件时视觉不变，顶部下限 48px / 36px 不动（保护 yume 耳朵）。
  - 删除死代码：`toolbar-control-height` 里 gates inbox 的 `/pending-nodes` mock；e2e helper 中只在本文件使用的 `isBackendApiRequest`、`ONBOARDING_SUPPRESS_KEY` 去掉 `export`。knip 报告的其余「未使用」项（harness 入口、vite.e2e.config 引用、`<style src>` CSS、预览页 `cli-model-select` / `node-model-picker`）核实为在用，保留；`src/` 无未使用文件。
- 为什么：#1121 列出的 main 上桌面全量失败中仍有 20 个未修复，多数是产品已改而断言没跟上，少数是 mock / 环境问题；#800 是真实布局回归。
- 如何验证：`npm run test:e2e -- --project=desktop` 全量；改动 spec `--repeat-each=3`；`vitest run`（576 文件 / 6258 用例）；`vue-tsc --noEmit`；`eslint .`。
