---
name: git-mr
description: 实现 Agent 的分支/提交/push/MR 约定与检查项（不含任何凭据）
---

# Git / MR 操作检查清单

本 skill 补充实现 Agent 在多仓工作区中的版本管理约定。凭据由运行时环境或 Agent Studio 注入，**禁止**写入仓库或本 Agent 目录。

## 工作区布局

- 工作区根通常不是 git 仓库；每个仓在 `/root/workspace/<name>/`
- 对每个有改动的仓分别 `cd` 进其目录再执行 git 操作
- 多个仓可以使用同一工作分支名，但必须各自 push

## 推荐流程

1. 在目标仓创建或切换到工作分支（如 `feature/<简短描述>`），避免直接推 `main` / 受保护分支
2. 实现并完成本地验证后：`git add` 相关文件 → `git commit`（语义化说明 why）→ `git push -u origin HEAD`
3. 对齐目标分支：`git fetch origin <目标分支>` 后把它合入工作分支（merge 或 rebase 均可），逐个解决冲突再提交、推送
4. 创建或复用合并请求（工作流目标要求开 MR/PR 时）：
   - GitLab（远端为 gitlab.com 或 `GITLAB_URL` 主机）：先 `glab mr list --source-branch <源> --target-branch <目标> --state opened` 查已有单，命中则复用；没有再 `glab mr create --source-branch <源> --target-branch <目标> --fill --yes`
   - GitHub（远端为 github.com 或 `GITHUB_URL` 主机）：先 `gh pr list --base <目标> --head <源> --state open`，没有再 `gh pr create --base <目标> --head <源> --fill`
   - create 报 already exists / No commits between 等幂等错误时不要直接判失败：查询 open 或已合并的单后据实说明
   - 托管商不支持自动建单时不要假装已建单，在实现结果里说明
5. 在 `set_implementation_result` 中写明各仓工作分支名；MR/PR 地址写进 `node_complete` 的 `outputs.mr_url`

## 检查项

- [ ] 每个有改动的仓都已 commit 且 push 到远端工作分支
- [ ] 需要 MR/PR 时已创建或复用，并把地址写入 `outputs.mr_url`
- [ ] 提交信息不包含密钥、Token、私钥或 `.env` 实值
- [ ] 未将 ACP/Git 凭据、Token、私钥写入 `agents/`、业务源码或任何明文文件；配置仅允许 `${...}` 占位模板
- [ ] 未擅自修改 `.github/workflows` / `.gitlab-ci.yml` / `Dockerfile`（除非计划明确要求）
- [ ] 推送失败时先暴露错误原因，不静默重试掩盖问题

## 禁止事项

- **禁止密钥入库**：不得将 `GITLAB_TOKEN`、`GITHUB_TOKEN`、SSH 私钥、ACP Key 等写入仓库
- 不要 force push 到 main/master；不要跳过 hooks，除非任务明确要求
- 不要提交 `dist/`、构建产物、本地密钥文件或与计划无关的大文件
