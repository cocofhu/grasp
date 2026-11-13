# 2026-11-13（Cursor 模型清单与 CLI 2026.11.11 对齐）

- 日期：2026-11-13
- 范围：`server/internal/clicatalog/catalog.go`、`server/internal/clicatalog/catalog_test.go`、`web/e2e/cli-model-select-main.ts`
- 做了什么：按 `cursor-agent models`（2026.11.11-b912975）的输出和顺序重排 Cursor 模型清单与测试快照（279 个，不含 Auto）。新增 Gemini 4 Carbon（low / medium / high）、Gemini 4 Hydrogen（low / medium / high / xhigh）、Claude Fable 5.2 各档位及 Fast 版、DeepSeek V4.1 Flash 1017（none / low / medium / high / max）；移除 CLI 已不再列出的 Gemini 3.6 Flash 与 Gemini 3.7 Flash。快照测试的厂商前缀补上 `deepseek`。e2e 页面里的 Cursor 清单同步为同一份。
- 为什么：选择器缺少 Gemini 4 Carbon 等新模型；清单注释约定与 CLI 输出逐条一致，已经落后。清单只用于展示，已保存的旧 ID 仍原样传给 CLI。
- 如何验证：`go test ./internal/clicatalog/... ./internal/handlers/... ./internal/services/...`、`go vet`、`gofmt`；与本机 `cursor-agent models` 逐条比对无差异。
