# 开发日志

仓库内部流水，不发布到公开站点。最新条目在上。不回填 git 历史，不写排期。

对外行为变化仍按原有惯例另记仓库根目录 `CHANGELOG.md` 的 `Unreleased`。没有对外行为时不要改 `CHANGELOG.md`。

## 条目模板

复制下面这一块，填完后放在本文件「记录」一节的最上方。

```text
### YYYY-MM-DD

- 日期：YYYY-MM-DD（写下这条的当天，不是排期）
- 范围：路径
- 做了什么：
- 为什么：
- 如何验证：
```

## 记录

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/sandbox/{acp,acp_turn,acp_turn_test}.go`、`server/internal/runtime/{acp_timeline,acp_timeline_test,acp_sandbox,acp_react}.go`、`server/internal/engine/approve_first_message_test.go`
- 做了什么：ACP 客户端记录当前有几个调用方在读事件通道（连接握手、一轮对话、等待取消确认）；没有读取方时 `readLoop` 只更新 queue_state 镜像，不再把帧塞进通道。真正丢帧时的告警限为每分钟一次，并带丢弃计数。时间线的事件日志轮询只在这一轮正在执行（本客户端有轮次在跑，或 bridge 报 busy）时每 2 秒拉一次，轮次结束后再拉最后一次；拿不到 ACP 客户端时保持原来的行为。补了一个测试，确认审批节点暂停后投递首条消息的那一轮在会话快照里显示为 busy。
- 为什么：run 3f471c4b 从 03:10 起持续打印 `acp event channel full, dropping message`。原因是 grasp 节点暂停后 ACP 连接一直开着，但两轮之间没人读通道，bridge 的广播很快把 512 的缓冲占满。同时时间线每 2 秒拨一次 `/ws` 又断开，沙箱日志里刷出大量连接和 broken pipe，每次断开还会触发 bridge 再广播一次 queue_state。
- 如何验证：`go test ./internal/sandbox/ ./internal/runtime/ ./internal/engine/`（新增：暂停的连接被灌 2000 条 queue_state 不缓冲、不告警且镜像正确；告警限频；读取方计数不泄漏；空闲不轮询、busy 时轮询、结束后停；首条消息那一轮 busy）；`-race` 通过；golangci-lint 0 issues。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/mcp/preview{,_test,_more_test}.go`、`server/internal/services/preview_keepalive{,_test}.go`
- 做了什么：`set_preview` 改为先探测端口（最多 5 次、间隔 500ms），能访问后再保活，保活后再探测一次确认，整体超时 60 秒。端口不可达时，按沙箱里 `ss` 看到的监听地址给出提示：没有进程监听、只监听回环地址（附实际地址，要求改为 0.0.0.0）、或已监听但无响应。保活失败时错误里带上脚本自己的 `keepalive: …` 原因。
- 为什么：run 3f471c4b 里 Agent 在服务还没监听时调用 `set_preview`，先跑的保活脚本以 `no listener` 退出 1，但输出被丢弃，Agent 只看到 `Process exited with status 1`，看不出该怎么修。探测只请求一次，也不说服务实际监听在哪里。
- 如何验证：`go test ./internal/mcp/ ./internal/services/`（新增先探测再保活、不可达不保活、三种监听地址提示、保活原因透传）；golangci-lint 0 issues。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/handlers/{preview_vnc,sandbox_vnc,gate_share_public_preview}.go`、`server/internal/handlers/preview_vnc_test.go`、`web/src/lib/shared/vncReconnect{,.test}.ts`、`web/src/components/run/NovncPreviewPanel{.vue,.test.ts}`、`web/src/locales/{zh-CN,en}/pages.json`、`web/LIB_DOMAIN_MAP.json`
- 做了什么：三个 VNC 代理收到客户端任何消息（含二进制 RFB 和新增的 `ping`）都刷新会话活跃时间，限频 15 秒一次。预览面板在页面可见且已连接时每 60 秒发一次 `ping`。服务端 `closed` 和意外断线改为本地化提示；`idle`、`desktop-closed` 和断线按 1s、2s、4s… 退避自动重连（最长 30s，最多 6 次），页面在后台时等切回前台再连；`superseded`、`evicted` 不自动重连，保留手动按钮。公开分享页自动重连同样走 `reconnect-request` 换新票据。
- 为什么：活跃时间只在文本控制消息时刷新，只看不操作的观看者在 `TabIdleTTL`（300 秒）后被 sweep 以 `idle` 断开，面板直接显示原文 `idle`，只能手动重连。
- 如何验证：`go test ./internal/handlers/`、golangci-lint 0 issues；`vue-tsc --noEmit`、eslint、全量 vitest 通过（新增 idle 自动重连、后台等待、superseded 不重连、重试用完出按钮、心跳只在可见时发送）。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/browser/rod.go`、`server/internal/browser/rod_desktop{,_live}_test.go`、`server/internal/handlers/preview_vnc{,_test}.go`、`sandbox-gateway/sandbox/scripts/vnc-preview.sh`、`web/src/components/run/NovncPreviewPanel{.vue,.test.ts}`、`web/src/locales/{zh-CN,en}/pages.json`
- 做了什么：noVNC 预览窗口固定在 0,0、1920x1080 正常态，标签栏和地址栏留在屏幕上，视口锁定为工具栏下方的内容区；每次 setWindowBounds 后轮询到外框匹配且连续三次读数一致再判定，失败也不再移动窗口。`SetInspect(false)` 关闭时也带 highlightConfig，失败退回 `Overlay.disable`；关闭失败推送 `inspect-off-failed`，面板显示提示。「仅观看」提示改为 `text-txt2`、11px、带描边。
- 为什么：#724 把工具栏移出屏幕，但调整窗口后立刻读 innerHeight 读到旧值，线上算出工具栏 263px（实际约 88px），窗口被多推上去约 175px，页面顶部导航被裁，且失败后窗口停在错误位置。Chromium 的 `Overlay.setInspectMode` 在 mode none 时也要求 highlightConfig，否则报 "highlight configuration parameter is missing"，取点模式一直开着，「取消标注」无效。提示文字在浅色主题下对比度约 2.3:1。
- 如何验证：`go test ./internal/browser/ ./internal/handlers/`；`PREVIEW_DESKTOP_LIVE=1` 实机 Xvfb 测试通过（内容区 1919x992、工具栏像素为浏览器 UI、进出全屏后取点命中、取点开关均返回 nil），同一 Chromium 上不带 highlightConfig 的 none 请求复现了线上报错；golangci-lint 0 issues；`vue-tsc --noEmit`、eslint、`NovncPreviewPanel.test.ts` 通过。

### 2026-10-05

- 日期：2026-10-05
- 范围：`.github/scripts/{actionlint,shellcheck-error,govulncheck-check}.*`、`.github/workflows/{ci,ci-web,ci-sandbox,security}.yml`、`.golangci.yml`、`govulncheck-allowlist.json`、`docs/scripts/audit-check.mjs`、`docs/audit-allowlist.json`、`AGENTS.md`、`CONTRIBUTING.md`，以及 server / gateway / sandbox 里为通过 errcheck、unused 做的机械修改
- 做了什么：始终执行的 ci 工作流加上 actionlint 和 error 级 shellcheck，根目录按一层 `*.sh` 通配收集。security 工作流对三个 Go 模块跑 govulncheck，过期豁免会失败；docs 也像 web 一样拦 high/critical npm 漏洞。共享 golangci 对非测试代码启用 errcheck 和 unused，sandbox-go 补上 `go vet`。
- 为什么：这些检查仓库文档里已经点名过，但一直没有接进 CI；工作流和 shell 脚本出错要等真正跑到才会发现。
- 如何验证：rebase 到最新 main 后，shellcheck-error 和 actionlint 退出 0；三个模块 golangci-lint 0 issues，`go vet` 通过；`cover-check-server.sh 90` 为 91.4%。本地 npm 镜像不支持 audit 接口，govulncheck 需要 Go 1.25.14，这两项以 PR CI 为准。待办：`golang.org/x/crypto` 两条豁免（GO-2026-6354/6355）2026-12-31 到期，补丁要求 Go 1.26，到期前复查，不要静默续期。

### 2026-10-05

- 日期：2026-10-05
- 范围：`sandbox-gateway/scripts/{mock-chat-model.mjs,mock-chat-model.test.mjs,test-agent-connect.sh,agent-ws-check.mjs}`、`.github/workflows/ci-sandbox.yml`、`AGENTS.md`、`CONTRIBUTING.md`
- 做了什么：宿主机起一个不出网的 OpenAI 兼容 mock chat model（夹具 `ci-e2e`，回复 `GRASP_AGENT_E2E_OK`），`test-agent-connect.sh` 让 opencode 通过 `host.docker.internal` 每次都跑一轮对话；`scripts` 作业跑 mock 契约测试。是否打到替身改为看本轮对话前后 hits 的差值。
- 为什么：以前没有 `CURSOR_API_KEY`（fork PR）时整段对话断言都被跳过，镜像里 Agent 能不能真正对话没人验证。
- 如何验证：`node --test scripts/mock-chat-model.test.mjs` 8/8 通过；`bash -n` 通过；`sandbox-images` CI 日志显示 opencode 经 mock 跑完一轮，mock 收到补全请求。

### 2026-10-04

- 日期：2026-10-04
- 范围：`server/internal/browser/`、`server/internal/handlers/{preview_vnc,sandbox_vnc,gate_share_public_preview}.go`、`server/internal/config/config.go`、`server/cmd/server/main.go`、`server/config.example.yaml`、`sandbox-gateway/sandbox/scripts/vnc-preview.sh`、`web/src/components/run/{AppPreviewPanel,PublicAppPreviewPanel,NovncPreviewPanel,DirectPreviewLauncher}.vue`、`web/src/locales/*/{pages,nodes}.json`、`docs/content/{,en/}guide/concepts.md`
- 做了什么：每个沙箱只保留一个常驻「桌面页」，noVNC 断开只解绑观看者，不再关标签页；再次进入时只要已经在同一 scheme+host 就不 Goto，换端口就在同一页跳转；页面被关掉时探活重建；新增 `desktop_idle_ttl_seconds`（默认 0，一直保留）。修掉 rod `Browser.Close()` 会发 `Browser.close` 杀掉 Chromium 的问题：引擎自己持有 CDP WebSocket，关闭只断连。NewTab 改用默认 context，以前 rod 的 `Browser.Page()` 本来就会覆盖 context。Chromium 使用固定的 `--user-data-dir`，登录态在重启后保留。ready 消息带上页面当前 URL。IP 直连端口也走 noVNC，直连只是工具栏上一个「新标签页直连打开」按钮；公开分享页的直连端口也会签 VNC ticket。noVNC 默认只看，点「接管」才把鼠标键盘交给远端，「交还」后回到只看，重连也回到只看。公开分享页的 `goto` 只能去沙箱回环地址（127.0.0.1 / localhost / ::1）的 http(s) 页面：页面共用 owner 的浏览器配置，跳去外部站点就会用上别人的登录态。
- 为什么：用户要的是 Grok Bot 云桌面和 dots 那样的常驻电脑：每次打开预览不重载，保留屏幕和登录态，默认观看、显式接管；直连和 VNC 不应该是两套互斥的界面。
- 如何验证：`server/` 下 golangci-lint 0 issues，`go vet`、`gen-configdoc -check`、`go test ./...` 全部通过，`cover-check-server.sh 90` 为 91.4%，browser 包加 `-race` 通过。`web/` 下 lint 0 error，`vue-tsc` 通过，vitest 431 个文件全部通过，行覆盖率 90.9%，`npm run build` 通过。`vnc-preview.sh` 做了 `bash -n`；需要 Docker 的 runtime e2e 没跑。已知限制：server 重启后内存里的桌面表清空，会新开一个标签页，旧窗口留在原处。没有接管已有标签页，是因为 chrome-devtools-mcp 也连在同一个 Chromium 上。

### 2026-10-04

- 日期：2026-10-04
- 范围：`docs/agent/README.md`、`docs/dev/DEVLOG.md`、`docs/README.md`、`AGENTS.md`
- 做了什么：新增英文文档边界手册；在根 `AGENTS.md` 文首增加「读哪份文档」表和开发日志硬规则，命令、门禁数字、六条已知坑和勿碰清单保持原样；建立本日志并写入这一条；在 `docs/README.md` 布局表标明 `agent/` 与 `dev/` 不进入 `public/`。
- 为什么：编码 Agent 需要从根短入口找到命令、文档边界和日志写法，同时不把角色包使命、公开帮助站和对外 CHANGELOG 混在一起。
- 如何验证：根 `AGENTS.md` 到 `docs/agent/README.md` 与 `docs/dev/DEVLOG.md` 的相对链接可打开；`CHANGELOG.md` 与 `agents/*/workspace/AGENTS.md` 无改动；在 `docs/` 执行 `npm run build` 后，`public/` 中不出现 `agent/` 或 `dev/` 页面。
