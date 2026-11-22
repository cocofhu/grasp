# 2026-11-22（ACP 通道读取用量；Antigravity 改走 `agy acp`）

- 日期：2026-11-22
- 范围：`sandbox-gateway/sandbox/internal/acp/`、`sandbox-gateway/sandbox/internal/provider/acpx/`、`sandbox-gateway/sandbox/internal/agents/`、`sandbox-gateway/sandbox/scripts/install-agent.sh`、`sandbox-gateway/sandbox/docs/BACKEND.md`；删除 `internal/provider/antigravity/`
- 做了什么：
  - `acp.Panel` 新增 `PromptTurn`，从 `session/prompt` 结果读取用量（顶层 `usage`，或 `_meta.usage` 按模型的 `modelUsage`），写进 `prompt_done`；被取消的回合保留 Agent 返回的用量并标记为部分用量。按 `totalTokens` 判断输入是否含缓存、输出是否含思考 token，统一换算成"输入不含缓存、输出含思考"。
  - `PanelSpec.AuthMethod`：初始化后先调 `authenticate`。
  - `acpx.Spec` 新增 `AuthMethodFn`、`ResultUsage`、`ConfigModel`（用 `model` / `thought_level` 配置项选模型和思考强度，Agent 不提供时报错）和 `FailureText`（Agent 把执行错误当作普通文字回复时，把回合标为失败）。会话累加 `CumulativeUsage`。
  - Grok 打开结果用量；Antigravity 从纯文本 `agy -p` 改为常驻 `agy acp`：工具调用、权限、接回会话和用量都走 ACP。`install-agent.sh antigravity` 从官方更新清单下载并校验 sha512。
- 为什么：这两个 Agent 之前回合用量一直为空，stop 后也拿不到；Antigravity 的纯文本模式看不到工具调用，也不能接回会话。对照 t3code 的 ACP 用量读取和 Antigravity ACP 接入。
- 如何验证：
  - 单测：`acp` 的用量换算表（agy 不含缓存口径、t3code 1.4.5 fixture 的含缓存口径、Grok `modelUsage`、取消、空用量、非法 JSON），Panel 的 `authenticate` 和结果用量，`acpx` 的模型/思考强度选择、执行错误判失败、取消保留部分用量、累计用量。
  - 实测：真实 `agy` 1.5.14 和 `grok` 1.0.92 通过本地 MITM 指向假模型端点，经我们的 provider 跑完整回合：Antigravity 正常回合用量 800/50/1200，执行工具途中取消得到 400/25/600 且标记部分用量，错误 key 打到真实 Google 端点时回合判为 `failed`，不存在的模型直接报错；Grok `_meta.usage` 为本回合计费用量（输入含缓存），换算后 400/20/600。Grok 在模型请求进行中被取消时不返回用量。
  - 安装：`install_antigravity` 实跑下载 1.5.14，sha512 校验通过。
