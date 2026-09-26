# approving 后端(Go)

有限状态机(FSM)编排引擎 + 按 run 隔离的 artifact-store + 真实沙箱执行(cursor-agent)。

## 执行后端(ExecProvider)

支持五类 ACP 后端(`cursor` / `claude_code` / `codebuddy` / `trae` / `opencode`),由 Agent 卡片
`agent.json` 的 `acpBackend` 字段选择;`ProviderRegistry` 按 Agent profile (`agent_profile`) 路由到
对应 Provider。统一沙箱镜像内 `acp-bridge` 按 `AGENT_PROVIDER` 单活启动 bridge(:8765)。
兼容期容器内 `acp-gateway` / `cursor-acp` 为指向 `acp-bridge` 的软链(计划 0.2.0 移除)。

**鉴权**:各后端 Key / 站点可配置在 **项目沙箱 env**(流水线底噪)或 **Agent env**(同名覆盖;
见「ACP 后端怎么用」)。平台级 `GRASP_CURSOR_API_KEY` / `sandbox.cursor_api_key`
已废弃且不再注入沙箱;Agent Studio 不继承项目 env。

`GRASP_EXEC_PROVIDER` 已废弃(读取时 WARN,不影响路由);请改用 Agent `acpBackend`。

### 真实沙箱链路(多后端)

```
engine → ProviderRegistry → baseACPProvider → sandbox-gateway REST(创建统一镜像沙箱)
       → acp-bridge(AGENT_PROVIDER) → ACP WebSocket → chat / MCP / harvest
       → 数据面(exec / 文件 / 终端 / 变化上报)走 SSH 直连沙箱
```
- **原生 artifact-store MCP(已落地)**:每个节点的容器都会接入一个 **run 级 HTTP MCP**:
  - 平台在同一 Gin 端口暴露 `POST /mcp/runs/:runId`(Streamable-HTTP JSON-RPC:`initialize` / `tools/list` / `tools/call`),按 run-token 鉴权。
  - 注入方式有两条、互为冗余:ACP `session/new` 的 `mcpServers`(URL+`Authorization: Bearer <token>`)+ 容器内 `{configRoot}/mcp.json`。
  - 容器经 `host.docker.internal:<GRASP_PORT>` 回连平台(`--add-host host.docker.internal:host-gateway` 已设)。
  - 工具:`write_artifact` / `read_artifact` / `list_artifacts` / `node_complete` 等。Agent **原生调用** `write_artifact` 写回产物,结束前必须 `node_complete` 标记完成(已 live 验证:cursor-agent 完成 initialize→tools/list→tools/call 全链路)。
- **`{configRoot}` 配置树(对齐 auto-coder)**:每个节点按 Agent profile 的 `acpBackend` 解析 configRoot(Agent 卡片可覆盖),在控制面生成一份配置树后注入沙箱:
  - 默认映射:`cursor`→`/root/.cursor`、`claude_code`→`/root/.claude`、`codebuddy`→`/root/.codebuddy`、`trae`→`/root/.trae`、`opencode`→`/root/.config/opencode`;
  - `rules/base.md`(基础约束,alwaysApply)、`rules/artifact-store.md`(produces 契约 + MCP 用法)、`react` 节点附 `rules/react.md`;
  - `rules/<profile>.md` 来自平台 `AgentService`(`GRASP_PROFILES_ROOT`);
  - 需要 push/MR 的节点可在 Agent 工作目录附 `skills/git/SKILL.md`;
  - `mcp.json` 写入 artifact-store MCP 配置(含改写后的 `GRASP_ARTIFACT_URL`);
  - 注入路径:**gateway `config.bundleUrl` 启动前 inject** — 控制面把 ConfigHome 打成 `.tgz`，经 `/sandbox-inject/:id` 短时下载；gateway 设 `SANDBOX_INJECT`，镜像 `startup.sh` 在 acp-bridge/agent 启动**之前**解压到 `{configRoot}`。URL 基址与 `mcp_advertise` 相同(沙箱可达)。Attach 重连仍可 SSH 补种。不使用 `config.hostPath`(远程 K8s 会挂空卷)。
- **产物契约(produces) + 完成标记**:节点完成时引擎要求已调用 `node_complete`,并校验声明的产物必须存在于平台 store。优先由 Agent 经 MCP `write_artifact` 写入;若只在工作区留了文件,provider 仍会从容器取回该 `produces` 文件并写入 store,**双保险**。默认校验通过后才可能调用业务 RPC 校验;`submit_mr` 不再由平台代验 git 推送/MR/冲突。
- **上游产物读取**:run 内已有产物名会在 prompt 中列出,Agent 用 MCP `read_artifact` / `list_artifacts` 按名读取(按 run token 隔离)。产物**不**落盘到工作区,以免污染节点的代码变更报告。
- **隔离**:每个 run 一枚 token,artifact-store 按 run 命名空间读写、token 绑定校验,run 间互不可见;容器 ACP 端口只绑定 `127.0.0.1` 临时端口。
- **token 生命周期 = 沙箱生命周期**:token 无单独的过期时间。内存注册命中即放行;run 结束或服务重启后,只要该 run 仍有存活的沙箱(回合执行中或回合结束后的 `run_sandbox_ttl_minutes` 保活窗口内),持久化 token 依旧可鉴权,让仍存活的沙箱 agent 继续写产物而非 401;最后一个沙箱销毁后 token 自然失效。

## ACP 后端怎么用

五个后端共用配置入口:**项目沙箱 env**(流水线默认底噪,官方鉴权键强制 Secret)+
**Agent Studio → Meta 选 `acpBackend` → Env**(同名覆盖;Studio 调试须在 Agent env 单独配置)。
平台级不提供全局 ACP Key;`MergeAuthEnv` 把项目/Agent 侧别名收成 CLI 认的变量后注入流水线沙箱。

| acpBackend | 沙箱内 CLI | 默认 configRoot | 容器内鉴权变量 |
|------------|------------|-----------------|----------------|
| `cursor` | `cursor-agent … acp` | `/root/.cursor` | `CURSOR_API_KEY` |
| `claude_code` | `npx @zed-industries/claude-code-acp` | `/root/.claude` | `ANTHROPIC_API_KEY` |
| `codebuddy` | `codebuddy --acp` | `/root/.codebuddy` | `CODEBUDDY_API_KEY` |
| `trae` | `traecli acp serve` | `/root/.trae` | `TRAECLI_PERSONAL_ACCESS_TOKEN` |
| `opencode` | `opencode run --format json` | `/root/.config/opencode` | 厂商原生 Key(由 `OPENCODE_API_KEY` 映射) |

Agent Studio 的 Env 页会按当前后端提示所需 Key;CodeBuddy / Trae 另有「站点」下拉,
写入 `GRASP_*_REGION`(运行时再规范化成官方变量)。OpenCode 另有厂商 / API Base / model
(`GRASP_OPENCODE_PROVIDER`、`GRASP_OPENCODE_BASE_URL`、`ACP_BRIDGE_MODEL`)。

### Cursor

1. Meta:`acpBackend = cursor`(默认)。
2. Env 任选其一:
   - `GRASP_CURSOR_API_KEY`
   - `CURSOR_API_KEY`
3. Key 来自 Cursor Dashboard / CLI 登录后的 API Key。
4. 可选:宿主设 `GRASP_CURSOR_AUTH` 指向已登录的 Cursor 配置目录(只读挂载复用登录态)。

```json
{
  "acpBackend": "cursor",
  "env": {
    "GRASP_CURSOR_API_KEY": "crsr_xxx"
  }
}
```

无「国际/国内站」区分;全球一套端点。

### Claude Code

1. Meta:`acpBackend = claude_code`。
2. Env 任选其一:
   - `GRASP_CLAUDE_API_KEY`
   - `ANTHROPIC_API_KEY`
3. Key 来自 Anthropic Console。
4. 可选透传(平台不改写):`ANTHROPIC_BASE_URL` 等 Anthropic/兼容网关变量。

```json
{
  "acpBackend": "claude_code",
  "env": {
    "GRASP_CLAUDE_API_KEY": "sk-ant-xxx"
  }
}
```

### CodeBuddy

1. Meta:`acpBackend = codebuddy`。
2. Env 鉴权任选其一:
   - `GRASP_CODEBUDDY_API_KEY`
   - `CODEBUDDY_API_KEY`
3. **必须选对站点**(Key 与站点绑定;错站会 401 `not_found`):

| 站点 | Agent env | 运行时效果 | Key 获取 |
|------|-----------|------------|----------|
| 国际站(默认) | `GRASP_CODEBUDDY_REGION=public` 或不写 | `CODEBUDDY_INTERNET_ENVIRONMENT=public` | https://www.codebuddy.ai/profile/keys |
| 国内站 | `GRASP_CODEBUDDY_REGION=internal` | `CODEBUDDY_INTERNET_ENVIRONMENT=internal` | https://copilot.tencent.com/profile/ |
| iOA | `GRASP_CODEBUDDY_REGION=ioa` | `CODEBUDDY_INTERNET_ENVIRONMENT=ioa` | https://tencent.sso.copilot.tencent.com/profile/keys |
| Staging | `GRASP_CODEBUDDY_REGION=staging` | `public` + 写入 `{configRoot}/settings.json`(`envRouteMode=staging`, endpoint=`https://staging-codebuddy.tencent.com`) | https://staging-codebuddy.tencent.com/profile/keys |

也可直接写官方变量 `CODEBUDDY_INTERNET_ENVIRONMENT`;**显式官方变量优先于区域别名**。
不要只设 `CODEBUDDY_BASE_URL` 指向 staging——CLI 会打错 chat 路径;请用 `REGION=staging`
让平台写 `settings.json`。

```json
{
  "acpBackend": "codebuddy",
  "env": {
    "GRASP_CODEBUDDY_API_KEY": "ck_xxx",
    "GRASP_CODEBUDDY_REGION": "public"
  }
}
```

Staging 示例:

```json
{
  "acpBackend": "codebuddy",
  "env": {
    "GRASP_CODEBUDDY_API_KEY": "ck_xxx",
    "GRASP_CODEBUDDY_REGION": "staging"
  }
}
```

### Trae

1. Meta:`acpBackend = trae`。
2. Env 鉴权(官方 CLI 登录令牌,须含 `trae-lt-` 前缀)任选其一:
   - `GRASP_TRAE_API_KEY`(平台别名)
   - `TRAE_API_KEY`(旧别名)
   - `TRAECLI_PERSONAL_ACCESS_TOKEN`(官方名;注入沙箱时统一写成此名)
3. 令牌在 Trae 企业控制台 **个人信息 → 访问令牌 → CLI 登录令牌** 生成
   (见 [CLI 登录令牌](https://docs.trae.cn/cli_login-token);旗舰版)。
4. 站点:

| 站点 | Agent env | 运行时效果 |
|------|-----------|------------|
| 国内站(默认) | `GRASP_TRAE_REGION=cn` 或不写 | 不强制 `TRAECLI_HOST`(镜像按 `docs.trae.cn` 安装) |
| 国际站 | `GRASP_TRAE_REGION=intl` | `TRAECLI_HOST=https://www.trae.ai` |

企业专属域名可直接设 `TRAECLI_HOST=https://your-corp.example`;**显式 host 不会被区域别名覆盖**。

```json
{
  "acpBackend": "trae",
  "env": {
    "GRASP_TRAE_API_KEY": "trae-lt-xxx",
    "GRASP_TRAE_REGION": "cn"
  }
}
```

### OpenCode

1. Meta:`acpBackend = opencode`(沙箱镜像 `universal-sandbox`;CLI 为 `opencode run --format json`,经 ACP 桥包装)。
2. 共享 Agent env:
   - `GRASP_OPENCODE_API_KEY`(别名 `OPENCODE_API_KEY`)
   - `GRASP_OPENCODE_PROVIDER`:OpenCode 模型目录(models.dev)里的厂商 id(`openai` / `anthropic` / `deepseek` / `zai` / …);目录里没有的可以直接自己起一个名字(如 `tokenhub`),runtime 会按 OpenAI 兼容端点生成适配器,此时 `GRASP_OPENCODE_BASE_URL` 必填。`custom` 是这类自定义端点的默认名字,没有特殊含义
   - 可选 `GRASP_OPENCODE_BASE_URL`(`custom` 必填)
   - 可选 `GRASP_OPENCODE_MODEL_VISION=1`:为 models.dev 尚未收录的模型声明图片输入能力。未设置时保持文本模型，避免把图片误发给不支持视觉的端点
   - `ACP_BRIDGE_MODEL`,格式 `provider/model`(如 `deepseek/deepseek-v4-pro`);只填模型 id 也认,缺的厂商前缀由 runtime 补上(已带前缀不会重复补)。模型 id 自身带斜杠的照原样填在前缀后面(聚合网关常见,如 `openrouter/anthropic/claude-sonnet-4-5`);目录里第一段恰好等于厂商名的模型要写满两段(OpenRouter 的 `openrouter/auto` → `openrouter/openrouter/auto`)。模型 id 必须是该端点真有的,否则 `opencode run` 直接以 exit 1 结束。补前缀发生在注入沙箱的 env 上,不只是 `opencode.json`:桥会把这个变量原样交给 `opencode run --model`,而命令行参数优先于配置文件,少了前缀就会路由到第一段同名的厂商
3. 启动时若配置树里还没有用户自己的 `opencode.json`,runtime 可按厂商/Base/Model 生成一份:有 Key 时把 `apiKey` 写在该厂商名下(`{env:OPENCODE_API_KEY}`),因此目录里的任意厂商都能用,不依赖各家专属环境变量。
   厂商是否在目录里,由同一份 models.dev 快照(`opencodecatalog`,也供 UI 的厂商/模型下拉用)判定:在目录里就保留 OpenCode 自己的 SDK 与模型表;不在目录里(或就是 `custom`)则补 `npm: @ai-sdk/openai-compatible` 并声明所填模型 id,所以自建/聚合网关直接用自己的名字即可。快照拉不到时保守处理——只有 `custom` 补适配器,避免凭猜测覆盖本来能用的厂商。
4. 鉴权门认 `opencode.json` 或 `settings.json`;两者都没有时必须有 API key。厂商文档见 https://opencode.ai/docs/providers/

```json
{
  "acpBackend": "opencode",
  "env": {
    "GRASP_OPENCODE_API_KEY": "sk-xxx",
    "GRASP_OPENCODE_PROVIDER": "deepseek",
    "GRASP_OPENCODE_MODEL_VISION": "1",
    "ACP_BRIDGE_MODEL": "deepseek/deepseek-v4-pro"
  }
}
```

### 速查:鉴权别名 → 容器变量

| acpBackend | Agent env(任选其一) | 容器内 CLI 变量 |
|------------|---------------------|-----------------|
| `cursor` | `GRASP_CURSOR_API_KEY` / `CURSOR_API_KEY` | `CURSOR_API_KEY` |
| `claude_code` | `GRASP_CLAUDE_API_KEY` / `ANTHROPIC_API_KEY` | `ANTHROPIC_API_KEY` |
| `codebuddy` | `GRASP_CODEBUDDY_API_KEY` / `CODEBUDDY_API_KEY` | `CODEBUDDY_API_KEY` |
| `trae` | `GRASP_TRAE_API_KEY` / `TRAE_API_KEY` / `TRAECLI_PERSONAL_ACCESS_TOKEN` | `TRAECLI_PERSONAL_ACCESS_TOKEN` |
| `opencode` | `GRASP_OPENCODE_API_KEY` / `OPENCODE_API_KEY` | 按厂商映射(`OPENAI_API_KEY` 等) + `OPENCODE_API_KEY` |

> 平台级 `GRASP_CURSOR_API_KEY` / `sandbox.cursor_api_key` 已废弃,**不会**注入沙箱。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `GRASP_PORT` | `8080` | HTTP 端口 |
| `GRASP_DB` | `grasp.db` | SQLite 路径(`:memory:` 用于测试) |
| `GRASP_EXEC_PROVIDER` | `sandbox` | 沙箱执行后端;`cursor` 为兼容别名,其它值回落 |
| `GRASP_MAX_RUNS` | `5` | 并发 run 上限 |
| `GRASP_SANDBOX_GATEWAY_URL` | `http://127.0.0.1:8899` | sandbox-gateway 控制面地址(创建/销毁/管理沙箱) |
| `GRASP_GATEWAY_API_KEY` | — | 网关可选 Bearer 令牌(网关关鉴权时留空) |
| `GRASP_SANDBOX_IMAGE` | — | 每次创建时的镜像覆盖;留空即用网关默认镜像(universal-sandbox) |
| `GRASP_SANDBOX_ENV` | — | 通用 `K=V,K2=V2` 环境变量,注入每个沙箱(厂商无关;**不含** ACP API Key) |
| `GRASP_CURSOR_AUTH` | — | 可选(cursor 专用):挂载宿主 Cursor 配置目录(只读)复用 CLI 登录 |
| `GRASP_AGENT_TIMEOUT_SEC` | `600` | 单轮 agent/react 回合硬超时(全局默认);单节点可在其 Agent 卡片填「超时(分钟)」单独放宽 |
| `GRASP_CHAT_IDLE_SEC` | `720` | 回合内多久无事件即判卡死并中断 |
| `GRASP_MCP_ADVERTISE` | `http://host.docker.internal:<PORT>` | 沙箱内 agent/MCP 客户端回连 run 级 artifact-store MCP 的 base URL。K8s gateway 须改为沙箱可达且挂载 `/mcp` 的实例基址(如 `http://api.example.com`);勿用仅 SPA/无 `/mcp` 路由的入口域名。若误配 `spa.example.com`,加载配置与注入时会改写为 `api.example.com`(见 `RewriteMisconfiguredMCPAdvertise`) |
| `GRASP_PROFILES_ROOT` | `data/profiles` | Agent profile 规则根(挂入 `{configRoot}/rules`) |

> **GitLab 不是平台配置**:平台不依赖任何固定的 GitLab 地址或令牌。仓库地址由工作流
> 全局变量 `repo_url` 提供;凭据(`GITLAB_TOKEN` / `GITLAB_URL` 等)在 **Agent 元信息的
> 环境变量**里配置,值可引用 `${vars.<全局变量名>}`。run 执行时按 run 注入对应沙箱用于
> clone/push/MR。`GITLAB_URL` 未显式配置时会自动从 `repo_url` 推导(scheme+host)。

## 配置(YAML)+ 存储(SQLite)

**单个 YAML 配置文件**,数据落 SQLite。**配置不打进镜像**——运行时由环境变量与挂载的
`CONFIG_PATH` 提供。

- 进程读 `CONFIG_PATH`(默认 `config.yaml`,也可作为首个命令行参数传入)指向的 YAML,
  解析为类型化结构(`internal/config/config.go`),并经 `internal/config/watcher.go` 监听
  文件目录,文件被替换后防抖 1s 重载(`atomic.Pointer` 原子替换;`server.port` /
  `database.path` 这类需重启的项变更时打 WARN)。
- Kubernetes 部署时通常把 YAML 写入 ConfigMap,以只读卷挂到
  `/etc/config/config.yaml`;主容器读它。配置示例见 `config.example.yaml`。
- 优先级 **显式环境变量 > 配置文件 > 代码默认值**。文件缺失时以 env/默认值启动,
  零外部依赖(本地开发可不写文件;单测不触网)。

配置结构(YAML 键):

```yaml
server: { port, mcp_advertise }
database: { path }
engine: { exec_provider, max_concurrent_runs, profiles_root }
sandbox: { image, env, cursor_auth_path, agent_chat_timeout_seconds, ... }
```

> GitLab/代码托管**不在**这份配置里:仓库地址走工作流全局变量 `repo_url`,凭据走
> Agent 元信息环境变量,不是平台级配置。

环境变量覆盖(env 优先级最高,上表对应字段):`GRASP_PORT`、`GRASP_DB`、
`GRASP_EXEC_PROVIDER`、`GRASP_MAX_RUNS`、`GRASP_PROFILES_ROOT`、`GRASP_MCP_ADVERTISE`、
`GRASP_SANDBOX_IMAGE`、`GRASP_AGENT_MODEL`、`GRASP_AGENT_TIMEOUT_SEC` 等。

> **ACP 鉴权不在平台级配置**:各后端 API Key / 站点通过 **项目沙箱 env**(流水线底噪)或
> **Agent 元信息 env**(同名覆盖)注入流水线沙箱(见「ACP 后端怎么用」)。
> `sandbox.cursor_api_key` / `GRASP_CURSOR_API_KEY` 若仍出现在旧配置中会打 WARN 且**不会**
> 注入沙箱。Agent Studio 不继承项目 env。
> Git 托管凭据（`GITLAB_*` / `GITHUB_*` / SSH）同样配在 Agent env，按 run 注入沙箱，
> 不进平台配置或镜像。

完整选项表见 [`CONFIGURATION.md`](CONFIGURATION.md)。公开发布栈见仓库根
`compose.release.yaml` 与 `./release-smoke.sh`；镜像发布见 `.github/workflows/publish-image.yml`。

### VNC 预览（沙箱内置）

app_preview 前端 Tab 与沙箱控台 noVNC 走沙箱内置 VNC 栈（`VNC_PREVIEW=1` 时启动
Xvfb+Chromium+x11vnc+websockify）。VNC 栈由 sandbox-gateway 的 `universal-sandbox`
镜像提供；本仓不构建沙箱镜像。

CDP `:9222` / noVNC `:6080` **不是**对外 data-plane（无应用层鉴权，不 publish
到宿主/LB）。用户只经本服务 WebSocket：

- `GET /sandbox-vnc/:sandboxId/ws`
- `GET /preview-vnc/:runId/:nodeId/:port/ws`

鉴权始终启用时（`auth` 无开关）上述路径 `RequireSession`（仅校验 Session 有效，
**不**校验沙箱/跑步归属）；`Auth == nil` 的测试形态不 401。
`/preview/:runId/:nodeId/:port` HTTP 反代**不加** Session（iframe 无法带 cookie）。
Pick/导航与 RFB 共套，不回退直连 websockify，也不回退 `sandboxIP:9222/6080`
（有 gateway 命名端点时缺内部地址则失败关闭）。集群外 Grasp 不能拨沙箱
CDP/noVNC。K8s 存量 LB 在 gateway 启动调和完成前仍可能对外暴露 9222/6080。

## 种子数据

首次启动不自动写入样例流水线或密钥。「默认项目」为空时走第一次安装引导：配置 ACP 后端 / Token / Git（写入共享 Agent 配置），再生成综合项目组与「默认工作流」。

## 运行(真实沙箱)

需要本机有 Docker 守护进程与可联网拉取镜像。

```bash
cd server
GRASP_SANDBOX_IMAGE=universal-sandbox:local \
GRASP_PORT=8090 \
go run ./cmd/server
```

先在 Agent 元信息里选好 `acpBackend` 并配置鉴权(及 CodeBuddy/Trae 站点),可附带
Git 凭据(可引用全局变量)。Cursor 示例:

```json
{
  "acpBackend": "cursor",
  "env": {
    "GRASP_CURSOR_API_KEY": "crsr_xxx",
    "GITLAB_TOKEN": "glpat_xxx",
    "GITLAB_URL": "${vars.repo_url}"
  }
}
```

CodeBuddy 国际站 / Trae 国内站等其它后端的 env 写法见上文「ACP 后端怎么用」。
创建并发布流水线后,用 `POST /api/workflows/<id>/runs` 触发 run。

## 测试

```bash
go test ./...                       # 引擎 FSM + fake-bridge E2E,零凭证,~2s
# 真实沙箱集成测试(需可达的 sandbox-gateway; key 写在测试 Agent profile env 或 GRASP_CURSOR_API_KEY):
GRASP_LIVE=1 GRASP_SANDBOX_GATEWAY_URL=http://127.0.0.1:8899 GRASP_CURSOR_API_KEY=crsr_xxx \
  go test ./internal/runtime/ -run TestCursorLiveRunAgent -v
# 原生 MCP 验证(Agent 真的调用 write_artifact,无 produces/harvest):
GRASP_LIVE_MCP=1 GRASP_CURSOR_API_KEY=crsr_xxx \
  go test ./internal/runtime/ -run TestCursorLiveMCP -v
```

### E2E(真实沙箱端到端)

live 测试跑真实沙箱路径:经 sandbox-gateway 起沙箱 → ACP 驱动 cursor-agent → 按
`produces` 契约取回产物。需要一个可达的 sandbox-gateway + Cursor API key,慢(分钟级),
所以默认单测不跑。运行前先起好 sandbox-gateway(见其仓库 `start.sh`),再:

```bash
GRASP_LIVE=1 GRASP_SANDBOX_GATEWAY_URL=http://127.0.0.1:8899 GRASP_CURSOR_API_KEY=crsr_xxx \
  go test ./internal/runtime/ -run TestCursorLiveRunAgent -v -timeout 30m
```

`cmd/acpsmoke <port> "<prompt>"` 是对接已运行容器 ACP 的手动冒烟工具。

## Git 接入(Agent 元信息环境变量)

Git/代码托管由**用户自己配置**,不是平台级设置。仓库地址来自工作流全局变量 `repo_url`;
凭据在 **Agent 元信息的环境变量**里配置,值支持模板替换:

- `${GRASP_ARTIFACT_URL}` / `${GRASP_ARTIFACT_TOKEN}` / `${GRASP_RUN_ID}` / `${GRASP_NODE_ID}` — 运行级变量;
- `${vars.<全局变量名>}` — 工作流全局变量,如 `${vars.repo_url}`。

### 托管商支持矩阵

| 方式 | 托管商 | 环境变量 | Agent `submit_mr`（建单） | 平台 `detect_push` + `create_mr` |
|------|--------|----------|--------------------------|----------------------------------|
| HTTPS | GitHub (github.com) | `GITHUB_TOKEN` | `gh pr`（需沙箱预装 `gh`） | 不代建 PR（仍仅推送检测） |
| HTTPS | 自建 GitHub/GHE | `GITHUB_TOKEN` + `GITHUB_URL` | 同上（主机与 `GITHUB_URL` 一致） | 不代建 PR |
| HTTPS | GitLab (gitlab.com) | `GITLAB_TOKEN` | `glab` | glab 自动建 MR |
| HTTPS | 自建 GitLab (如 git.example.com) | `GITLAB_TOKEN` + `GITLAB_URL` | 同上（主机与 `GITLAB_URL` 一致） | 同上 |
| SSH | 任意 (Gitea/Bitbucket/自建等) | `GIT_SSH_PRIVATE_KEY` + `GIT_SSH_KNOWN_HOSTS`(必填) | 主机可匹配 GitLab/GitHub 时走对应 CLI；否则推送后建单失败 | 非 GitLab 不代建 |

**双路径能力差**:Agent 侧可按远端主机在 `glab` / `gh` 间分流创建合并请求（MR/PR）；平台自动建单本迭代**仍仅 GitLab**。GitHub 场景依赖 Agent 执行 `gh`，开启 `create_mr` 也不会由平台代建 PR。匹配不上的托管商（如 Gitea）应先完成冲突解决与 `git push`，再以建单失败结束（失败摘要标明已推送）。

Gitea/Bitbucket/Codeberg 等 **HTTPS 不支持 Token 注入**,请改用 SSH。`startup.sh` 在 clone 前按每个仓库 URL 的 scheme 路由凭据(HTTPS 与 SSH 互斥);SSH 未配 `GIT_SSH_KNOWN_HOSTS` 时 fail-fast。仓库列表通过 `GIT_REPOS`(逗号分隔,每项 `name|url|branch`)注入,每个仓 clone 到 `/root/workspace/<name>/`。

`GIT_REPOS` 与凭据同样用「环境变量 + 引用」的方式在 Agent 元信息里显式接线,引用工作流的 `repos` 全局变量:`{"GIT_REPOS":"${vars.repos}"}`(`${vars.repos}` 会被展开成上面的 `name|url|branch` 逗号格式)。平台不做任何兜底/特殊注入 —— 没接线就不会 clone(和不配 `GITLAB_TOKEN` 就不能 push 同理)。

例如在 `go-backend` Agent 元信息里配置 `{"GIT_REPOS":"${vars.repos}","GITLAB_TOKEN":"glpat_xxx","GITLAB_URL":"https://git.example.com"}`。
run 执行时这些环境变量按 run 注入(kind-agnostic)沙箱用于 clone/push;`GITLAB_URL` 未显式配置
且首仓确为 GitLab(非 github.com / 非 `GITHUB_URL` host)时由 `repos[0].url` 自动推导(scheme+host,见 `gitBaseURL`)。
沙箱启动时只要配置了 `GITHUB_TOKEN` / `GITLAB_TOKEN` 就会两边都注入凭据并登录 `gh` / `glab`。平台进程本身不持有任何
全局 Git 凭证。`detect_push` + `create_mr` 仅对 GitLab 仓库(含 `GITLAB_URL` 匹配的自建实例)调用 glab;非 GitLab 输出 `pushed`/`branch`/`pushed_sha`,`mr_url` 为空。
