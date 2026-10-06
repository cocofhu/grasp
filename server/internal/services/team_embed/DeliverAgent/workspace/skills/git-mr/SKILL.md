---
name: git-mr
description: 交付 Agent 的合入目标分支与 MR/PR 约定与检查项（不含任何凭据）
---

# Git / MR 操作检查清单

本 skill 补充交付 Agent 在多仓工作区中的版本管理约定。凭据由运行时环境或 Agent Studio 注入，**禁止**写入仓库或本 Agent 目录。

## 工作区布局

- 工作区根通常不是 git 仓库；每个仓在 `/root/workspace/<name>/`
- 对每个有改动的仓分别 `cd` 进其目录再执行 git 操作
- 多个仓可以使用同一工作分支名，但必须各自 push

## 推荐流程

1. 在目标仓 `git fetch origin`，切换到上游实现推送的工作分支（`git checkout <工作分支>`），不要在 `main` / 受保护分支上提交
2. 确认工作分支已在远端：`git status` 与 `git log origin/<工作分支>..HEAD` 无未推送提交；有则 `git push -u origin HEAD`
3. 对齐目标分支：`git fetch origin <目标分支>` 后把它合入工作分支（merge 或 rebase 均可），逐个解决冲突再提交、推送
4. 创建或复用合并请求：
   - GitLab（远端为 gitlab.com 或 `GITLAB_URL` 主机）：先 `glab mr list --source-branch <源> --target-branch <目标> --state opened` 查已有单，命中则复用；没有再 `glab mr create --source-branch <源> --target-branch <目标> --fill --yes`
   - GitHub（远端为 github.com 或 `GITHUB_URL` 主机）：先 `gh pr list --base <目标> --head <源> --state open`，没有再 `gh pr create --base <目标> --head <源> --fill`
   - create 报 already exists / No commits between 等幂等错误时不要直接判失败：查询 open 或已合并的单后据实说明
   - 托管商不支持自动建单时不要假装已建单
5. 调用 `set_merge_request`，每个有改动的仓一条：`repo`、`sourceBranch`、`targetBranch`、`provider`（github|gitlab|other）、`state`、`url`、`note`
   - 新建的单 `state=created`，复用已有 open 单 `state=reused`，已合入目标分支 `state=merged`
   - 托管商不支持自动建单或冲突无法机械解决时 `state=unsupported`，`url` 可省略，`note` 写原因与手动操作方式

## 检查项

- [ ] 每个有改动的仓都已合入目标分支并 push 到远端工作分支
- [ ] 每个有改动的仓都已创建或复用 MR/PR，并写入 `set_merge_request`
- [ ] `state` 不是 `unsupported` 的条目都有真实的 `url`
- [ ] 提交信息不包含密钥、Token、私钥或 `.env` 实值
- [ ] 未将 ACP/Git 凭据、Token、私钥写入 `agents/`、业务源码或任何明文文件；配置仅允许 `${...}` 占位模板
- [ ] 推送失败时先暴露错误原因，不静默重试掩盖问题

## 禁止事项

- **禁止密钥入库**：不得将 `GITLAB_TOKEN`、`GITHUB_TOKEN`、SSH 私钥、ACP Key 等写入仓库
- 不要 force push 到 main/master；不要跳过 hooks，除非任务明确要求
- 不要修改业务代码；冲突解决只做机械合并
