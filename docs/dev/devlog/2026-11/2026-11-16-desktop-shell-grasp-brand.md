# 2026-11-16（桌面镜像：Grasp 品牌桌面、启动台改为新标签页、Dock 图标修复）

- 日期：2026-11-16
- 范围：`sandbox-gateway/sandbox/{Dockerfile.desktop,scripts/desktop-session.sh,scripts/vnc-preview.sh,internal/desktop,desktop,share/desktop}`、`sandbox-gateway/sandbox/README.md`
- 做了什么：
  - 启动台改为 Chromium 新标签页：`share/desktop/newtab` 扩展不再跳转到 `127.0.0.1:6091`，改为 iframe 嵌入，地址栏保持空白（显示搜索提示），标签页图标为 Grasp。浏览器默认打开新标签页；Chromium 加 `--disable-features=NtpFooter`，去掉新标签页底部的扩展署名和「自定义 Chrome」按钮。
  - 启动台页面重做：Grasp 幽灵猫吉祥物 + 气泡说明、大号时钟与问候、白色圆角图标格；背景是淡紫渐变，散布低透明度的猫形涂鸦；图标缺失或加载失败时显示强调色字母块。
  - Dock 图标修复：原来的 `.desktop` 用主题图标名，当前图标主题下找不到，Plank 显示空白窗口图标。现在运行时包自带 `share/desktop/icons/{grasp,browser,terminal,files}.svg`，`.desktop` 改为绝对路径。「我的电脑」用 Grasp 标志。浏览器项的 `StartupWMClass` 写入 Chromium 带配置目录的 WM_CLASS 实例名，运行中的浏览器窗口归到浏览器图标下，不再单独出现 Chrome for Testing 图标。
  - 桌面外观：壁纸改为 Grasp 紫色渐变；镜像新增 `xcompmgr`，Plank 用自带的 `Grasp` 主题，显示为半透明圆角 Dock，窗口带阴影（Dock 自身不加阴影）。
  - 启动台图标：`grasp-desktop launcher` 优先使用运行时包的 `icons/<id>.svg`（`GRASP_DESKTOP_SHARE`），跳过浏览器无法渲染的 XPM；`build-apps.py` 在所有目录里先找 SVG / PNG，最后才用 XPM，并补上 hicolor 96x96 / 32x32 目录与 3D Slicer 的 `Slicer.png`，ParaView、3D Slicer 有了真实图标。
  - 启动台 CSP 增加 `frame-ancestors 'self' chrome-extension:`：只有新标签页扩展能嵌入启动台，其他网页不能用 iframe 诱导点击启动应用。
  - `xcompmgr` 只是可选增强：脚本用 `command -v` 检测，旧镜像上不启动合成器（Dock 为不透明矩形），因此不提升 `IMAGE_LEVEL`。
- 为什么：原来的桌面像未装修的 Linux：地址栏露出 `127.0.0.1:6091`，Dock 图标空白，没有 Grasp 标识。参照 dot 的电脑页，做成带品牌的个人电脑。
- 如何验证：
  - `go test ./internal/desktop/`、`go vet`、`python3 -m unittest test_build_apps` 通过；新增启动台图标优先级 / XPM 跳过、构建脚本 XPM 兜底的测试；两个脚本均通过 `bash -n`。
  - 在本沙箱的真实桌面（Xvfb :99）上用仓库里的脚本和新构建的 `grasp-desktop` 重新拉起桌面壳、Plank、xcompmgr 和 Chromium，整屏截图检查新标签页、Dock 和壁纸，并在 Grasp 的「电脑」页签中查看。
