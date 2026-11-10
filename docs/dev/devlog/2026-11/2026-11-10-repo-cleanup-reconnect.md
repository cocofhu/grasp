# 清理近期死代码并统一聊天 WebSocket 重连

- **日期**：2026-11-10
- **范围**：`web/src/components/agent/AgentChatTester.vue`、`web/src/lib/pm/usePmLeaderChat.ts`、`web/src/lib/run/wsReconnect*`、`web/src/components/dashboard/HomeModeSwitch*`（删除）、`web/src/components/ui/TokenUsageHoverTip.vue`（删除）、中英文文案、`web/package.json`、`server/internal/modelrouting/`、`server/internal/runtime/acp_backend.go`、`server/internal/{objectstore,filestore,payloadstore}` 包注释。
- **做了什么**：
  - Agent 测试聊天与工作区助手聊天改用共享的 `createWsReconnectController`，删除两份手写的计时器和重试计数。控制器新增可选 `delayFor(attempt)`，工作区助手保持原有 1s/2s/4s/8s/15s 无抖动退避、页面隐藏时不重连、回到前台立即重连。
  - 删除无引用的 `HomeModeSwitch`、`TokenUsageHoverTip` 组件及其专用文案，卸载未使用的 `@vue-flow/background` 依赖。
  - 删除生产代码无调用的 `modelrouting.TokenHub`、`OpenCodeCatalogVariant`、`WithSavedEffort`、`runtime.KimiPlatformBaseURL` 及随之无用的 Kimi 地址常量（站点地址由 sandbox-gateway 解析）。
  - 补充三个存储包的分层说明：objectstore 是字节后端，filestore 在其上负责引用格式与序列化，payloadstore 负责启动时选择、校验并组装两者。
- **为什么**：全局 review 发现同一重连逻辑在多处各自实现，退避、卸载和前后台处理不一致；近期功能迭代留下了无入口的组件、文案和函数；三个存储包名字相近，注释未说明层次。
- **刻意保留**：
  - `SharedAppPreviewChat` 的重连会先轮询补齐历史、仅在连接稳定一段时间后才重置退避、上限 30s，语义与共享控制器不同，暂不合并。
  - `WorkspaceSnapshotJobHandler` / `WorkspaceSnapshotJobs` / `workspacearchive.Restore` 是未接线的工作区快照任务基础设施，handler 注释明确要求先提供身份、授权和执行权限再注册路由，属于分阶段落地而非死代码。
  - `formatguard.Check`、各 `SetClock`、`*ForTest` 等是测试接缝，保留。
  - `AppTopbar.vue` 是 StatusMetrics e2e 的 harness，保留。
- **后续拆分计划（本次不改）**：
  - `web/src/lib/solo/useSoloChat.ts`（约 1375 行）：按职责拆为 ① 本地发送与确认（`LocalSend`、`adoptLocalSend`、`confirmLocalSend`、`retryPendingSend`）；② 历史与过程分页（`applyMessages`、`readHistory`、`loadEarlier`、`loadProcess`）；③ 帧处理与续传（`onFrame`、`noteSeq`、`requestResync`、`checkStall`、`openWs`，并改用共享重连控制器）；④ 队列操作（`dropQueued`、`takeQueued`、`reorderQueued`）；⑤ 模型设置。主 composable 只负责组装与生命周期。
  - `server/internal/services/solo.go`（约 2100 行）：拆出 `solo_model.go`（模型/推理强度对齐与校验，约 1048–1487 行）、`solo_preview.go`（`Preview*` 只读视图）、`solo_turn.go`（`soloChat` 的 `execute`、`waitSandbox` 等执行路径）、`solo_sandbox.go`（`openSandbox`、恢复与回收回调），`solo.go` 只保留服务构造、会话 CRUD 与发送入口。纯移动、不改行为，按文件分 PR 以便 review。
- **如何验证**：`vue-tsc --noEmit`、`eslint .` 通过；全量 vitest 通过（含 Agent 测试聊天断线重连/卸载不重连、工作区助手退避时序与前后台重连、新增 `delayFor` 单测）。`go build ./...`、`go vet` 通过，`go test ./...` 通过（`internal/engine` 在满载并行时偶发超时，单独重跑通过，与本次改动无关）。
