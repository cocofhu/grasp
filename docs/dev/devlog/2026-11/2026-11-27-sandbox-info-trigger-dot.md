# 2026-11-27（简化 Solo 沙箱信息按钮）

- 日期：2026-11-27
- 范围：`web/src/components/sandbox/SandboxInfoButton.vue`、对应单测、`CHANGELOG.md`
- 做了什么：移除顶栏沙箱信息按钮右上角的状态圆点。面板页眉继续显示状态和对应颜色。
- 为什么：简化顶栏上的信息图标。
- 如何验证：面板单测检查按钮和运行、暂停、异常三种状态。用 Chromium 验证面板布局和交互。
