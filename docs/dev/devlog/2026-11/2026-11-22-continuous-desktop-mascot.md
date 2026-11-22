# 2026-11-22（桌面启动页连续吉祥物动画）

- 日期：2026-11-22
- 范围：`sandbox-gateway/sandbox/internal/desktop/launcher.html`
- 做了什么：将桌面启动页顶部的静态猫形角色改为 A 方案风格的内嵌 SVG 角色，增加待机、开心、大笑、惊讶、困倦、生气、担心、思考、挥手、跳跃、奔跑和探头动作。动作通过逐帧数值插值连续过渡，支持悬停、点击、Enter 和空格触发互动，并保留 `prefers-reduced-motion` 处理。
- 为什么：让角色适配当前启动页的气泡、时钟和应用网格布局，同时让多个表情和动作在主页里连贯播放。
- 如何验证：`node --check /tmp/launcher-script.js`；`cd sandbox-gateway/sandbox && go test ./...`；`cd sandbox-gateway/sandbox && go vet ./...`；在 Chromium 预览页加载修改后的 `launcher.html`，检查页面截图和连续动作录屏，录制规格为 1920×920、60fps、16 秒。
