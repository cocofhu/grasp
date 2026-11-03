# 2026-11-03（Solo 预览实时回复）

- 日期：2026-11-03
- 范围：`web/src/lib/solo/soloEmbedClient.ts`、`web/e2e/conversation-workspace.spec.ts`。
- 做了什么：Solo 预览浮层的 WebSocket 与 HTTP 请求使用同一个预览会话路径前缀。
- 为什么：消息提交和历史读取有会话前缀，实时连接漏了前缀。发送后的界面会停在等待沙箱就绪，直到刷新读取历史。
- 如何验证：通过新标签打开应用预览，发送带取点的消息，再从预览会话的 WebSocket 发送状态、回复和结束帧。原页面会展示回复并收起进行中提示。
