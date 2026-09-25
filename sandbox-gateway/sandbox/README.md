# 统一通用沙箱镜像（Universal Sandbox）

一体化远程开发沙箱镜像，作为平台统一使用的标准镜像。由两份现有镜像合并而来：

- **运行环境**（来自 ai-tool/sandbox）：Ubuntu 22.04、多语言工具链、容器内 **Docker（DinD）**、**SSH**、**code-server**（浏览器 IDE）、DB 客户端（mysql/redis/psql/mongosh）、Cursor CLI、Claude Code、glab、gh。
- **agent 与代码能力**（来自 code-flow/sandbox）：多后端 **backend**（ACP 桥接服务）、**多仓库 PULL**、多托管商 **git 凭据路由**、**Playwright（Chromium）+ noVNC 预览栈**。

五类 agent 后端 CLI 均已预装，`AGENT_PROVIDER` 单活切换：`cursor`（Cursor CLI）、`claude_code`（原生 Claude CLI）、`codebuddy`（`@tencent-ai/codebuddy-code`）、`trae`（Trae CLI）、`opencode`（`opencode-ai`，`run --format json`）。
发布一张图 `ghcr.io/cocofhu/universal-sandbox`。本地打薄镜像：`--build-arg AGENT_PROVIDERS=cursor`。

## 目录结构

```
sandbox/
├── Dockerfile / .dockerignore / docker-compose.yml   # 镜像构建
├── go.mod / go.sum / .gitignore                       # ACP 桥接服务（module: backend）
├── cmd/backend/        # 服务入口
├── cmd/preview-inject/ # 直连预览 HTML 注入进程（听 17980）
├── internal/           # acp(ACP 协议传输层) + backend(...) + previewinject + service/handler/router/...
├── web/                # 前端静态资源（打进镜像）
├── scripts/            # 运行时脚本（打进镜像）：startup.sh / vnc-preview.sh / preview-inject.sh / claude-env.sh
├── docs/               # PROTOCOL.md / ARCHITECTURE.md / BACKEND.md
└── README.md           # 本文（镜像总览）
```

## 暴露端口

| 端口 | 用途 |
| --- | --- |
| `8765` | backend（ACP 桥接）：agent 会话 `/ws` + `/api/capabilities` / `/api/events` 等（多后端） |
| `8744` | code-server：浏览器 IDE（密码 = `ROOT_PASSWORD`；刻意避开常用的 8080） |
| `22`   | SSH（密码同上；或用 `SSH_KEY` 免密）— 可对外发布 |
| `9222` | Chromium CDP（`VNC_PREVIEW=1` 时）— **仅容器网/ClusterIP**，不映射宿主 |
| `6080` | noVNC websockify（`VNC_PREVIEW=1` 时）— **仅容器网/ClusterIP**，不映射宿主 |

## Playwright + noVNC 预览

- 镜像预装 **Playwright Chromium**（`PLAYWRIGHT_BROWSERS_PATH=/ms-playwright`，版本 `1.61.1`）及系统依赖与 CJK 字体，`npx playwright test` / 无头浏览器验收开箱即用；项目 pin 其它版本时 `npx playwright install chromium` 会按需再拉。
- 设 `VNC_PREVIEW=1` 启动 **headed Chromium on Xvfb + x11vnc + websockify** 预览栈：箱内 CDP `9222`、noVNC `6080`（无应用层鉴权，**不** publish 到宿主/LB）。用户经 Grasp `/sandbox-vnc/:id/ws` 与 `/preview-vnc/.../ws`（Auth 开启时须 Session）。默认关闭，避免普通场景吃 headed Chromium 资源。
- 设 `PREVIEW_DIRECT=1`（且有 `PREVIEW_PORT`）启动 **直连预览 HTML 注入**：应用仍听 `0.0.0.0:$PREVIEW_PORT`，入站经 iptables REDIRECT 到箱内 `17980` 的 `preview-inject`，只给 `text/html` 插入同域 `<script src="/__grasp/preview-pick.js">`（由注入层自己提供，不依赖 Grasp 的 `localhost` 地址）。浏览器 origin 仍是 `http://IP:PREVIEW_PORT/`。无 iptables / 非 privileged 时打日志跳过，不拖垮启动。旧镜像没有该进程时，Agent 仍可手写 script 兜底。注入层同时提供 `/__grasp/page-control.js`（Agent 页面操作执行器），只在用户于抽屉打开「允许 Agent 操作页面」后由 `preview-pick.js` 按需加载。仅覆盖 `PREVIEW_PORT`，额外 `set_preview` 口不会注入。HTTPS / CSP nonce / `strict-dynamic` / 仅 IPv6 不在此层处理。

## 浏览器 MCP（可选）

设 `BROWSER_MCP=1`：把沙箱内 headed Chromium 经 CDP 暴露成 **MCP 工具**给 agent（导航 / 点击 / 输入 / 截图 / 读 DOM / 看 console 等）。启用时：

- 预装的 **`chrome-devtools-mcp`**（Google 官方）会以 `--browser-url=http://127.0.0.1:9222` **attach 到预览栈的那个 Chromium**——即 agent 驱动的正是 noVNC 里人能看到的同一个浏览器；
- 自动开启预览栈（隐含 `VNC_PREVIEW=1`），并把该 MCP **非破坏地合并**进 `$CONFIG_ROOT/mcp.json`（保留业务注入的其它 MCP），在 backend 启动前完成，agent connect 即可见；
- 之所以 attach 而非让 MCP 自起 Chrome：容器内自起 Chrome 有 sandbox 权限问题，attach 到已 `--no-sandbox` 起好的 Chromium 更稳（官方推荐做法）。

## 快速运行

需要 **特权模式** 才能启动容器内 `dockerd`（DinD）：

```bash
docker build -t universal-sandbox:local sandbox/
docker run --privileged -d \
  -e ROOT_PASSWORD='你的密码' \
  -p 8744:8744 -p 22:22 -p 8765:8765 \
  universal-sandbox:local
```

不需要容器内 Docker：加 `-e SKIP_INNER_DOCKER=1`。

或使用 compose：`cd sandbox && docker compose up --build`。

## 多仓库 PULL

- 单仓（兼容旧行为，clone 到 `WORKSPACE_DIR` 根）：`-e GIT_CLONE_URL=https://.../repo.git`
- 多仓（平级布局，每个 clone 到 `WORKSPACE_DIR/<name>/`）：

```bash
-e GIT_REPOS="api|https://github.com/owner/api.git|main,web|https://github.com/owner/web.git"
```

  每项 `name|url|branch`；`branch` 可省；`name` 省略时从 URL 末段推导。已克隆仓库清单
  （`name` + `path`）汇总到 `/root/.sandbox/repos.json`，供业务方/工具发现。

## 环境变量

所有配置都通过环境变量注入，均可选（留空则用默认值 / 跳过对应功能）。按用途分组：

### 1. 基础运行

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `ROOT_PASSWORD` | `toor` | root 账户密码，同时作为 code-server 与 SSH 登录密码 |
| `WORKSPACE_DIR` | `/root/workspace` | 工作目录；代码 clone 到此、code-server 与 backend 均以它为 cwd |
| `CODE_SERVER_PORT` | `8744` | code-server（浏览器 IDE）监听端口（默认避开常用的 8080） |
| `SSH_KEY` | 空 | 一行 SSH 公钥，追加到 `~/.ssh/authorized_keys` 以免密登录（不设则用密码登录） |

### 2. 容器内 Docker（DinD）

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `SKIP_INNER_DOCKER` | 空 | `1` 时不在容器内启动 `dockerd`（不需要嵌套 Docker 时用，省资源） |
| `INNER_DOCKER_STORAGE_DRIVER` | `vfs` | 容器内 dockerd 的存储驱动；`vfs` 兼容性最好，无特殊需求勿改 |
| `DOCKER_INSECURE_REGISTRIES` | 空 | 逗号分隔的 `host:port` 列表，写入 `daemon.json` 的 `insecure-registries`（拉私有 HTTP 仓库用） |

> 启用 DinD 需以 `--privileged` 运行容器。

### 3. 代码拉取（择一；均在 clone 前自动配置凭据）

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `GIT_REPOS` | 空 | 多仓，逗号分隔，每项 `name\|url\|branch`（`branch`/`name` 可省）；各 clone 到 `WORKSPACE_DIR/<name>/` |
| `GIT_CLONE_URL` | 空 | 单仓（兼容旧行为），clone 到 `WORKSPACE_DIR` 根 |

### 4. Git 凭据与用户信息

已配置的 `GITHUB_TOKEN` / `GITLAB_TOKEN` 都会注入（`~/.git-credentials` + `gh` / `glab` 登录），不依赖当前 clone 仓库属于哪个平台。HTTPS clone 与 SSH 仍按仓库 URL 的 scheme 互斥选择。

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `GITHUB_TOKEN` | 空 | GitHub token（`github.com` 或由 `GITHUB_URL` 匹配的自建实例）；已配置则自动 `gh auth login` |
| `GITHUB_URL` | 空 | 自建 GitHub 的 `scheme+host`，用于匹配 repo host |
| `GITLAB_TOKEN` | 空 | GitLab token；已配置则自动 `glab auth login` |
| `GITLAB_URL` | 空 | GitLab 实例地址（自建建议显式配置；空则默认 `gitlab.com`。不会把 GitHub / `GITHUB_URL` host 当成 GitLab） |
| `GIT_SSH_PRIVATE_KEY` | 空 | SSH 私钥内容，写入 `~/.ssh/id_rsa`（SSH clone 用） |
| `GIT_SSH_KNOWN_HOSTS` | 空 | SSH known_hosts 内容（SSH clone 时必填，禁用 accept-new 兜底） |
| `GIT_USER_NAME` | `sandbox` | 全局 `git config user.name` |
| `GIT_USER_EMAIL` | `sandbox@localhost` | 全局 `git config user.email` |

### 5. backend（ACP 桥接）/ agent

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `AGENT_PROVIDER` | `cursor` | agent 后端，单活：`cursor` / `claude_code` / `codebuddy` / `trae` / `opencode` |
| `ACP_BRIDGE_PORT` | `8765` | backend 监听端口 |
| `ACP_BRIDGE_PASSWORD` | 空 | 设置后 backend 启用登录页鉴权 |
| `ACP_BRIDGE_MODEL` | 空 | 锁定 agent 模型（不设则用后端默认） |
| `CONFIG_ROOT` | 随后端 | 能力发现的配置树根，默认按后端取 `/root/.cursor` `/.claude` `/.codebuddy` `/.trae` `/.config/opencode` |

> `CURSOR_ACP_PORT` / `CURSOR_ACP_PASSWORD` / `CURSOR_ACP_MODEL` 为上述三项的 deprecated 兼容别名。

### 6. 后端鉴权（通用别名，归一化到各 CLI 原生变量）

设 `ACP_*` 别名即可，backend 会在拉起后端子进程时映射为其真正读取的原生变量：

| 变量 | 归一化目标 | 作用 |
| --- | --- | --- |
| `ACP_CURSOR_API_KEY` | `CURSOR_API_KEY` | Cursor 后端鉴权 |
| `ACP_CLAUDE_API_KEY` | `ANTHROPIC_API_KEY` | Claude Code 后端鉴权 |
| `ACP_CODEBUDDY_API_KEY` | `CODEBUDDY_API_KEY` | CodeBuddy 后端鉴权 |
| `ACP_CODEBUDDY_REGION` | `CODEBUDDY_INTERNET_ENVIRONMENT` | CodeBuddy 区域：`public` / `internal` / `ioa` |
| `ACP_TRAE_API_KEY` | `TRAECLI_PERSONAL_ACCESS_TOKEN` | Trae 后端鉴权 |
| `ACP_TRAE_REGION` | `TRAECLI_HOST` | Trae 区域：`intl` → `https://www.trae.ai`；`cn` 用默认 |

> 也可直接注入各 CLI 原生变量（`CURSOR_API_KEY` / `ANTHROPIC_API_KEY` / `CODEBUDDY_API_KEY` 等）；`ACP_*` 仅为跨后端统一命名的便捷别名。

### 7. Claude Code 环境（`claude_code` 后端 / `ai-code` 便捷命令）

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `ANTHROPIC_BASE_URL` | 空（用官方） | Claude/Anthropic 兼容端点（对接自建/代理模型时设） |
| `ANTHROPIC_AUTH_TOKEN` | 空 | 上述端点的鉴权 token |
| `CLAUDE_MODEL` | 空（用 claude 默认） | `ai-code` 封装命令使用的模型名 |
| `IS_SANDBOX` | `1` | 标记沙箱环境 |
| `API_TIMEOUT_MS` | `6000000` | Claude 请求超时（毫秒） |

### 8. noVNC 预览栈

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `VNC_PREVIEW` | 空 | `1` / `true` 启动 headed Chromium on Xvfb + x11vnc + websockify（CDP `9222` / noVNC `6080`）；`ENABLE_VNC_PREVIEW` 同义 |
| `PREVIEW_DIRECT` | 空 | `1` / `true` 启动直连预览 HTML 注入（`preview-inject` 听 `17980` + iptables REDIRECT `$PREVIEW_PORT`）。须同时有 `PREVIEW_PORT` |
| `PREVIEW_AUTO_INJECT` | `1`（直连时） | `0` / `false` 时不启动注入（节点开关「自动注入」关闭） |
| `PREVIEW_PORT` | 空 | 应用监听口（与 Docker/K8s 发布同号）。注入进程**不得**占用此口 |
| `PREVIEW_PICK_SCRIPT_URL` | 自动注入时为 `/__grasp/preview-pick.js` | Agent 兜底写入 HTML 的 script src。注入层本身始终提供同域该路径，不使用 Grasp 的 `localhost` 绝对地址 |
| `BROWSER_MCP` | 空 | `1` / `true` 把沙箱内 Chromium 经 CDP 注册为 `chrome-devtools` MCP（合并进 `$CONFIG_ROOT/mcp.json`），并隐含开启预览栈。见[浏览器 MCP](#浏览器-mcp可选) |
| `CDP_PORT` | `9222` | 箱内 Chromium CDP 监听端口（浏览器 MCP attach 目标；仅容器网可达，不对外发布） |
| `WS_PORT` | `6080` | 箱内 noVNC websockify 端口（仅容器网可达；用户走平台 VNC WS） |
| `VNC_PID_DIR` | `/tmp/sandbox-vnc` | 预览栈各进程 PID / 锁文件目录 |

### 9. 契约注入（启动期，先于所有服务）

| 变量 | 默认值 | 作用 |
| --- | --- | --- |
| `SANDBOX_INJECT` | 空 | 逗号分隔 `src[\|dest]`；`src` 为容器内文件/目录/归档或 `http(s)://` URL，归档解压、其余复制到 `dest`；`dest` 省略默认 `$CONFIG_ROOT` |
| `SANDBOX_INJECT_HEADERS` | 空 | 仅 URL 下载用：每行一个 HTTP 头（如 `Authorization: Bearer xxx`），经 `curl -K` 下发做远端鉴权 |

见下方[契约注入](#契约注入)。

## 契约注入

业务方常需在 agent/IDE 起来**之前**把配置注入沙箱（如 `mcp.json`、`rules/`、`skills/` 或密钥文件）。
`startup.sh` 在 **backend / code-server / SSH / noVNC 等服务启动之前**提供两种注入方式：

- **声明式** `SANDBOX_INJECT`：逗号分隔，每项 `src[|dest]`
  - `src`：容器内已存在的文件/目录/归档（bind-mount 或 `docker cp` 进来），或 `http(s)://` URL；
  - 归档（`.tar` / `.tar.gz` / `.tgz` / `.tar.bz2` / `.tar.xz` / `.zip`）**解压**到 `dest`，其余**复制**到 `dest`；
  - `dest` 省略时默认 `$CONFIG_ROOT`（随 `AGENT_PROVIDER` 取 `/root/.cursor` `/.claude` `/.codebuddy` `/.trae`）。

```bash
# 把已就位的目录复制进配置根，并从 URL 下载一个 tgz 解压到工作区
-e SANDBOX_INJECT="/mnt/inject/.cursor,https://example.com/seed.tgz|/root/workspace"
```

- **钩子式** `/root/.sandbox/init.d/*.sh`：把可执行/可读脚本放进该目录，按文件名排序在服务启动前依次 `source` 执行（任意自定义就位逻辑）。

### 鉴权

`SANDBOX_INJECT` 本身**不是运行时接口、不单独鉴权**：它是启动期一次性从环境变量/已挂载文件消费的，能否设置该 env、能否 bind-mount / `docker cp` 文件，取决于**谁有权拉起沙箱**（网关/平台），授权是隐式的。

只有 `src` 为 `http(s)://` 远端且需要凭据时才涉及鉴权：

- 首选**预签名 URL**（凭据在 query 里，天然带鉴权，无需额外配置）；
- 或设 `SANDBOX_INJECT_HEADERS`（每行一个 HTTP 头）为 URL 下载附带鉴权头。经 `curl -K` 配置文件下发，**不会出现在进程参数（`ps`）里**；日志打印时也隐去 URL 的 query，避免泄露 token。

```bash
-e SANDBOX_INJECT="https://registry.example.com/seed.tgz|/root/.cursor" \
-e SANDBOX_INJECT_HEADERS="Authorization: Bearer ${MY_TOKEN}"
```

本地文件/目录/归档（bind-mount 或 `docker cp`）不需要任何鉴权。

## 协议

backend（ACP 桥接）对外协议（`/api/capabilities`、会话 `/ws`、`/api/events` 等）见 [PROTOCOL.md](./docs/PROTOCOL.md)。

> 「本次会话产生了什么代码变化」不由沙箱提供：业务方在需要时经 `docker exec` / `kubectl exec` 进沙箱用 git 命令自取。

## 与沙箱网关的关系

本镜像是 sandbox-gateway 服务默认拉起的沙箱。网关负责生命周期编排（Docker/K8s）与 API 暴露，
镜像负责提供环境、IDE、agent 会话与代码拉取能力。
