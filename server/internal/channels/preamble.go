package channels

import "github.com/cocofhu/grasp/internal/models"

// ChannelPreamble is a short session preamble injected on the first turn of a
// channel conversation's sandbox. It orients the PM Leader for IM-style replies
// and, critically, instructs it to embed shareable image URLs in markdown so
// the adapter can extract and re-upload them as native rich-media messages.
func ChannelPreamble(channelType string) string {
	imHint := "系统会按需转发到外部 IM，勿刷屏"
	tableRule := "但不要使用表格（QQ 原生 Markdown 不支持表格）。"
	switch channelType {
	case models.ChannelTypeWeCom:
		imHint = "系统会按需转发到企业微信，勿刷屏"
		tableRule = "可使用 Markdown 表格提升结构化表达。"
	case models.ChannelTypeFeishu:
		imHint = "系统会按需转发到飞书，勿刷屏"
		tableRule = "可使用 Markdown 表格提升结构化表达。"
	case models.ChannelTypeDingTalk:
		imHint = "系统会按需转发到钉钉，勿刷屏"
		tableRule = "可使用 Markdown 表格提升结构化表达。"
	case models.ChannelTypeQQ:
		imHint = "系统会按需转发到 QQ，勿刷屏"
	}
	return "你正在通过外部 IM 渠道（" + channelType + "）与用户对话，回复会被转发到聊天窗口。" +
		"请用简洁、可直接阅读的自然语言回答；可使用 Markdown（标题、加粗、斜体、有序/无序列表、块引用、链接、分割线、代码块）提升可读性，" +
		tableRule +
		"通过 pm-leader / context-store / memory-store 等 MCP 工具获取工作区进度、记忆与历史后再作答，不要编造。" +
		"长任务执行中，请在实质进展、阻塞/失败风险、或需要用户确认时，用单独一行并以标记开头汇报（" + imHint + "）：" +
		"[进度] … / [阻塞] … / [确认] …；工具调用细节与思考过程不要写成用户可见进度。" +
		"如果需要发送图片，请在回复中用 Markdown 图片语法给出可公网访问的图片直链，例如 ![](https://example.com/x.png)，" +
		"系统会自动把这些直链作为图片消息发送；请勿发送本地路径或需要鉴权的链接。"
}
