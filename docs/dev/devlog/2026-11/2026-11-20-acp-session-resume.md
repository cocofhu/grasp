# 2026-11-20 ACP 会话接回

- 日期：2026-11-20
- 范围：`sandbox-gateway/sandbox/internal/acp`、`internal/provider`（`acpx`、`deepseek`、`kimicode`）
- 做了什么：
  - `acp.NewPanel` 的九个位置参数收为 `PanelSpec`，新增 `ResumeSessionID`。
  - `initialize` 声明 `sessionCapabilities.resume` 时用 `session/resume` 接回（不回放历史），否则声明 `loadSession` 时用 `session/load`。`session/load` 回放的 `session/update` 被丢弃：面板在调用返回后才采用会话 ID，读循环又按序先送达通知。
  - Agent 以 JSON-RPC 错误拒绝接回（会话已不存在）时改开 `session/new`，桥接据此上报 `sessionReset`；其他错误照常失败。
  - ACP 会话的 `Capabilities().Resume` 由 Agent 的声明决定。桥接的换模型接回、崩溃复活和休眠 / 超龄重建因此覆盖 ACP 家族。
  - 删除 Kimi provider 里随参数收拢而不再使用的 `firstNonEmpty`。
- 为什么：ACP Agent 之前只走 `session/new`，任何进程重建都会丢掉原生会话。#1311 第四阶段。
- 如何验证：沙箱模块 `go test -race ./...`、golangci-lint、覆盖率门槛；新增假 ACP Agent 测试覆盖 load / resume 接回、回放不外发、会话不存在与不支持时开新会话。实测 `initialize`：`dsh` 与 `kimi` 声明 `resume`，`traecli` 声明 `loadSession`。DeepSeek 实测：`deepseek_harness` 对话中 SIGKILL Agent 进程，桥接以 `session/resume` 接回同一会话 ID，Agent 答出之前约定的口令。
- 后续：OpenCode serve、上下文交接组件，见 #1311。
