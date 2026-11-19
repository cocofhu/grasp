# 2026-11-19（云电脑支持声音，websockify 换成 Go 的 vnc-ws）

- 日期：2026-11-19
- 范围：`sandbox-gateway/sandbox/internal/vncws/`、`sandbox-gateway/sandbox/cmd/desktop/main.go`、`sandbox-gateway/sandbox/scripts/vnc-preview.sh`、`sandbox-gateway/sandbox/scripts/desktop-session.sh`、`sandbox-gateway/sandbox/Dockerfile`、`server/internal/handlers/sandbox_audio.go`、`server/internal/handlers/sandbox_vnc.go`、`server/internal/browser/service.go`、`server/internal/router/router.go`、`web/src/lib/shared/desktopAudio.ts`、`web/src/lib/composables/useDesktopAudio.ts`、`web/src/components/run/NovncPreviewPanel.vue`
- 做了什么：
  - 沙箱：`vnc-preview.sh` 启动 PulseAudio，加载 null sink `grasp`，socket 固定在 `/tmp/grasp-pulse/native`，并通过 `PULSE_SERVER` 传给 Chromium 和桌面应用（写入 `session.env`）。`:6080` 改由 `grasp-desktop vnc-ws` 提供：`/audio` 为每个连接起一个 `parec` 录 `grasp.monitor`，按 20 ms 一帧发送 s16le 48 kHz 立体声，全零帧不发送；其余路径与 websockify 一样桥接到 x11vnc。运行时里没有 `grasp-desktop` 时回退 websockify；镜像里没有 `pulseaudio` 时桌面照常，只是没有声音。
  - 服务端：新增 `/sandbox-audio/:sandboxId/ws`，鉴权和沙箱解析与 `/sandbox-vnc` 共用（不会唤醒已停止的沙箱）。端点复用已解析的 novnc 地址并拼上 `/audio`，网关、E2B relay、K8s 和 Docker 的端口暴露都不用改。
  - 前端：noVNC 预览工具栏、控制台工具栏和「电脑」底栏增加声音按钮。点击后才建立 `AudioContext`（浏览器要求用户手势），用 AudioBufferSourceNode 按时间排队播放；欠载后预留 80 ms 余量，排队超过 0.4 s 的帧直接丢弃，避免声音越来越落后于画面。公开分享票据没有声音路由，不显示按钮。
- 为什么：VNC 协议不传音频，原来的预览栈既没有声卡也没有音频通道。放在现有 6080 端口上按路径区分，是改动层数最少的方案；同时少了一个 Python 转发进程（websockify）。没有用 WebCodecs 或 AudioWorklet，因为它们只在安全上下文里可用，而部分部署走的是 HTTP。
- 如何验证：
  - `go test ./internal/vncws ./cmd/desktop`（沙箱）、`go test ./internal/handlers ./internal/browser ./internal/router`（服务端）、vitest `desktopAudio`、`useDesktopAudio`、`NovncPreviewPanel`。
  - 在 E2B 沙箱里实测：按 `vnc-preview.sh` 的参数起 pulseaudio，`paplay` 播放 2 秒 440 Hz 测试音，`vnc-ws /audio` 收到 84 帧 PCM，`/` 返回 `RFB 003.008` 握手；设置了 `PULSE_SERVER` 的 Chromium 播放 `<audio>` 时，`grasp.monitor` 能录到信号。
  - 已知限制：PCM 未压缩，有声音时约 1.5 Mbps。已在运行的沙箱热更新运行时后，websockify 仍在跑，要等预览栈重启才会切到 vnc-ws；声音还需要镜像里有 `pulseaudio`。
