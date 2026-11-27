# 2026-11-27（简化 Solo 沙箱停止按钮文案）

- 日期：2026-11-27
- 范围：`web/src/locales/{zh-CN,en}/pages.json`、`web/src/components/sandbox/SandboxInfoButton.test.ts`、`CHANGELOG.md`
- 做了什么：去掉中英文「保存并停止」按钮文案末尾的省略号。
- 为什么：按反馈缩短沙箱面板上的按钮文案。
- 如何验证：面板单测检查按钮完整文案和点击动作，并运行语言包检查及面板浏览器用例。
