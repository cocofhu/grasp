# 2026-11-15（沙箱暂停可见可恢复，工作流沙箱失联后不误判模型切换、右侧面板自动重连）

- 日期：2026-11-15
- 范围：`server/internal/services/`（solo 暂停检测与 wake）、`server/internal/runtime/chat_route.go`、`server/internal/runtime/acp_react.go`、`server/internal/sandbox/`、`web/src/components/solo/SoloSessionPane.vue`、`web/src/components/run/`、`web/src/lib/solo/useSoloChat.ts`、locales
- 做了什么：
  - Solo：会话视图新增 `sandbox.paused`（运行中、非 worktree、provider 报告 exited/stopped）。复用 `releaseIfVanished` 的 ProbeStatus 发现暂停后通过 status 帧推送（内存去重），恢复成功（含发消息自动唤醒）后再推一次。新增 `POST /api/solo/sessions/:id/wake`（不开放 embed）。前端「停止环境」按钮在暂停时原位变为「恢复环境」，浏览器/终端/IDE 标签页显示暂停占位和恢复按钮。
  - 工作流对话路由：`AlignChatRoute` 先检查 ACP 是否连接。沙箱不可达（ACP 断开、ErrConnClosed、网络错误、网关/桥 502/503/504）视为“未对齐”，不回滚、不报 model_switch_failed，`ReactReply`/`ReviseInPlace` 重建会话后再对齐。`parkReactSession` 记录实际启动的模型组。
  - 工作流右侧面板：`ReactArtifactStage` 在会话忙碌约 3 秒后、空闲时、以及终端/IDE 失联时静默刷新节点沙箱。没有沙箱且页面可见时每 5 秒轮询，最多 2 分钟，只在沙箱 id 变化时重连。终端 WS 在同一 id 上按 5/10/20 秒退避重连。
- 为什么：E2B AutoPause 后页面没有任何提示也无法恢复；沙箱失联时对话路由误判切换失败并回滚模型；右侧终端/IDE 在沙箱重建后停留在断开状态。
- 如何验证：`go test ./...`（server）；`npx vitest run --maxWorkers=2`（web 全量 585 文件通过）；`vue-tsc --noEmit`、`npm run lint` 无新增问题。新增用例：`solo_paused_test.go`、`chat_route_test.go`、`visit_model_test.go`、`ReactArtifactStage.sandbox.test.ts`、`SandboxStageSurface.test.ts`、`SoloSessionPane.test.ts`、`useSoloChat.test.ts`。未在真实 E2B 环境手工验证。
