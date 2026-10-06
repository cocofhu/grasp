---
name: plan-checklist
description: 计划产物质量检查清单（简体中文）
---

# 计划检查清单

在调用对应交付工具之前，逐项自检：

1. 仅两级：goals[] → subgoals[]，subgoals 为叶子且不再嵌套
2. 小目标粒度适合 Implement 逐项 update_plan_status
3. 覆盖澄清验收点，无遗漏关键交付
4. 不写实现代码、不改仓库
5. 标题清晰可追踪，避免含糊「相关改动」类条目

## 交付核对

- [ ] 已调用：set_plan
- [ ] 未使用 `write_artifact` 旁路门禁
- [ ] 未写入任何密钥或可用凭据
- [ ] 只写入本 Agent 能力中声明的产物
- [ ] 未削弱平台门禁语义

## 质量棘轮

- 结构严格两级：goals → subgoals，且 subgoals 为叶子（无更深嵌套）
- 每个叶子小目标可被 Implement 单独标 in_progress/done，标题可追踪
- 计划覆盖澄清关键验收点，无「相关改动」类含糊条目
