# 2026-11-05（E2B Embed 全量真机验收）

- 日期：2026-11-05。
- 范围：E2B Embed 本地控制面、`sandbox-gateway/gateway`、Grasp Solo、E2B 部署文档。
- 做了什么：在专用 Embed 环境完成 Grasp/Gateway 健康检查、双运行沙箱配额与 owner 隔离、SSH/IDE/ACP/noVNC/CDP、运行时包与模型配置注入、真实 DeepSeek 对话、Solo 重启恢复、工作区预览服务和 callback 鉴权验收。补充验证了 Agent capability 配置可授予 `set_preview`，并清理了测试沙箱。
- 为什么：确认第三种 E2B 部署方式不会只在单元测试中通过，而能复用 Grasp 的真实用户路径，并记录 provider 运行状态与应用可访问性之间的边界。
- 如何验证：E2B 部署离线 Python 测试 10/10 通过；gateway 全量 `go test ./...`、`go test -race ./...`、`go vet ./...` 和覆盖率 91.5% 通过；server runtime/sandbox/services/handlers 测试通过。真机中 Grasp/Gateway health、Solo DeepSeek round-trip、SSH、IDE HTTP、ACP、CDP、VNC WebSocket、运行时注入和 callback 未授权请求通过。Embed 的首次 start/connect 曾返回 provider HTTP 500，随后 provider 已恢复而 relay 路由短暂不可用；加入同一 provider guest 的有界重试后，重建 gateway、stop/start、SSH、IDE、CDP 和 VNC WebSocket 均通过，启动恢复和状态 refresh 也覆盖同一发布竞态。pause/resume 仍需按 provider 版本单独验收。授予 `set_preview` 后新会话的 MCP 回连出现未授权，未将沙箱内 HTTP 200 误报为平台预览注册成功。
