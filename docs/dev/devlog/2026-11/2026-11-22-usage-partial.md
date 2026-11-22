# 2026-11-22（未完成回合的用量统一标为部分用量）

- 日期：2026-11-22
- 范围：`sandbox-gateway/sandbox/internal/acp/panel.go`、`internal/provider/acpx`、`internal/provider/deepseek`、`internal/provider/oneshot`、`internal/provider/opencode`、`internal/provider/pi` 及其测试，`CHANGELOG.md`
- 做了什么：对齐 t3code 的回合用量规则——没有正常完成（取消、超时、失败）且带有用量的回合，一律标记 `usagePartial`。
  - ACP：Agent 先以 `stopReason: "cancelled"` 应答的回合（用户停止时 bridge 先发 `session/cancel` 再取消 ctx），`prompt_done` 与 `TurnResult` 都标部分；回复文本判定为失败的回合同样标部分。覆盖 dsh、Grok、Antigravity。
  - dsh：取消或报错后从用量缓存补算的本轮用量标部分。
  - oneshot：取消、超时或失败结束的回合统一标部分，覆盖 DevEco、OpenClaw、Claude Code、CodeBuddy、Gemini。
  - Pi、OpenCode：失败回合已完成部分的用量标部分。
- 为什么：账本只允许替换标记为部分的报告。原先这些回合把未完成时的用量当成最终结果记死，之后同一操作的最终报告无法更正。token 数值本身不变。
- 未覆盖：OpenCode 被中断的 step 在服务端记为 0 token 且不补发（实测 abort 后查询 `/session/{id}/message`），属于上游限制。
- 如何验证：新增单测在撤回修复后全部失败、恢复后通过；sandbox 全量 `go test`、`go vet`、golangci-lint 通过；用 DeepSeek key 实测 dsh 按 bridge 顺序取消（333 输入 / 3981 输出，`partial=true`）和 Claude Code 取消（`usagePartial:true`），下一轮用量正常且不重复。
