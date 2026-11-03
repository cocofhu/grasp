# 2026-11-03（预览浮层和工作区会话隔离）

- 日期：2026-11-03
- 范围：`web/src/{App.vue,router,lib/shared/authGuard.ts}`、`web/vite.config.ts`、`server/internal/{auth,handlers,router}`、`sandbox-gateway/sandbox/internal/previewinject`。
- 做了什么：浮层只挂载预览路由。工作区页面禁止被嵌入，工作区 Cookie 接口和 WebSocket 校验请求来源，预览代理隔离平台登录 Cookie。
- 为什么：预览浮层启动时可能先渲染工作区壳，同主机不同端口的应用也需要与工作区会话隔离。
- 如何验证：用测试会话覆盖跨端口请求、WebSocket、Cookie 透传和匿名分享票据。浏览器测试观察预览浮层启动过程，并检查被嵌入的工作区路由。
