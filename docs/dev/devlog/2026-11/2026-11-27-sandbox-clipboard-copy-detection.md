# 2026-11-27 根据沙箱剪贴板更新选择粘贴来源

- 日期：2026-11-27
- 范围：`sandbox-gateway/sandbox/internal/handler`、`server/internal/{handlers,sandbox,services}`、noVNC 剪贴板、浏览器回归用例和中英文帮助。
- 做了什么：转发复制或剪切快捷键前记录 X11 CLIPBOARD 的版本。下一次粘贴时，只有检测到同一 X11 连接中的剪贴板已更新，才切换到沙箱剪贴板。检测过程只传递版本和所有权状态。等待检测期间先确定粘贴内容，再按原顺序发送后续键鼠事件。
- 为什么：LXTerminal 的普通 `Ctrl+C` 会中断前台任务。只根据按键切换来源，会让中断后的粘贴使用沙箱里之前的内容。
- 如何验证：Xvfb 用例覆盖真正取得 CLIPBOARD、同一窗口再次更新和失去所有权。Go 用例覆盖版本不变、连接重建和公共分享。Playwright 覆盖终端中断后的本机粘贴和沙箱剪贴板更新后的粘贴。用真实 noVNC、Chromium 验证实际复制后粘贴沙箱内容，并验证紧随本机粘贴的 Ctrl+A。用真实 LXTerminal 中断前台任务后，验证 Ctrl+Shift+V 粘贴本机中文和 Emoji。
