# 2026-11-03（分享链接打开直连应用与浮动对话）

- 日期：2026-11-03。
- 范围：`web/src/views`、`web/src/components/run`、`web/public/preview-pick.js`、`server/internal/handlers`、`sandbox-gateway/sandbox/internal/previewinject`。
- 做了什么：Solo 和工作流的分享链接跳转到注册应用的直连地址，访客在应用上打开浮动对话并取点。保留主会话和独立会话两种分享权限。注入器在缺少运行级 MCP 凭据时用沙箱启动配置里的服务地址校验短票据。
- 为什么：分享者和访客需要同样的整页应用体验。旧 Solo 沙箱只在 Agent 内配置产物 MCP，注入器此前无法确认票据来源。
- 如何验证：覆盖 Solo 和工作流的分享权限与撤销、外部主机的 iframe 限制、缺少 MCP 凭据的注入器，以及匿名浏览器里的两种对话模式、取点和刷新行为。
