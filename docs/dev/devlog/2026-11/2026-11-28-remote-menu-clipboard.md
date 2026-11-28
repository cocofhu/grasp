# 2026-11-28（保留远端菜单复制的内容）

- 日期：2026-11-28
- 范围：`web/src/components/run/NovncPreviewPanel.vue`、`web/e2e/sandbox-clipboard.spec.ts`、`server/internal/handlers/preview_vnc.go`、`CHANGELOG.md`
- 做了什么：接管画面后，在转发鼠标和触屏按下之前记录 X11 剪贴板状态。粘贴快捷键复用已有的状态检查，发现所有权更新时在远端粘贴。
- 为什么：用应用菜单复制时没有复制快捷键可供探测，本机文本可能覆盖刚复制的远端内容。
- 如何验证：Playwright 复现本机有文本时从远端右键菜单复制再粘贴，剪贴板与控制台共 12 项通过；服务端 VNC 和剪贴板专项测试通过。
