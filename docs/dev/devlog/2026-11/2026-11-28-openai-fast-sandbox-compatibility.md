# 2026-11-28（OpenAI Fast 沙箱升级与代理兼容）

- 日期：2026-11-28
- 范围：`server/internal/{runtime,sandbox,services}/`、`sandbox-gateway/sandbox/internal/provider/codex/`、`sandbox-gateway/sandbox/scripts/reprime.sh`
- 做了什么：从当前绑定还原升级前的指纹。指纹和 Git 授权匹配时，只向已有沙箱同步模型线路和可用模型两项派生配置，在空闲时重启后端。Responses 转发器按协议读取代理设置，使用 `ALL_PROXY` 作为后备，并沿用 `NO_PROXY`。
- 为什么：增加 Fast 能力后，旧测试沙箱使用相同模型与凭据也会被拒绝。使用通用代理的沙箱需要保持标准和 Fast 请求的出网路径。
- 如何验证：覆盖已有测试沙箱的重新打开、首条消息和真实凭据变更；通过实际脚本验证原有工作区文件和环境；用本机代理验证两档请求与 HTTPS CONNECT。
