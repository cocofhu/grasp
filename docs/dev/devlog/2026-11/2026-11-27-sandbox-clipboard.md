# 2026-11-27 沙箱桌面粘贴本机文本

- 日期：2026-11-27
- 范围：`web/src/components/run/NovncPreviewPanel.vue`、中英文文案与帮助、`server/internal/{handlers,services,sandbox}`、`sandbox-gateway/sandbox` 的桌面 API、相关用例与 `CHANGELOG.md`。
- 做了什么：接管浏览器或电脑画面后，可用本机粘贴快捷键，也可从工具栏打开剪贴板，把纯文本粘到当前窗口或放入沙箱剪贴板。手动放入后，画面保持焦点时直接使用沙箱剪贴板。平台经现有沙箱会话通道写入 X11 的 UTF-8 剪贴板，写入成功后才转发粘贴按键。x11vnc 仅接收传统剪贴板消息，避免把本机文本广播给其他观看者。
- 为什么：Grasp 嵌入 noVNC 时只接了画面与键鼠，x11vnc 的传统剪贴板编码也无法保留中文和 Emoji。使用浏览器的粘贴事件和可编辑输入框，普通 HTTP 部署也能使用这个入口。
- 如何验证：Playwright 的 `web-e2e` 用例覆盖接管前后的快捷键、手动剪贴板和焦点切换。真实 noVNC、平台代理、沙箱运行时与 Chromium 验证了中文、Emoji、多行文本、无 Clipboard API 的 HTTP 粘贴，以及 128 KiB 文本完整传输和超限提示。Web 终端实测 `Ctrl+Shift+V` 和方括号粘贴。Go 用例验证请求限制、旧运行时提示和 RFB 按键序列。
