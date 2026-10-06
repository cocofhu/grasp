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

## 分支、提交与推送

凭据由运行时环境或 Agent Studio 注入，**禁止**写入仓库或本 Agent 目录。

- 工作区根通常不是 git 仓库；每个仓在 `/root/workspace/<name>/`，对每个有改动的仓分别 `cd` 进其目录再执行 git 操作
- 多个仓可以使用同一工作分支名，但必须各自 push
- 在目标仓创建或切换到工作分支（如 `feature/<简短描述>`），避免直接推 `main` / 受保护分支
- 完成本地验证后：`git add` 相关文件 → `git commit`（语义化说明 why）→ `git push -u origin HEAD`
- 不创建 MR/PR：合入目标分支与建单由下游交付节点负责
- 不要 force push 到 main/master；不要跳过 hooks，除非任务明确要求
- 不要提交 `dist/`、构建产物、本地密钥文件或与计划无关的大文件
- 推送失败时先暴露错误原因，不静默重试掩盖问题

## 交付核对

- [ ] 唯一必达：`set_implementation_result` + 各改动仓 git 提交/推送（有 plan 时另需 `update_plan_status` 全部 done）
- [ ] 每个有改动的仓都已 commit 且 push 到远端工作分支
- [ ] 提交信息不包含密钥、Token、私钥或 `.env` 实值
- [ ] 未擅自修改 `.github/workflows` / `.gitlab-ci.yml` / `Dockerfile`（除非计划明确要求）
- [ ] 未使用 `write_artifact` 旁路门禁
- [ ] 未写入任何密钥或可用凭据
- [ ] 只写入本 Agent 能力中声明的产物
- [ ] 未削弱平台门禁语义

## 质量棘轮

- 动手改代码前已在工作分支（非 main/master 等受保护分支）
- 有 plan 时全部叶子项为 done；set_implementation_result 写明各仓工作分支名
- 未把密钥/Token/私钥写入仓库；敏感配置仅用 `${...}` 占位模板
