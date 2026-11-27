# 2026-11-27（Solo 沙箱任务信息）

- 日期：2026-11-27
- 范围：`server/internal/{handlers,router,services}/`、`web/src/components/{sandbox,solo}/`、Solo API 客户端、文案和浏览器用例、`CHANGELOG.md`
- 做了什么：在沙箱「基本信息」展示会话参考、曾引用该会话的 Grasp 工作流运行、跨环境重建的累计 Token 和任务价值估算。按各模型输入、输出和缓存单价合计，标记不完整或未计价的用量。
- 为什么：在当前会话查看相关任务和用量，减少在对话、工作流和 Token 分析之间切换。
- 如何验证：接口测试覆盖会话权限、关联运行去重、重建后的用量累加、部分计价和零用量。25 项相关 Web 单测和 12 项沙箱面板 Playwright 用例通过。
