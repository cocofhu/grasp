# 2026-11-03（修复 Solo 应用与产物预览）

- 日期：2026-11-03。
- 范围：`server/internal/{handlers,mcp,models,router,runtime,services}`、`server/cmd/server`、`web/src/components/{run,solo}`、`web/src/lib/api`。
- 做了什么：Solo 创建沙箱时按 Agent 能力配置预览注入。应用预览注册后保留注入端口，代理按实际映射探测上游。产物钉选写入当前 Solo 沙箱，资源接口按会话所有者读取并稳定排序预览列表，右侧面板展示应用和产物预览。HTML 缩略图的点击交给外层卡片。Solo 的 MCP 列表与调用均过滤依赖工作流对话的工具。
- 为什么：Solo 允许调用预览工具，但此前缺少工作流路径上的沙箱预览配置和界面接线，钉选也会误查工作流 ReAct 会话。沙箱内已经能访问的应用因此无法在平台预览。
- 如何验证：使用临时 SQLite、模拟网关、Vitest 和 Chromium 覆盖 Solo 创建、预览注册和代理、产物钉选、会话归属、沙箱切换及面板交互。Go 全量测试通过，核心覆盖率为 90.7%。
