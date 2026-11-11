# 删除不再被调用的代码

- **日期**：2026-11-11
- **范围**：`server/internal/`、`sandbox-gateway/gateway/internal/`、`sandbox-gateway/sandbox/internal/`、`web/`。
- **做了什么**：
  - Go 三个模块按 `deadcode` 删除生产入口到达不了的函数：服务端 84 个、网关 1 个、沙箱运行时 9 个。专门测这些函数的测试一并删除；只是借它们做准备或断言的测试，改为调用生产接口（如 `PrepareAuth`、`UpdateWithBrand`、`StartPrepared`），或把原有的薄包装移到该包的 `helpers_test.go`。`staticcheck U1000` 找出的 3 个未用测试字段一并删除。
  - 前端删除未接入任何入口的开发预览配置和页面、不再渲染的 `AppTopbar` 及其 e2e、无人引用的 `gitCredentialType.ts`；去掉只在本文件内使用的导出，并按 `noUnusedLocals` 删除无人使用的函数、常量和类型。
- **为什么**：v3.2 重建后，旧工作流的自动推送与建 MR、GitLab 判断、沙箱能力与变更探测、SSH 注入打包、旧的分享消息清洗等路径在生产代码里已经没有调用方，只剩测试在维持。
- **保留**：`envauth.SecretEnvKeys` 只被测试调用，但它是敏感变量名单的唯一来源，安全测试据此逐个验证 Agent 不能经普通环境变量注入这些值，所以保留。测试基础设施（`sandboxtest`、网关 `driver/fake`、各 `Set...ForTest` 钩子、`OpenSQLiteTest`）不在清理范围内。
- **如何验证**：三个 Go 模块再次运行 `deadcode`（含 `-test`）和 `staticcheck -checks U1000` 均无输出；`go vet`、`go mod tidy` 无变化，服务端与网关测试通过。沙箱运行时测试中 `internal/agents` 与 `internal/handler` 的 4 个失败在未修改的 `main` 上同样失败，与本机已安装的 CLI 有关。前端 `vue-tsc`、`vitest`（6392 个）和 `npm run build` 通过。
- **后续**：`sandbox.Spec.SSHPrivateKey` / `SSHKnownHosts` 在生产中只读不写，旧的 SSH 字面量注入路径（`buildMultiInject`、`packGitInjectBundle` 的对应参数、worktree 的 `legacy-ssh`）实际已不可达；它涉及注入包格式与 reprime 哈希，单独处理。
