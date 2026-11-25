# 2026-11-25（预览暴露抽象层与按网关可选子域名）

- **日期**：2026-11-25
- **范围**：`server/internal/previewexpose`、`server/internal/config`、`server/internal/sandbox`、`server/internal/models`、`server/internal/services`、`server/internal/mcp`、`server/internal/handlers`、`server/internal/router`、`server/cmd/server`、`compose.e2b.yaml`、`server/config.example.yaml`、`server/CONFIGURATION.md`、`GATEWAY.md`、`sandbox-gateway/deploy/e2b/README.md`。
- **做了什么**：
  - 新增 `server/internal/previewexpose` 抽象层（`Exposer`、可选 `Releaser` / `Inbound`、`Policy`），第一版内置 `direct` / `subdomain` / `none` 三种策略；
  - 拆分「预览暴露策略」与「路径代理安全加固」：`config.setDefaults` 不再因存在 `gateway` 通道而自动改写 `sandbox.private_endpoints`，新增 `SandboxConfig.PreviewProxyHardening()` 保持路径代理与审批分享代理加固行为不变；
  - 支持全局 `preview.expose` 与连接级 `sandbox.preview_expose` / `sandbox.gateways[*].preview_expose` 独立配置与启动校验（`gateway` 通道拒绝 `direct`、基址拒绝 IP、`production` 要求 `https` 且跨 eTLD+1、禁止基址重叠），启动时日志打印各连接生效策略；
  - 网关归属由 `sandbox.GatewayClient.ConnectionID` 权威提供并经 `PreviewService.PreviewConnection` 暴露给 `mcp`，未知绑定 fail-closed 到 `none`；
  - `RunPreviewPort` 新增 `ExposeStrategy`（仅存最终策略）与可空唯一索引 `ExposeKey`（`*string`），同沙箱同端口复用 key、更换端口或沙箱轮换、`Bind` 失败降级为 `none`；
  - 从 `PreviewProxy` 抽出共享 `livePreviewUpstream` 内核（代次校验 + E2B 暂停自动唤醒 + 活跃续租 + 刷新上游），在 `router.go` 的 `cors()` 之前挂载 `PreviewExposeMiddleware`，实现跨基址归属校验、Host 隔离防穿透、`X-Grasp-Upstream` 校验、保留头与 `cf_session` 双向剥离、剥离 `Set-Cookie` 的 `Domain` 属性防子域间 Cookie Tossing、`Origin`/`Location` 改写与 WebSocket 支持。
- **为什么**：#1368：E2B 与 `gateway` 认证通道此前无 `DirectURL`，失去新标签打开、取点、浮动聊天抽屉、Live 与应用分享；同时多网关混连场景下全局 `private_endpoints` 会一刀切压制直连网关。
- **如何验证**：`go test -race ./...`、`go vet ./...`、`go run ./cmd/gen-configdoc -check` 全部通过，覆盖生效规则矩阵、启动校验、多网关共存、存量登记与策略/基址切换、子域名入站安全与 WebSocket 升级回归。
