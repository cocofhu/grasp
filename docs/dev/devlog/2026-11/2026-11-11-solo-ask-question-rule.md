# 引导创建的 Solo Agent 不再调用 AskQuestion

- **日期**：2026-11-11
- **范围**：`server/internal/services/onboarding.go`、`server/internal/services/onboarding_names.go`、`server/internal/services/onboarding_solo_test.go`。
- **做了什么**：引导创建 Solo Agent 时写入一份按向导语言（中文或英文）生成的 `AGENTS.md`，要求 Agent 需要用户选择或确认时，在回复里用文字列出选项、说明推荐，然后结束本轮等待回复，不调用 AskQuestion。已存在的 Solo Agent 不会被改写。
- **为什么**：Cursor 的 AskQuestion 需要客户端在回合中途回答。默认的 `cursor-agent -p` stream-json 模式没有回传通道，CLI 直接按跳过处理；`agent acp` 模式下沙箱对 `cursor/ask_question` 回 `-32601`。两种情况在 Solo 里都显示为失败的工具调用，问题也没有卡片可选。Claude 和 CodeBuddy 已用 `--disallowedTools AskUserQuestion` 禁用同类工具，Cursor CLI 没有对应参数，因此用提示词约束。artifact-store 的 `ask_question` 只用于工作流澄清，Solo 有意不提供。
- **如何验证**：`TestOnboardingCreatesEmptySoloOutsideWorkflow` 断言中英文两种语言下 Solo Agent 只带这一份 `AGENTS.md`，且语言正确。
- **后续**：如果需要可点选的卡片，可以在前端识别 AskQuestion 工具调用，把用户的选择作为下一条消息发出。
