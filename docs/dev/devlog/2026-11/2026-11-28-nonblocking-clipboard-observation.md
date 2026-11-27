# 2026-11-28（剪贴板观察与普通远端输入分离）

- 日期：2026-11-28
- 范围：`server/internal/handlers/preview_vnc.go`、`web/src/components/run/NovncPreviewPanel.vue` 及回归用例、`CHANGELOG.md`
- 做了什么：VNC 连接提前异步读取剪贴板快照，普通输入锁定已开始读取的快照，选择粘贴来源时再等待。失去焦点或写入本机内容后刷新快照。
- 为什么：画面转发和控制消息共用一条 WebSocket，串行查询沙箱状态会推迟第一次按键或点击。
- 如何验证：用挂起的沙箱状态响应确认普通输入可继续，粘贴仍使用同一快照。运行 VNC 剪贴板 race 测试及 Playwright 桌面用例。
