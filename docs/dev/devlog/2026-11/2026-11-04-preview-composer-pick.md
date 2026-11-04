# 2026-11-04（预览聊天加号菜单增加取点）

- 日期：2026-11-04。
- 范围：`web/src/components/{chat/ChatComposer.vue,solo/ReferencePlusMenu.*,run/ClarifyChat*}`、`web/src/views/EmbedNodeChatView.vue`、`web/src/lib/{inbox/embedChat.ts,shared/previewPickScript.test.ts}`、中英文语言包、三个 `preview-pick.js` 副本、`web/e2e/{conversation-workspace,live-variants}.spec.ts`。
- 做了什么：只有宿主预览 iframe 提供的能力会在聊天加号菜单显示「取点」，支持 Solo、工作流、分享主会话和独立会话。通过已校验来源和窗口的消息启动宿主页取点；暂时收起聊天并把焦点交给宿主取点按钮，选中后直接加入聊天上下文并退出取点，Esc 取消也回到同一个聊天 iframe，保留草稿与附件。失效会话和 Agent 正在操作页面时不接受取点请求。工作流即使没有引用权限也能取点，不开放引用入口。
- 为什么：预览聊天只能从宿主工具条开始取点，编写消息时缺少就近入口；跨源 iframe 隐藏后仍可能保留键盘焦点，必须显式交给宿主页才能接收 Esc。取点作为消息引用，与现有 Live 工具条操作共存。
- 如何验证：菜单与工作流单测验证能力隔离、引用权限和草稿保持；宿主脚本用例覆盖普通／分享会话的选中与取消、来源窗口校验、会话失效、Agent 页面操作互斥和 Live 共存。35 项 Playwright 回归通过，覆盖 Solo、工作流、两种分享模式的真实 iframe 通信、草稿／附件保持和中英文菜单。Web lint、类型检查、全量 6235 项单测（行覆盖率 91.71%）和生产构建通过；服务端完整门禁与 90.5% 核心覆盖率、沙箱检查与 96.6% 核心覆盖率、运行时包构建、文档构建／审计及 actionlint／shellcheck 均通过。
