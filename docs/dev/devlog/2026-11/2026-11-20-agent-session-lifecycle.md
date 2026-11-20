# 2026-11-20 Agent 会话生命周期

- 日期：2026-11-20
- 范围：`sandbox-gateway/sandbox/internal/provider`（`provider`、`oneshot`、`codex`、`acpx`）、`sandbox-gateway/sandbox/internal/acp`、`sandbox-gateway/sandbox/internal/service`、`sandbox-gateway/sandbox/internal/handler`、`server/internal/sandbox`、`server/internal/services`、`server/internal/runtime`
- 做了什么：
  - `provider.Session` 新增 `Capabilities()`（`Steer`、`Resume`），取代 `Steerer.SteerSupported` 与 `provider.CanSteer`。`connected` 帧和 `/capabilities` 的 `session.resume` 随之下发。
  - 桥接按用途重开 Agent：换模型 / 思考强度 / 速度档位时带原会话 ID 重开并保留可见聊天记录，`POST /api/model` 返回 `resumed`；`/new`、`/compact`、`restart_agent` 总是新对话。
  - 长驻进程意外退出时，可接回会话的 Agent 在 10 分钟内最多自动重启 3 次，排队消息在新进程上继续。
  - 长驻进程在下一轮开始前重建：检测到沙箱休眠恢复（墙上时间相对单调时钟跳变超过 30s），或进程运行超过 `SANDBOX_AGENT_MAX_AGE`（默认 6h）。
  - 重启或重建未能接回原会话时，下一轮 `prompt_done` 带 `sessionReset` 与 `previousSessionId`，复用服务端已有的对话记录补发逻辑。
  - Codex `thread/resume` 遇到 JSON-RPC 错误（线程已不存在）时改走 `thread/start`。`acp.RPCError` 导出供判别。`thread/resume` 未指定模型时会沿用线程记录的模型和思考强度，进程的 `-c model_reasoning_effort` 不生效；现在接回时总是带上模型（选定模型，否则 `config/read` 的配置模型，再否则 `model/list` 的默认模型），切换或清除思考强度都能生效。
  - 正在复活的 Agent 期间到达的消息照常排队，复活失败才清空队列并拒收。复活或新上下文成功后清掉上一次的 `sessionReset` 标记，避免对新对话误报重置。
  - Claude Code 接模型连接时使用临时 `CLAUDE_CONFIG_DIR`，其中 `projects`、`sessions` 软链到持久配置目录，进程重开后 `--resume` 仍能找到会话记录。`--resume` 被拒（结果为错误且 `num_turns` 为 0）时不再沿用旧会话 ID，桥接据此新开会话并上报 `sessionReset`。
  - Claude Code 的 `result.modelUsage` 是整会话累计值（`--resume` 会恢复上一个完成回合的计数），以前每个后续回合都把之前的用量再记一遍。现在它标为会话累计，按会话与上一次累计值相减。差值基线（含 CodeBuddy 的扁平计数）从单个 engine 移到 Provider 级的 `sessionLedger`，按原生会话 ID 保存，进程替换后接回的会话首轮也能正确相减。
  - 服务端 `SetChatModel*` 返回 `resumed`；`AlignChatModel` 删除，三个对齐接口合并为 `AlignChatSettings` 返回 `ChatSwitch{Restarted, Resumed}`。Solo 切换模型若原生接回会话且路由未变，不再补发上下文前言。
- 为什么：常驻 Agent（Codex app-server、ACP）以前换模型、崩溃或沙箱休眠恢复后都会丢掉原生会话，只能靠平台补发截断的对话记录。这是 #1311 的第二阶段（生命周期）。
- 如何验证：沙箱与服务端模块全量 Go 测试（沙箱带 `-race`）、golangci-lint、`gen-configdoc -check`、服务端覆盖率 90.5%。新增桥接生命周期测试覆盖换模型接回 / 不接回、新上下文不接回、崩溃复活与预算、复活丢会话上报 `sessionReset`、休眠与超龄重建。实测（真实沙箱运行时 + 服务端 `ACPClient`，DeepSeek 模型）：Claude Code 回合中引导生效，切换模型后 `resumed=true` 且记得切换前的口令，`restart_agent` 新开会话。
- 后续：ACP `session/load` 接回会话、OpenCode serve、移除 `*_acp` 回退、切换 Agent 时的上下文交接组件，见 #1311。
