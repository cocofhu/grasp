# 恢复 Solo 聊天体验与工作流命令入口

- **日期**：2026-11-10
- **范围**：`web/src/components/chat/`、`web/src/components/run/AgentTimeline*`、`web/src/components/solo/SoloSessionPane*`、`web/src/lib/chat/useComposerCommands.ts`、`web/src/lib/solo/`、`web/src/lib/run/useRunLive*`、`web/src/views/DashboardView*`、`web/src/views/RunChatView.vue`、`web/src/views/RunLiveView.vue`、中英文文案、`web/LIB_DOMAIN_MAP.json`、`web/e2e/solo-workflow*`、`web/e2e/run-live-continue*`、`CHANGELOG.md`。
- **做了什么**：移除 Solo 输入框工具栏常驻的 `/workflow` 按钮。输入 `/` 或已知命令前缀后显示菜单，鼠标、Enter 或 Tab 选择只打开工作流配置，仍需用户明确启动。首页沿用已有的空 Solo 创建和工作流配置路径，工作流关闭及嵌入聊天不提供命令。把历史过程加载放回原有“用时”折叠，展开才读取首个事件分页，后续分页、错误重试和旧记录提示均位于折叠内；优先使用真实记录的耗时。保存正文与过程分页分开处理，完整读取后恢复原来的消息、思考和工具顺序。
- **为什么**：对照 `523dd4bfa` 的中文输入与引用修复、`a026df789` 的队列交互、`401095a2d` 的执行活动展示和 `3a09b8cfb` 的共享聊天兼容行为。`609a0848d` 引入历史分页后，先展示的正文缺少已保存在服务端的思考与工具步骤，原组件因而误判无需折叠；完成后的新回复重新读历史时也会丢失折叠，同时额外出现独立过程按钮。本次将持久化过程的可用性明确传给现有时间线，保留按需读取，不改 Agent 执行、消息受理、重试身份、队列或环境恢复流程。
- **如何验证**：真实 ChatComposer、SoloSessionPane 和首页回归覆盖命令键盘选择、IME/229、Shift+Enter、`@` 引用、附件、取消、选区校验、禁用和嵌入模式；时间线、Solo 历史转换与过程缓存回归覆盖完成后折叠、真实耗时、分页正文、进度消息、失败重试及重新读取历史。Playwright 用实际输入框验证用户手动启动、多个独立工作流、功能关闭及 390px 视口。执行仓库要求的 Web lint、类型检查、覆盖率、构建和浏览器关键路径检查，以及基础 CI 脚本与文档构建、安全审计。

同批修复历史执行页永久加载：未关联 Solo 的已结束 Run 在新聊天入口中复用旧 Live 页面，而旧逻辑会跳回同一个 `/runs/:id`。跳转前页面已停止状态更新，同路由又不会卸载，因此一直显示加载。历史聊天模式改为在原地址展示已有 conversation-chain 和终态；只有显式打开执行详情才展示详情，继续聊天仍需用户配置。真实浏览器回归同时挂载 RunView、RunChatView 和 RunLiveView，覆盖完成、失败、取消、空历史及切换记录，不再把内部 Live 页面 stub 掉。
