# 2026-11-03（应用预览分享与 Solo 预览对话）

- 日期：2026-11-03。
- 范围：`server/internal/{gateshare,handlers,models,router,services}`、`server/cmd/server`、`web/src/{components,lib,views}`。
- 做了什么：应用预览工具栏统一提供分享弹窗。链接绑定注册端口、沙箱和会话模式，匿名访客可打开预览并聊天。独立会话使用新的沙箱 Agent 对话，主会话链接允许读取文字历史并向现有队列发消息。Solo 登录用户的直连预览使用单独的票据接回原对话。
- 为什么：Solo 的预览工具栏此前无法获得票据，工作流和 Solo 也没有统一的应用分享入口。访客对话需要区分工作区和对话的共享范围。
- 如何验证：使用临时 SQLite 和模拟应用上游验证票据、两种分享模式、跨范围拒绝、撤销及代理隔离。用 Chromium 验证 Solo 直连预览的 Pick 与发送，以及分享弹窗和匿名页。
