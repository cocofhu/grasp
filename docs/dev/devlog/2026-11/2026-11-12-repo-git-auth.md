# 2026-11-12（Git 授权改由仓库配置）

- 日期：2026-11-12
- 范围：`server/internal/models/{solo,workflow}.go`、`server/internal/runtime/`（仓库凭据解析、`CloneRepos`、`run_sandbox_env.go`）、`server/internal/engine/{start_run,run_repos}.go`、`server/internal/services/{project,workflow,workflow_import,onboarding,team,agent,shared_agent,sandbox*,solo*}.go`、`server/internal/handlers/{run,workflow,onboarding,shared_agent,dto}.go`、`server/cmd/server/main.go`、`server/openapi.yaml`、`agents/*/agent.json` 与 `team_embed/*/agent.json`；Web 的 `ProjectForm`、新组件 `RepoCredentialSelect`、`RepoChoiceList`，`WorkflowDefaultsPanel`、`RunLaunchModal`、`AgentChatTester`、`HomeCreateBaselineModal`、`WorkflowsView`、引导与团队向导，删除 `AgentGitCredentialPicker`、`ReposEditor` 和 `repos` 变量类型；文档与对应测试。
- 做了什么：
  - 仓库（Project）新增 `credentialId`、`commitName`、`commitEmail`。保存时校验凭据存在、未吊销，并与地址的协议和主机一致；删除仓库时从工作流绑定里移除。
  - 工作流在 `defaults.repos` 绑定仓库，保存时校验仓库存在、分支合法；导入时丢弃绑定。启动运行可传 `repos` 选子集并改分支，引擎快照到 `Run.Repos`，省略时取全部绑定仓库，cron 和渠道触发同样取全部。
  - 沙箱每次启动按快照仓库实时读取凭据和提交身份：凭据以 `repo:<目录名>` 进入受限 catalog，只对该仓库地址生效；提交身份经新的 `GIT_REPO_AUTHORS` 写入各仓库 `git config`。仓库凭据计入 Git 指纹。运行 env 禁止设置 `GIT_REPOS`、`GIT_REPO_AUTHORS`。
  - 删除 Agent 与默认配置的 `gitBindings`、`inheritGit` 及其解析、UI 和测试；删除 `repos` 变量类型、`${vars.repos}` 标题与解析；内置 Agent 不再声明 `GIT_REPOS`。
  - 引导向导的 Git 步骤改为填写仓库地址、分支、凭据和提交身份，生成时登记为仓库并绑定到默认工作流；团队向导改选一个已登记仓库（`repoId`）。
  - Web：仓库表单增加凭据（只列出与地址匹配的 Git 凭据）和提交身份；工作流设置增加仓库绑定；启动弹窗按绑定仓库勾选子集并改分支；对话测试、从基线新建工作流改为勾选已登记仓库；运行详情显示快照仓库名。
- 为什么：10 月的设计把凭据挂在 Agent 上、仓库清单靠环境变量引用工作流变量，凭据与仓库要分两处配置并靠 scope 匹配，容易配错。凭据和提交身份本来就是仓库的属性，绑定关系也应属于工作流，这样每处只配一次、含义直接。按要求不做迁移、不留兼容层，CHANGELOG 写明重新配置的步骤。
- 如何验证：`go vet ./...`（含 `-tags e2ereal ./e2e/`）、`golangci-lint run`、`go run ./cmd/gen-configdoc -check`、`go test ./internal/... ./cmd/...`。重点用例：`runtime/repo_git_test.go`（凭据只对本仓库地址生效、失效时拒绝启动、提交作者写入、凭据变化触发共享沙箱 reprime）、`engine/run_repos_test.go`（全部 / 子集改分支 / 显式空 / 未绑定或已删除时拒绝且不建运行）、`services/project_git_test.go`（凭据与提交身份校验、删除仓库解绑、保存工作流校验绑定）、`services/onboarding_test.go`（引导登记仓库并绑定、重复进入复用）、`handlers/onboarding_test.go`。Web：`npm run lint`、`vue-tsc --noEmit`、vitest 全量（591 个文件 6384 项通过）；新组件见 `RepoChoiceList.test.ts`、`RepoCredentialSelect.test.ts`，启动选子集见 `RunLaunchModal.interactions.test.ts`，工作流绑定见 `WorkflowDefaultsPanel.test.ts`，导出不带仓库绑定见 `workflowIO.test.ts`。
- 未做：旧的 Agent Git 授权和 `repos` 变量不迁移，升级后需按 CHANGELOG 重新配置。沙箱 `Spec.GitBindingsConfigured` 现恒为 true，其 false 分支（宿主环境变量里的 Git 令牌与旧 SSH 私钥回退）已无生产调用方，留待单独清理。
