# Workflow Sandbox Protocol (WSP/1)

本文件定义 **工作流与「沙箱」之间的抽象协议**。沙箱是一种**能力**,
不绑定任何特定工具:它为工作流提供「代码开发/测试环境 + Agent 会话」,
以及可选的「应用预览桌面」(headed Chromium + VNC,供 `app_preview` 人工审批)。
只要一个容器镜像满足本协议,无论内部用不用 Cursor、Git、GitLab,都能被工作流
当作沙箱驱动。

本目录的实现(`Dockerfile` + ACP 桥接服务(`cmd`/`internal`/`web`) + `vnc-preview.sh`)是一个**参考实现**:
用 ACP 兼容 agent 跑会话、用 code-server 当 IDE、用 Xvfb/Chromium/x11vnc/websockify
做预览桌面。这些都是 **reference-only**,可被替换。

> 「本次会话产生了什么代码变化」不属于本协议:由业务方在需要时经 `docker exec` /
> `kubectl exec` 进沙箱自行用 git/vcs 命令获取。

---

## 0. 传输与端点

- 沙箱在容器内监听一个 HTTP/WebSocket 端口(参考实现:`8765`,可由 `ACP_BRIDGE_PORT` 覆盖;
  deprecated 别名 `CURSOR_ACP_PORT`,计划 0.2.0 移除)。
- code-flow 通过宿主已发布的 `127.0.0.1:<port>` 访问;浏览器经 `/sandbox-bridge/:id/`
  反向代理(兼容期 `/sandbox-acp/:id/` 双注册,不做重定向)。
- 所有 `/api/*` 端点与 `/ws` 同源同端口。
- 鉴权:默认 loopback 免鉴权;参考实现可设 `ACP_BRIDGE_PASSWORD` 启用登录(deprecated
  别名 `CURSOR_ACP_PASSWORD`,计划 0.2.0 移除)。code-flow 默认不启用。

协议表面:

| 能力 | 端点 / 端口 | 说明 |
| --- | --- | --- |
| 能力发现 | `GET /api/capabilities` | 握手:返回能力描述符,通信前先调用 |
| 会话 | `GET /ws` + `GET /api/events` | Agent 交互通道(对话 + 流式事件 + 历史) |
| IDE(可选) | code-server 端口 | 浏览器内编辑器,纯参考实现 |
| 预览桌面(可选) | CDP `:9222` + websockify `:6080` | 沙箱内 headed 浏览器;见 §5 |

---

## 1. 能力发现(握手)

`GET /api/capabilities` → 能力描述符。工作流据此决定走哪些路径、如何注入配置、能否
上报用量,并对缺失能力**安全降级**。

```json
{
  "protocol": "wsp/1",
  "agent":   { "runtime": "cursor-agent", "name": "...", "version": "..." },
  "session": { "ws": "/ws", "events": "/api/events", "tokenUsage": false },
  "ide":     { "codeServer": true, "port": 8744 },
  "preview": {
    "vnc": true,
    "cdpPort": 9222,
    "websockifyPort": 6080,
    "enableEnv": "VNC_PREVIEW"
  },
  "config": {
    "mcp":    { "path": "<configRoot>/mcp.json", "schema": "mcpServers" },
    "rules":  { "dir": "<configRoot>/rules" },
    "skills": { "dir": "<configRoot>/skills" },
    "env":    { "via": "container-env" }
  }
}
```

- `protocol`:协议版本。客户端对未知大版本仅告警、不阻断(向前兼容)。
- `session.tokenUsage`:会话事件/连接负载是否携带用量(token)字段。参考实现据实为
  `false`(当前 cursor-agent ACP 不上报用量);未来 agent 能报时在事件里透出 `usage`
  并置 `true`。
- `preview` 缺省 / `preview.vnc=false` → 沙箱不提供预览桌面,平台对 `app_preview`
  的 noVNC Tab 应降级(不可用或提示镜像不支持)。见 §5。
- `config.*`:**配置注入契约**,见 §4。

---

## 2. 环境能力

沙箱提供一个可开发/可运行命令的工作区。

- 入参语义:**工作区来源** + **环境变量**。
  - core 语义:`WORKSPACE_DIR`(工作目录),容器进程 cwd 即此目录。
  - 参考实现:`GIT_REPOS`(逗号分隔,每项 `name|url|branch`)触发逐仓 git clone 到
    `WORKSPACE_DIR/<name>/`(根目录不是仓库);`GITLAB_URL`/`GITLAB_TOKEN` 供 glab,
    `GITHUB_URL`/`GITHUB_TOKEN` 供 gh。
    这些都是 reference-only —— 换其它来源/VCS,改对应实现即可。
  - `GIT_REPOS` 与凭据一样用「环境变量 + 引用」在 Agent 元信息里显式接线:
    `"GIT_REPOS": "${vars.repos}"`(引用工作流 `repos` 变量,展开为上述格式)。
    平台不做兜底/特殊注入 —— 没接线即不 clone。
- 终端/命令:由宿主 `docker exec` 进入容器执行(本镜像因此不内置 SSH 与 DinD)。

---

## 3. 会话能力

WebSocket `/ws`,JSON 帧:

- `→ {op:"connect", cwd?, fsRoot?, mcpServers?, autoPermission?}`
- `← {op:"connected", sessionId, eventLog, totalTurns, hasMoreTurns, agent:{name,version}, ...}`
- `→ {op:"chat", content, images?}` → 流式 `← {op:"event", data:{type:"session_update"|...}}`,
  以轮次边界事件收尾(见下)。
- `← {op:"queue_state", busy, queue_length, queue_capacity, queue_entries, running?}` ——
  每次入队/出队以及**轮次开始/结束时**广播;`busy` 是**权威的会话忙/闲信号**
  (`true` 表示一次 `session/prompt` 正在处理中,`false` 表示当前空闲)。
- `→ {op:"cancel"}` 取消当前轮。
- `← {op:"error", message, agentExited?}`。
- 历史回放:`GET /api/events?before=<turn>&limit=<n>` → `{events, hasMore}`。
- 可选 `usage`:能力声明 `session.tokenUsage=true` 时,事件/连接负载携带用量字段。

工作流既可呈现「原始事件日志」也可呈现「对话」两种视图。

### 3.1 轮次边界与忙/闲推导

一轮 `chat` 的事件流由一对边界事件包裹:

- `← {op:"event", data:{type:"prompt_begin"}}` —— 本轮开始(可选;`busy` 随之翻为 `true`)。
- `← {op:"event", data:{type:"prompt_done", stopReason}}` —— 本轮结束;`stopReason`
  取值如 `end_turn` / `cancelled` / `max_tokens` 等。

客户端可由 `queue_state.busy`(权威),或等价地由 `prompt_begin`/`prompt_done`
这对边界,推导「运行中 / 空闲中」用于展示。注意:一轮长时间的工具调用期间**没有事件帧**
但 `busy` 仍为 `true`,因此忙/闲必须以 `busy`(或轮次边界)为准,**不能**用「多久没有事件」
去猜,否则会把静默的长工具调用误判为空闲。

### 3.2 完成判定(协议信号)

客户端收到 `prompt_done` 即认为**本轮**结束(对应参考实现服务端 `dispatchEventData` 中
`ev.Type == "prompt_done"` 返回 done)。补充说明:框架类节点在协议轮次完成之外,还会做
**产物契约校验**(是否写出约定 artifact、计划是否全部完成),这是 code-flow 的上层策略,
**非协议要求**;校验不满足时可能对同一节点发起多轮 `chat`(即出现多个 `prompt_done`)。

### 3.3 超时语义(客户端策略,reference-only 默认值)

以下三类超时都是**客户端策略**,不是沙箱通过协议下发的;沙箱侧只负责如实产出事件流与
`prompt_done`:

- **空闲超时**:对一轮设「无事件帧」看门狗——每收到一帧就重置计时器,窗口内无任何帧则
  判定 agent/沙箱卡死并中止本轮。参考实现默认 120s(`chat_idle_timeout_seconds`),
  该错误视为**可重试**(换新沙箱重试,默认最多 3 次)。
- **硬超时**:整轮 wall-clock 上限,即使持续产生事件也会到点切断。参考实现默认 600s
  (`agent_chat_timeout_seconds`),可按节点用 `chat_timeout` 覆盖;硬超时**不可重试**。
- **连接握手超时**:`connect` 握手参考实现为 3 分钟,内含约 90s 的鉴权预热重连窗口
  (cursor-agent 冷启动登录期间的瞬时 `Authentication required` 会自动重连)。

---

## 4. 配置注入契约

`capabilities.config.*` 声明工作流应把 MCP / 规则 / 技能 / 环境变量注入到**哪里**:

- `mcp`:MCP 服务清单文件路径与 schema(参考:`/root/.cursor/mcp.json`,`mcpServers`)。
- `rules`:规则目录(参考:`/root/.cursor/rules`)。
- `skills`:技能目录(参考:`/root/.cursor/skills`)。
- `env`:环境变量注入方式(参考:`container-env`,即 `docker run -e`)。

参考布局为 `configRoot`(默认 `/root/.cursor`)整树 bind-mount 进容器。**注意**:config
挂载发生在容器 `docker run` 时,早于桥接进程起来提供 `/api/capabilities`;因此 `config.*`
是**协议层的静态约定**(供第三方实现与自省对齐),而非运行时可改写预启动挂载的开关。
实现可自定义这些路径,但需在 capabilities 中如实声明,并保证 `docker run` 时按该约定接受注入。

### 4.1 启动期注入接口(参考实现,先于所有服务)

除 bind-mount 外,参考实现在 `startup.sh` 里提供一个**服务启动前**的复制/解压注入接口,
供无法 bind-mount 的场景(如 K8s ConfigMap 挂到临时目录、`docker cp` 进容器后再就位)使用。
**它一定早于 backend / code-server / SSH / noVNC 起来**,因此这些服务启动时即可看到注入内容。

- 声明式 `SANDBOX_INJECT="src[|dest],src2[|dest2],…"`:
  - `src` 为容器内已存在的文件/目录/归档,或 `http(s)://` URL;
  - 归档(`.tar` `.tar.gz` `.tgz` `.tar.bz2` `.tar.xz` `.zip`)**解压**到 `dest`,其余**复制**到 `dest`;
  - `dest` 省略时默认 `$CONFIG_ROOT`(即 `configRoot`)。
- 钩子式 `/root/.sandbox/init.d/*.sh`:按文件名排序,在服务启动前依次 `source` 执行(任意自定义逻辑)。

**鉴权**:注入接口本身不是运行时 API、不单独鉴权——授权由「谁能拉起沙箱 / 挂载文件」隐式赋予
(设 env、bind-mount、`docker cp` 均是容器启动方的权限)。仅 `http(s)://` 远端需凭据时:首选
预签名 URL;或经 `SANDBOX_INJECT_HEADERS`(每行一个 HTTP 头)下发鉴权头(用 `curl -K` 配置文件,
不进 `ps` 参数,日志隐去 query)。本地文件/目录/归档无需鉴权。

### 按 Agent 配置的注入布局(参考实现)

参考实现把两个注入根做成**每个 Agent 可配置、随 Agent 持久化**的字段(存于
`agent.json` 的 `layout`),并在**创建沙箱时**按其挂载/设环境变量,而非写死:

- `configRoot`:Agent 工作目录(rules/、skills/、mcp.json)RW 挂载到容器内的路径(默认 `/root/.cursor`)。
- `workspaceDir`:仓库克隆与代码执行目录,作为 `WORKSPACE_DIR` 注入(默认 `/root/workspace`)。

`mcp.json` / `rules/` / `skills/` 为 `configRoot` 下的协议固定子路径(派生,不单独存)。

环境变量是厂商无关的通用注入通道:换非 Cursor 的 agent 镜像时,把其所需密钥经 `env`
注入即可,平台不依赖 Cursor。

---

## 5. 预览桌面能力(可选,`app_preview`)

`app_preview` 节点需要在**同一沙箱网络命名空间**内打开应用页面,供人工审批(noVNC
画面 + CDP Pick/导航)。预览浏览器**必须跑在沙箱本体内**,不得由平台在同网桥上
另起全局 Chromium 池(避免跨 sandbox 串扰与扁平网络可达)。

### 5.1 启用

- 平台在创建 `app_preview` 沙箱时注入环境变量 `VNC_PREVIEW=1`(或 `true`)。
- 参考实现:`startup.sh` 检测到该变量后后台执行 `/usr/local/bin/vnc-preview.sh`;
  若沙箱已启动后才需要预览,平台可 `docker exec … /usr/local/bin/vnc-preview.sh`
  幂等拉起(脚本探测 CDP 已就绪则直接退出 0)。
- 非 `app_preview` 节点**不应**开启,以免每个沙箱都吃 headed Chromium 内存。

> **经 MCP 暴露(可选)**:CDP(`:9222`)不仅供平台侧 Pick/导航,也可作为 **agent 的浏览器工具**。
> 参考实现设 `BROWSER_MCP=1` 时:隐含开启预览栈,并把 `chrome-devtools-mcp`(以 `--browser-url`
> attach 到该 Chromium)按 §4 的配置注入契约合并进 `configRoot/mcp.json`——agent 与人看到的是同一个浏览器。

### 5.2 端口与可达性契约

沙箱在**容器网 / ClusterIP** 上监听 CDP/noVNC(平台侧 dial 容器 IP 或
`<svc>.<ns>.svc.cluster.local`,**不得** publish 到宿主机或外部 LoadBalancer)。
用户/不可信网络不可直连;无应用层鉴权(`socat` + `x11vnc -nopw`)。

| 端口 | 协议 | 用途 | 可达面 |
| --- | --- | --- | --- |
| `9222` | HTTP CDP (`/json/version` + DevTools) | Pick / navigate / 开隔离 tab | 仅集群内 / 容器网;Grasp 拨号 |
| `6080` | WebSocket(websockify → RFB) | 前端 noVNC 画面 | 仅集群内 / 容器网;用户经 Grasp WS |

用户只走平台代理:

- `/sandbox-vnc/:sandboxId/ws`
- `/preview-vnc/:runId/:nodeId/:port/ws`

启用平台 Auth 时上述 WS **须有效 Session**（仅校验登录有效，不校验沙箱/跑步归属）。
集群外 Grasp 不能拨 CDP/noVNC,不是支持的拓扑。旧书签 `host:9222` /
`host:6080` 不可达为预期破坏性变更。Docker 已运行容器的 `-p` 须 TTL/Reinstall；
K8s 存量 `*-lb` 在网关启动调和 / Start / Reinstall 完成前仍可能对外暴露这两口。

参考实现细节(可替换,只要对**平台**契约不变;勿改 `vnc-preview.sh` 监听与 `-nopw`):

- Chromium remote-debugging 绑 loopback(如 `:9223`),再经 `socat` 转到 `0.0.0.0:9222`。
- `x11vnc` 只听 localhost RFB,`websockify` 把 `:6080` 转到该 RFB。
- 建议 `--shm-size≥1g`,避免 Chromium `/dev/shm` 耗尽。
- 不做传统 VNC 8 字符密码;隔离靠发布面 + 平台 Session,不靠 RFB 鉴权。

### 5.3 导航目标(隔离)

平台经 CDP 让沙箱内 Chromium 打开的 URL 必须是:

```
http://127.0.0.1:<port>/
```

其中 `<port>` 为 Agent 经 `set_preview(port)` 注册的应用端口。流量不出沙箱网络命名空间。

对照:iframe `PreviewProxy` 仍反代容器网桥 IP(`http://<sandbox-ip>:<port>/`),应用进程
仍须监听 `0.0.0.0:<port>`(不能只绑 loopback),否则代理 502 —— 这与预览桌面的
loopback 导航是两条路径,互不替代。

### 5.5 直连预览 HTML 注入(可选,`PREVIEW_DIRECT`)

IP 直连预览只在新标签页打开 `http://<sandbox-ip>:$PREVIEW_PORT/`,Grasp 页面不再
iframe 内嵌它。跨源页面无法由 Grasp 注入脚本,参考实现在**沙箱内**做入站 HTML 注入,
不改网关发布面、不改应用监听口、不加 `<base>`:

- 平台注入 `PREVIEW_DIRECT=1`、`PREVIEW_PORT`。自动注入开启时
  `PREVIEW_PICK_SCRIPT_URL=/__grasp/preview-pick.js`(同域相对路径)。
  节点开关「自动注入」(`auto_inject`,默认开)对应 `PREVIEW_AUTO_INJECT`;显式 `0` 时不启动注入。
- `startup.sh` 后台执行 `preview-inject.sh`:先让 `preview-inject` 听 `17980`,
  再在自有 nat 链 `APPROVING-PREVIEW` 上
  `REDIRECT --dport $PREVIEW_PORT --to-ports 17980`。
- 注入层在 `/__grasp/preview-pick.js` 直接返回脚本,HTML 插入同域
  `<script src="/__grasp/preview-pick.js">`。不得注入
  `http://localhost:8080/preview-pick.js`:审批人浏览器 origin 是
  `http://IP:PREVIEW_PORT/`,打不开 Grasp 的 loopback。
- 脚本在页内画 Pick 操作条(Shadow DOM)。没有对话抽屉时,点选暂存在页内
  (最多 20 条,存 `sessionStorage`,同标签页整页跳转后仍在)。
- **对话抽屉**:Grasp 打开预览时在片段里带一次性票据
  `#__grasp_embed&run=<runId>&node=<nodeId>&ticket=<ticket>`(60 秒,只能兑换一次)。
  脚本读到后立即从地址栏去掉,再请求同域 `GET /__grasp/embed-origin?ticket=&node=`。
  注入层用沙箱环境里的 `GRASP_ARTIFACT_URL` / `GRASP_ARTIFACT_TOKEN` 调
  `GET <GRASP_ARTIFACT_URL>/embed-origin?ticket=&nodeId=`(Bearer run token,不消耗票据),
  只在 Grasp 确认后返回 `{origin, runId, nodeId}`;缺凭证、票据无效或 origin 不是纯
  `http(s)://host[:port]` 时一律 404,脚本退回无抽屉模式。抽屉的 origin 只信这一步的
  结果,不信 URL。
- 抽屉是 Shadow DOM 里右侧的 iframe:`<origin>/embed/runs/<runId>/nodes/<nodeId>/chat#ticket=<ticket>`。
  该页用票据换对话凭证后存在自己的 `sessionStorage`;`{origin, run, node, open}` 存在应用页的
  `sessionStorage`(不含票据),整页跳转后不带票据重建抽屉。
- 抽屉就绪后发 `grasp-embed:ready`(仅认 `source` 为抽屉 iframe 且 `origin` 相符);之后每次
  点选以 `{type:'grasp-embed:pick', payload:{selector, tagName, text(≤120), outerHTML(≤1024), url}}`
  发往抽屉 origin,页内不留列表。就绪前的点选和抽屉出现前暂存的点选排队补发。
- 抽屉只能对话,不能确认/驳回;这些仍在 Grasp 页面完成。应用自带 CSP 的 `frame-src`
  不允许 Grasp origin 时抽屉加载不出来。
- 回环(应用 ← 注入进程)走 OUTPUT,不进 PREROUTING,不会环。
- 注入进程**不得**听 `PREVIEW_PORT`(平台 `KeepalivePort` 按该口找应用进程)。
- 进程挂了必须拆规则或立刻拉起:箱外 `ProbeHTTPPort` 打的是发布口,会走 REDIRECT;
  规则在而进程死 = 应用其实活着也会探失败。上游不可达时注入层不写 HTTP 状态
  (避免探活把 502 当成健康)。
- 只改 `text/html`; WebSocket `101` / 非 HTML 原样转发;保留 `Host`;不改 `Location`。
- 已知限制:明文 HTTP; brotli 响应不改写; CSP nonce / `strict-dynamic` 仍可能挡脚本;
  仅 IPv4 `iptables`; 只覆盖 `PREVIEW_PORT`。无 `iptables` / 非 privileged 时跳过。
- 旧镜像没有该进程时,Agent 在 HTML 入口手写
  `<script src="$PREVIEW_PICK_SCRIPT_URL"></script>` 兜底。

### 5.4 生命周期

- 预览桌面随沙箱 Create/Destroy;销毁后 CDP/websockify 不可达,平台应断开预览 WS。
- 同一沙箱默认**一个 X 桌面**;多 viewer 共享同一显示(约定单人审批,或自行串行)。
- `capabilities.preview` 声明本镜像是否具备该能力及端口号;缺省视为不支持。

---

## 6. 参考实现(reference-only)

| 协议能力 | 本目录参考实现 |
| --- | --- |
| 会话 | ACP agent 经 backend 桥接(ACP/JSON-RPC) |
| IDE | `code-server`(8744) |
| 预览桌面 | `vnc-preview.sh`(Xvfb + Chromium + x11vnc + websockify);`VNC_PREVIEW=1` |
| 直连预览注入 | `preview-inject` + `preview-inject.sh`;`PREVIEW_DIRECT=1` |
| 鉴权密钥 | `CURSOR_API_KEY`、`GITLAB_TOKEN` 等仅参考实现需要 |

---

## 一句话

**任何镜像,暴露约定端口、实现 `capabilities` / 会话、按声明接受配置注入,
并在需要时提供沙箱内预览桌面(CDP + websockify),即可被工作流当作沙箱 ——
无论内部是不是 Cursor / Git / GitLab。**
