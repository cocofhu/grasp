# 2026-11-28（为 OpenAI Responses 请求明确选择档位）

- 日期：2026-11-28
- 范围：`server/internal/modelrouting/`、`server/internal/runtime/model_binding.go`、沙箱 Codex 适配器及其原生 CLI 探针、`CHANGELOG.md`
- 做了什么：按官方 Fast 表补齐 GPT-5.4、GPT-5.4 mini 和表中列出的其他 Responses 模型。官方连接的 Codex 会话通过沙箱内的本机转发写入档位。关闭 Fast 时，Responses 请求及压缩请求显式使用 `default`。启用时，为选中的模型写入 `priority`。
- 为什么：Codex CLI 会省略 `default`，而 OpenAI 对未指定档位的请求沿用项目设置。显式标准档位让 Solo 的开关生效。见 [Responses API 档位](https://developers.openai.com/api/reference/resources/responses/methods/create/)。
- 如何验证：原生 Codex CLI 在旧运行时的标准请求因缺少 `service_tier` 失败。在加速、关闭、隔离和恢复共 8 个场景中验证实际请求，模型为 GPT-5.4 mini。Go 测试覆盖两种 Responses 路径、上游请求数据和兼容线路隔离。
