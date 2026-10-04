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
- 范围：`server/internal/handlers/preview-pick.js`、`web/public/preview-pick.js`、`sandbox-gateway/sandbox/internal/previewinject/preview-pick.js`、`web/src/lib/shared/previewPickScript.test.ts`
- 做了什么：重做直连预览底部工具条的视觉样式，统一按钮尺寸、间距、圆角、边框、阴影和浅深色主题；为取点、产物、对话和原版预览加入线性 SVG 图标；将原版预览说明从浏览器原生长 tooltip 改为可聚焦的自定义提示，并增加窄屏自动换行。
- 为什么：原工具条依赖默认按钮样式，emoji 眼睛图标在不同环境中比例和颜色不一致，长原生 tooltip 会遮挡页面并破坏层级，截图中的控件难以辨识。
- 如何验证：`previewPickScript.test.ts` 61/61 通过；`go test ./internal/handlers -run 'TestPreviewPickScript|TestLiveOverlayScript'` 通过；三份脚本副本同步且 `git diff --check` 通过。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/{envauth,services,runtime,handlers}`、`README.md`、`server/README.md`
- 做了什么：项目凭据收口安全边界。去掉沙箱从服务进程环境读取 `GITHUB_TOKEN` / `*_API_KEY` 等的回退（含 `gitToken` / `gitLabURL`）；`fallbackEnvKey` 只从项目/Agent env 取值，不再 `os.LookupEnv`。凭据 env key 必须是合法标识符，且不能是平台保留变量（新增 `envauth.IsPlatformReservedEnvKey`）；交互/测试沙箱叠加凭据时跳过保留键，流水线沙箱的 `GRASP_*` 平台变量恢复为最后写入。渠道/外部 MCP/工作流类型只作为只读视图，不能新建。未填值的内置槽位不再挡住 Run env。项目凭据中的 SSH 私钥/known_hosts 优先于 Agent 元信息。删除项目时一并删除凭据。
- 为什么：原实现会把服务端宿主机的 Token 注入所有项目的沙箱；项目用户可通过 `fallbackEnvKey`（如 `GRASP_SECRETS_KEY`）读出服务端任意环境变量；自定义凭据可覆盖 `GRASP_ARTIFACT_TOKEN` / `GRASP_PM_TOKEN` 等平台令牌；打开凭据页即生成空槽位，会让 Run 级 `GITHUB_TOKEN` 静默失效。
- 如何验证：新增服务与 runtime 用例覆盖保留键/非法键拒绝、空槽位、平台键不被覆盖、进程 env 不泄漏、删除级联；`go test ./...`、`go vet ./...`、`gen-configdoc -check` 通过。

### 2026-10-05

- 范围：`web/src/components/project/ProjectCredentialsPanel.vue`、`web/src/components/project/ProjectCredentialsPanel.test.ts`、`web/src/locales/{zh-CN,en}/pages.json`
- 做了什么：重做项目凭据页的布局，按模型 API Key、代码托管、SSH 和其他凭据分组，增加配置摘要和响应式卡片网格；新增凭据弹窗拆分为基本信息、运行时绑定和凭据值区块，并明确 API Key 与仅写入语义。
- 为什么：旧页面单列卡片在宽屏留下大量空白，API Key 与 Git/SSH 凭据混排，新增表单字段层级也不清楚，用户难以判断凭据该填在哪里。
- 如何验证：项目凭据组件 Vitest 4/4 通过；`npm run lint`（0 errors，仅仓库既有 warnings）和 `npx vue-tsc --noEmit` 通过；`git diff --check` 通过。

### 2026-10-04

- 日期：2026-10-04
- 范围：`server/internal/{models,services,handlers,runtime,router}`、`server/cmd/server/main.go`、`web/src/{components/project,lib/api,lib/project,locales,views}`
- 做了什么：新增项目凭据加密存储、掩码 CRUD、清除/撤销和项目详情凭据页；把 AI CLI、Git、SSH、MCP、自定义凭据接入统一解析器，并为渠道、外部 MCP 和工作流 Key 提供只读适配视图。UI 凭据覆盖项目共享 env、Agent env；已绑定凭据键不能被 Run env 覆盖，MCP 支持 `${credential:<id>}` 展开，SSH 材料写入文件。
- 为什么：让项目密钥有统一的高优先级管理入口，同时保留旧环境变量部署的兼容路径，避免服务端专用鉴权和沙箱运行时凭据互相泄露。
- 如何验证：`go test ./...`、`go vet ./...`、`go run ./cmd/gen-configdoc -out CONFIGURATION.md -check`、服务端覆盖率 91.4%；Web `npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --run --coverage`（3976 tests）和 `npm run build` 通过。`golangci-lint` 未运行，当前环境未安装该二进制。

### 2026-10-04

- 日期：2026-10-04
- 范围：`README.md`、`server/README.md`、`server/config.example.yaml`、`docs/content/{guide,help}`、`docs/site/{index.html,en/index.html}`、`CHANGELOG.md`
- 做了什么：把 ACP、Git 等运行时凭据的项目凭据 UI 标为首选，将兼容的项目/Agent 环境变量保留为回退；同时明确平台服务配置仍按环境变量 > YAML > 默认值解析。
- 为什么：项目凭据由专门的 UI 统一管理，避免新部署继续把运行时密钥散落在环境变量中，同时保持已有部署的兼容路径。
- 如何验证：用 `rg` 检查公开 README、服务端说明、帮助页和站点文案中的凭据优先级；`git diff --check` 通过。未重新生成 `server/CONFIGURATION.md`，因为平台配置项和生成器未改变。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/chatsession/stream{,_test}.go`、`server/internal/services/{sandbox_chat,sandbox_chat_test,pm_turn}.go`、`server/internal/handlers/{sandbox,handlers}.go`、`server/cmd/server/main.go`、`web/src/components/agent/AgentChatTester{.vue,.test.ts,.interactions.test.ts}`
- 做了什么：把 PM 里"帧编号 + 当前轮回放缓冲 + 订阅者扇出"抽成 `chatsession.Stream`，PM 改用它。Agent Studio / 沙箱控制台的对话从"每个 WS 连接一条队列"改成 `SandboxChats`：每个沙箱一个 chatsession FIFO，连接断开不影响排队和正在跑的轮次；WS 连上先发 queue_state 快照，忙时回放当前轮，帧格式和 PM 一致（`{type:'session',event}` / `{type:'acp'}`）。客户端发 chat 时带 `id`，`turn_begin` 按 id 取回本地附件预览。`AgentChatTester` 断线按 1s·2^n（封顶 15s）重连；历史恢复完成前先暂存实时帧，并去掉事件日志里正在跑的那一轮，避免回放重复。销毁沙箱时先取消它的对话。
- 为什么：统一聊天的最后一块。Studio 原来刷新页面或断线就丢队列，正在跑的轮次也看不到了；PM 和 Studio 的回放逻辑本质相同，收成一份。
- 如何验证：`go test ./...` 全绿（新增 `stream_test.go`、`sandbox_chat_test.go`，`go test -race` 通过）；`chatsession` 覆盖率 97.0%，`cover-check-server.sh 90` 为 91.7%；golangci-lint 0 issues；`npx vitest run` 全绿（新增重连回放用例）；`vue-tsc --noEmit`、eslint 无错误。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/services/{pm_turn,pm_thread,pm,sandbox_view}.go`、`server/internal/handlers/pm.go`、`server/internal/router/router.go`、`server/cmd/server/main.go`、`web/src/lib/pm/{usePmLeaderChat,pmTurnState}.ts`、`web/src/lib/api/clients/pmClient.ts`、`web/src/components/pm/PmLeaderChat.vue`、相关测试与 locales
- 做了什么：PM 对话改跑在 `chatsession` 上。每个线程一个 FIFO 会话，网页、IM 渠道、定时任务、审批自动回复都通过原有的 `Start` / `Active` / `Cancel` / `Subscribe` 入队。新增 `POST .../turns`（起轮或 `retryOf` 重试，忙时排队，返回 `waiting`）和 `POST .../turns/cancel`；线程 WS 只负责订阅：连上先发 queue_state 快照，忙时回放当前轮的 turn_begin 和 acp 帧，再接实时帧，另有 `phase` 帧报告沙箱准备进度（preparing / pulling / running）。沙箱准备从前端挪到服务端（`openPmSandbox` + `SandboxView.WaitReady`）。启动时把残留的 streaming 草稿标成新的 failKind `interrupted`。删掉 `/draft` 接口和前端的草稿续接、孤儿判定、90 秒期限。前端断线按 1s·2^n（封顶 15s）重连，页面回到前台时立即重连。
- 为什么：PM 断线、刷新、轮次超过 90 秒都会在前端被判成"连接中断"，可服务端其实还在跑。改成以服务端为准，和 ReAct 澄清走同一套排队与快照恢复。
- 如何验证：`go test ./...` 全绿（新增 `pm_turn_session_test.go`、`pm_turn_prompt_test.go`，覆盖排队、重复入队、重连回放、取消清队、准备失败、失败类型、空闲回收）；golangci-lint 0 issues；`npx vitest run` 全绿（`PmLeaderChat.test.ts`、`usePmLeaderChat.actions.test.ts` 按新协议重写）；`vue-tsc --noEmit`、eslint 无错误。

### 2026-10-05

- 日期：2026-10-05
- 范围：`web/src/lib/chat/sessionQueue{,.test}.ts`、`web/src/lib/inbox/{useClarifyChat,useGateApproval}.ts`、`web/LIB_DOMAIN_MAP.json`
- 做了什么：新建前端 `lib/chat` 域，把 ReAct 澄清和审批热修订各自维护的排队对账抽成纯函数：`reconcileQueue`（queue_state 重建队列：先按 id、再按文本匹配乐观行，无进行中轮次时最多保留一条本地领先行）、`takeTurnBeginItem`、`dropGhostItems`、`isAuthoritativeIdle` 以及附件克隆。两个 composable 改为调用这些函数，队列类型统一为 `SessionQueueItem`。
- 为什么：两处代码逐行重复，后续 PM 和 Agent Studio 迁到 chatsession 后也要用同一套对账，先收成一份。审批面板的 turn_begin 原来直接 `shift()` 队首，queue_state 先裁掉该条时会误删下一条等待消息；现在与澄清一致，按 id 匹配，id 已不在队列时不按文本回退。
- 如何验证：`npx vitest run`（新增 `sessionQueue.test.ts`；`ClarifyChat`、`useGateApproval`、`PublicGateApprovalView` 等既有用例全部通过）；`vue-tsc --noEmit`、eslint 无新增问题。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/chatsession/{session,registry,session_test}.go`、`server/internal/engine/{review_session,visitor_lane,live,engine}.go`、`server/internal/engine/{resume_review_external,clarify_session}_test.go`、`server/scripts/cover-check-server.sh`
- 做了什么：新建 `chatsession` 包，把 ReAct 澄清、复审 / 预览审批、分享页访客通道共用的排队、单 pump、Cancel（只停当前轮或连队列一起清）、删除 / 重排、快照，以及 queue_state / turn_begin / turn_done / error 的发布抽成泛型 `Session[T]` 和 `Registry`。engine 的 `reviewSession` 改为包一层 `chatsession.Session`，执行、落库、Live、page session、反馈台账仍留在 engine，通过 Config 回调接入。`chatsession` 加入服务端覆盖率门禁。顺手修了 `TestClarifyReactReplyEnqueues` 不加锁改 `reactHold` 的数据竞争（main 上 `-race` 已失败）。
- 为什么：统一聊天的第一步。平台上有好几套"排队 + 一次跑一轮 + 断线后靠快照恢复"的实现，PM 和 Agent Studio 各写了一份，PM 断线就判失败。先把 ReAct 这套已经验证过的逻辑抽出来、行为不变，后面 PM、Studio 接同一个包。
- 如何验证：`go test ./...` 全绿；`go test -race ./internal/engine/ ./internal/chatsession/` 通过；`chatsession` 覆盖率 96.5%，`cover-check-server.sh 90` 为 91.6%；golangci-lint 0 issues。行为差异只有一处：turn_begin 和快照里的 images / annotations 为空时统一给空数组（原来有的路径给 null）。

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
