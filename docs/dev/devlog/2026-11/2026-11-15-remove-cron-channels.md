# 2026-11-15（移除定时任务、IM 渠道与运行通知）

- 日期：2026-11-15
- 范围：`server/`（删除 `internal/channels/**`、`internal/schedulermcp`、定时任务调度与运行通知服务及其路由）、`sandbox-gateway/sandbox`（删除 `internal/qqbot` 与沙箱页 QQ 配置）、`web/`（删除定时任务、渠道、通知策略面板与相关客户端、类型、文案、测试），以及 README、SECURITY、AGENTS、配置示例与官网文案
- 做了什么：
  - 删除定时任务：工作区与 Agent 的 cron-jobs 接口、调度器、`task-scheduler` MCP（`/mcp/task-scheduler/:agentName`）、Agent Studio 数据页「定时任务」子 Tab 与 MCP 预设。
  - 删除全部 IM 渠道（QQ、企业微信、飞书、钉钉）：`/api/workspace/channels*` 接口、渠道会话与只读线程标记、工作区助手设置里的渠道面板、沙箱自带的 QQ 机器人与 `/api/qq/config`。
  - 删除运行通知：工作区与工作流的通知策略（`notifyPolicy`、`PATCH /api/workflows/:id/notify-policy`）、投递回执（`/api/workspace/notify-receipts`）及工作流列表的通知列。
  - 连带删除只为这些功能存在的代码：记忆 MCP 的写入开关（只有渠道会话会关闭写入）、`Handlers.PublicAdvertise` 字段、`qq` 图标与对应 live-overlay 打包产物。
  - 启动时清理旧数据：删除 `agent_cron_jobs`、`agent_cron_runs`、`channel_configs`、`notify_delivery_receipts` 表，`workspaces`/`workflow_defs` 的 `notify_policy` 列，`kind=cron` 及渠道用户（`cron:`/`qq:`/`wecom:`/`feishu:`/`dingtalk:`）的会话与消息；从各 Agent 的 `agent.json` 去掉 `task-scheduler` MCP。均可重复执行。
- 为什么：产品定位收敛到工作流与云端 Agent，定时任务和 IM 渠道不再维护；不保留兼容层，避免死代码与残留数据。
- 如何验证：server `go vet ./...`、`golangci-lint run`（0 issues）、`go test ./...`、`gen-configdoc -check`、`cover-check-server.sh 90`（90.8%）、`deadcode` 与改动前对比无新增；sandbox `go vet`、`golangci-lint`、`cover-check-sandbox.sh 90`（96.6%），`go test ./...` 仅 `TestChatsCRUD` 失败，改动前同样失败；web `npm run lint`、`vue-tsc --noEmit`、`npm test -- --coverage`（573 文件 6217 用例通过，行覆盖 91.75%）、`npm run build`、`npm run build:live-overlay`。新增 `TestDropRetiredCronChannelsIsIdempotent` 与 `agent_retired_mcp_test.go` 覆盖旧数据清理。
