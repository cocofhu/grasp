# Agent、模型配置与凭据

本文记录 PR #940 的最终数据与交互契约。以当前结构为唯一执行来源，删除旧数据模型读取、默认继承与自动迁移。

## 数据关系

```text
Agent
 ├─ modelBinding → ModelConfiguration
 │                    ├─ backend：固定编码工具 / CLI
 │                    └─ binding
 │                         ├─ platform → 该工具的 Credential + 模型 ID
 │                         └─ connection → ModelConnection + 模型 ID + 工具角色配置
 │                                           └─ Credential
 └─ gitBindings → 多个 Git / SSH Credential + 首选 + 适用范围
```

- Credential 只保存加密秘密及用途、发行方、工具、秘密种类；模型服务地址、协议和模型目录属于 ModelConnection。
- ModelConnection 保存协议、API Base、凭据引用与原始模型 ID。支持 Chat Completions、Responses、Anthropic Messages，具体可用性由 CLI 能力约束。
- ModelConfiguration 保存名称、backend、具体 binding、启停状态和 revision。内部只允许 platform 或 connection；工具创建后不可修改。
- Agent.modelBinding 只允许 `{kind:"configuration", configurationId}`。不在 Agent 上重复保存 Key、连接、模型角色或默认继承设置。Agent.acpBackend 必须与所选配置的 backend 一致。
- Agent 草稿允许 modelBinding 为空，表示未配置；没有可执行的默认模型。运行入口在创建或复用沙箱前验证配置及其依赖。
- SharedAgent 只保留通用文件、MCP、环境和布局。它不指定 CLI 或模型，不给 Agent 补模型或 Git 授权。
- 项目独立保存的逐仓库访问配置仍是项目功能；Agent 不再通过 repoIds 自动继承仓库授权。

## 凭据与模型页面

凭据和模型使用紧凑列表，显示 Logo、名称和短说明，详情留在编辑器。两个标签共享新增菜单；所有下拉复用可搜索组件。

先创建凭据，可使用服务商预设、带 Logo 的厂商选择或自定义 API Base 与 Key。再创建模型配置：选择编码工具 → 选择可用凭据 → 选择模型。工具、凭据都显示 Logo。新建凭据后返回当前模型草稿。

Agent 向导使用“基础信息 → 工具与模型 → Git → 确认创建”。工具与模型同页，按工具筛选启用模型配置；更换工具后必须重新选择。编辑、初始化向导和团队创建使用同一模型引用，不提供直接来源或默认继承入口。

初始化向导可打开凭据/模型管理创建组合，再将选中的配置绑定到各角色。角色可显式选择另一条模型配置。默认工作流的必需角色缺失或未配置时返回可操作错误，不从共享默认补建。

## 多个 Git 授权

`gitBindings.items` 保存 credentialId、可选 scope、SSH known-hosts 凭据引用；preferredCredentialId 必须属于非空集合。未开启 Git 继承时，缺省和空列表均表示没有 Git 授权。

GitHub / GitLab 凭据保存 `host`，默认分别为 `https://github.com` / `https://gitlab.com`，也可填写自建站点及端口。Host 与 Token 在同一凭据中管理，不使用独立的 GitLab URL 凭据。Agent scope 只能细化同一 Host 内的仓库，不能把凭据用于另一站点或改变协议。

Agent 默认配置可以保存当前的 `modelBinding.configurationId` 和 `gitBindings`。每个 Agent 通过 `inheritModel`、`inheritGit` 分别明确选择是否继承；缺省为关闭，关闭时使用本地选择，开启时动态使用默认项且不复制密钥。默认模型的 CLI 由模型配置确定。缺失或失效的默认引用报错，不回退到本地或另一账号；默认项变化也参与沙箱指纹校验及共享节点切换。

所选授权全部放入沙箱受限文件，禁止用全局 Token 环境代替用户选择。按协议、主机和仓库路径选择一个身份，仓库范围优先于主机范围；同范围优先首选，否则按选择顺序。认证失败不尝试其他账号。所选凭据失效时明确报错。

工作流独立沙箱在创建时注入；共享沙箱在节点切换时替换整组授权，包括切为空。交互沙箱在复用和每轮对话前检查模型与 Git 指纹，变更后要求关闭重开，避免使用过期权限。

## 失效与变更

运行时展开具名配置并即时读取秘密；秘密不进入 Agent JSON、模型配置 JSON 或运行事件。模型配置修改影响全部引用的 Agent。更换 CLI 需新建配置并重新绑定。

配置停用、删除、工具不符、模型不存在或凭据清除/撤销均阻止执行，不选其他 Key 或模型。不维护旧 Agent 直接绑定、环境变量模型、旧 OpenCode SDK 路径或工作区默认 API，不执行旧数据转换。已有数据需要在当前页面重新配置；本 PR 不承诺原配置无感运行。

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

连接模式由适配器生成工具配置，禁止凭品牌或相似 API Base 猜测协议。CodeBuddy 工作目录配置读取限制在会话 FSRoot 内，避免项目配置覆盖显式选择或越界读取。运行时逻辑通过 server 的 runtime bundle 发布。

调研来源保留为适配依据，具体实现与验证范围由当前测试和 capability 声明决定：

- Cursor：[CLI 鉴权](https://cursor.com/docs/cli/reference/authentication)、[CLI 参数](https://cursor.com/docs/cli/reference/parameters)。
- Claude Code：[网关连接](https://code.claude.com/docs/en/llm-gateway-connect)、[模型配置](https://code.claude.com/docs/en/model-config)。
- CodeBuddy：[模型配置](https://www.codebuddy.cn/docs/cli/models)、[ACP](https://www.codebuddy.cn/docs/cli/acp)。
- Trae：[CLI 快速开始](https://docs.trae.cn/cli_get-started-with-trae-cli-2)、[企业模型设置](https://docs.trae.cn/enterprise_model-settings-for-trae-enterprise)。
- OpenCode：[自定义 provider](https://opencode.ai/docs/providers/#custom-provider)。
- DSH：[固定版本协议实现](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/llm/llm-pi-ai/src/provider.ts)、[DeepSeek native](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/llm/llm-deepseek/README.md#endpoint-and-wire-format)。
