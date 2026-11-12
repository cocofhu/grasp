# 2026-11-12（Solo 支持 ask_question / ask_form）

- 日期：2026-11-12
- 范围：`server/internal/mcp/outcome.go`、`server/internal/models/solo.go`、`server/internal/services/{solo,solo_artifact,onboarding,onboarding_names}.go`、`web/src/components/solo/{SoloAskCard,SoloSessionPane}.vue`、`web/src/lib/solo/{acpTurn,useSoloChat}.ts`、`web/src/lib/api/apiTypes.ts`，以及对应测试。
- 做了什么：`workflowOnlyTool` 去掉 `ask_question`、`ask_form`，Solo 与工作流节点一样按 Agent 能力授予这两个工具。Solo 回合开始前清掉 host 上残留的待答问题，回合结束后取出本轮的问题和表单，存进 assistant 消息新增的 `questions`、`forms` 列。前端在最后一条已结束的回复下显示 `SoloAskCard`（推荐项预选、单选/多选、「其他」、表单），提交文本沿用澄清的「我的选择:」「我的填写:」格式作为下一条消息发送；回合刚结束仍保留实时回复对象时，`mergePair` 把已存的问题合并过去。引导新建的 Solo Agent 默认授予 `ask_question`、`ask_form`、`set_artifact_preview`、`set_preview`，`AGENTS.md` 改为使用 artifact-store 的 `ask_question`，仍禁止编码工具自带的 AskQuestion。
- 为什么：Solo 原先硬编码屏蔽这两个工具，Agent 只能在文字里列选项；默认 Solo Agent 也没有任何能力。现在与普通 Agent 一致，不再有 Solo 专属的屏蔽逻辑。
- 如何验证：`TestSoloToolSurfaceExcludesWorkflowActions`（Solo 列出并可调用两个工具，问题与表单进入 host）、`TestSoloReplyKeepsQuestionsRaisedThisTurn`（回复持久化本轮问题与表单，丢弃上一轮残留）、`TestOnboardingCreatesEmptySoloOutsideWorkflow`（默认能力与新说明）；Web：`SoloAskCard.test.ts`、`solo.test.ts`、`SoloSessionPane.test.ts`（只在最后一条回复显示卡片，提交后发出消息并收起）。
- 未做：已有 Solo Agent 不自动补能力；编码工具自带的 AskQuestion 仍无法回答。
