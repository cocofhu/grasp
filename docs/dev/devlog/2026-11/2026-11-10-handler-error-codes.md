# 补齐 gate / agent / run 报错错误码与公开分享页报错本地化

- 日期：2026-11-10
- 范围：`server/internal/handlers`（gate、agent、run、workflow）、`web/src/lib/api/httpCore.ts`、`web/src/lib/inbox/gateShareLink.ts`、`web/src/views/PublicGateApprovalView.vue`、`common.apiErrors` 文案

## 做了什么

- 登录态接口中仍只返回中文 `error` 的 8 处补上 `code`：`review_session_unavailable`（gate 3 处）、`run_read_failed`、`agent_data_clear_failed`、`agent_data_rename_failed`、`workflow_reference_rename_failed`、`invalid_request_body`。中文文案保持不变。
- `httpCore` 抽出 `localizedErrorText(code, serverText)`：中文界面保留服务端更具体的中文文案，其余语言有对应 `common.apiErrors.<code>` 时显示译文，否则回退原文。`req()` 与公开分享页共用这一规则。
- 公开分享接口（`{error: 机器码, message: 中文}`）的 12 处重复 `throw Object.assign(new Error(body.message || body.error || ...))` 合并为 `publicShareError`；公开审批页回复失败也改用 `publicShareErrorText`。
- `common.apiErrors` 中英文补齐上述新码与公开页使用的 16 个机器码（rate_limited、csrf、nonce、permission_denied、visitors_full 等）。

## 为什么

英文界面下这些接口报错只能显示中文；公开页同样的取错逻辑复制了十余份，新增错误码时容易漏改。

## 如何验证

- 新增 `gateShareLink.errors.test.ts`：中文界面保留服务端文案、英文界面按码翻译、未知码回退原文、公开 API 抛出的错误带本地化文案与原始 body。
- 核对新增码名未与其他登录态接口的 `code` 冲突，管理面板 `shareApiErrorMessage` 走的无 `code` 路径不受影响。
- `vue-tsc`、`eslint`、全量 `vitest`（599 文件 / 6437 用例）、`go vet` 与 `go test ./internal/handlers` 均通过。
