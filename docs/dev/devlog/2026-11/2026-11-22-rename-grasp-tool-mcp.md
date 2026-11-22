# 2026-11-22（平台内置 MCP artifact-store 改名为 grasp-tool）

- 日期：2026-11-22
- 范围：`server/internal/services/`、`server/internal/runtime/acp_config.go`、`server/internal/sandbox/`（含 `skills_embed/rules/grasp-tool.md`）、`server/internal/mcp/rpc.go`、`agents/`、`web/src/`、`sandbox-gateway/scripts/`、文档
- 做了什么：平台内置 MCP 的名字从 `artifact-store` 改为 `grasp-tool`（Codex 下的命名空间从 `mcp__artifact_store` 改为 `mcp__grasp_tool`）。涉及预设 ID、默认注入名、`serverInfo.name`、产物约定规则文件（`rules/artifact-store.md` 改名为 `rules/grasp-tool.md`）、Solo AGENTS.md 和对话历史交接里的工具提示、内置团队 `agent.json` 与 `agents/pack.sh` 校验、前端预设 / 向导 / 语言包（key `mcp.artifactStore.*` 改为 `mcp.graspTool.*`，Agent Studio 显示名改为「Grasp Tool」）、mock 与 e2e 脚本。Go 与前端标识符同步改名（`GraspToolMCP`、`MCPPresetGraspTool`、`hasGraspTool`、`IncludeGraspTool`、`GRASP_TOOL` 等）。`GRASP_ARTIFACT_URL` / `GRASP_ARTIFACT_TOKEN` 与 `/mcp/runs/<runId>` 不变。
- 为什么：这个 MCP 已不只读写产物，还提供 `ask_question`、`ask_form`、`get_history`、`set_preview`、页面操作等工具，`artifact-store` 不再能概括它。不保留旧名兼容：Agent 配置里仍叫 `artifact-store` 的条目不再被识别为平台 MCP，需要改名为 `grasp-tool`。
- 如何验证：server `go build`、`go vet`、`go test ./...` 通过（`engine` 包全量时偶发失败一次，单独重跑通过）；`go run ./cmd/gen-configdoc` 无差异。sandbox-gateway `go test ./...` 在清除宿主 `CONFIG_ROOT` / `AGENT_PROVIDER` 后通过，`node --test scripts/*.test.mjs` 46 项、`test_agent_switching_test` 12 项通过。web `vue-tsc --noEmit` 通过，vitest 4074 项中 1 项（`dialogueLintGate`）全量时失败，单独重跑通过。
