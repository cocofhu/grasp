# 2026-11-19 Codex 重连时保留当前回合

- 日期：2026-11-19
- 范围：`sandbox-gateway/sandbox/internal/provider/codex`、`sandbox-gateway/sandbox/internal/provider/oneshot`
- 做了什么：Codex 把重连次数和切换至 HTTPS 的通知发在错误事件中。适配器现在将这些通知作为重试进度处理。若 CLI 在重试中途退出，保留最后一次提示和进程退出原因。
- 为什么：之前收到重连提示就启动了 5 秒退出计时，重连中的 CLI 会被强制终止。
- 如何验证：子进程回归测试覆盖延迟 6 秒恢复、重试耗尽、进程中断和最终认证失败。另在隔离容器中使用 Codex CLI 0.169.0，让本地 WebSocket 模型端连续断开三次，第四次延迟 7 秒返回答案，确认 Grasp 收到完整回复和 `end_turn`。沙箱模块通过全量 Go 测试和竞态检测。
