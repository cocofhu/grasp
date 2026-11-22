# 2026-11-22（Cursor 改用 `@cursor/sdk`）

- 日期：2026-11-22
- 范围：`sandbox-gateway/sandbox/internal/provider/cursorsdk/`（新增）、`internal/provider/streamjson/`（删除 Cursor 方言）、`internal/agents/`、`internal/service/`、`sandbox-gateway/sandbox/cursor-sdk/`、`scripts/install-agent.sh`、`Dockerfile`、`IMAGE_LEVEL`、`deploy/e2b/`；`sandbox-gateway/scripts/` 的连接检查与切换 E2E；`server/internal/clicatalog/`、`modelrouting/`、`services/solo*.go`、`database/cursor_models.go`、`runtime/acp_backend.go`；`web/src/lib/solo/`、`web/src/lib/agent/`、`web/src/components/{agent,solo,run}/`；`BACKEND.md`、`PROTOCOL.md`、`server/README.md`
- 做了什么：
  - 沙箱：Cursor 从每轮起一个 `cursor-agent -p` 换成常驻的 `@cursor/sdk` 本地 Agent，由 `bridge.mjs`（Node，stdio JSON 行）驱动。`Agent.create` / `Agent.resume(agentId)` 接回，`run.steer` 引导（并入当前 run 或排成跟进 run），`run.cancel` 中断，用量取 run 结果的 `usage`，没有时累加 `model-call-completed`。只加载 project 与 user 设置，不加载插件、团队与 MDM 配置，`~/.cursor/mcp.json` 照常生效，平台 MCP 随会话下发。Cursor 在第一个检查点前被停下的 run 不保留输入，桥接把它并进下一条消息重发。`ErrSteerDeclined` 让 Cursor 拒绝的引导走"中断并排队"。
  - 模型：对齐 SDK 的基础 id + 参数。思考强度和 fast 是独立选项，按账号的模型列表解析成一个提供的组合，没指定的参数取 Cursor 的默认组合（fast 除外，默认关）。服务端目录换成 49 个基础模型，带各自的思考强度、默认强度和 fast；Solo 在用户没选强度时用 Cursor 的默认强度（此前是 max，否则最后一档），会话接口新增 `defaultReasoningEffort`，前端各选择器跟着用这个默认值。
  - 一次性迁移：旧的 270 个 cursor-agent id（如 `claude-opus-5-5-high-fast`、`cursor-grok-4.6-xhigh-fast`）在 Cursor 配置、Solo 会话、访客对话和运行记录里改写成基础 id，原 id 带的强度和 fast 填进还空着的强度 / 速度档位；跟随配置主模型的会话沿用旧主模型的强度。运行时不再认旧 id。Claude 的 thinking / 非 thinking 旧 id 落到同一个基础模型，按 Cursor 的默认（开思考）运行。
  - 镜像：`/opt/grasp/cursor-sdk` 按锁文件 `npm ci` 安装 `@cursor/sdk` 1.4.2，`IMAGE_LEVEL` 升到 3；E2B 模板探测 SDK 而非 `cursor-agent`；`test-agent-connect.sh` 检查 SDK 可加载，无密钥时跳过 Cursor 握手。
  - 切换 E2E：新增 `platform-sdk-fixture` 方式，镜像里的 `@cursor/sdk` 换成转发到 `agent-platform-wire-fixture.mjs` 的替身，真实 `bridge.mjs` 照常运行；替身校验模型组合是 Cursor 提供的变体、把思考强度带到假模型请求、按 agentId 持久化对话以覆盖接回。Cursor 矩阵改用 `gpt-5.5` 并覆盖 low / high / 默认（medium）。
- 为什么：`cursor-agent -p` 不能引导、停止后拿不到用量、每轮冷启动，模型 id 把强度和 fast 拼进名字，选择器只能列一长串组合。t3code 已改用 SDK；SDK 提供同一 Agent 的接回、引导、中断和逐次用量。
- 如何验证：
  - 单测：`cursorsdk` 的模型解析（精确 id 优先于别名、拒绝账号不提供的组合）、MCP 转换、脚本化假桥接下的回合 / 用量 / 接回 / 引导 / 未入检查点的中断重发 / 失败 / 关闭（`-race`）；服务端目录快照、迁移（两遍幂等、其它后端不动）、270 个旧 id 都落在目录提供的强度与档位上、Solo 的 Cursor 默认强度；前端默认强度与 Cursor 选项；替身的 Node 测试驱动真实 `bridge.mjs`（MCP、强度、用量、接回、找不到的 agent）。
  - 实测：真实密钥经 Go provider 跑通 Composer 2.5 的 shell 工具调用；换成 GPT-5.5（low、fast）的新进程接回同一 Agent 并记得上一轮的回答；运行中引导送达；中断后回合为 `cancelled` 且带部分用量，下一轮正常。
  - 门禁：sandbox / server / web 全量测试，`go vet`、golangci-lint、`vue-tsc`、eslint；`agent-e2e-sandbox.Dockerfile` 的替换循环在本机目录结构上跑过。
