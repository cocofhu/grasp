---
name: git
description: 沙箱内 git clone/push 与合并请求（GitLab glab / GitHub gh；HTTPS 与 SSH 通用凭据）
---

# Git 操作指引

沙箱 startup.sh 按 `repo_url` scheme 注入凭据:

| 场景 | 环境变量 | 说明 |
|------|----------|------|
| GitHub HTTPS | `GITHUB_TOKEN` | `https://github.com/...` |
| 自建 GitHub/GHE | `GITHUB_TOKEN` + `GITHUB_URL` | URL 的 scheme+host 须与 repo 主机一致 |
| GitLab / 自建 GitLab | `GITLAB_TOKEN` + `GITLAB_URL` | 例: `GITLAB_URL=https://git.example.com`; 未配时从 repo_url 推导 |
| 任意 SSH 托管商 | `GIT_SSH_PRIVATE_KEY` + `GIT_SSH_KNOWN_HOSTS` | 必填 known_hosts,禁止 accept-new |

Gitea/Bitbucket 等 HTTPS 不支持 Token 注入,请改用 SSH。

## 工作分支 + push

分支名固定为 `<type>/<topic>-<运行短ID>`:`type` 按需求 work_kind 取 `feat`(feature)/ `fix`(bug)/ `chore`(other;纯重构 `refactor`、纯文档 `docs`);`topic` 为 2–5 个英文小写单词的连字符主题;运行短 ID 取 `${GRASP_RUN_ID#run-}`。

```bash
cd /root/workspace/<name>
git checkout -b feat/<topic>-${GRASP_RUN_ID#run-}
# ... 实现改动,跑通测试 ...
git add <改动的文件>   # 点名文件,不要 git add -A / git add .
git commit -m "<语义化提交信息>"
git push -u origin HEAD
```

## 合并请求（MR/PR）— 按托管商分流

`submit_mr` 步骤顺序与 DefaultMRContract 一致:**解决冲突 → 推送源分支 → list open → create（仅当无 open）→ 解析幂等错误 → list merged/view → `node_complete(outputs.mr_url)`**。

选型规则（按远端主机与环境变量,不按 Token 有无或 CLI 轮询）:

- **GitLab**（gitlab.com,或与 `GITLAB_URL` 主机一致）→ `glab`
- **GitHub**（github.com,或与 `GITHUB_URL` 主机一致,含 GHE）→ `gh pr`
- **匹配不上**（如 Gitea）→ 不支持自动建单:仍须完成冲突解决与 `git push`,再 `node_complete(status=failed)`;摘要须含「冲突已解决」「源分支已推送」及建单/CLI/托管商原因

### 幂等闭环（gh / glab 对称）

1. **先 list open**:命中同源→目标的 open 单 → 复用 Web URL,跳过 create,`node_complete(status=success, outputs.mr_url=该 URL)`。
2. **无 open 再 create**:`glab mr create` / `gh pr create`。
3. **create 非零不得直接 failed**:若输出含 `already exists` / `No commits between` / 已无差异等同类文案,转入查 open 或 merged;能复用则 success 并回填 URL。
4. **已合并无新提交**:优先回填已合并单 URL;查不到 URL 时仍可 success + 空 `mr_url`,summary 说明已合入/无新提交。
5. **无历史单且已同步**:open/merged 均无单且源相对目标无差异 → 允许 success + 空 `mr_url`,summary 说明原因。
6. **仍须 failed**:仅 closed 未合并且无法新建;无法 push、鉴权/权限失败、冲突未解决、缺 CLI、不支持托管商、其它非幂等建单错误。

```bash
# GitLab — list-first, then create; on idempotent error query merged
glab mr list --source-branch "$(git rev-parse --abbrev-ref HEAD)" --target-branch <target> --state opened
# if open hit: reuse URL, skip create
glab mr create --fill --yes --source-branch "$(git rev-parse --abbrev-ref HEAD)" --target-branch <target>
# if create fails with already exists / No commits between / no diff:
glab mr list --source-branch "$(git rev-parse --abbrev-ref HEAD)" --target-branch <target> --state merged

# GitHub — same order
gh pr list --base <target> --head "$(git rev-parse --abbrev-ref HEAD)" --state open
# if open hit: reuse URL, skip create
gh pr create --fill --base <target> --head "$(git rev-parse --abbrev-ref HEAD)"
# if create fails with already exists / No commits between / no diff:
gh pr list --base <target> --head "$(git rev-parse --abbrev-ref HEAD)" --state merged
# or: gh pr view <n> --json url,state
```

成功时合并请求 Web URL（含 GitHub PR）写入 `outputs.mr_url`（字段名不变）。幂等成功路径下允许空 `mr_url`(已合入查不到 / 无历史单已同步)。需预装对应 CLI（`glab` / `gh`）及凭据（`GITLAB_*` / `GITHUB_*`）。

> 平台可选路径 `detect_push` + `create_mr` 本迭代仍仅 GitLab；GitHub 依赖 Agent 侧 `gh`，平台不代建 PR。

## 注意

- 只 push 到上面命名规则的工作分支,不要碰 `main` / 受保护分支。
- 不要修改 `.github/workflows` / `.gitlab-ci.yml` / `Dockerfile`,除非任务明确要求。
- 命令失败先 surface 错误原因,不要静默重试;但对 create 的幂等错误须按上方闭环转入查询后再申报。
