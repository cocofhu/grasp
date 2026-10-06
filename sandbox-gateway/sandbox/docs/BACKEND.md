# acp-bridge

在浏览器里连接本机 **Cursor Agent（ACP 模式）** 的轻量网关：HTTP 静态页 + WebSocket，对齐 Cursor 侧 ACP 协议（`cursor-agent acp`），便于在网页里对话，并把文件、终端等能力交给官方子进程处理。

## 功能概览

- **单进程网关**：Go（Gin）提供页面与 **`/ws`**，按会话（chat）桥接 Agent。
- **多会话 Tab**：页面顶栏可开多个 Tab，每个 Tab 是同一 provider 下独立的 Agent 会话（各自的队列、事件日志与模型），可并发运行。`/ws` 不带 `chat` 参数时落到 **`default`** 会话（平台调用即走这里，行为与单会话时代一致）；多个浏览器窗口打开同一 Tab 看到的是同一会话。会话列表只存于进程内存：刷新或换浏览器可恢复，关闭 Tab 才销毁，容器重启后只剩 `default`。
- **队列与取消**：用户消息按 FIFO 与一轮 `session/prompt` 对应；顶栏 **停止** 图标仅在 **有待处理队列**（含「进行中 / 等待模型」或仍有排队未消费）时显示，用于取消当前轮次。
- **权限与自动授权**：可在连接时开启自动授权；否则通过页面处理 `session/request_permission`。
- **聊天记录**：按 ACP `sessionId` 落在浏览器 **IndexedDB**（库 `acp-bridge-chat`），刷新可对齐同一会话。后端也可在 `connected` 时下发事件日志用于恢复界面。
- **主题**：浅色 / 深色可切换，键名 **`acp-bridge-theme`**（`light` / `dark`）。

更细的分层与扩展点见 **[ARCHITECTURE.md](./ARCHITECTURE.md)**。

## 启动沙箱：Provider / 密钥 / 模型（上游必读）

一张沙箱镜像预装五个对外 Agent CLI，运行期靠环境变量决定「用哪个 agent、拿哪个密钥、锁哪个模型」。
下面是拉起沙箱时上游需要设置的全部约定。**对外 WSP/1 协议不受影响**——这些都只作用于容器内部。

> 两条投喂路径等价：
> - **直接 `docker run -e KEY=VAL`**（自建/本地）；
> - **经网关 `POST /api/v1/sandboxes`** 的 `provider` / `env` / `config` 字段（见 `gateway/docs/API.md`）——
>   网关注入 `AGENT_PROVIDER`，`env` 原样透传进容器。默认用同一张 `universal-sandbox`。

### 1) 选 Agent —— `AGENT_PROVIDER`

- 选型：**`AGENT_PROVIDER`**，未设置时默认 `cursor`。
- 发布镜像预装五个对外 CLI（`cursor` / `claude_code` / `codebuddy` / `trae` / `opencode`）。设成镜像里没有的 provider（或本地薄镜像没装的那个）会因找不到可执行文件而失败。
- 经网关时只传 `provider`，网关注入 `AGENT_PROVIDER`（默认用同一张 `universal-sandbox`）。
- 全部取值见文末「默认 transport」表（`cursor` / `claude_code` / `codebuddy` / `gemini` / `codex` /
  `opencode` / `deveco` / `copilot` / `pi` / `openclaw` / `antigravity` / `kimi` / `hermes` / `kiro` /
  `qoder` / `grok` / `trae` 及 `*_acp` 回退）。

### 2) 配密钥 —— 环境变量（首选）或注入登录态

**方式 A · 环境变量密钥（最简单）**：backend 在拉起 CLI 子进程前，会把下表的别名**归一化**成该 CLI 的原生变量。
因此**原生名或任一别名都可**，只需按所选 provider 传一个：

| provider | 归一化后的原生变量 | 也接受的别名 |
| --- | --- | --- |
| `cursor` / `cursor_acp` | `CURSOR_API_KEY` | `ACP_CURSOR_API_KEY` |
| `claude_code` / `claude_code_acp` | `ANTHROPIC_API_KEY` | `ACP_CLAUDE_API_KEY` |
| `codebuddy` / `codebuddy_acp` | `CODEBUDDY_API_KEY` | `ACP_CODEBUDDY_API_KEY` |
| `gemini` | `GEMINI_API_KEY` | `ACP_GEMINI_API_KEY` / `GOOGLE_API_KEY` |
| `codex` | `OPENAI_API_KEY` | `ACP_CODEX_API_KEY` / `CODEX_API_KEY` |
| `opencode` | `OPENCODE_API_KEY` | `ACP_OPENCODE_API_KEY` |
| `deveco` | `DEVECO_API_KEY` | `ACP_DEVECO_API_KEY` |
| `copilot` | `GITHUB_TOKEN` | `ACP_COPILOT_API_KEY` / `COPILOT_API_KEY` |
| `pi` | `ANTHROPIC_API_KEY` | `ACP_PI_API_KEY` / `PI_API_KEY` |
| `antigravity` | `GEMINI_API_KEY` | `ANTIGRAVITY_API_KEY` / `GOOGLE_API_KEY` |
| `openclaw` | `OPENCLAW_API_KEY` | `ACP_OPENCLAW_API_KEY` |
| `kimi` | `MOONSHOT_API_KEY` | `ACP_KIMI_API_KEY` |
| `hermes` | `HERMES_API_KEY` | `ACP_HERMES_API_KEY` |
| `kiro` | `KIRO_API_KEY` | `ACP_KIRO_API_KEY` |
| `qoder` | `QODER_PERSONAL_ACCESS_TOKEN` | `ACP_QODER_API_KEY` |
| `grok` | `XAI_API_KEY` | `ACP_GROK_API_KEY` |
| `trae` | `TRAECLI_PERSONAL_ACCESS_TOKEN` | `ACP_TRAE_API_KEY` / `TRAE_API_KEY`（另 `ACP_TRAE_REGION`→`TRAECLI_HOST`） |

**方式 B · 注入 CLI 原生登录态/配置**：面向靠 OAuth 登录（而非纯 API key）的 CLI，或需要下发完整配置树时。
把该 CLI 的登录文件/配置（如 `~/.cursor` 下的登录 json、`config.toml` 等）经下节的 `SANDBOX_INJECT`
放进 `CONFIG_ROOT` 即可。方式 A、B 可叠加。

### 3) 选模型 —— `ACP_BRIDGE_MODEL`

- **不设**：默认 `auto`，前端顶栏可按 Tab 切换（切换即重启该 Tab 的 agent）。
- **设置**：作为各 Tab 的**默认模型**；Tab 仍可另选，选「默认」即回到它。等价的启动参数是 `-model`（优先级高于环境变量）。

```bash
-e ACP_BRIDGE_MODEL=claude-4.6-opus-max      # 环境变量（推荐，经网关 env 透传）
# 或 backend -model claude-4.6-opus-max       # 启动参数
```

可用模型：`cursor` 走 `cursor-agent --list-models`；多数 provider 无静态目录，返回空即表示交给 CLI 自行决定（auto）。

### 4) 配置根与注入 —— `CONFIG_ROOT` / `SANDBOX_INJECT`

- **`CONFIG_ROOT`**：agent 配置根，按 provider 自动取（`cursor`→`/root/.cursor`、`claude*`→`/root/.claude`、
  `codebuddy`→`/root/.codebuddy`、`opencode`→`/root/.config/opencode`、其余→`/root/.<provider>`），可用 `CONFIG_ROOT` 覆盖。
  它同时是 `mcp.json` / `rules/` / `skills/` 以及上面「方式 B」登录文件的落点。
- **`SANDBOX_INJECT`**（服务启动前完成注入）：`"src[|dest],src2[|dest2],…"`。
  - `src` 可为容器内已存在的文件/目录/归档，或 `http(s)://` URL；归档自动解压，其余直接复制；
  - `dest` 省略时默认 `CONFIG_ROOT`；
  - 远端下载可选 `SANDBOX_INJECT_HEADERS`（每行一个 HTTP 头，经 `curl -K` 下发，不进 `ps`）。
- **`/root/.sandbox/init.d/*.sh`**：按文件名排序、在服务启动前依次 `source` 的钩子（更灵活的注入方式）。

### 5) 端到端示例

```bash
# 直接 docker run：universal-sandbox + 环境变量密钥 + 锁定模型
docker run --rm -p 8765:8765 \
  -e AGENT_PROVIDER=gemini \
  -e GEMINI_API_KEY=xxxxx \
  -e ACP_BRIDGE_MODEL=gemini-2.5-pro \
  universal-sandbox:latest
```

```bash
# 经网关：provider 选镜像并注入 AGENT_PROVIDER，密钥/模型走 env 透传
curl -X POST https://<gateway>/api/v1/sandboxes \
  -H 'Content-Type: application/json' \
  -d '{
        "provider": "gemini",
        "env": { "GEMINI_API_KEY": "xxxxx", "ACP_BRIDGE_MODEL": "gemini-2.5-pro" }
      }'
```

> **token 用量**：会话若上报用量，`prompt_done` / `connected` 帧会按模型带上累计量；不上报的 provider
> 直接省略该字段（帧结构不变，`capabilities.session.tokenUsage=false`）。无需上游额外配置。

## 前置条件（本地开发）

- **Go** 1.22+
- 已安装并可执行**目标 provider 的 CLI**（默认 `cursor-agent`），且已按上面「配密钥」完成登录 / API 配置
- 在项目仓库根目录构建与运行（依赖 `web/` 前端资源）

## 快速开始

```bash
go build -o backend ./cmd/backend
./backend
```

在终端输出的地址用浏览器打开（默认监听 **0.0.0.0:8765**，本机一般为 **http://127.0.0.1:8765**）。页面加载后通过 WebSocket 与后端通信；后端按会话（chat）维护 Agent，默认会话为 `default`，页面顶栏可新增 Tab。

### Agent、刷新与聊天记录

- **Agent 子进程**在运行 `acp-bridge` 的那台机器上、随进程常驻**；关掉终端或结束 `acp-bridge` 才会结束 Agent。**只刷新浏览器**不会停 Agent；只要服务仍在且仍是同一个 ACP `sessionId`，会重新连上同一会话。
- **聊天记录**缓存在浏览器 **IndexedDB**（库 **`acp-bridge-chat`**，键为 `sessionId`）。刷新后若 `sessionId` 一致会自动还原；IndexedDB 不可用时展示固定文案横幅，并依赖后端 eventLog 降级。**「重启」** 会拿到新的 `sessionId`：界面清空，并删除上一会话对应的本地快照；后端事件日志与用户时间线也会在重建 Agent 时清空。
- 离开页面前会尽量 **立即落盘**（`pagehide` / `visibilitychange`），减少「刚发完就刷新导致没写上」的情况。

### 界面与操作（简要）

- 顶栏显示连接状态（如 **已连接**）、**重启**（新会话）、**停止**（方形图标，仅在模型处理中或队列未清空时显示）、主题切换等。
- **显示/隐藏详情**：控制工具调用、思考过程等辅助信息的展示（偏好可记在 **`acp-bridge-hide-detail`**）。

## 命令行参数

| 参数          | 默认值            | 说明 |
|---------------|-------------------|------|
| `-listen`     | `0.0.0.0:8765`    | HTTP 监听地址。需要局域网其它设备访问时勿只绑 `127.0.0.1`。 |
| `-gin-mode`   | `debug`           | `debug` / `release` / `test`。 |
| `-web`        | `web`             | 静态资源根目录，须含 `index.html` 与 `static/`（相对当前工作目录或绝对路径）。 |
| `-model`      | *(空，即 auto)*   | 默认 Agent 模型。也可通过环境变量 `ACP_BRIDGE_MODEL` 设置（优先级低于本参数）。各 Tab 可另选。 |
| `-password`   | *(空)*            | 登录口令，也可通过环境变量 `ACP_BRIDGE_PASSWORD` 设置。设置后须先 `POST /api/login`，凭 `agentchat_session` cookie 访问 `/ws` 与 `/api/*`。Grasp 创建的沙箱总会设置。 |

环境变量 `SANDBOX_MAX_CHATS`（默认 `8`，含 `default`）限制同时存在的会话数，超出时 `POST /api/chats` 返回 409。

## 模型选择

每个 Tab（会话）可在顶栏模型按钮里单独选模型，切换只重启该 Tab 的 Agent。未选择（或选「默认」）时用默认模型：`-model` → `ACP_BRIDGE_MODEL` → 都没有则 `auto`（不传模型，由 CLI 自行决定）。默认会话 `default` 不选模型时行为与之前一致。
配置方式与环境变量见上面 **[启动沙箱 · 选模型](#3-选模型--acp_bridge_model)**。

## HTTP 路由（摘要）

| 路径 | 说明 |
|------|------|
| `/` | 主页面（`index.html`） |
| `/assets/*` | 前端静态资源（JS/CSS 等） |
| `/ws?chat=<id>` | WebSocket，JSON 消息体（见下节）；`chat` 缺省为 `default` |
| `GET /api/chats` | 会话列表 `{chats:[{id,title,model,busy,connected,createdAt}], max, defaultModel}` |
| `POST /api/chats` | 新建会话 `{title?, model?}`（`model` 空 = 跟随默认），Agent 在首次 connect 时启动；超上限 409 |
| `PATCH /api/chats/:id` | 重命名 `{title}` |
| `DELETE /api/chats/:id` | 结束该会话 Agent 并移除；`default` 不可删 |
| `/api/prompt_queue?chat=` | 只读队列快照 JSON（对齐调试 / 外部观测） |
| `/api/events?chat=` · `/api/models?chat=` · `POST /api/model?chat=` | 同原语义，作用于指定会话（缺省 `default`）；`POST /api/model` 传空串恢复跟随默认 |

新会话的 `connect` 若未带 cwd / MCP，会沿用 `default` 会话的工作目录与 MCP 列表；各会话共享同一工作区，并发改同一文件可能冲突。

其它未知 **GET** 会回退到 `index.html`（便于前端路由）；**/api/** 下未匹配路径返回 404。

## WebSocket 消息（摘要）

前端与后端通过 **`/ws`** 交换 JSON，常见 `op`：

- **`connect`**：同步工作目录、fsRoot、MCP、自动授权等；若尚无 Agent 则拉起 / 握手。
- **`chat`**：用户文本入队；忙时 FIFO，与一轮 `session/prompt` 对应。
- **`cancel`**：停止当前轮次（对应顶栏停止图标）。
- **`restart_agent`**：结束子进程并重新握手 / `session/new`。
- **`permission`**：用户对权限请求的选择（非自动授权时）。

具体字段以 `internal/handler/websocket.go` 与前端 `web/static/js/ws/session.js` 为准。

### 自定义 WebSocket 地址（反代 / 非默认路径）

默认逻辑：与当前页面 **同主机、同协议**（`https` → `wss`），路径为 **`/ws`**。若页面是 **`file://`** 打开或没有 `host`，则回退 **`ws://127.0.0.1:8765/ws`**。

部署在反向代理、子路径或独立 WS 域名时，可在 `web/index.html` 中设置：

```html
<meta name="acp-bridge-ws" content="wss://你的域名/ws" />
```

**反代须正确转发 WebSocket**（`Upgrade`、`Connection` 等），否则会出现连接超时或异常断开（如 close code **1006**）。

## 安全与访问控制（重要）

- 本服务**默认不对浏览器做登录鉴权**；能访问 HTTP 端口的客户端一般都能连 **`/ws`** 并驱动本机 Agent（视 `cursor-agent` 配置而定）。
- **不要将未加防护的实例暴露到公网**；若必须远程使用，请配合 VPN、SSH 隧道、或前置带鉴权与 TLS 的反向代理。
- 服务端对浏览器响应带有较宽松的 **CORS**（`*`），设计取向是本地/受信网络工具页；面向公网时请自行收紧策略。

## 常见问题

| 现象 | 可能原因与处理 |
|------|----------------|
| 一直连不上 / 超时文案里带了尝试的 WS URL | 未通过本服务提供的 **http(s)/** 打开页面（例如双击本地 **file://** HTML），或反代未转发 **WebSocket**。请用终端打印的地址访问，并检查代理配置。 |
| **1006** 异常断开 | 多为代理不支持 **Upgrade**、网络中断或中间设备断开长连接。 |
| Agent 立刻退出 / 聊天报错 | 确认本机可运行 **`cursor-agent acp`**，且已完成 **login** 或 API 密钥配置；详见官方 ACP 文档，并在运行 `acp-bridge` 的终端查看日志。 |
| 修改前端不生效 | 浏览器强刷或清缓存；确认 `-web` 指向你正在编辑的 `web` 目录。 |

## 日志串联（排查问题）

服务端日志使用统一前缀 **`acp-bridge`**。跨模块排查可在同一段日志里搜：

| 键 | 含义 |
|----|------|
| **`cid=`** | 一条 WebSocket 连接（连接到断开） |
| **`oid=`** | 一次用户 `chat` → 入队 → `session/prompt` 执行 |
| **`sid=`** | ACP `sessionId`；stdio 侧在 `session/new` 前可能为 `handshake` |
| **`pid=`** | Agent 子进程，stderr 行前缀 `agent pid=…` |

示例：先看到 `ws cid=… oid=…: chat 已入队`，再用同一 **`oid=`** 搜 `prompt … oid=` 与 `acp … Call`，即可对齐浏览器入站到 Agent RPC。

## 目录结构（简）

```
cmd/backend/          # 入口
internal/
  acp/                # JSON-RPC over stdio、Panel、fs/terminal/权限
  provider/           # 与具体 CLI 协议解耦的 Session/Provider 抽象（不引入任何 CLI）
    acpx/             # 长驻 transport：把 acp.Panel 适配为 provider.Session
    oneshot/          # one-shot transport 引擎：每轮起进程 + resume + 统一事件 mapper
    streamjson/       # codec：Anthropic 风格 stream-json（NDJSON，支持 -p 值/stdin 原文/stdin 信封三种投喂）
    opencodejson/     # codec：run --format json 事件流（type + 嵌套 part，含 token 用量）
    copilot/          # codec：dotted 事件（assistant.message* / tool.execution_complete + 合成 result）
    pi/               # codec：assistantMessageEvent.delta 事件流；会话为 --session 日志文件
    openclaw/         # codec：单个整块 JSON 结果文档（payloads + meta.agentMeta），NDJSON 兜底
    antigravity/      # codec：纯文本 stdout + 从 --log-file 回收 conversation id
    codex/            # codec：codex exec --json（msg 包裹式 JSONL）
  agents/             # provider 注册表：AGENT_PROVIDER 选型 + FromEnv/Current/ConfigRoot（唯一选型入口）
  backend/            # 长驻 ACP 的 argv/configRoot/authEnv（仅供 acpx.FromBackend，不是选型入口）
    common/           # Backend 接口、Base、env 助手
    cursor/ claude/ codebuddy/ trae/
  service/            # Bridge：Agent 生命周期、WS 广播、排队与 prompt（依赖 provider.Session）
  handler/            # HTTP / WebSocket
  router/             # Gin 路由与优雅退出
  correl/             # 短 ID 生成（日志串联）
web/                  # 前端（ESM + 静态资源）
```

## Agent Provider 抽象（多 transport，不改对外协议）

网关不再绑定单一 CLI 协议。`internal/provider` 定义了与传输无关的 `Session` / `Provider`
抽象，`Bridge` 只依赖 `provider.Session`；`internal/agents` 是唯一的选型入口。据此，一套代码可
驱动多类 Agent CLI，而 **对外 WSP/1 协议（`/ws`、`/api/capabilities`、`/api/events`）帧与语义完全不变**。

- **transport 两类**：
  - **长驻（long-lived）**：一个常驻子进程 + JSON-RPC over stdio，多轮走 `session/prompt`。
  - **one-shot**：每轮拉起一个新进程，跨轮靠 `--resume <sessionId>` 续接；引擎内置 resume
    失败回退（一次性以全新会话重试）、stderr 环形缓冲（便于定位原生崩溃），并把各 CLI 的流式
    输出归一化为统一事件（text / thinking / tool_use / tool_result / error），再映射为既有的
    `session/update` 帧。
- **codec**：one-shot 下各 CLI 的差异只体现在「如何拼 argv」「如何解析输出」两点，分别由
  `streamjson` / `opencodejson` / `copilot` / `pi` / `openclaw` / `antigravity` / `codex`
  等 codec 承担；新增一个同类 CLI 通常只是加一条注册项。
- **引擎解析模式**（codec 可选实现，用以覆盖非「逐行 NDJSON」的 CLI）：
  - `StatefulCodec`：每轮返回一个全新的行解析器，用于需要跨行状态的 CLI（活跃模型、增量去重、
    文本清洗缓冲），避免单例 codec 在并发会话/多轮间串状态（`copilot` / `pi`）。
  - `WholeOutputCodec`：CLI 输出的是单个（通常美化过的）整块 JSON 文档而非 NDJSON 时，引擎一次性
    读满 stdout 再整体解析（`openclaw`）。
  - `LogFileCodec`：CLI 把机读旁路（会话 id、结构化错误）写入 `--log-file` 而非 stdout 时，引擎分配
    临时日志文件、注入 argv，进程退出后回收解析（`antigravity`）。
  - `SessionInitializer`：会话句柄需在首轮前显式分配（如会话文件路径）而非从输出中发现时，引擎在
    Open 时分配并作为每轮 resume 指针（`pi` / `openclaw`）。
  - 附件（图片 / 文件）统一策略：Bridge / oneshot 将 base64 落到 `/tmp/sbx-attach-*`，
    把绝对路径写入 prompt 文本，由 Agent 用 Read 打开；不再依赖各 CLI 原生 image block。
- **默认 transport 按 CLI 的主用无头契约选择**（在 `internal/agents/registry.go` 单点维护）：

  | provider | 默认 transport | 启动 / 续接要点 |
  | --- | --- | --- |
  | `cursor` | stream-json（one-shot） | `-p --output-format stream-json --yolo --workspace <cwd>`，prompt 走 stdin 原文；`--resume` |
  | `claude_code` / `codebuddy` | stream-json（one-shot） | `-p --output-format stream-json --input-format stream-json`，prompt 走 stdin stream-json 信封；`--resume` |
  | `gemini` | stream-json（one-shot） | `-p <prompt> --output-format stream-json`；`-r` |
  | `opencode` / `deveco` | run --format json（one-shot） | `run --format json --dangerously-skip-permissions <prompt>`；`--session` |
  | `copilot` | dotted 事件 JSONL（one-shot，专用 codec） | `-p <prompt> --output-format json --allow-all --no-ask-user`；`--resume`；delta 与 result 去重 |
  | `pi` | assistantMessageEvent 事件流（one-shot，专用 codec） | `-p --mode json --session <path>`；会话即 `--session` 日志文件（首轮分配、逐轮复用） |
  | `openclaw` | 整块 JSON 结果文档（one-shot，专用 codec） | `agent --local --json --session-id <id> --message <prompt>`；整块解析 payloads+meta，NDJSON 兜底 |
  | `antigravity` | 纯文本 stdout（one-shot，专用 codec） | `-p <prompt> --dangerously-skip-permissions --log-file <tmp>`；从日志回收 `--conversation` id |
  | `codex` | JSONL（one-shot） | `codex exec --json`（`resume <id>` 续接） |
  | `kimi` / `hermes` / `kiro` / `qoder` / `grok` / `trae` | 长驻 ACP | CLI 原生 ACP over stdio（`… acp` / `--acp` / `agent … stdio`） |
  | `cursor_acp` / `claude_code_acp` / `codebuddy_acp` | 长驻 ACP（可选回退） | 强制走 stdio 的备选项，默认不单独出镜像 |

- **验证状态**（务必据实使用，勿把"已接线"当成"已验证"）：
  - ✅ **已验真**：`cursor`（端到端跑通）、`gemini`/qwen 系（对真实 stream-json 抓样解析全绿）。stream-json codec 已同时兼容两种方言：顶层 thinking + camelCase 用量（cursor），以及 content-block thinking（`thinking` 字段）+ `tool_use_id` 关联 + snake_case 用量（claude/codebuddy/qwen）。
  - 🟢 **专用 codec 已落地（按各 CLI 的真实无头契约实现，附单测）**：`copilot`（dotted 事件 + `data.deltaContent`/`content`，delta 与最终 message 去重、toolRequests/tool.execution_complete、合成 result 取 session/exit）、`pi`（`assistantMessageEvent.delta` 文本/thinking、tool 起止、turn_end 用量、控制标记清洗；会话为 `--session` 日志文件）、`openclaw`（整块 JSON 结果文档：payloads[].text + meta.agentMeta 的 session/model/usage，NDJSON 事件兜底）、`antigravity`（纯文本 stdout 逐行透出 + 从 `--log-file` 回收 conversation id，并把仅写日志的 print-timeout/provider error 提升为失败）。这些 codec 结构已对齐真实字段，仍建议真机各跑一轮抓样复核。
  - 🟡 **结构对、待真机确认**：`claude_code` / `codebuddy`（与 qwen 同族方言，已按其字段解析）、`opencode` / `deveco`（`run --format json` 的 `type`+嵌套 `part`；tool_use 事件含 `state` 时同时透出 tool_result）、`codex`（`exec --json` 的 msg 包裹事件，tool 结果取 `output`，含 patch_apply 起止）。
  - ⚙️ **ACP 家族**（`kimi`/`hermes`/`kiro`/`qoder`/`grok`/`trae`）：走已验证的长驻 ACP 通道；其中非 `trae` 的 argv/config 为按公开事实拼装，需真机校准。

- **选型**：`AGENT_PROVIDER` 指定 provider（未设置时默认 `cursor`）。
  `CONFIG_ROOT` 覆盖 provider 的默认配置根。`/api/capabilities` 的 `agent.runtime` 与
  `session.tokenUsage` 均据当前 provider 与会话实况声明。
- **token usage**：协议早已预留可选 `usage` 字段；会话若上报用量，`prompt_done` 帧与 `connected`
  载荷会按模型带上累计量，否则完全省略（帧不变）。不上报的 provider `tokenUsage=false`，平台安全降级。

## 开发与构建

```bash
go build ./...
go test ./...
go run ./cmd/backend -listen 127.0.0.1:8765
```

前端为原生 **ESM**，修改 `web/static/` 后刷新页面即可（无需单独打包步骤）。

## 参考

- [Cursor 文档 · Agent Client Protocol (ACP)](https://cursor.com/cn/docs/cli/acp)
