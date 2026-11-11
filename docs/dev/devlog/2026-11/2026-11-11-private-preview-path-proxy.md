# 私有网络预览改走路径代理

- **日期**：2026-11-11
- **范围**：`server/internal/handlers/preview_private.go`、`server/internal/handlers/preview.go`、`server/internal/handlers/gate_share_public_preview.go`、`server/internal/mcp/preview.go`、`server/internal/services/preview.go`、`server/internal/config`、`compose.e2b.yaml`、`sandbox-gateway/deploy/e2b/README.md`。
- **做了什么**：删除 `privatepreview` 随机子域名、`GRASP_PREVIEW_ADVERTISE` 及相关中间件。`sandbox.private_endpoints` 开启时预览统一使用 `/preview/:runId/:nodeId/:port/` 路径代理：登记的沙箱和当前节点沙箱不一致时返回 410；注入器端口返回的 `X-Grasp-Upstream` 必须等于登记端口；转发时去掉 `e2b-*`、`proxy-authorization` 请求头和响应头。旧数据中存的子域名地址在列表和详情中改写回路径代理地址。
- **为什么**：#1198：私有网络部署被迫准备通配域名和证书，而路径代理已能满足同样的隔离要求。
- **如何验证**：`handlers/preview_private_test.go` 覆盖未绑定返回 410、上游端口不符被拒、保留头被剥离；内部单测覆盖运行时脚本路径豁免与升级请求。
- **限制**：预览应用中以 `/` 开头的绝对路径请求和 Vite HMR 需要应用使用相对路径，与路径代理原有行为相同。
