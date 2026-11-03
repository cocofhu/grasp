# 2026-11-03（固定重启接管测试的模型工具调用）

- 日期：2026-11-03。
- 范围：`.github/workflows/e2e-restart.yml`、`server/e2e/restart_reattach_test.go`。
- 做了什么：重启接管 CI 使用现有 OpenAI 协议测试模型，明确下发 OpenCode 的 shell 与 MCP 工具调用。沙箱接管后由 MCP 写入必需的实现产物，再报告节点完成。测试仍检查真实沙箱进程跨重启继续执行、原工作流仅执行一次、排队中的第二个运行得到接纳。开发者仍可用 DeepSeek 运行同一个场景。
- 为什么：一次 main 检查在沙箱启动后等不到真实模型发起的 shell 调用，未能走到要验证的重启阶段。
- 如何验证：用带 `e2ereal` 标签的测试验证 onboarding 和模型夹具注册，运行模型协议用例并检查 workflow。
