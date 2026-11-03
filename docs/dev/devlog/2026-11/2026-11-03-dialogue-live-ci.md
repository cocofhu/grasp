# 2026-11-03（稳定对话快照与 Live 复审预览）

- 日期：2026-11-03。
- 范围：`server/internal/engine/dialogue_fixtures_test.go`、`web/src/lib/inbox/liveVariants*.ts`、`web/e2e/live-variants.spec.ts`。
- 做了什么：对话快照按场景独立采集、比对和清理，等模拟 Agent 收到已持久化的人类消息及队列发布终态后再采样，归一化时复制行数据。Live 聊天在先收到新状态、后收到旧回执时，用可跨 iframe 传输的会话数据同步页面。
- 为什么：固定延时可能采集到对话中间态，快照归一化也不应修改引擎缓存的对话行。Live 的旧回执路径会返回 Vue 响应式代理，浏览器无法通过 `postMessage` 传输它，导致候选切换未同步到聊天。
- 如何验证：重复运行 Go 对话快照测试并启用竞态检测，比对原有基准 JSON。Vitest 校验较旧回执返回的数据可被结构化克隆。Chromium 回归测试将较旧回执延迟到页面收到新状态后再交付，并验证候选切换与确认。
