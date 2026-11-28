# 2026-11-27 沙箱剪贴板评审修复

- 日期：2026-11-27
- 范围：`server/internal/handlers`、`web/src/components/run/NovncPreviewPanel.vue`、剪贴板回归用例、中英文帮助和 `CHANGELOG.md`。
- 做了什么：VNC 和 ACP 代理写入 Solo 沙箱剪贴板前共用 `Solo.Require` 校验。沙箱画面里的复制、剪切快捷键让后续粘贴在当前焦点内使用沙箱剪贴板。本机无文本时也会尝试沙箱粘贴。粘贴暂时释放的 Ctrl、Shift 或 Cmd 仍被按着时，会在下一个普通按键前恢复。电脑沙箱的剪贴板窗口可选终端粘贴。
- 为什么：避免其他账号写入 Solo 沙箱，保留接管画面后的远端复制和连续快捷键操作，并匹配内置 LXTerminal 的粘贴键位。
- 如何验证：服务端回归覆盖 Solo 所有者和非所有者的 VNC、ACP 写入。Playwright 覆盖复制后的粘贴、本机无文本时回退和按住修饰键。用真实 noVNC、X11 和 Chromium 验证远端复制、空剪贴板回退、按住 Ctrl 连按 V、A，以及松开 Ctrl 后的输入。
