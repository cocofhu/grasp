# 2026-11-03（预览浮层和工作区会话隔离）

- 日期：2026-11-03
- 范围：`web/src/{App.vue,router,lib/shared/authGuard.ts}`、`web/vite.config.ts`、`server/internal/{auth,config,handlers,router}`、`server/cmd/server`、`sandbox-gateway/sandbox/internal/previewinject`。
- 做了什么：浮层只挂载预览路由。工作区页面禁止被嵌入，工作区 Cookie 接口和 WebSocket 校验请求来源，预览代理隔离平台登录 Cookie。同一请求带两个有效会话 Cookie 时不认任何一个，并清掉所有可能匹配的副本。`/preview/` 在沙箱里运行，保留应用自己的 Cookie 和 Authorization，放开弹窗、对话框和下载，响应 `Origin: null` 的跨源请求，并注入内存版 Web Storage。工作区页面可用 `GRASP_FRAME_ANCESTORS` 允许指定来源嵌入。
- 为什么：预览浮层启动时可能先渲染工作区壳，同主机不同端口的应用也需要与工作区会话隔离。Cookie 不区分端口，预览页的脚本能写入 Path 更窄的 `cf_session`，所以只剥响应头不够。
- 如何验证：用测试会话覆盖跨端口请求、WebSocket、Cookie 透传和匿名分享票据。浏览器测试观察预览浮层启动过程，并检查被嵌入的工作区路由。另用 Chromium 确认沙箱页面里模块脚本、Web Storage、`document.cookie` 和带凭据的跨源请求可用。
