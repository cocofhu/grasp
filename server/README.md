# Grasp 后端(Go)

有限状态机(FSM)编排引擎 + 按 run 隔离的 artifact-store + 真实沙箱执行(cursor-agent)。

## 执行后端(ExecProvider)

支持七类 ACP 后端(`cursor` / `claude_code` / `codebuddy` / `trae` / `opencode` / `codex` / `deepseek_harness`),由 Agent 卡片
`agent.json` 的 `acpBackend` 字段选择;`ProviderRegistry` 按 Agent profile (`agent_profile`) 路由到
对应 Provider。统一沙箱镜像内 `acp-bridge` 按 `AGENT_PROVIDER` 单活启动 bridge(:8765)。

**鉴权**:各后端 Key 与 Git 凭据**只**在**工作区凭据** UI 中管理，运行时按 Run 注入沙箱。
Agent env、工作区共享 env、Run env 与平台 `sandbox.env` 中出现密钥键（见下）会在保存时被拒绝(400)；
服务进程自身的环境变量不会注入沙箱；Agent Studio 不继承工作区凭据。

凭据解析与注入范围如下。UI 保存的值使用 `security.secrets_key` 加密，列表和审计只显示掩码。

| 管理项 | UI 内容 | 注入/使用 |
|---|---|---|
| 编码工具厂商授权 | Cursor、Claude Code、CodeBuddy、Trae、OpenCode（含 OpenCode Go）、DeepSeek Harness 的官方 Key/Token；Codex 平台授权使用登录文件 | 保存为 `tool_auth`，只注入具名模型配置选中的授权；Codex 登录文件写入 `{configRoot}/auth.json` |
| 模型 API 账号 | 通用模型服务的 Key、Base URL、协议与原始模型 ID | 保存为 `model_api`；通过具名模型配置选择兼容编码工具和连接，不与厂商授权混用 |
| Git HTTPS | GitHub/GitLab Token、GitLab URL | 注入沙箱，由 startup 配置 `git`、`gh`/`glab` |
| Git SSH | 私钥、known_hosts | 写入 `~/.ssh/id_rsa`、`~/.ssh/known_hosts`，不作为普通 env |
| MCP / 自定义 | MCP Header/command env 或明确目标环境键；支持 `${credential:<id>}` | 仅展开到目标 MCP 或绑定键，不扩散到所有 Agent |
| 渠道、外部 MCP、工作流 Key | 在工作区凭据页汇总查看 | 复用现有加密/hash 适配器；渠道/鉴权服务端使用，不注入沙箱 |

只能来自工作区凭据的密钥键：`GRASP_CURSOR_API_KEY`、`GRASP_CLAUDE_API_KEY`、`GRASP_CODEBUDDY_API_KEY`、
`GRASP_TRAE_API_KEY`、`GRASP_OPENCODE_API_KEY`、`GRASP_CODEX_AUTH_JSON`、`GRASP_DEEPSEEK_API_KEY`、`GITHUB_TOKEN`、`GITLAB_TOKEN`、`GIT_SSH_PRIVATE_KEY`、
`GIT_SSH_KNOWN_HOSTS`，以及它们映射到的 CLI 变量 `CURSOR_API_KEY`、`ANTHROPIC_API_KEY`、`CODEBUDDY_API_KEY`、
`TRAECLI_PERSONAL_ACCESS_TOKEN`、`OPENCODE_API_KEY`、`DEEPSEEK_API_KEY`，以及 dsh 别名 `ACP_DEEPSEEK_API_KEY`、`ACP_DEEPSEEK_HARNESS_API_KEY`。`GRASP_CODEX_AUTH_JSON` 只是登录文件槽位，不会映射成 CLI 环境变量。站点等非敏感项可写在 Agent env；模型、服务商与 API 地址通过具名模型配置管理，不能用 env 覆盖所选绑定。
已配置值的凭据键不能被每次 Run 的临时 env 覆盖。平台注入的保留变量（`GRASP_ARTIFACT_*`、`GRASP_RUN_ID`、`GRASP_NODE_ID`、`GRASP_MEMORY_*`、`GRASP_CONTEXT_*`、`GRASP_SCHEDULER_*`、`GRASP_PM_*`、`CONFIG_ROOT`、`AGENT_PROVIDER`、沙箱密码）不能作为凭据键。部署密钥、数据库密码、Session/Gate/Artifact Token 等仍是平台配置或系统自动管理项，不进入工作区凭据页。

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
  - 默认映射:`cursor`→`/root/.cursor`、`claude_code`→`/root/.claude`、`codebuddy`→`/root/.codebuddy`、`trae`→`/root/.trae`、`opencode`→`/root/.config/opencode`、`codex`→`/root/.codex`、`deepseek_harness`→`/root/.dsh`;
  - `rules/base.md`(基础约束,alwaysApply)、`rules/artifact-store.md`(产物 MCP 与 `node_complete` 用法),两者内嵌、不可修改;
  - Agent 的行为规则来自 Agent 工作目录的 `AGENTS.md` 与 skills(`GRASP_PROFILES_ROOT`),能力来自 `agent.json` 的 `capabilities`;
  - 需要 push/MR 的节点可在 Agent 工作目录附 `skills/git/SKILL.md`;
  - `mcp.json` 写入 artifact-store MCP 配置(含改写后的 `GRASP_ARTIFACT_URL`);
  - 注入路径:**gateway `config.bundleUrl` 启动前 inject** — 控制面把 ConfigHome 打成 `.tgz`，经 `/sandbox-inject/:id` 短时下载；gateway 设 `SANDBOX_INJECT`，镜像 `startup.sh` 在 acp-bridge/agent 启动**之前**解压到 `{configRoot}`。URL 基址与 `mcp_advertise` 相同(沙箱可达)。Attach 重连仍可 SSH 补种。不使用 `config.hostPath`(远程 K8s 会挂空卷)。
- **产物契约 + 完成标记**:节点完成时引擎要求已调用 `node_complete`,并校验 Agent 在 `capabilities.writes` 中声明为必填的结构化产物已经由对应的 `set_*` 工具写入平台 store,缺失即失败。推分支、开 MR 由实现模板的 git-mr skill 负责,平台不代验。
- **上游产物读取**:run 内已有产物名会在 prompt 中列出,Agent 用 MCP `read_artifact` / `list_artifacts` 按名读取(按 run token 隔离)。产物**不**落盘到工作区,以免污染节点的代码变更报告。
- **隔离**:每个 run 一枚 token,artifact-store 按 run 命名空间读写、token 绑定校验,run 间互不可见;容器 ACP 端口只绑定 `127.0.0.1` 临时端口。
- **token 生命周期 = 沙箱生命周期**:token 无单独的过期时间。内存注册命中即放行;run 结束或服务重启后,只要该 run 仍有存活的沙箱(回合执行中或回合结束后的 `run_sandbox_ttl_minutes` 保活窗口内),持久化 token 依旧可鉴权,让仍存活的沙箱 agent 继续写产物而非 401;最后一个沙箱销毁后 token 自然失效。

## ACP 后端怎么用

各后端的 Key 统一保存在**工作区凭据**中，编码工具厂商授权与通用模型 API 账号分别保存。
先在「模型」中保存编码工具、凭据和上游模型的具名配置，再让 Agent 绑定该配置或继承默认配置。
下面的 CLI 环境变量仅说明运行时注入结果；站点等选项可由 Agent env 管理，模型路由以保存的配置为准。
Codex 平台授权使用登录文件，启动前写入沙箱 `auth.json`。

| acpBackend | 沙箱内 CLI | 默认 configRoot | 容器内鉴权变量 |
|------------|------------|-----------------|----------------|
| `cursor` | `cursor-agent … acp` | `/root/.cursor` | `CURSOR_API_KEY` |
| `claude_code` | `npx @zed-industries/claude-code-acp` | `/root/.claude` | `ANTHROPIC_API_KEY` |
| `codebuddy` | `codebuddy --acp` | `/root/.codebuddy` | `CODEBUDDY_API_KEY` |
| `trae` | `traecli acp serve` | `/root/.trae` | `TRAECLI_PERSONAL_ACCESS_TOKEN` |
| `opencode` | `opencode run --format json` | `/root/.config/opencode` | 厂商原生 Key(由 `OPENCODE_API_KEY` 映射) |
| `codex` | `codex exec --json` | `/root/.codex` | 登录文件 `{configRoot}/auth.json`（不注入环境变量） |
| `deepseek_harness` | `dsh --profile acp` | `/root/.dsh` | `DEEPSEEK_API_KEY` |

Agent Studio 的 Env 页会按当前后端提示所需 Key;CodeBuddy / Trae 另有「站点」下拉,
写入 `GRASP_*_REGION`(运行时再规范化成官方变量)。OpenCode 另有厂商 / API Base / model
(`GRASP_OPENCODE_PROVIDER`、`GRASP_OPENCODE_BASE_URL`、`ACP_BRIDGE_MODEL`)。

### Cursor

1. Meta:`acpBackend = cursor`(默认)。
2. 工作区凭据「Cursor API Key」保存 Key(来自 Cursor Dashboard / CLI 登录后的 API Key)。

无「国际/国内站」区分;全球一套端点。

### Claude Code

1. Meta:`acpBackend = claude_code`。
2. 工作区凭据「Claude Code API Key」保存 Key(来自 Anthropic Console)。
3. 可选透传(平台不改写):Agent env 中的 `ANTHROPIC_BASE_URL` 等网关变量。

### CodeBuddy

1. Meta:`acpBackend = codebuddy`。
2. 工作区凭据「CodeBuddy API Key」保存 Key。
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
    "GRASP_CODEBUDDY_REGION": "staging"
  }
}
```

### Trae

1. Meta:`acpBackend = trae`。
2. 工作区凭据「Trae API Token」保存 CLI 登录令牌(须含 `trae-lt-` 前缀;注入沙箱时写成 `TRAECLI_PERSONAL_ACCESS_TOKEN`)。
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
    "GRASP_TRAE_REGION": "intl"
  }
}
```

### OpenCode

1. 工作区凭据中的「OpenCode CLI」保存官方 Key，域为 `tool_auth` / `toolId=opencode`。OpenCode（`opencode/<模型>`）与 OpenCode Go（`opencode-go/<模型>`）是两套订阅：各存一把同类型钥匙，用别名区分。一份模型配置只绑一把钥匙。保存时不核对钥匙属于哪一家，配错要到运行才失败。第三方模型服务的 Key 仍保存在「API 账号」。
2. 创建模型配置，选择 OpenCode CLI 和所需授权。官方模式可以使用 `opencode/<模型>` 或 `opencode-go/<模型>`，目录更新后两边都会出现。同一配置的主模型与辅助模型必须同属一家。没有非空模型时仍只启用 `opencode`；主模型留空但辅助模型全是 Go 时只启用 `opencode-go`。API 账号模式仍使用所选连接的原始模型 ID。
3. 官方模式只注入选中的 Key，映射为 `OPENCODE_API_KEY`。非空模型全属 Go 时，隔离配置只启用原生 `opencode-go`；全属 Zen 或没有非空模型时仍只启用 `opencode`。不覆盖该 provider 的原生模型 SDK 与端点。旧全局、项目配置和账号不会覆盖路由；独立 data/state 目录保留会话历史。该模式要求 OpenCode >=1.4.1，不兼容的 CLI 会明确报错。
4. API 账号模式继续按所选协议生成连接适配器，不复用厂商授权；模型 ID 中的 `/` 原样保留。厂商文档见 https://opencode.ai/docs/providers/ 与 https://opencode.ai/docs/go/

```json
{
  "name": "OpenCode 官方模型",
  "backend": "opencode",
  "binding": {
    "kind": "platform",
    "credentialId": "已保存的 OpenCode 厂商凭据 ID"
  }
}
```

### Codex

1. 在模型配置中选择 Codex，使用平台授权或兼容的 Responses API 账号，再让 Agent 绑定该配置。
2. 平台授权在工作区凭据「Codex Login File」保存登录文件；通用 API Key 保存在「API 账号」，不能粘贴到登录文件字段。
3. 平台模式启动前把登录文件写到配置目录的 `auth.json`，不注入 `OPENAI_API_KEY`；缺文件时拒绝启动。API 账号模式使用明确选择的连接与模型。
4. 平台模式一轮结束后若登录文件更新，加密写回同一条凭据；登录被拒绝或无法刷新时不写回。

### DeepSeek Harness

1. 在「用编码工具」路径选择 **DeepSeek Harness**，或设置 Agent 的 `acpBackend = deepseek_harness`，使用原生 `dsh` 执行。
2. 工作区凭据「DeepSeek Harness」保存官方 DeepSeek Key，创建模型配置时选择该授权；运行时把 `GRASP_DEEPSEEK_API_KEY` 映射为 `DEEPSEEK_API_KEY`，固定使用官方原生路由。通用模型 Key 保存为 API 账号，并通过兼容连接模式使用。
3. 官方模式默认 `deepseek-flash`，也可在模型配置中选择 `deepseek-v4-pro`。沙箱通过 ACP `session/set_config_option` 应用模型，不向 `dsh` 传 `--model`。
4. `CONFIG_ROOT` 映射为 `DSH_HOME`。生成的 `AGENTS.md` 合并角色指令和显式 `alwaysApply: true` 的规则，保留条件规则原文件与完整 `skills/`；澄清节点仍裁剪完成标记规则。stdio/HTTP MCP 经 ACP 创建会话时传入。
5. 沙箱镜像预装固定版本 `@deepseek-ai/dsh@0.2.1-rc.2`，需要 `IMAGE_LEVEL >= 2`。按服务端、沙箱镜像的顺序发布；已有旧镜像中的运行时热更新不会安装 dsh。

dsh ACP 文本按已提交的消息块推送，`usage_update` 仅表示上下文占用，不计入输入/输出/缓存 token 统计。每轮输入/输出/缓存用量改为读取固定版本 dsh 的 `tokenUsage` 会话投影缓存，按轮差分后随 `prompt_done.usage` 上报：缓存未落盘或会话无模型调用时省略 `usage`，帧结构与旧版一致；子代理会话的用量不在主会话投影内，暂不汇总。会话续接遵循现有 ACP bridge 生命周期：网络或服务端重连可复用仍存活的会话，bridge 重启后的自动会话恢复尚未接入。

### 模型目录与 Solo 会话设置

- `GET /api/workspace/model-connections/:connectionId/models` 使用该连接绑定的 API 账号读取模型目录，仅返回 ID、名称和明确提供的能力信息，不写入连接的模型列表。标准模型目录不可用时返回 `{ "models": [], "error": "..." }`，可重试或手动填写；只有保存配置时选中的模型会追加到账号。
- 创建 Solo 会话时可传 `reasoningEffort`；`POST /api/solo/sessions/:id/model` 可单独提交 `modelId`、`reasoningEffort` 或同时提交。省略模型会保留当前模型，空强度表示默认。仅接受当前模型及适配器提供的档位，明确的 `reasoning:false` 优先。
- 模型与强度设置在下一轮开始前共同应用；失败恢复已确认设置，不影响正在运行的一轮。旧运行时继续支持模型选择，但不接受其未公布的非默认强度设置。

### 速查:凭据键 → 容器变量

| acpBackend | 工作区凭据键 | 容器内 CLI 变量 |
|------------|------------|-----------------|
| `cursor` | `GRASP_CURSOR_API_KEY` | `CURSOR_API_KEY` |
| `claude_code` | `GRASP_CLAUDE_API_KEY` | `ANTHROPIC_API_KEY` |
| `codebuddy` | `GRASP_CODEBUDDY_API_KEY` | `CODEBUDDY_API_KEY` |
| `trae` | `GRASP_TRAE_API_KEY` | `TRAECLI_PERSONAL_ACCESS_TOKEN` |
| `opencode` | `GRASP_OPENCODE_API_KEY` | 按厂商映射(`OPENAI_API_KEY` 等) + `OPENCODE_API_KEY` |
| `codex` | `GRASP_CODEX_AUTH_JSON` | 无（写入 `/root/.codex/auth.json`） |
| `deepseek_harness` | `GRASP_DEEPSEEK_API_KEY` | `DEEPSEEK_API_KEY` |

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `GRASP_PORT` | `8080` | HTTP 端口 |
| `GRASP_DB` | `grasp.db` | SQLite 路径(`:memory:` 用于测试) |
| `GRASP_MAX_RUNS` | `5` | 并发 run 上限 |
| `GRASP_SANDBOX_GATEWAY_URL` | `http://127.0.0.1:8899` | sandbox-gateway 控制面地址(创建/销毁/管理沙箱) |
| `GRASP_SANDBOX_GATEWAY_API_KEY` | — | 网关可选 Bearer 令牌(网关关鉴权时留空) |
| `GRASP_SANDBOX_IMAGE` | — | 每次创建时的镜像覆盖;留空即用网关默认镜像(universal-sandbox) |
| `GRASP_SANDBOX_ENV` | — | 通用 `K=V,K2=V2` 环境变量,注入每个沙箱(厂商无关;含密钥键时启动失败) |
| `GRASP_AGENT_TIMEOUT_SEC` | `600` | 单轮 agent/react 回合硬超时(全局默认);单节点可在其 Agent 卡片填「超时(分钟)」单独放宽 |
| `GRASP_CHAT_IDLE_SEC` | `720` | 回合内多久无事件即判卡死并中断 |
| `GRASP_MCP_ADVERTISE` | `http://host.docker.internal:<PORT>` | 沙箱内 agent/MCP 客户端回连 run 级 artifact-store MCP 的 base URL。K8s gateway 须改为沙箱可达且挂载 `/mcp` 的实例基址(如 `http://api.example.com`);勿用仅 SPA/无 `/mcp` 路由的入口域名。若误配 `spa.example.com`,加载配置与注入时会改写为 `api.example.com`(见 `RewriteMisconfiguredMCPAdvertise`) |
| `GRASP_PROFILES_ROOT` | `data/profiles` | Agent profile 规则根(挂入 `{configRoot}/rules`) |

> **GitLab 不是平台配置**:平台不依赖任何固定的 GitLab 地址或令牌。仓库地址由工作流
> 全局变量 `repo_url` 提供；凭据只在**工作区凭据** UI 中配置，run 执行时
> 按 run 注入对应沙箱用于 clone/push/MR。`GITLAB_URL` 未显式配置时会自动从 `repo_url` 推导。

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
engine: { max_concurrent_runs, profiles_root }
sandbox: { image, env, agent_chat_timeout_seconds, ... }
```

> GitLab/代码托管**不在**这份服务端配置里：仓库地址走工作流全局变量 `repo_url`，凭据只走
> 工作区凭据 UI，不是平台级配置。

环境变量覆盖(env 优先级最高,上表对应字段):`GRASP_PORT`、`GRASP_DB`、
`GRASP_MAX_RUNS`、`GRASP_PROFILES_ROOT`、`GRASP_MCP_ADVERTISE`、
`GRASP_SANDBOX_IMAGE`、`GRASP_AGENT_TIMEOUT_SEC` 等。

> **ACP 与 Git 鉴权只来自工作区凭据**:各后端 API Key 与 Git Token / SSH 从工作区凭据解析后按 run
> 注入沙箱，不进平台配置或镜像。Agent Studio 不继承工作区 env。

完整选项表见 [`CONFIGURATION.md`](CONFIGURATION.md)。公开发布栈见仓库根
`compose.release.yaml` 与 `./release-smoke.sh`；镜像发布见 `.github/workflows/publish-image.yml`。

### VNC 预览（沙箱内置）

应用预览 Tab 与沙箱控台 noVNC 走沙箱内置 VNC 栈（`VNC_PREVIEW=1` 时启动
Xvfb+Chromium+x11vnc+websockify）。VNC 栈由 sandbox-gateway 的 `universal-sandbox`
镜像提供；本仓不构建沙箱镜像。

CDP `:9222` / noVNC `:6080` **不是**对外 data-plane（无应用层鉴权，不 publish
到宿主/LB）。用户只经本服务 WebSocket：

- `GET /sandbox-vnc/:sandboxId/ws`：每个沙箱只有这一个桌面地址，桌面上就是 Agent 用 chrome-devtools MCP 操作的那个浏览器；应用端口是这个桌面里的页面，不再有按端口的 VNC 地址。

鉴权始终启用时（`auth` 无开关）该路径 `RequireSession`（仅校验 Session 有效，
**不**校验沙箱/跑步归属）；`Auth == nil` 的测试形态不 401。
`/preview/:runId/:nodeId/:port` HTTP 反代**不加** Session（iframe 无法带 cookie）。
Pick/导航与 RFB 共套，不回退直连 websockify，也不回退 `sandboxIP:9222/6080`
（有 gateway 命名端点时缺内部地址则失败关闭）。集群外 Grasp 不能拨沙箱
CDP/noVNC。K8s 存量 LB 在 gateway 启动调和完成前仍可能对外暴露 9222/6080。

## 种子数据

首次启动不自动写入样例工作流或密钥。工作区为空时走第一次安装引导：在工作区凭据 UI 配置 ACP 后端 / Token / Git（Git 可跳过），再生成内置 Agent 与「默认工作流」。

## 运行(真实沙箱)

需要本机有 Docker 守护进程与可联网拉取镜像。

```bash
cd server
GRASP_SANDBOX_IMAGE=universal-sandbox:local \
GRASP_PORT=8090 \
go run ./cmd/server
```

先在工作区凭据 UI 保存 `acpBackend` 对应的鉴权，可附带 Git 凭据。
CodeBuddy / Trae 站点等非敏感选项的 env 写法见上文「ACP 后端怎么用」。
创建并发布工作流后,用 `POST /api/workflows/<id>/runs` 触发 run。

## 测试

```bash
go test ./...                       # 引擎 FSM + fake-bridge E2E,零凭证,~2s
# 真实沙箱集成测试（Key 经测试进程环境变量传入；需可达的 sandbox-gateway）:
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

## Git 接入（工作区凭据 UI）

Git/代码托管由**用户自己配置**，不是平台级设置。仓库地址来自工作流全局变量 `repo_url`；
Token、GitLab URL、SSH 私钥与 known_hosts 都保存在工作区凭据中。
Agent 元信息 env 中的非敏感项（如 `GIT_REPOS`、`GITHUB_URL`）支持模板替换：

- `${GRASP_ARTIFACT_URL}` / `${GRASP_ARTIFACT_TOKEN}` / `${GRASP_RUN_ID}` / `${GRASP_NODE_ID}` — 运行级变量;
- `${vars.<全局变量名>}` — 工作流全局变量,如 `${vars.repo_url}`。

### 托管商支持矩阵

| 方式 | 托管商 | 凭据 | Agent 建单（git-mr skill） | 平台 `detect_push` + `create_mr` |
|------|--------|----------|--------------------------|----------------------------------|
| HTTPS | GitHub (github.com) | `GITHUB_TOKEN` | `gh pr`（需沙箱预装 `gh`） | 不代建 PR（仍仅推送检测） |
| HTTPS | 自建 GitHub/GHE | `GITHUB_TOKEN` + `GITHUB_URL` | 同上（主机与 `GITHUB_URL` 一致） | 不代建 PR |
| HTTPS | GitLab (gitlab.com) | `GITLAB_TOKEN` | `glab` | glab 自动建 MR |
| HTTPS | 自建 GitLab (如 git.example.com) | `GITLAB_TOKEN` + `GITLAB_URL` | 同上（主机与 `GITLAB_URL` 一致） | 同上 |
| SSH | 任意 (Gitea/Bitbucket/自建等) | 工作区凭据中的 SSH 私钥 + known_hosts(必填) | 主机可匹配 GitLab/GitHub 时走对应 CLI；否则推送后建单失败 | 非 GitLab 不代建 |

**双路径能力差**:Agent 侧可按远端主机在 `glab` / `gh` 间分流创建合并请求（MR/PR）；平台自动建单本迭代**仍仅 GitLab**。GitHub 场景依赖 Agent 执行 `gh`，开启 `create_mr` 也不会由平台代建 PR。匹配不上的托管商（如 Gitea）应先完成冲突解决与 `git push`，再以建单失败结束（失败摘要标明已推送）。

Gitea/Bitbucket/Codeberg 等 **HTTPS 不支持 Token 注入**,请改用 SSH。`startup.sh` 在 clone 前按每个仓库 URL 的 scheme 路由凭据(HTTPS 与 SSH 互斥);SSH 未配 known_hosts 时 fail-fast。仓库列表通过 `GIT_REPOS`(逗号分隔,每项 `name|url|branch`)注入,每个仓 clone 到 `/root/workspace/<name>/`。

`GIT_REPOS` 可在 Agent 元信息里引用工作流的 `repos` 全局变量:`{"GIT_REPOS":"${vars.repos}"}`(`${vars.repos}` 会被展开成上面的 `name|url|branch` 逗号格式)。认证信息只来自工作区凭据；未配置时不会 clone/push。

run 执行时工作区凭据与这些环境变量按 run 注入(kind-agnostic)沙箱用于 clone/push;`GITLAB_URL` 未显式配置
且首仓确为 GitLab(非 github.com / 非 `GITHUB_URL` host)时由 `repos[0].url` 自动推导(scheme+host,见 `gitBaseURL`)。
沙箱启动时只要配置了 `GITHUB_TOKEN` / `GITLAB_TOKEN` 就会两边都注入凭据并登录 `gh` / `glab`。平台进程本身不持有任何
全局 Git 凭证。`detect_push` + `create_mr` 仅对 GitLab 仓库(含 `GITLAB_URL` 匹配的自建实例)调用 glab;非 GitLab 输出 `pushed`/`branch`/`pushed_sha`,`mr_url` 为空。
