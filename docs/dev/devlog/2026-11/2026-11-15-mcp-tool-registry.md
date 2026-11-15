# 2026-11-15（MCP：内置工具改为登记表分发，按工具域组织实现）

- 日期：2026-11-15
- 范围：`server/internal/mcp/tools.go`，新增 `tools_artifact.go`、`tools_interaction.go`、`tools_plan.go`、`tools_history.go`、`tools_structured.go`、`tools_preview.go`、`tools_registry_test.go`。
- 做了什么：#1206 第 6 项。`runTool` 原先是 352 行的 `switch`，现在只保留文件工具、Solo 下的工作流专属工具、页面工具、`live_update` 这几个前置分流，其余按 `builtinToolHandlers` 登记表（名称 → 处理函数）分发。各 `case` 的代码原样移到按域划分的 `tool<Name>` 方法中：产物、交互、计划、历史、结构化产物、预览。9 个 `get_<x>` 结构化读取工具改用 `structuredGetter` 生成。`structuredSet` / `structuredGet` 随结构化工具移到 `tools_structured.go`。`node_complete` 在澄清确认前视为未知工具的规则，移到 `toolNodeComplete`。
- 为什么：新增工具原先要在 switch、声明、可见性多处同步修改，容易漏改。现在登记表是分发的唯一入口，一致性测试负责核对声明和处理函数。
- 保持不变：`artifactTools()` 的 schema 和顺序不变，所以 `tools/list` 输出不变。可见性（`listedTools`、`toolListed`、`workflowOnlyTool`、`hideNodeComplete`）和能力规则（`toolAllowed`、`readAllowed`）沿用原实现。每个工具的授权检查、错误文案和返回值逐字保留。前置分流的顺序也不变。纯结构调整，不改 CHANGELOG。
- 如何验证：新增 `TestBuiltinToolsDeclaredAndHandled`，检查每个声明的工具都有处理函数，每个处理函数都有声明（`upload_image_artifact` 只供沙箱 `artifact-upload` CLI 调用，显式列为不出现在列表中的例外），以及文件、页面、Live 工具不会重复登记。新增 `TestRunToolUnknownName` 固定未知工具的文案。原有 MCP 协议、可见性、授权测试不变通过。`server/` 下 golangci-lint、`go vet ./...`、gen-configdoc 检查、`go test ./...`、`cover-check-server.sh 90` 通过。
