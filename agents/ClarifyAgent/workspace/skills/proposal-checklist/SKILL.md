---
name: proposal-checklist
description: 候选方案产物质量检查清单（简体中文）
---

# 候选方案检查清单

在调用对应交付工具之前，逐项自检：

1. 只有存在至少两个方向不同、取舍有意义的候选时才写方案集；推荐方案最多 1 个且理由充分
2. context / decision_drivers 与上游澄清、调研对齐
3. 每个方案含 pros/cons 或 tradeoffs，effort/risk 估计合理
4. 不提前写成全局计划（那是 Plan 的职责）
5. 方案可落地到现有仓库约束，不引入已明确 out_of_scope 的能力

## 交付核对

- [ ] 已调用：set_proposals
- [ ] 未使用 `write_artifact` 旁路门禁
- [ ] 未写入任何密钥或可用凭据
- [ ] 只写入本 Agent 能力中声明的产物
- [ ] 未削弱平台门禁语义

## 质量棘轮

- proposals 至少 2 个；recommended 至多 1 个，且能用 decision_drivers 解释为何胜出
- 每个方案具备可比较维度（pros/cons 或 tradeoffs，以及 effort/risk）
- 未把方案写成 goals/subgoals 全局计划，也未引入澄清已排除的能力
