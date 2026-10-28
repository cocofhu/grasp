# Agent、模型配置与凭据

本文记录 Agent 模型路由的当前数据与交互契约。模型配置是模型路由的唯一来源；Agent 可显式继承默认配置中的模型组列表和 Git 授权。

## 数据关系

```text
Agent
 ├─ modelBinding → 多个 ModelConfiguration（configurationId 指定默认组）
 │                    ├─ backend：固定编码工具 / CLI
 │                    └─ binding
 │                         ├─ platform → 该工具的 Credential + 模型 ID
 │                         └─ connection → ModelConnection + 模型 ID + 工具角色配置
 │                                           └─ Credential
 └─ gitBindings → 多个 Git / SSH Credential + 适用范围
```

- Credential 只保存加密秘密及用途、发行方、工具、秘密种类；模型服务地址、协议和模型目录属于 ModelConnection。
- ModelConnection 保存协议、API Base、凭据引用与原始模型 ID。支持 Chat Completions、Responses、Anthropic Messages，具体可用性由 CLI 能力约束。
- ModelConfiguration 保存名称、backend、具体 binding、启停状态和 revision。内部只允许 platform 或 connection；工具创建后不可修改。
- Agent.modelBinding 使用 `{kind:"configuration", configurationId, configurationIds?}`。`configurationId` 指定默认组；可选 `configurationIds` 是包含默认组的有序、无重复绑定列表，缺省表示现有单组引用。列表可跨编码工具；Agent.acpBackend 对应默认组，实际会话按所选组确定工具。不在 Agent 上重复保存 Key、连接或模型角色。
- Agent 草稿允许 modelBinding 为空，表示未配置；没有可执行的默认模型。运行入口在创建或复用沙箱前验证配置及其依赖。
- SharedAgent 保存通用文件、MCP、环境和布局，以及可显式继承的模型组和 Git 授权默认项；Agent 使用独立开关决定是否继承。
- 仓库登记只保存克隆信息。访问方式、凭据绑定和环境变量只来自当前有效 Agent，历史仓库配置不会自动迁移。

## 凭据与模型页面

凭据和模型使用紧凑列表，显示 Logo、名称和短说明，详情留在编辑器。两个标签共享新增菜单；所有下拉复用可搜索组件。

先创建凭据，可使用服务商预设、带 Logo 的厂商选择或自定义 API Base 与 Key。再创建模型配置：选择编码工具 → 选择可用凭据 → 选择模型。工具、凭据都显示 Logo。新建凭据后返回当前模型草稿。

Agent 向导使用“基础信息 → 工具与模型 → Git → 确认创建”。编辑和创建可增加多个启用模型组并明确选择默认组；默认组变化同步默认 CLI 和配置目录。初始化、默认配置和团队创建保存同一模型引用格式，不提供直接来源绑定。

初始化向导可打开凭据/模型管理创建组合，再将选中的配置绑定到各角色。角色可显式选择另一条模型配置。默认工作流的必需角色缺失或未配置时返回可操作错误，不从共享默认补建。

## 多个 Git 授权

`gitBindings.items` 保存 credentialId、可选 scope、SSH known-hosts 凭据引用。未开启 Git 继承时，缺省和空列表均表示没有 Git 授权。旧数据中的 preferredCredentialId 会被忽略。

GitHub / GitLab 凭据保存 `host`，默认分别为 `https://github.com` / `https://gitlab.com`，也可填写自建站点及端口。Host 与 Token 在同一凭据中管理，不使用独立的 GitLab URL 凭据。Agent scope 只能细化同一 Host 内的仓库，不能把凭据用于另一站点或改变协议。

Agent 默认配置保存完整 `modelBinding` 和 `gitBindings`。每个 Agent 通过 `inheritModel`、`inheritGit` 分别明确选择是否继承；关闭时使用本地选择，开启时动态使用默认项且不复制密钥。默认模型的 CLI 由默认组确定。缺失或失效的默认引用报错，不回退到本地或另一账号；默认项变化也参与沙箱指纹校验及共享节点切换。

所选授权全部放入沙箱受限文件，禁止用全局 Token 环境代替用户选择。按协议、主机和仓库路径选择一个身份，仓库范围优先于主机范围；同范围按列表顺序使用先出现的授权。认证失败不尝试其他账号。所选凭据失效时明确报错。

工作流独立沙箱在创建时注入；共享沙箱在节点切换时替换整组授权，包括切为空。Solo 在每轮边界按实际所选组检查模型指纹，使用 reprime 更新原执行环境的工具、配置目录和授权并重连 ACP；工作区保持。容器模式保留同一容器；worktree 模式只替换本地 backend 进程，保留原目录、文件和 launch 持有关系，成功或回滚后保存新的 ACP 端口，最终关闭才释放目录。宿主环境中的其他账号不会替代所选授权。Git 指纹变化继续要求重新打开，不借模型切换更新其他权限。容器模式下工具专用镜像不同也明确拒绝原地切换。

## 会话选择与历史交接

Solo 创建支持 `initialConfigurationId` 和原有 `initialModelId`；切换支持 `configurationId`、`modelId`、推理强度。会话分别保存初始、当前请求和实际已应用的模型组；模型标识必须与模型组共同定位，不能按原始模型 ID 跨账号去重。省略模型组保留当前组，显式空组选择 Agent 默认组。保存后立即通知同一会话已连接的视图，当前轮使用原路由，新请求在下一轮应用；忙时保留下一轮生效提示，早先发起的详情读取不能覆盖新的选择通知。失败回退使用包含组标识的条件更新，避免覆盖较新的用户选择。

新工具首次输入主动携带早期用户要求及近期对话，保留原始消息 ID、失败状态、附件引用与可见工具结果；超出输入预算的部分明确标注摘录并提供历史入口。artifact-store 的只读 `get_history(view="summary"|"messages", cursor?, limit?)` 复用原始 `SoloMessage` 数据；默认当前会话，Token 与当前沙箱代次决定范围，调用方不能指定其他会话。分页固定快照并按时间/消息 ID 读取，返回 `history_version`、`next_cursor`；页内顺序为正序，游标继续读取更早消息。关闭会话和旧沙箱代次失去访问权限。现有工作流 `list_run_history` / `get_history_detail` 保留原执行历史语义。

模型组、连接、凭据、Agent 和默认配置成功保存后发出目录失效通知。本页使用响应式版本更新，浏览器其他标签页通过 BroadcastChannel/storage 通知重读；不广播密钥、对话或执行状态。读取代次阻止旧请求盖回新目录，读取失败保留已选项，失效引用要求用户重新选择。其他用户刷新时读取当前服务端配置。

## 失效与变更

运行时展开具名配置并即时读取秘密；秘密不进入 Agent JSON、模型配置 JSON 或运行事件。模型配置修改影响全部引用的 Agent。更换 CLI 需新建配置并重新绑定。

配置停用、删除、工具不符、模型不存在或凭据清除/撤销均阻止执行，不选其他 Key 或模型。现有具名配置的单组引用继续生效；不恢复更早已弃用的 Agent 直接绑定、环境变量模型、旧 OpenCode SDK 路径或工作区默认 API。

## 工具适配与验证边界

当前对外能力以 capability 列表和实际适配器为准。平台授权与第三方服务是两种具体来源；不支持第三方的 CLI 不提供该选项。适配和本地模拟通过不代表已用真实厂商服务验证。

| 编码工具 | 当前可选来源 |
| --- | --- |
| Cursor、Trae | 工具平台授权 |
| Claude Code | 平台授权；Anthropic Messages，连接模式要求 CLI ≥ 2.1.257 |
| CodeBuddy | 平台授权；Chat Completions，按 CLI 2.162.0 本地协议探测收口 |
| OpenCode | Chat Completions、Responses、Anthropic Messages |
| Codex | ChatGPT 登录文件；Responses |
| DeepSeek Harness | 三种连接协议；0.2.1-rc.2 的官方 DeepSeek native 路由另行限制 |

连接模式由适配器生成工具配置，禁止凭品牌或相似 API Base 猜测协议。CodeBuddy 工作目录配置读取限制在会话 FSRoot 内，避免项目配置覆盖显式选择或越界读取。受管 MCP 路径使用当前 `CONFIG_ROOT`，支持 worktree 临时配置目录；OpenCode 替换模型连接的原生配置时只继承当前目录的 MCP 与 instructions，不继承其他 provider、plugin 或账号配置。运行时逻辑通过 server 的 runtime bundle 发布。

调研来源保留为适配依据，具体实现与验证范围由当前测试和 capability 声明决定：

- Cursor：[CLI 鉴权](https://cursor.com/docs/cli/reference/authentication)、[CLI 参数](https://cursor.com/docs/cli/reference/parameters)。
- Claude Code：[网关连接](https://code.claude.com/docs/en/llm-gateway-connect)、[模型配置](https://code.claude.com/docs/en/model-config)。
- CodeBuddy：[模型配置](https://www.codebuddy.cn/docs/cli/models)、[ACP](https://www.codebuddy.cn/docs/cli/acp)。
- Trae：[CLI 快速开始](https://docs.trae.cn/cli_get-started-with-trae-cli-2)、[企业模型设置](https://docs.trae.cn/enterprise_model-settings-for-trae-enterprise)。
- OpenCode：[自定义 provider](https://opencode.ai/docs/providers/#custom-provider)。
- DSH：[固定版本协议实现](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/llm/llm-pi-ai/src/provider.ts)、[DeepSeek native](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/llm/llm-deepseek/README.md#endpoint-and-wire-format)。
