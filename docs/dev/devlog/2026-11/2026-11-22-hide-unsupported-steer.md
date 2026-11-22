# 2026-11-22 隐藏不支持的 steer 入口

- 日期：2026-11-22
- 范围：`server/internal/{engine,handlers,runtime,sandbox,services}`、`web/src/components`、`web/src/lib`
- 做了什么：把沙箱桥接层的原生 steer 能力传到 Solo、Agent Studio、工作流对话、审批和访客会话的队列状态。前端只在当前会话声明支持原生 steer 时显示闪电按钮，原生不支持的 Agent 继续沿用中断后下一轮的服务端降级路径。
- 为什么：不支持原生 steer 的 Agent 点击后不会把消息并入当前回合，继续显示该入口会误导用户。
- 如何验证：`go test ./internal/engine -run TestDialogueContractFixtures -update-dialogue-fixtures -count=1`、服务端各受影响包的编译测试、`npx vue-tsc --noEmit`、定向 Vitest 150 项通过，`npm run lint -- --no-warn-ignored` 通过。
