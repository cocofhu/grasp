# 2026-11-27（将 Solo 任务计价明细移入悬浮提示）

- 日期：2026-11-27
- 范围：`web/src/components/sandbox/SandboxInfoButton.vue`、相关文案和测试、`CHANGELOG.md`
- 做了什么：任务价值一行默认显示金额。悬浮在金额上或键盘聚焦时，可以查看已计价和未计价的 Token 数。
- 为什么：减少基本信息里的附加文案，计价覆盖情况可以按需查看。
- 如何验证：面板单测和 Chromium 用例检查默认金额、明细数值、深浅主题和 Escape 关闭提示。
