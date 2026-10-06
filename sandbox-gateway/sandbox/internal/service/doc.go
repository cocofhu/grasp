// Package service 为应用层：聚合 ACP Panel、WebSocket 广播、prompt 队列、权限与时间线。
//
// 分文件职责见仓库根目录 ARCHITECTURE.md 表格。心智摘要：
//   - 会话更新 / 广播 → Bridge.Broadcast（op:event / connected / queue_state）
//   - 入队与泵送 → ChatWithOpID、enqueueMu、pumpPromptQueue、executePrompt
//   - 助手卡片持久化 → 浏览器 IndexedDB（acp-bridge-chat）+ userTimeline（补已完成用户句）
//
// 扩展：宿主→浏览器新载荷在 acp.Panel/bridge_* 中 Broadcast；前端 session/update 在 web/static/js/conversation 注册处理器。
package service
