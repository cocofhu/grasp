# 2026-11-27（精简 Solo 沙箱任务摘要）

- 日期：2026-11-27
- 范围：`web/src/components/sandbox/`、`web/e2e/sandbox-info.spec.ts`、`CHANGELOG.md`
- 做了什么：累计 Token 和工作流名称改成上下两行，与环境信息使用相同的字号和右对齐方式。两组信息之间用细线分隔。
- 为什么：让展开后的摘要与面板里其他字段看起来统一。
- 如何验证：面板单测及 Chromium 用例覆盖桌面深浅主题、窄屏排列、工作流链接和 Token 悬浮明细。
