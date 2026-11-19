# 2026-11-18 Agent 斜杠命令界面

## 范围

`web/src/components/chat/`、`web/src/components/run/`、`web/src/components/agent/`、`web/src/components/solo/`、`web/e2e/`、`server/internal/handlers/app_preview_*.go`、`sandbox-gateway/sandbox/web/`

## 改动

命令菜单展示图标、标题、说明和快捷指令，支持键盘导航。选择 `/skill` 后，可从已配置的技能中选择，也可输入名称。`/new` 在记录中加分隔线，`/compact` 显示回执，`/skill` 把技能名称和任务分开显示。应用预览和沙箱控制台使用会话返回的命令类型展示回执。

## 原因

让用户在发送前看清每条命令的作用，并在对话记录中辨认上下文变化和本轮选用的技能。

## 验证

组件测试验证选中、技能选择和技能标签。浏览器用工作流澄清、审批预览和 Solo 验证命令菜单。
