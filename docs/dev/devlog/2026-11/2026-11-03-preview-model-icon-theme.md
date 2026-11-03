# 2026-11-03（预览模型图标跟随浮层主题）

- 日期：2026-11-03
- 范围：`web/src/lib/shared/theme.ts`、`web/src/components/agent/VendorMark.vue`、`web/src/components/workspace/CredentialProviderLogo.vue`、`web/e2e/conversation-workspace.spec.ts`。
- 做了什么：模型和服务商图标读取页面实际渲染的主题。单色图标按主题反色，彩色图标保留原色，Kimi 使用对应主题的品牌图标。
- 为什么：预览浮层可单独切换主题，图标之前读取的是用户保存的偏好，导致 Claude 图标在浅色浮层显示成白色。
- 如何验证：浏览器用例在保存深色偏好的情况下切换浮层主题，校验 Claude 的按钮和菜单图标，以及 Gemini 的彩色图标。主题单测覆盖应用和清除覆盖值。
