# 2026-11-22（应用预览接口改为单一对象）

- 日期：2026-11-22
- 范围：`server/internal/mcp`、`server/internal/services`、`server/internal/database`、`server/internal/handlers`、`server/internal/gateshare`、`server/internal/runtime`、`web/src/components/run`、`web/src/lib/api`、`web/src/lib/inbox`、预览 E2E
- 做了什么：`ListPreviewPorts` / `HasHealthyPreviewPorts` / `ListPreviewKeepalivePIDs` 改为 `ActivePreview` / `HasHealthyPreview` / `PreviewKeepalivePID`；`PreviewStore` 改为 `ReplacePreview`、`ActivePreview`、`ActivePreviewForSandbox`，删除可选接口和多余的回退查询；节点接口改为 `GET /runs/:id/nodes/:nodeId/preview` 返回 `{preview}`，Solo 资源与公开分享 DTO 改为单个 `preview`；前端 `AppPreviewPanel`、`PublicAppPreviewPanel`、`ReactArtifactStage` 改为单对象 props；旧数据库迁移同时按节点和沙箱只保留最新登记；合并三段重复的行转换代码，删除旧隔离源 ProxyURL 重算。
- 为什么：上一轮已把运行时限制为单一活动预览，但接口仍是列表形状，调用方到处写 `len(ports) == 1 && ports[0]`，迁移结果也和“每沙箱一条”的运行时约束不一致。
- 如何验证：`go vet ./...`；mcp、database、handlers、gateshare、engine、runtime、router、services 预览相关测试通过，新增重启后跨节点注入器回滚测试和按沙箱合并的迁移测试；`npm run lint`、`vue-tsc --noEmit`、生产构建通过；完整 Vitest 570 个文件 6165 项通过；Playwright 预览相关 8 个 spec 共 60 项通过。
