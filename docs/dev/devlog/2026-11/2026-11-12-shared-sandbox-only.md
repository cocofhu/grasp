# 2026-11-12（工作流只保留共享沙箱）

- 日期：2026-11-12
- 范围：`server/internal/models/models.go`、`server/internal/engine/{start_run,node_req}.go`、`server/internal/runtime/{acp_sandbox,acp_shared,provider}.go`、`server/internal/services/{workflow,sandbox_shared}.go`、`server/internal/services/team_embed/default-workflow.json`、`web/src/components/workflow/WorkflowDefaultsPanel.vue`、`web/src/views/WorkflowEditorView.vue`、`web/src/lib/shared/types.ts`、`web/src/locales/{zh-CN,en}/pages.json`、`docs/content/{,en/}guide/concepts.md`、`sandbox-gateway/sandbox/README.md`，以及对应测试。
- 做了什么：删除 `WorkflowDefaults.Sandbox`、`WorkflowSandboxPolicy`、`SandboxModePerNode/Shared`、`ValidSandboxMode`、`Run.SandboxMode`、`NodeReq.SandboxMode` 和 `runSandboxMode`。`openSandbox` 与 `adoptSharedSandbox` 不再判断模式，工作流运行一律先尝试共享沙箱；被挂起的会话占用、镜像不同或工作目录不兼容时仍降级为节点自己的沙箱。编辑器的「共享沙箱」开关、文案和内置模板里的 `sandbox.mode` 一并删除。旧数据里的 `defaults.sandbox` 在解码时被忽略，`Run.sandbox_mode` 列保留不读。
- 为什么：新建工作流已默认共享，开关只剩历史包袱；后续仓库与 Git 授权改为按运行固定后，共享沙箱是唯一需要支持的模型。
- 如何验证：`go test ./internal/...` 全部通过；`go vet`、`gen-configdoc -check`、`golangci-lint`。运行时测试的 provider 默认不装共享表，继续覆盖降级用的单节点路径；`sharedHarness` 装共享表覆盖共享路径，新增 `TestReactReplyRehydrateReusesSharedSandbox`（会话丢失后在共享沙箱里重连，不新建容器并发送恢复提示）。Web：`WorkflowDefaultsPanel.test.ts`、`WorkflowEditorView.test.ts`、`npm run lint`、`vue-tsc --noEmit`。删除只测开关的 e2e 用例。
