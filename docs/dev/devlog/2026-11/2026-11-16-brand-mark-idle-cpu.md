# 2026-11-16（侧边栏小猫常驻动画导致空闲页面 CPU 高）

- 日期：2026-11-16
- 范围：`web/src/components/shell/BrandCharacterMark.vue`、`CHANGELOG.md`
- 做了什么：去掉小猫 `sway` / `breathe` / `blink` 三个 `infinite` 动画的常驻播放，改为只在悬停（小猫本身或 `sidebar-brand-lockup`）时循环；空闲时只在已有的 `idle-look` / `idle-tilt` 动作期间眨眼一次（1.4s）。
- 为什么：用户反馈 Solo 对话页空闲时单个标签页 CPU 占用 20%–28%。动画作用在 SVG 内部的 `<g>` / `<ellipse>` 上，不能交给合成器处理，每帧都要重新绘制和栅格化。小猫在所有页面的侧边栏常驻，所以任何页面开着都在持续重绘。
- 如何验证：用 Playwright 有头 Chromium（Xvfb）挂载 `workspace-config.html?view=/solo` 和 `/solo/:id`（接口全部 mock），空闲 10s，通过 CDP `SystemInfo.getProcessInfo` 统计各进程 CPU。修复前 browser 进程约 22%、GPU 进程 62%–70%；关闭全部动画后接近 0；修复后 browser 2%–3%、GPU 5%–7%（只剩间隔出现的 idle 动作）。单独测量 `inbox-boot` 旋转图标的开销相同，但它只在会话启动中显示，属于真实的加载状态，没有改。`BrandCharacterMark` / `AppSidebar` 单测，`e2e/yume-motion`、`e2e/live-variants` 通过。
