# 删除只剩测试引用的前端模块，合并平台 MCP 的 JSON-RPC 分发

- **日期**：2026-11-13
- **范围**：`web/src/`、`web/e2e/node-model-picker-main.ts`、`web/LIB_DOMAIN_MAP.json`、`server/internal/{platformmcp,contextmcp,memorymcp,schedulermcp,pmmcp,runtime,services,browser}/`。
- **做了什么**：
  - 前端删除从 `main.ts` 到达不了、只有自身测试还在引用的 8 个模块：`CodingToolPicker.vue`、`AgentCredentialBindings.vue`、`TokenUsageByModelTip.vue`、`lib/agent/{cliModelEnv,openCodeCatalog,openCodeProvider}.ts`、`lib/run/{canvasLayout,workflowEditorDirty}.ts`，连同各自的测试。它们的生产引用分别在 #744（画布改用 `useAutoLayout` / `useWorkflowSave`）、#940、#943、#944 和 ce662387 中移除。随之没有调用方的 `api.openCodeProviders` / `api.openCodeModels` 客户端方法与对应类型、测试里的 mock 和 `node-model-picker` 预览页里的 mock 一并删除；服务端 `/opencode/providers` 接口不动。去掉 27 个无人引用的导出类型上的 `export`（`.vue` 中的转发导出直接删除）。`LIB_DOMAIN_MAP.json` 同步。
  - 服务端 `contextmcp`、`memorymcp`、`schedulermcp`、`pmmcp` 四份逐字相同的 JSON-RPC 方法分发合并为 `platformmcp.ServeTools`；各 host 的鉴权、解析错误日志、先执行再审计的顺序不变。artifact-store（`internal/mcp/rpc.go`）声明 `listChanged`、工具调用路径不同，不在合并范围内。
  - `services` 中复制自 `runtime` 的 `substTemplate*` 和 `mcpServersJSON` 删除，改用导出的 `runtime.SubstVars` / `SubstMap` / `SubstSlice` / `MCPServersJSON`（原注释"避免跨包依赖"已不成立，`services` 早已依赖 `runtime`）。`services/sandbox_test_pool.go` 是生产代码（Agent Studio 测试沙箱），改名为 `sandbox_studio.go`。`browser.inspectCancelFilter.expireSkipNow` 只被测试调用，移到 `inspect_cancel_test.go`。
- **为什么**：只靠测试维持的模块会让人误以为功能还在；四份分发逻辑任何一处修改都要同步四次。
- **保留**：与 #1204 一致，测试基础设施（`sandboxtest`、网关 `driver/fake`、`Set...ForTest`、跨包测试使用的 `version.Override` / 网关 `config.Default`）、`envauth.SecretEnvKeys`、e2e 路由用的 `src/data/shellNavPaths.ts` 不动。`gateshare` 的 nonce / preview ticket 签发、`gate_share_public.go` 的公开队列接口虽有重复，但属于分享链接安全路径（限流、CSRF、权限的检查顺序），不在本次合并。
- **如何验证**：服务端 `golangci-lint`（v2.12，根配置）0 issues，`go vet ./...`、`go test ./...`、`cover-check-server.sh 90`、`gen-configdoc -check` 通过，`deadcode -test` 无输出；新增 `TestServeTools` 覆盖全部分支。前端 `npm run lint`、`vue-tsc --noEmit`、`vitest`、`npm run build` 通过。
