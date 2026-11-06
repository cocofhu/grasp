# 2026-11-06（保留直连预览的应用 CSP）

- 日期：2026-11-06
- 范围：`sandbox-gateway/sandbox/internal/previewinject`、`web/public/preview-pick.js`、预览脚本源码及同步副本、沙箱说明、`CHANGELOG.md`
- 做了什么：按 HTML token 定位 CSP meta，保留应用策略。预览脚本、带 nonce 的 Grasp 样式、同域票据查询和当前运行的抽屉路径获得限定许可。含 CSP sandbox 的 HTML 原样转发。
- 为什么：共享预览中的应用策略会拦截工具栏样式、票据查询和抽屉。保留策略可以继续隔离不可信 HTML，也能避免误改应用脚本中的 meta 字面量。
- 如何验证：沙箱 Go 测试、覆盖率门槛和预览上游切换 smoke 通过。用真实注入器和 Chromium 检查直连与转发地址，确认工具栏、跨源抽屉、Live 与页面操作样式在限制性 CSP 下正常加载，应用脚本字面量、CSP sandbox 和防嵌入策略保持有效。
