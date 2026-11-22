# 2026-11-22（统一 Steer 引导气泡样式）

- 日期：2026-11-22
- 范围：`server/internal/models/graph.go`、`server/internal/sandbox/timeline.go`、`web/src/components/run/AgentSteerBubble.vue`、`web/src/components/run/AgentTimeline.vue`、`web/src/lib/run/acpReply.ts`、`web/src/lib/run/acpTools.ts`、`web/src/lib/shared/types.ts`
- 做了什么：让 Agent 对话中的 Steer 气泡使用普通用户气泡的字号、内边距、背景透明度和圆角，中文前缀显示为「引导」，图片附件独立展示在气泡上方，并保留图片附件 URL、使用安全 Markdown 渲染复杂内容。
- 为什么：Steer 内容由用户在回合中追加，应该与普通用户消息保持一致的视觉层级，同时保留前缀说明其作用。
- 如何验证：运行 `npm run lint`、`npx vue-tsc --noEmit`、`npx vitest run src/components/run/AgentTimeline.test.ts src/components/run/AgentSteerBubble.test.ts` 和 `go test ./internal/sandbox ./internal/models ./internal/gateshare`。
