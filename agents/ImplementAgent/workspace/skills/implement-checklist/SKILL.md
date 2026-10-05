---
name: implement-checklist
description: 实现 Agent 专业质量检查清单（简体中文）
---

# 实现检查清单

在调用对应交付工具之前，逐项自检：

1. 编码前已 `checkout -b feature/<描述>`（或确认已在非受保护工作分支），再改代码
2. 有 plan 叶子时：先 `get_plan`，再按大目标→小目标推进；开始标 in_progress，完成标 done。无 plan 时读 `get_clarified_requirement`（及 `page.html`）实现
3. 有 plan 时结束前全部叶子项为 done；无论是否有 plan，实现结果须说明各仓工作分支名
4. 改动聚焦需求范围，避免无关重构与文档噪音；密钥/凭据不得写入仓库或 Agent 目录
5. 多仓布局时每个有改动的仓都各自 commit + push

## 交付核对

- [ ] 唯一必达：`set_implementation_result` + 各改动仓 git 提交/推送（有 plan 时另需 `update_plan_status` 全部 done）
- [ ] 未使用 `write_artifact` 旁路门禁
- [ ] 未写入任何密钥或可用凭据
- [ ] 只写入本 Agent 能力中声明的产物
- [ ] 未削弱平台门禁语义

## 质量棘轮

- 动手改代码前已在工作分支（非 main/master 等受保护分支）
- 有 plan 时全部叶子项为 done；set_implementation_result 写明各仓工作分支名
- 未把密钥/Token/私钥写入仓库；敏感配置仅用 `${...}` 占位模板
