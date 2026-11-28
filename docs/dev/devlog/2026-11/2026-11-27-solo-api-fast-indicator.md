# 2026-11-27（OpenAI API 接入的 Fast 开关）

- 日期：2026-11-27（Asia/Hong_Kong）
- 范围：`server/internal/modelrouting/`、`server/internal/services/`、`server/internal/runtime/service_tier_test.go`、`web/src/components/solo/SoloModelSelect.vue`、`web/src/lib/solo/`、`web/src/lib/api/apiTypes.ts`。
- 做了什么：服务端为 OpenAI 官方 Responses 接入的受支持模型提供只读 Fast 能力。Codex 复用既有的速度档位选择和请求路径，Solo 仅在所选 API 路径提供 Fast 时显示可用的闪电按钮。
- 为什么：闪电表示一项可操作的加速能力。按实际模型和接入判断，可以让每个 API 账户显示正确的控制项。
- 如何验证：server Go 全量测试、lint、vet、配置文档和运行时回归通过，核心覆盖率 90.3%。Web lint、类型检查、构建及 6197 项单测通过，行覆盖率 91.88%。两项 Solo 端到端回归通过。Chromium 模拟官方 API、兼容接口和 Codex PIN，新建会话发送 Fast，已打开会话可关闭 Fast。本地 Codex 0.172.0 对回环 Responses 测试服务发送 `priority`，关闭后停止显式请求加速。docs 构建与审计、actionlint、shellcheck、品牌检查和离线工具回归通过。
