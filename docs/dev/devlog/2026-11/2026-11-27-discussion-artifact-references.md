# 2026-11-27（聊天支持 Discussion 和产物引用）

- 日期：2026-11-27。
- 范围：`web/src/components/{chat,solo,run}/`、`web/src/lib/{api,solo}/`、`web/src/locales/`、`web/src/views/DashboardView.vue`、`web/e2e/{conversation-workspace,mobile-browser-contract}.spec.ts`、`server/internal/{models,services,mcp,handlers,router}/`、沙箱运行时的 Discussion 读取脚本及其打包、回归测试、中英文 README 和概念指南。
- 做了什么：`@` 选择器和「+」菜单增加 Discussion 与产物。Discussion 使用仓库凭据搜索 GitHub GraphQL，支持状态筛选、编号查询和粘贴链接。沙箱读取讨论时按仓库地址选择凭据，独立翻页读取评论和楼中回复，同时返回采纳答案。公开讨论可直接读取 GitHub 的公开网页。产物按工作区可见范围搜索，发送时记录版本。Agent 可用 `read_reference` 读取文本摘录，并通过当前会话或运行授权下载原文件。消息中的产物链接也指向该版本。手机上的选择器随可视区域调整位置，六种类型可一键切换。候选标题最多显示两行，产物显示文件格式，继续挑选时仍能看见已选引用。数量和「包含已关闭」筛选随当前类型显示，切换时保留输入焦点。
- 为什么：在聊天里选中讨论或已有产物后，Agent 可以直接查看相应内容。排队期间和后续回合仍使用发送时的产物版本。
- 如何验证：`go test ./...` 通过，引用相关竞态测试重复 20 次通过，覆盖 Discussion 查询和链接解析、产物版本保持及跨运行访问拒绝。前端 6237 项 Vitest 通过，行覆盖率 91.90%。Playwright 验证了工作流聊天可一次发送 Issue、Discussion 和产物。移动 Chromium 和 WebKit 覆盖了中英文 320px、390px 下的跨类型搜索、六类标签同排可见、一键切换、包含已关闭讨论、缩短视口后选取产物以及连续选择时已选引用的可见性。用本地 HTTP 验证了工作流环境变量和 Solo MCP 配置两种下载方式。匿名网页和分两条一页的 GitHub GraphQL 请求均从 `cli/cli#8794` 读到同一条已采纳的楼中回复。离线用真实凭据选择程序和两个模拟仓库验证了工作流重读、翻页和私有内容的访问边界。
