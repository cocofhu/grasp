# 2026-11-13（门禁拒绝未配置动作，队列重排拒绝重复 ID）

- 日期：2026-11-13
- 范围：`server/internal/engine/resume.go`、`server/internal/chatsession/session.go` 及对应测试
- 做了什么：
  - `resumeGateLocked` 在标记门禁已处理前校验 action 属于 `gate.Actions`（空白或未配置均报错），登录侧决策、MCP 和外部分享入口共用此校验。比较前 `TrimSpace`，与 `gateshare.IsWhitelistedExternalAction` 一致，避免外部分享链接先被消费再被引擎拒绝。
  - `Session.Reorder` 拒绝重复 ID，失败时不修改、不持久化、不广播队列。Solo、流程 review/clarify、公开分享页重排共用。
- 为什么：#1135 未知动作会消费一次性审批，无匹配边时 Run 被误标 `completed`；#1136 `[first, first]` 被当成合法重排，第二条待处理消息丢失。
- 如何验证：新增 `TestGateRejectsUnknownAction`，扩展 `TestSessionRemoveReorder`，两者在修复前失败、修复后通过；`go test` 覆盖 `engine`、`handlers`、`pmmcp`、`services`、`gateshare`、`chatsession` 全部通过。`Gate.Actions` 自首次发布即持久化，旧的待审批门禁无需兼容；前端只提交 `gate.actions` 中的 ID。
