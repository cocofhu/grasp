# 2026-11-18（Token 用量按每轮模型统计；预览访客用量入账）

- 日期：2026-11-18
- 范围：`server/internal/sandbox/{acp.go,eventlog.go}`、`server/internal/services/{turn_usage.go,sandbox_studio.go,solo.go,solo_turn.go,preview_guest.go}`、`server/internal/handlers/app_preview_guest.go`
- 做了什么：
  - 一条规则：每轮开始前把本轮的 execution（回复「统计信息」里显示的路由）设到 ACP 客户端上；编码工具上报用量时不带模型名（`default`/`unknown`/空 key，如 Codex），这部分记到 execution 的模型，没有 execution 时退回启动模型（`ACP_BRIDGE_MODEL`）。
  - 工作流原本就按轮设置 execution（含对话路由切换）。Solo 把每轮的 execution 快照传给 `ChatWithTimeout`；预览访客对话用 `PreviewGuestExecution`（预览的 Agent + 访客 chat 当前选择的模型），访客回复也因此显示模型。
  - services 层的回合入账合并成 `recordTurnUsage`，Solo、Studio 和预览访客共用，按 ACP 操作 ID 入账一次。删除 `recordSoloUsage`、`recordStudioUsage` 两个兜底函数。
  - 隔离模式的预览访客对话以前完全没有入账，现在按预览归属入账：Solo 预览记到该会话（`solo/chat`），工作流预览记到节点（`workflow/interactive`）。
  - 去掉事件日志聚合（`dispatchFrame`）里没人读的用量解析。
- 为什么：线上 `grasp-e2b` 的 Solo 用量全部记成「未知模型」，而同一条回复的「统计信息」能显示模型：Solo 的连接从未设置模型。记账改为使用回复显示的同一个模型，服务端重启后重新接管的沙箱也一样。
- 删除和统计：ledger（`token_usage_events`/`token_usage_reports`）只由 `tokenledger` 写入，删除 Run、工作流或 Solo 会话都不会动它，读侧也不 join 这些表，名称在写入时就做了快照。新增测试固定这一行为。删除接口不变。
- 如何验证：`sandbox/token_usage_test.go`（execution 模型优先、退回启动模型）、`solo_usage_model_test.go`（工具给出模型名时保留，`default` 记到本轮模型，每轮只入账一次）、`preview_guest_test.go`（访客 execution、入账及归属）、`token_ledger_retention_test.go`。`server` 全量 `go test ./...` 通过。已经记下的历史用量不改写。
