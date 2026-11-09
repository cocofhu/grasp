# 2026-11-09 IDE 浏览器入口导航到当前工作区

- 日期：2026-11-09
- 范围：`server/internal/handlers/sandbox.go`、`workspace_ide_navigation_test.go`。
- 做了什么：已取得当前授权与 canonical consumer 后，仅将 IDE 顶层 GET 导航到当前 folder，移除旧 workspace 参数；保留主题等其它参数，避免重复跳转。静态资源、文件接口、WebSocket 与其它方法保持原代理路径。
- 为什么：真实 K8s 的 code-server 会从浏览器 window.location 选择工作区。仅重写代理上游的 query 会让普通入口仍显示空 Welcome，或让旧浏览器参数与当前 canonical root 不一致。
- 如何验证：真实 HTTP 客户端跟随入口跳转，检查唯一 canonical folder、移除旧 workspace、保留其它参数、两次请求后结束；覆盖已对齐 URL、资源请求、WebSocket、POST 和无 canonical grant 的不跳转回归。另保留修复前真实 Chromium 证据，部署后需继续原环境实际 IDE 相对文件验收。
