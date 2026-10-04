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

### 2026-10-04

- 日期：2026-10-04
- 范围：`server/internal/{models,services,handlers,runtime,router}`、`server/cmd/server/main.go`、`web/src/{components/project,lib/api,lib/project,locales,views}`
- 做了什么：新增项目凭据加密存储、掩码 CRUD、清除/撤销和项目详情凭据页；把 AI CLI、Git、SSH、MCP、自定义凭据接入统一解析器，并为渠道、外部 MCP 和工作流 Key 提供只读适配视图。UI 凭据覆盖项目共享 env、Agent env、进程 env；已绑定凭据键不能被 Run env 覆盖，MCP 支持 `${credential:<id>}` 展开，SSH 材料写入文件。
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
