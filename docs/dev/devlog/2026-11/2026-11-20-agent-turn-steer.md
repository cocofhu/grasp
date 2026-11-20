# 2026-11-20 回合中引导（steer）

- 日期：2026-11-20
- 范围：`sandbox-gateway/sandbox/internal/provider`（`codex`、`streamjson`、`oneshot`）、`sandbox-gateway/sandbox/internal/service`、`server/internal/sandbox`、`server/internal/chatsession`、`server/internal/runtime`、`server/internal/services`、`server/internal/engine`、`server/internal/handlers`、`web/src`
- 做了什么：
  - 沙箱侧新增 `provider.Steerer` 契约。WebSocket 操作 `steer` 回 `steer_ack`（`delivered` / `unsupported` / `no_turn` / `failed`），并以当前回合的 `opId` 广播 `user_steer` 事件。`connected` 帧携带 `capabilities.steer`。
  - Codex 从 `codex exec` 改为常驻 `app-server`，用 `turn/steer` 和 `turn/interrupt`，删除仅服务 exec 的会话累计用量换算和诊断错误压制。Claude Code / CodeBuddy 的 stream-json 在回合内保持 stdin 打开，引导作为新的用户消息写入。
  - 服务端 `ACPClient.Steer` → `chatsession.Session.Steer` → `runtime.SessionTurnSteerer`，Solo、Agent Studio、工作流澄清 / 复审 / 审批、访客分享链接的排队消息都可「立即引导」。Agent 不支持或投递失败时，统一退回「中断当前回合，排队消息作为下一轮」。
  - 引导成为时间线的 `steer` 片段：实时事件和事件日志回放都记录，分段落库时保留，转写和 Solo 上下文前言以用户发言呈现，`get_history` 返回 `steers`，访客分享脱敏保留。前端在流式过程中原位显示，回合结束后显示在折叠区外。
  - 访客队列三个接口合并为一个请求解析入口 `openPublicQueueRequest`。
- 为什么：之前回合中途补充要求只能排队等下一轮或手动中断，Codex exec 也拿不到中断和完整事件流。这是 #1311 的第一阶段。
- 如何验证：服务端与沙箱模块全量 Go 测试、golangci-lint、覆盖率门槛；前端 vue-tsc、eslint、vitest 全量覆盖率。实测：Codex CLI 0.171 app-server 跑通 Fast 模式探针 8 个用例；构建出的沙箱运行时以 Claude Code 接 DeepSeek，服务端客户端在 Agent 执行 `sleep 12` 期间引导，收到 `delivered`，最终回复执行了引导内容，时间线记一条 `steer`，回合结束后再引导返回 `no_turn`。
- 后续：常驻进程生命周期（resume 指针持久化、崩溃重启、看门狗软中断）、OpenCode serve 与移除 `*_acp` 回退、切换 Agent 时的上下文交接，见 #1311。
