# 2026-11-20（打开云电脑/浏览器预览时不再把窗口提前）

- 日期：2026-11-20
- 范围：`server/internal/browser/service.go`、`server/internal/browser/rod.go`
- 做了什么：
  - `reuseDesktopLocked` 复用已有桌面页面时不再调用 `presentDesktop`，同时去掉只为这一处存在的 `desktopPresenter` 接口。
  - `rodEngine.OpenDesktop` 接管浏览器已有页面（`adopted`）时不再调用 `presentDesktop`，只有自己新建的页面才摆放窗口。
  - 进入点选元素（`SetInspect(true)`）时仍会调用 `presentDesktop` 做就绪检查，行为不变。
- 为什么：每次打开云电脑或沙箱浏览器预览，服务端都会对接管的页面执行 `Page.bringToFront`、`Activate`、恢复普通窗口、拉伸到工作区，并启动每 300 ms 一次的窗口守护。在有 Openbox 的云电脑上，这会把某个 Chromium 窗口提到最前、取消最小化或最大化，用户在桌面里摆好的窗口每次连接都被打乱。连接只应附着画面，不应操作桌面。
- 如何验证：`go build ./...`、`go vet ./internal/browser/`、`go test ./internal/browser/ ./internal/handlers/ ./internal/services/`、`./scripts/cover-check-server.sh 90`。
- 已知限制：浏览器模式（无窗口管理器）下接管的页面不再在连接时铺满画面，依赖 `vnc-preview.sh` 启动参数 `--window-size=1920,1080 --window-position=0,0`；进入点选元素后才按内容区固定视口。连接前的就绪探测（`probeTabCreate`，`PUT /json/new` 后关闭）保持不变，它用来发现 CDP 假活。
