# 2026-11-22（应用预览收敛为单一活动项）

- 日期：2026-11-22
- 范围：`server/internal/mcp`、`server/internal/services`、`server/internal/models`、`server/internal/handlers`、`web/src/components/run`、`web/src/lib/shared`、预览 E2E、预览文档
- 做了什么：`set_preview` 改为按沙箱替换当前活动预览，服务端删除多端口注册、注入器置换和历史 ItemKey 逻辑；应用预览和公开预览移除多标签切换；为旧数据库增加单预览迁移，切换失败时恢复之前的注入器；同步更新预览提示、测试夹具和文档。
- 为什么：每个沙箱只有一个 noVNC 桌面，取点注入也只对当前端口生效，多 Preview 标签会留下无法独立操作的历史入口。
- 如何验证：数据库迁移、MCP、服务端、模型和处理器专项测试通过；新增旧数据库迁移与注入器回滚测试；此前 `go test ./...` 与 `go vet ./...` 通过；Web 相关 Vitest 116 项通过，`vue-tsc --noEmit`、ESLint、生产构建通过；`app-preview.spec.ts` 5 项通过；`gate-share-link.spec.ts` 与 `conversation-workspace.spec.ts` 24 项通过。完整 Web coverage 运行还触发了仓库既有的 `useHomeApproveChat`、`WorkflowCanvas` 与布局测试失败以及本地 3000 端口连接错误，未归因于本次改动。
