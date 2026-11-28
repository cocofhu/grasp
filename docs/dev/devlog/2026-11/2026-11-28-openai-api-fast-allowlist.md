# 2026-11-28（补齐 OpenAI Responses API 的 Fast 模型）

- 日期：2026-11-28
- 范围：`server/internal/modelrouting/service_tier.go`、`server/internal/modelrouting/service_tier_test.go`、`CHANGELOG.md`
- 做了什么：为官方 Responses API 连接添加 `gpt-5.6-terra`、`gpt-5.6-luna` 和 `gpt-5.5` 的 Fast 能力。
- 为什么：这三个模型支持官方 API 的 Fast，能力表决定 Solo 是否显示开关及服务端是否接受该档位。见 [OpenAI Fast 支持表](https://openai.com/api-fast-mode/)。
- 如何验证：官方线路三个新增模型的白名单回归、服务端档位校验和运行时档位测试通过。
