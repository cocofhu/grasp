# 2026-11-17（Codex 模型选择补齐 GPT-6.2）

- 日期：2026-11-17
- 范围：`server/internal/clicatalog/catalog.go`、`server/internal/clicatalog/catalog_test.go`、`server/internal/modelrouting/reasoning_test.go`、`web/e2e/cli-model-select-main.ts`、`web/e2e/model-routing.spec.ts`、`CHANGELOG.md`
- 做了什么：按本机 Codex 0.169.0 的 `models_cache.json` 中 `visibility=list` 的条目与顺序，补上 GPT-6.2 Sol（500K）、GPT-6.2 Astra（272K）、GPT-6.1 Terra（500K）。三者均支持 low / medium / high / xhigh / max / ultra；6.2 两款置于清单前部并标记推荐。同步选择器示例清单、模型清单快照和平台 / 连接模式的推理档位回归。
- 为什么：Grasp 的 Codex 选择器使用服务端内置清单，旧清单没有同步 CLI 新增模型，导致配置模型时找不到 6.2。会话内的可切换模型仍由已保存的模型分组决定。
- 如何验证：与本机 CLI 缓存逐项核对，10 个可见模型的 ID、顺序、上下文与推理档位一致。服务端 `go test ./...`、`go vet ./...`、配置文档校验、golangci-lint 2.12.0、指定 runtime 回归均通过，核心覆盖率 90.7%。前端 lint、类型检查、6074 项单测与 25 项模型配置浏览器回归通过，行覆盖率 91.75%；构建在设置 `NODE_OPTIONS=--max-old-space-size=4096` 后通过（默认堆上限不足）。全量浏览器检查以 `--max-failures=1` 运行，在已有 `conversation-workspace.spec.ts:228` 的 Solo 预览历史消息断言失败后停止：91 通过、1 失败、243 未运行；原失败用例单独复跑通过，本次未修改该功能，不能据此宣称全量浏览器检查通过。全局 CI 脚本、文档构建与审计通过；Agent E2E 的 45 项 Node、10 项 Python 契约回归、服务端 / 网关 / 运行时和完整测试镜像构建，以及 8 种工具的完整 84 轮切换检查均通过。集成检查使用模拟模型端点与清单声明的线协议 fixture，不代表供应商平台实际推理验证。
