# 统一工作区校验、Solo 工作流 API、轮询与错误码

- **日期**：2026-11-10
- **范围**：`server/internal/services/workspace_*`、`server/internal/services/solo*.go`、`server/internal/handlers/workspace_*`、`server/internal/handlers/workflow_instructions.go`、`web/src/lib/api/clients/soloWorkflowClient.ts`、`web/src/lib/api/httpCore.ts`、`web/src/lib/composables/useVisiblePolling*`、`web/src/components/solo/`、`web/src/lib/shared/htmlPreviewSandbox*`、`web/src/locales/*/common.json`、`web/eslint.config.mjs`、`web/LIB_DOMAIN_MAP.json`、`.github/workflows/ci-web.yml`、`CHANGELOG.md`。
- **做了什么**：
  - 后端：两个相同的请求 ID 正则（`workspaceRequestID` / `workspaceJobID`）合并为一个；三处逐字复制的操作人校验与两处 Solo ID 校验改用 `validWorkspaceKey`；`workspace_*` 中的 `191` 改为具名常量 `workspaceKeyMax`（对应 `size:191` 索引列）。handler 的请求体上限与导入超时改为 `workspace_limits.go` 中的具名常量；补充要求持久化失败补上 `instruction_not_persisted` 错误码。
  - `solo.go`（约 2100 行）按职责纯移动拆为 `solo_model.go`（模型与推理强度）、`solo_preview.go`（预览只读视图）、`solo_turn.go`（`soloChat` 执行路径）、`solo_sandbox.go`（环境打开、恢复与回收回调），不改逻辑。
  - 前端：新增 `soloWorkflowClient`，Solo 工作流面板、补充要求、代码导入、环境释放不再在组件里拼 URL、定义接口类型。新增 `startVisiblePolling` / `useVisiblePolling`，标签页隐藏时暂停、回到前台立即刷新一次，替换 Solo 下四处各自的 `setInterval`。
  - 错误码：服务端文案为中文，非中文界面下按 `code` 显示 `common.apiErrors` 译文；中文界面保留服务端更具体的原文。
  - 新增 eslint 规则禁止直接使用 `randomUUID`，统一走 `randomUuid()`；HtmlPreview 的实例 ID 也改用它。
  - CI：`web-e2e` 超时由 30 分钟调为 45 分钟。正常一次 25–26 分钟，准备阶段在 10–16 分钟间波动，#1157 曾因此被取消。
- **为什么**：全局 review 指出的重复校验、魔法数字、组件绕过 API 层、轮询各自实现、英文界面显示中文报错等问题。
- **刻意保留**：
  - `objectstore` 服务块在两份 compose 中重复：`scripts/test-local-objectstore.py` 已逐项断言两者一致，拆共享文件需引入 `extends` 并分别覆盖数据目录，收益小。
  - `services` 中 workspace 拆独立子包、`useSoloChat` 拆分：前者 46 个文件大量互相调用私有函数，后者内部闭包共享状态，均需先设计接口再分步迁移，不适合与本次清理混在一起。
  - handler 中其余无 `code` 的中文报错（如 gate、agent、run）：需要逐个补错误码与文案，按模块另行处理。
- **如何验证**：`vue-tsc --noEmit`、`eslint .` 通过，新规则对 `crypto.randomUUID()` 与 `globalThis.crypto?.randomUUID?.()` 均报错；新增 `startVisiblePolling`、错误码翻译单测；全量 vitest 通过。`go build ./...`、`go vet`、`go test ./internal/services ./internal/handlers` 通过。
