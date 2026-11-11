# 2026-11-11 Solo 上限弹窗显示真实占用

- 日期：2026-11-11
- 范围：`server/internal/{handlers/solo.go,handlers/solo_usage_test.go,services/sandbox_solo.go}`、`web/src/{components/solo/SoloLimitModal.vue,components/solo/SoloLimitModal.test.ts,components/solo/HomeSoloPanel.vue,lib/solo/useSoloSessions.ts,lib/api/apiTypes.ts,locales/*/pages.json}`、`CHANGELOG.md`
- 做了什么：`GET /api/solo/sessions` 新增 `usedSandboxes`，取值与准入判断使用同一个 `countSoloCompute`。上限弹窗显示「已用 X / 上限」，除自己启动中/运行中的会话外，也列出仍绑定环境的异常会话，异常会话的操作是关闭会话；列表之外的占用显示为「另有 N 个」。首页计数取全局已用与本人运行数的较大值。
- 为什么：名额按全工作区计算，还包括异常、清理中和未确认停止的绑定，但弹窗只列出本人运行中的会话。线上出现过"已达上限但只看到 5 个"的情况，用户无法判断占用来源。
- 如何验证：新增 handler 测试（列表只返回本人会话，`usedSandboxes` 统计所有成员）和弹窗组件测试（显示已用、列出绑定环境的异常会话、汇总剩余、异常会话走关闭而不是停止环境）。server handlers/services 测试、web lint、vue-tsc、全量 vitest 与 build 通过。
