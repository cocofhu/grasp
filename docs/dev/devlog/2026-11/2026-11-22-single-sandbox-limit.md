# 2026-11-22 并发上限统一为沙箱上限

- 日期：2026-11-22
- 范围：`server/internal/{config,engine,handlers,router,services}`、`server/cmd/server`、`server/e2e`、`web/src/views/SettingsView.vue`、`web/src/lib/api`、`web/src/locales`、`web/e2e`、配置文档
- 做了什么：删除 `max_concurrent_runs` 设置项、`engine.max_concurrent_runs` 配置和 `GRASP_MAX_RUNS` 环境变量；engine 的 run 排队上限改为跟随 `max_sandboxes`。设置页删除「调度并发」卡片和「运行中 / 并发上限」统计。随之失去调用方的 `/api/stats/dashboard`、`DashboardService.Compute`、前端 `api.dashboard()` 与 `DashboardStats`、`Engine.MaxConcurrent()` 和 `engine.New` 的默认 5 兜底一并删除。重启 e2e 改为通过设置接口把 `max_sandboxes` 设为 1 来保持第二个 run 排队。
- 为什么：设置页上两个数字含义重叠、默认值不同还容易让人误解；保留一个共享上限即可。
- 如何验证：`go build ./...`；`go test ./internal/{config,services,handlers,engine,router}`；`go vet`；`go run ./cmd/gen-configdoc -check`；`npx vue-tsc --noEmit`；`src/lib/api`、`SettingsView`、locales 的 Vitest 104 项通过；相关文件 ESLint 通过。Playwright 与真实环境重启 e2e 未在本地运行。
