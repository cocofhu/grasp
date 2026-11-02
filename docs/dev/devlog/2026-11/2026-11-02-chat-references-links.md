# 2026-11-02（聊天里的 @ 引用原位显示，链接新窗口 / 沙箱浏览器新标签打开）

- 日期：2026-11-02。
- 范围：`web/src/lib/shared/markdown.ts`、`web/src/components/run/ChatUserMessage.vue`、`web/src/lib/solo/referenceTokens.ts`、`web/src/components/solo/SoloSessionPane.vue`、`web/src/styles/chat.css`、`web/src/components/run/ReviewShell.vue`、`web/src/lib/run/stageLinks.ts`、`web/src/components/run/ReactArtifactStage.vue`、`web/src/components/run/NovncPreviewPanel.vue`、`server/internal/browser/`（`types.go`、`rod.go`）、`server/internal/handlers/preview_vnc.go`、`server/internal/handlers/gate_share_public_preview.go`。
- 做了什么：
  - `tightHeading` 的 `start()` 用了不带多行标志的 `^`。marked 会拿段落去掉首字符后的文本调用它，于是 `@#815 @#814 这两个看看` 在 `@` 后被切开，剩下的渲染成 `<h1>`。现在只在换行之后认 `#标题`，顺带修好了 `正文\n#标题` 不被识别为标题的问题。
  - 已发送消息里的 `@label` 原位显示成 `ReferenceChip`：`inlineReferences()` 把引用分成「正文里有」和「其余」，`ChatUserMessage` 先用私用区字符占位，渲染 Markdown 后再把 chip 挂进占位节点（沿用应用的 appContext）。正文里找不到的引用仍放在气泡上方。
  - 聊天 Markdown 的链接统一加 `target="_blank" rel="noopener noreferrer"`（仅对聊天渲染器的 DOMPurify 放行 `target`）。`ReviewShell` 拦截 `.chat-prose` 里 loopback 地址（`localhost`、`127.x`、`0.0.0.0`、`[::1]`、`*.localhost`）的点击：展开右侧资源面板，经 `StageLinks.openUrl` 切到浏览器，`NovncPreviewPanel` 在桌面就绪后发送 `{type:'navigate', action:'open', url}`。没有沙箱或只读嵌入时退回新窗口。
  - 服务端 `Page.OpenTab`：用 CDP `Target.createTarget` + `Target.activateTarget` 在同一窗口新开标签并切到前台，会话仍绑定原页面。公开分享访客的 `open` 与 `goto` 一样只允许 loopback。
- 为什么：截图里 `@#815` 被渲染成大标题；引用胶囊和正文分离不直观；聊天里的链接在当前页打开，而 loopback 地址只在沙箱内可达。
- 已知限制：新标签打开后，面板地址栏、前进后退和取点标注仍作用于原页面。
- 如何验证：`web` 下 `vue-tsc --noEmit`、相关文件 eslint、`vitest run src` 全部通过（新增 markdown、`inlineReferences`、`ChatUserMessage` chip、`isLoopbackUrl`、`ReviewShell` 链接拦截、`ReactArtifactStage.openUrl`、`NovncPreviewPanel` open 请求用例）。`server` 下 `go build ./...`、`go vet ./internal/...`、`go test ./internal/browser ./internal/handlers` 通过。本机无 Chromium/Xvfb，未在真实沙箱验证新标签。
