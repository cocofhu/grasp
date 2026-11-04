# 2026-11-04（节点执行收尾统一为 finishNode，删除 Agent 总结）

- 日期：2026-11-04。
- 范围：`server/internal/engine/{finish_node,execute,node_agent,node_react,outcome,resume,review,review_session,handoff,feedback,nodes}.go`、`server/internal/runtime/{provider,registry,acp_react,acp_review_git}.go`（删除 `turn_confirm_summary.go`）、`server/internal/mcp/{host,outcome}.go`、`server/internal/models/{feedback,prompts}.go`、`server/internal/services/feedback.go`、`web/src/components/run/product/FeedbackRoundCard.vue`、`web/src/locales/{zh-CN,en}/pages.json`，以及对应测试。
- 做了什么：自动主循环、澄清确认、复审确认、澄清队列收尾四个入口统一调用 `finishNode`：git 检查 → 交接检查 → 判定 → 已有通过结果的检查失败重开对话，否则路由。各步骤只看客观状态（会话是否在、工作区、`node_complete` 是否比节点最后一次变更新、判定结果、是否已持有通过结果、触发方式），不按节点类型分支。运行时用 `CheckGit` 替代 `OfferCommitOnConfirm`；新增 `EnsureHandoff`，在标记过期时补发 `node_complete`。MCP Host 记录标记与节点变更的先后顺序。复审确认的产物核对改为一次就地修改回合，并在该回合内调用 `node_complete`，交接说明随之刷新；`ReconcileOnConfirm` 已删除。审批门放行时，上游生产节点同样执行 git 与交接检查，刷新后的交接写回其执行记录。`handoffTargets` / `upstreamHandoff` 改为穿过 `human_gate`。删除隐藏的「Agent 总结」回合，以及从解析、模型字段、账本输出到前端卡片和文案的整条链路；历史数据中的该字段在读取时忽略。
- 为什么：四个入口各自拼装收尾逻辑，提交、交接和校验的顺序不一致，复审确认还要额外跑一个提交回合和一个总结回合。统一后各入口行为由同一套步骤决定，后续修改只需改一处。
- 行为保持：复审确认检查失败仍重开对话；澄清确认、自动节点（含复审节点首次运行）检查失败仍判失败；确认时模型报错仍留在对话；复审确认时预览问题清理失败仍视为检查失败并重开。自动重试和失败通知只在自动触发时发生。
- 如何验证：`finish_node_test.go` 按入口逐条断言上述行为，并覆盖交接刷新、缺标记时保留已通过结果、审批门上游交接写回，以及交接穿过审批门。Go 侧 `go vet ./...` 与 `go test ./... -p 1` 全部通过；Web 侧 `eslint .`（0 error）、`vue-tsc --noEmit` 与 `vitest run --maxWorkers=2`（571 个文件、6245 项）全部通过。
