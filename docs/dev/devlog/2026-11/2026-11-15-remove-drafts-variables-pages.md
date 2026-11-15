# 2026-11-15（移除需求草稿与工作区共享变量）

- 日期：2026-11-15
- 范围：
  - 前端：`web/src/router/index.ts`；`web/src/data/{settingsNav,sidebarNav,nodeRegistry,mcp}.ts`；`web/src/views/{WorkspaceToolView,WorkflowEditorView}.vue`；`web/src/components/workflow/WorkflowDefaultsPanel.vue`；`web/src/components/pm/PmSettingsPanel.vue`；`web/src/components/workspace/WorkspaceExternalMcpPanel.vue`；`web/src/lib/pm/usePmChannelMulti.ts`；`web/src/lib/api/clients/workspaceClient.ts`；`web/src/lib/shared/types.ts`；`web/LIB_DOMAIN_MAP.json`；中英文文案；删除 `WorkspaceDefaultsView.vue`、`RequirementDraftsPanel.vue`、`lib/workspace/{useRequirementDrafts,requirementDraftMarkdown,requirementDraftSchedule}.ts`、`e2e/requirement-drafts-schedule*` 及相关测试。
  - 后端：删除 `server/internal/{models,services,handlers}/requirement_draft*.go`、`server/internal/pmmcp/prd_manager*.go`、`server/internal/engine/output_leftover*.go`；修改 `cmd/server/main.go`、`internal/router/router.go`、`internal/handlers/{handlers,workspace,dto}.go`、`internal/models/{models,pm}.go`、`internal/pmmcp/host.go`、`internal/services/{pm,sandbox_pm,sandbox_agent,workspace,workspace_external_mcp,audit,onboarding}.go`、`internal/services/team_embed/default-workflow.json`、`internal/engine/{engine,start_run,output,handoff}.go`、`internal/database/database.go` 及相关测试。
- 做了什么：
  - 需求草稿：去掉 `/requirements` 页面和 `/api/workspace/requirement-drafts` 接口、模型与服务；去掉工作区助手的 `pm-prd-manager` MCP（默认启用列表、频道 / 自动审批 / 外部 MCP 选项）；去掉 output 节点的 `auto_leftover_draft`（运行结束自动写遗留需求草稿）。外部 MCP 设置读取时同样过滤失效的 MCP id。
  - 工作区共享变量：去掉 `/settings/variables` 页面、`Workspace.variables` 字段和 `PUT /api/workspace` 的 `variables` 参数；去掉工作流默认值里的 `inheritWorkspace` 开关。运行时只注入工作流自己的默认变量（引擎的 `SetWorkspaceVarsLookup` 改名为 `SetDefaultVarsLookup`）。`WorkspaceVariable` 类型继续用于工作流默认变量，密钥合并测试改为直接测 `mergeWorkspaceVars` / `sanitizeWorkspaceVars`。
  - `truncateRunes` 原在 `output_leftover.go`，移到仍在使用它的 `handoff.go`。
- 为什么：产品聚焦云端 Agent 和工作流，不再提供这两个功能。
- 数据：未做删表或删列迁移，已有数据库里的 `requirement_drafts` 表和 `workspaces.variables` 列保留但不再读写。
- 如何验证：
  - `server/`：`go vet ./...`、`golangci-lint run`（0 issues）、`go run ./cmd/gen-configdoc -out CONFIGURATION.md -check`、`go test ./...`、`./scripts/cover-check-server.sh 90`（90.6%）、`go test ./internal/runtime/ -run 'TestRunAgent|TestReact'`。
  - `web/`：`npm run lint`（0 error）、`npx vue-tsc --noEmit`、`npm test -- --coverage`（577 个文件全部通过，Lines 91.75%）、`npm run build`；Playwright desktop 跑了 workspace / workflow-canvas / gate-auto-pm-settings / pm-channel-multi / workspace-external-mcp 等 13 个相关 spec（86 passed），以及 `mobile-browser-contract`。
