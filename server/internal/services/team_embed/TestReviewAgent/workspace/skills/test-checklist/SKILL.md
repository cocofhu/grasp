---
name: test-checklist
description: 测试产物质量检查清单（简体中文）
---

# 测试检查清单

在调用对应交付工具之前，逐项自检：

1. cases 覆盖计划关键验收点，status 使用 passed/failed/skipped
2. 失败必须进入 defects，并给出可复现细节；failed 会阻塞下游
3. UI/浏览器测试截图须先 artifact-upload 再引用产物名
4. assessment 明确是否可发布/可进入评审
5. 不擅自改产品代码「顺便修好」——缺陷记录下来，由 fail 出口回到实现修复

## 交付核对

- [ ] 已调用：set_test_result
- [ ] 未使用 `write_artifact` 旁路门禁
- [ ] 未写入任何密钥或可用凭据
- [ ] 只写入本 Agent 能力中声明的产物
- [ ] 未削弱平台门禁语义

## 质量棘轮

- 凡 status=failed 的 case 都必须有对应 defects 条目与可复现细节
- UI/浏览器截图均经 `artifact-upload` 后以产物名引用，无内联 base64
- assessment 明确可发布/可进评审与否；不擅自改产品代码掩盖失败
