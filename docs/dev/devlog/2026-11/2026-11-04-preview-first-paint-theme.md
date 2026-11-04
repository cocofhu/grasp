# 2026-11-04（预览刷新首屏优先应用宿主主题）

- 日期：2026-11-04。
- 范围：`web/index.html`、`web/src/lib/shared/{theme.ts,themeBoot.test.ts}`、`web/e2e/shell-loading.spec.ts`。
- 做了什么：在 HTML head 的同步脚本中优先读取 embed URL 的主题，其次读取按预览地址隔离的 sessionStorage 主题缓存，再回退到账号偏好；首屏背景、文字和 color-scheme 立即一致。加载屏使用对应深浅色变量。运行时主题模块继承首屏 override，宿主主题切换更新同一预览缓存，保持全局 `grasp-theme` 不变。
- 为什么：加载屏硬编码为深色，而宿主主题直到 Vue 挂载才覆盖，导致浅色 Page Harness 卡片内先闪现深色加载页；URL 片段清理后直接刷新 iframe 也会丢失主题。
- 如何验证：单测验证首屏到运行时的深／浅色交接、预览缓存隔离、账号偏好保持和存储被阻止时的降级。Playwright 使用生产 HTML 并阻止应用模块加载，验证首屏与去掉片段后的刷新都保持正确主题，另一个节点不会继承前一节点的主题。4 项首屏主题单测、Web 全量 6228 项单测、行覆盖率 91.71%、lint、类型检查和生产构建通过；34 项相关 Playwright 用例、文档构建与审计通过。PR CI 随提交重新验证。
