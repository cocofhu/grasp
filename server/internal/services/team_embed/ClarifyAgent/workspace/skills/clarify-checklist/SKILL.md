---
name: clarify-checklist
description: 需求澄清 Agent 专业质量检查清单（简体中文）
---

# 需求澄清检查清单

在调用对应交付工具之前，逐项自检：

1. 凡未决点都已用 ask_question 让用户拍板；信息已充分时可直接收束，不必为提问而提问
2. 必填段齐全：`title` / `summary` / `background` / `goals` / `in_scope` / `out_of_scope` / `assumptions` / `dependencies` / `constraints`
3. 每条 functional_requirement 含 `detail` 与可验证的 `acceptance_criteria`（≥1）；`priority` 为 must|should|could
4. assumptions / dependencies / constraints 与用户确认一致；无实质内容时写明「无额外…（已与用户确认）」，不得省略键
5. 未写入排期/里程碑，未写入技术选型或架构方案
6. 结论可被后续 Research/Proposal/Plan 直接消费，无含糊代词与未定义缩写

## 交付核对

- [ ] 最终必须调用 set_clarified_requirement，且 open_questions 为空；凡未决点须先经 ask_question，不得把未决项留在结论里
- [ ] 未使用 `write_artifact` 旁路门禁
- [ ] 未写入任何密钥或可用凭据
- [ ] 只写入本 Agent 能力中声明的产物
- [ ] 未削弱平台门禁语义

## 质量棘轮

- open_questions 为空，且每个 functional_requirement 都能被下游写成可测验收点
- out_of_scope / constraints / assumptions / dependencies 与用户拍板一致；若用户未确认范围边界，不得自行假定
- 交付中无方案选型、技术栈拍板或实现细节（澄清边界说明除外）；无排期字段
