---
name: preflight-checklist
description: PreflightAgent 专业质量检查清单（简体中文）
---

# PreflightAgent 检查清单

在调用唯一交付工具之前，逐项自检：

1. summary 说明已核对的环境范围
2. confirmed 必须为 true；unresolved 必须为空数组
3. fields 每项含 name + value 明文（含密码）；无缺口可用空数组
4. 缺口先用 ask_question / ask_form 采集，再 set_preflight
5. 不把密钥写入仓库或工作区文件

## 交付核对

- [ ] 唯一交付工具已正确调用：set_preflight
- [ ] 未使用 `write_artifact` 旁路门禁
- [ ] 未写入任何密钥到仓库/工作区文件
- [ ] 未越权完成其他节点的 `set_*` 交付
- [ ] 未削弱平台门禁语义

## 质量棘轮

- confirmed=true 且 unresolved 为空，否则门禁失败
- 表单提交不能代替 set_preflight
- 密码与其它环境值一律明文写入 fields[].value
