# 2026-11-22 并发上限统一为沙箱上限

- 日期：2026-11-22
- 范围：`server/internal/{config,engine,handlers,router,services}`、`server/cmd/server`、`server/e2e`、`web/src/views/SettingsView.vue`、`web/src/lib/api`、`web/src/locales`、`web/e2e`、配置文档
- 做了什么：删除 `max_concurrent_runs` 设置项、`engine.max_concurrent_runs` 配置和 `GRASP_MAX_RUNS` 环境变量；engine 的 run 排队上限改为跟随 `max_sandboxes`。设置页删除「调度并发」卡片和「运行中 / 并发上限」统计。随之失去调用方的 `/api/stats/dashboard`、`DashboardService.Compute`、前端 `api.dashboard()` 与 `DashboardStats`、`Engine.MaxConcurrent()` 和 `engine.New` 的默认 5 兜底一并删除。节点创建沙箱碰到共享上限时改为每 5 秒重试、直到 run 取消或节点超时，并在执行日志提示「等待沙箱名额」（`runtime.ErrSandboxCapacity`，`services.ErrSandboxLimit` 通过 `errors.Is` 匹配）。启动时先执行 `SettingsService.ApplyOnBoot` 读取数据库里的生效值，再用它创建 engine 和沙箱服务，之后 `Bind` 接入运行时组件；随之删除不再使用的 `Config.SandboxIdleTTL()`。重启 e2e 改为通过设置接口把 `max_sandboxes` 设为 1 来保持第二个 run 排队。
- 为什么：设置页上两个数字含义重叠、默认值不同还容易让人误解；保留一个共享上限即可。run 名额在 run 结束时释放，沙箱名额要等空闲回收，共用一个数字后放行的 run 更容易碰到满额，所以节点改为等待。启动时 engine 曾先用配置文件的值开始调度，数据库里保存的上限要稍后才生效，重启后排队的 run 会被提前放行。
- 如何验证：`go build ./...`；`go test ./internal/{config,services,handlers,engine,router,runtime}`、`./cmd/...`；`go vet`；`go run ./cmd/gen-configdoc -check`；`npx vue-tsc --noEmit`；`src/lib/api`、`SettingsView`、locales 的 Vitest 104 项通过；相关文件 ESLint 通过。Playwright 与真实环境重启 e2e 未在本地运行。
