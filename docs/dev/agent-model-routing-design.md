# 编码工具、凭据与模型连接分离设计草案

状态：已批准的设计依据；首期实现见下述边界。日期：2026-10-24。

本文保留设计阶段的对象定义、调研和后续目标。首期现已实现独立 Credential 用途、ModelConnection、Agent.modelBinding、按工具的完整默认绑定、凭据与连接向导，以及所列工具子集的协议适配。公开 Agent 工具字段仍为 `acpBackend`。旧 DSH 凭据通过新增确定的官方服务连接和只读选择映射兼容，原秘密不改写；从未配置的内置空槽位不迁移。旧 OpenCode Agent 保留原 `openCodeCredentialId` 与原生供应商目录/SDK 路径，不凭品牌猜测协议或 API Base，也不自动使用先前合成的通用连接；原已选凭据失效时明确失败，不回退其他 Key。

最终交互按用户确认的公式补充：先新建凭据，第二页「模型」保存“编码工具 + 凭据 + 所选模型”的命名组合（`ModelConfiguration`）。第一页「凭据」选择预设或自定义服务商，填写 API Base 和 Key，内部仍保存独立凭据和连接。Agent 绑定 `{kind: "configuration", configurationId}`，执行时再展开完整具体绑定并读取密钥。旧 platform/connection/inherit 绑定继续供已有 Agent 编辑和工作区默认设置使用；Cursor/Trae 保留平台来源限制。

当前页面采用简洁列表：Logo、名称与单行模型 ID，服务地址、协议和模型目录保留在编辑或高级入口。「凭据」「模型」两个页签共用右上角的新增菜单，提供“新增凭据”和“添加模型”；添加模型会切到「模型」页签并打开向导，该页不再重复展示另一个新增模型按钮。

模型向导固定为“选编码工具 → 选凭据 → 选模型”；工具用带 Logo 的卡片，第二步只显示一个可搜索凭据选择器，内部按实际记录解析平台授权或兼容连接，不再让用户先选来源种类。OpenCode CLI 和 DeepSeek Harness 凭据入口保存真实厂商的模型 Key 与连接，再继续对应工具的模型配置。模型向导内补建凭据后回到原草稿，取消也保留名称、工具和已有选择；只在实际切换工具或凭据时清除相关模型选择。

新建 Agent 使用“基础信息 → 工具与模型 → Git → 确认创建”四步。工具和模型在同页选择，桌面左右并列，窄屏上下排列。模型区只保留一个可搜索的已保存模型配置选择器，按当前工具筛选启用的配置；不提供继承默认、高级直接配置、绑定摘要或凭据管理链接。初始选择为空，切换工具清空原选择，返回同一步则保留已选配置。必须显式选择引用有效的配置才能进入下一步或创建 Agent；工具与模型这一步不能跳过，Git 仍可跳过。凭据被清除或撤销、服务停用、模型配置删除或失效时，保留引用以便识别，并阻止前进，不自动选择其他配置或工作区默认。

上述创建流程只收紧新建 Agent 的界面及提交入口，不修改旧 Agent、团队模板或运行时的默认绑定兼容语义。已有 Agent 编辑保留命名配置、平台/连接直绑与继承选择；工作区默认继续按工具维护完整具体绑定。下文保留原始对象设计与调研记录，其中继承和直接来源交互应按这一兼容边界阅读。

`ModelConfiguration` 持久化 `id/name/backend/binding/state/revision/createdAt/updatedAt`；内层 binding 只能是 platform 或 connection，禁止继承和配置循环，backend 创建后不可变。停用、删除、工具不匹配或失效引用均拒绝执行，不回退旧授权。手填原始模型 ID 时显式补入关联服务目录，保留同服务最新地址、Key 引用及其他模型。

首期边界：模型目录手工填写；不提供自动发现、在线连接检查、持久化实测记录、依赖引用计数或永久删除阻止。删除连接后引用它的 Agent 会明确失败，界面提前提示影响。页面保持“未验证”，适配和模拟测试不等于供应商实测。Claude Code 连接模式要求 CLI ≥ 2.1.257；DSH 固定适配 0.2.1-rc.2。CodeBuddy 连接目前仅开放 Chat Completions：固定官方 CLI 2.162.0 的本地模拟服务实测表明，当前配置方式仍向 Chat Completions 发请求，不能按文档声称支持 Responses 或 Messages；平台授权保持可用。不修改沙箱镜像或 CLI 安装合约，运行时逻辑通过 bundle 发布。以下“后续”或“本轮不迁移”等表述记录原设计阶段，不代表首期的现状。

## Agent 的多个 Git 授权

Agent 分别引用一条模型配置与一组 Git 授权。Git 授权绑定不是仓库列表，不改变工作流传入的待克隆仓库。

```json
{
  "modelBinding": { "kind": "configuration", "configurationId": "model-main" },
  "gitBindings": {
    "items": [
      { "credentialId": "git-team" },
      { "credentialId": "git-project", "scope": "https://github.com/example/project.git" },
      { "credentialId": "ssh-team", "scope": "git.example.com", "sshHostsCredentialId": "known-hosts" }
    ],
    "preferredCredentialId": "git-team"
  }
}
```

- 数据只保存引用、适用范围与首选 ID。非空列表必须有属于所选集合的首选；空 `items` 明确不注入这组默认 Git 授权。缺少 `gitBindings` 时保留旧单槽及共享配置兼容行为，Agent 自己的显式旧授权优先于共享默认的新绑定。
- 页面搜索添加已保存的 GitHub、GitLab 或 SSH 私钥授权，选中行展示 Logo、别名、首选和移除。适用站点或仓库、SSH 已知主机在可选详情中配置，不要求用户再选择一次连接方式或填写密钥。
- 选中的授权全部通过受限文件注入沙箱。HTTPS 依据协议、主机和仓库路径匹配，SSH 依据站点或仓库选择单一身份；具体仓库范围优先于主机范围，同一范围优先首选，否则固定使用选择顺序中的第一条。不会在认证失败时遍历其他账号。空范围使用平台默认站点，SSH 空范围表示默认 SSH 身份；非首选可通过适用范围服务其他站点或仓库。
- 所选授权或显式已知主机失效时阻止运行，保留失效引用，不改用其他 Key。新的显式列表覆盖旧的全局 Git 凭据和环境绑定。已有独立配置的逐仓库授权仍优先于默认 Git 授权；新建 Agent 明确保存 `repoIds: []`，不会把未选择的共享仓库授权一起继承。
- 工作流节点使用它所引用 Agent 的模型与 Git 绑定。独立沙箱按节点注入；共享运行沙箱在切换节点时比较包含所有授权、范围、顺序与首选的指纹，发生变化先清理上一节点的 Git/SSH 文件与 CLI 身份，再注入新列表。显式空列表同样触发清理；不把前一 Agent 的授权并入后一 Agent。
- 交互会话已运行沙箱的授权发生变更时拒绝复用，要求关闭后重新打开，避免把尚未同步的旧权限当成新配置；工作流共享沙箱由节点切换的重新初始化流程自动应用。

Git helper 使用路径上下文，并遵循 Git 的 `get`、`store`、`erase` 协议；`store` 与 `erase` 不输出秘密。[Git 凭据文档](https://git-scm.com/docs/gitcredentials)、[credential helper 输入输出协议](https://git-scm.com/docs/git-credential)。

## 1. 目标与对象边界

用户应能分别回答三个问题：用哪个编码工具、通过什么服务访问模型、使用哪个模型。例如，同一个 DeepSeek API Key 可以供多个模型连接引用；Claude Code、CodeBuddy 和 DSH 能否使用其中某个连接，由各自 CLI 的协议能力和 Grasp 适配状态决定。创建 Key 时不要求先选模型。

**按真实能力支持是硬边界。** 不支持通用 BYOK 的工具可以继续只支持平台来源，尚无证据或适配的组合在 UI 中禁用并说明。首期只选择能力已确认的工具子集，不承诺所有工具覆盖，也不为了统一表单新增协议转换代理或网关。

| 对象 | 保存什么 | 不保存什么 |
| --- | --- | --- |
| `Credential` | 一个秘密及其用途、发行方、别名和生命周期 | 模型、模型目录、Base URL、CLI 环境变量名 |
| `ModelConnection` | 模型服务、协议、地址、凭据引用、可用模型目录 | 编码工具选择、某个 Agent 当前选用的模型、秘密明文 |
| `Agent.backend` | 执行工作的编码工具 | 模型服务商或模型 ID |
| `ModelConfiguration` | 编码工具与具体凭据来源、模型和角色映射的命名组合 | 秘密、重复的服务地址、可变工具或配置循环 |
| `Agent.modelBinding` | 命名模型配置引用；旧数据可保留直接来源与角色映射 | 另一份 Key、重复的运行时环境变量配置 |
| `ToolCapability` | 特定 CLI 版本和接入方式的能力证据、Grasp 适配状态和验证记录 | 用户秘密或“所有版本都支持”的笼统承诺 |

平台授权和模型 Key 可以长得一样，甚至最终写入同名 CLI 环境变量，但它们在产品中的用途不同。CodeBuddy 的平台 Key 与第三方模型 Key 就不能因为都写入 `CODEBUDDY_API_KEY` 而被视为同一种服务。

## 2. Credential：只管理秘密

建议的逻辑结构如下；命名在实现时统一，不代表本轮已经新增 API。

```ts
type CredentialPurpose = 'tool_auth' | 'model_api' | 'git' | 'ssh'

type Credential = {
  id: string
  name: string
  purpose: CredentialPurpose
  issuer?: string
  toolId?: string
  secretKind: 'api_key' | 'token' | 'login_file' | 'ssh_private_key' | 'ssh_known_hosts'
  encryptedValue: EncryptedSecret // 仅服务端持久化层可见
  state: 'active' | 'cleared' | 'revoked'
  secretRevision: number
  createdAt: string
  updatedAt: string
}
```

- `tool_auth` 表示工具账号或平台授权，例如 Cursor Key、Trae PAT、Codex 登录文件；这类记录需要明确 `toolId`。
- `model_api` 表示模型服务凭据，例如 DeepSeek、Anthropic 或一个兼容网关的 API Key。`issuer` 是说明发行方的元数据，不是工具绑定；同一条记录可以被多个连接引用。
- `git`、`ssh` 保持仓库用途，不因工具或模型改选而变化。`ssh_known_hosts` 是受管理的信任材料，用独立 `secretKind` 区分私钥。
- 对外查询只返回 `hasSecret`、`masked`、状态和元数据，不返回 `encryptedValue`。替换秘密继续使用显式保存动作。
- 不再把模型放入 `Credential.metadata.model`，也不把模型供应商 Key 固定定义成 `provider=opencode`。环境变量名属于工具适配器，不是凭据的身份。
- 用途不允许静默转换。已有平台授权若要改作第三方模型 Key，需要用户明确选择其新用途或新建记录。

## 3. ModelConnection：独立、可复用的模型服务连接

```ts
type ModelProtocol =
  | 'openai-chat-completions'
  | 'openai-responses'
  | 'anthropic-messages'

type ModelConnection = {
  id: string
  name: string
  providerId: string
  protocol: ModelProtocol
  baseUrl: string
  credentialId: string
  auth: { scheme: 'bearer' | 'x-api-key' | 'custom-header'; headerName?: string }
  service?: { region?: string; product?: string }
  models: Array<{
    id: string
    name?: string
    source: 'discovered' | 'manual'
    capabilities?: { toolCalls?: boolean; vision?: boolean; reasoning?: boolean }
    input?: Array<'text' | 'image'>
    contextWindow?: number
    maxTokens?: number
    reasoningEfforts?: string[]
  }>
  state: 'active' | 'disabled'
  revision: number
  discovery?: { checkedAt: string; status: 'ok' | 'unavailable' | 'failed' }
}
```

`providerId` 表示模型服务商或自定义服务类型，例如 `deepseek`、`anthropic`、`custom`；它不表示编码工具。同一发行方、同一 API Key 可以建立多个连接，分别指定协议、区域、产品套餐或网关地址。每条连接引用一个凭据；多个连接引用同一个凭据不产生多份秘密。

`service.region` 与 `service.product` 保存非敏感的区域和产品区别，例如按量 API 与 Coding Plan。不能仅凭供应商品牌推断它们使用相同地址、协议或鉴权方式。标准地址可以由明确的产品预设填入，用户保存前可检查。

`baseUrl` 保存连接的基础地址。适配器按所选协议生成请求端点，保留反向代理的路径前缀；不得用字符串猜测切换协议。工具要求完整请求地址时，由适配器生成，例如 CodeBuddy 的 Chat Completions 自定义配置。非标准路径若后续需要支持，应新增明确的端点覆盖字段，不能同时把基础地址和完整请求地址混入一个含义不明的字段。

模型目录可自动发现，也可手填。发现失败不等于凭据错误；不同服务并不保证存在相同的模型列表 API。创建连接允许暂不填写模型，但没有模型的连接不能被保存为可执行的 Agent 绑定。模型条目的输入类型、上下文窗口、输出上限与推理档位按实际适配需要补充，不统一要求全部填写。DSH 自定义 provider 需要协议、地址和非空模型列表，单条 `models: [{id}]` 本身允许；未声明的能力依其目录或默认值处理，不据此宣称图像、工具调用等能力已经验证。

`models[].id` 必须保存服务商的原始模型 ID，允许含 `/` 等字符，不通过分隔符解析供应商。OpenCode 的 `provider/model` 和 DSH 的 `[routeId, upstreamModelId]` 是各自适配器的输出，不能成为平台通用的模型 ID。

连接的验证状态分成两个层次：服务地址与鉴权检查、特定工具和版本的执行验证。前者成功不代表后者成功；模型能返回文本，也不代表具备工具调用、图像或角色模型所需的能力。验证记录绑定连接修订号、凭据的 `secretRevision`、模型与实际工具版本；地址、协议、Key 或适配器更新后，旧结果保留作历史，当前状态重新显示“未验证”。

## 4. Agent.modelBinding：来源、模型和角色配置一起绑定

```ts
type ModelRoles = Partial<Record<
  'main' | 'fast' | 'subagent' | 'opus' | 'sonnet' | 'haiku',
  string
>>
type ToolSettings = { modelRoles?: ModelRoles; routeProfile?: string }

type ConcreteModelBinding =
  | {
      kind: 'platform'
      credentialId: string
      modelId?: string
      toolSettings?: ToolSettings
    }
  | {
      kind: 'connection'
      connectionId: string
      modelId: string
      toolSettings?: ToolSettings
    }

type ModelBinding = { kind: 'inherit' } | ConcreteModelBinding

type Agent = { backend: string; modelBinding: ModelBinding }
type WorkspaceModelDefaults = { bindingsByBackend: Record<string, ConcreteModelBinding> }
```

`platform` 使用当前工具自己的授权，以及该平台提供的模型目录。这里的 `modelId` 可省略，表示让该工具平台选择默认模型。Trae 企业控制台已经接入的第三方模型仍经 Trae 平台授权访问，因此使用 `platform`，不伪装成 Grasp 直接持有的 BYOK 连接。

`connection` 引用独立的模型连接，并明确选择一个原始模型 ID。凭据、协议和地址从连接取得，不在绑定中再复制一份 `credentialId`、Base URL 或秘密。

`toolSettings.modelRoles` 是特定工具的模型角色映射。Claude 的默认别名、小模型和子任务模型，以及 CodeBuddy 的轻量、推理和子代理模型，需要由适配器明确处理；不能只改主模型后让辅助任务继续访问另一套服务。每个角色值引用本次所选来源下的模型 ID；角色是否必填、允许哪些角色、如何映射，由当前工具和版本的能力规则确定。通用连接不保存 Claude 专属字段。跨连接的角色路由不在第一阶段范围内。

`main` 如果由适配器使用，其值必须等于绑定的 `modelId`；不允许两个字段各自选择不同主模型。

`routeProfile` 表达工具专用路由，例如 DSH 的 DeepSeek native 路径；它不能改变公共 `providerId` 的含义，也不能仅因 Base URL 相同就把专用 Messages/Files 传输当成普通 Anthropic Messages。native profile 必须单独验证供应商、官方服务和额外文件能力，不能套在任意兼容连接上；未实现的 profile 不开放。

### 继承语义（既有 Agent 与工作区默认兼容）

继承只解析 `WorkspaceModelDefaults.bindingsByBackend[Agent.backend]`，一次继承整条绑定：来源、授权或连接、主模型和全部角色配置。它不会改变 Agent 已选择的编码工具，也不分别继承 Key 和模型。

例如，工作区只配置了 Claude Code 的默认连接，Trae Agent 选择“继承默认”时应显示“当前工具没有可继承的默认配置”，并要求显式配置。不能复制 Claude Code 的绑定后替换授权，也不能顺便将 Agent 的工具切成 Claude Code。

显式覆盖后，Agent 保存一条完整的 `platform` 或 `connection`。恢复继承是一个明确动作，不把多个局部空字段解释成复杂的自动回退链。

### 失效与切换语义

- 凭据清除或撤销、连接停用、引用删除、模型无效或当前工具不支持该协议时，绑定失败并显示具体原因；不换另一把 Key、不换连接、不自动换模型。
- 切换编码工具先展示影响：原绑定能否复用、哪些角色配置不再适用、是否需要工具平台授权。用户选择新绑定或明确恢复继承后再保存；不能静默改模型或替换秘密。
- 更换连接会重新检查模型与角色映射，不因字符串同名就假定语义相同。支持手填的工具可以保留原始 ID，但应展示“尚未在此连接确认”，并由用户明确保存。
- 保存 Agent 前校验来源与工具能力；运行前仍重新校验凭据有效性和引用状态。已保存不等于永远可执行。
- 后续运行快照记录解析后的绑定、连接修订号和实际 CLI 版本，秘密仅在运行时从凭据存储解析。快照不是第二套可编辑的 Agent 配置。
- 复用沙箱时按本次绑定重新生成认证与工具配置，并清理上一次服务留下的派生字段，避免切换连接后仍使用旧 Key、地址或角色模型。

## 5. ToolCapability：文档、适配与验证分别记录

能力不能只有一个 `supportsByok: true`。最少需要区分以下维度：

| 字段 | 含义 |
| --- | --- |
| `toolId`、`cliVersionRange`、`transport` | 工具、版本范围与 `acp` / `stream-json` / `exec-json` 接入方式 |
| `modes`、`protocols`、`roleNames` | 来源模式、协议、允许或必需的角色映射 |
| `routeProfiles` | 工具专用路由及额外约束，例如 DSH native 的文件能力 |
| `evidence[]` | 工具文档、供应商文档或官方源码；含 URL、检查日期和声明范围 |
| `graspAdapter` | `existing` / `planned` / `unsupported` |
| `verification` | `not-run` / `handshake-only` / `mock-tested` / `live-tested` / `failed`；附实际 CLI 版本、运行时修订、日期和局限 |

实际存储应按“工具 + CLI 版本 + transport + 模式/协议”的能力行展开，避免一个 `existing` 状态覆盖该工具所有接入方式。工具官方文档、模型供应商接入指南、官方源码可以提供不同级别的证据；“文档声明支持”与“Grasp 已适配”与“该版本已实测”独立展示。

“适用工具”依据所选连接协议、鉴权方式、产品、模型能力、CLI 版本和适配状态计算。只有文档而尚未适配的组合显示“待适配”，不出现在可执行选择中；文档和适配具备但尚无真实服务验证时显示“未验证”，不冒充已实测。

## 6. 目前可确认的工具能力与仓库边界

以下是 2026-10-24 的设计依据，不代表本轮实现了新的运行时能力。镜像采用构建时下载最新版本的工具，需要在部署后读取实际版本，不能把本表当作所有存量镜像的声明。

| 工具 | 平台授权与平台模型 | BYOK / 自定义服务证据 | Grasp 当前边界 |
| --- | --- | --- | --- |
| Cursor CLI | Cursor 账号或 `CURSOR_API_KEY`，选择 Cursor 提供的模型 | 官方论坛说明 CLI 尚不支持自带供应商 Key；IDE BYOK 不能作为 CLI 能力证据 | 当前平台授权与模型参数可用；不展示可执行的 CLI 直连 BYOK |
| Claude Code | 官方账号；Anthropic API Key 按模型服务凭据建模 | 官方网关文档给出 Base URL 与鉴权配置；DeepSeek、Z.ai、Kimi 有各自 Claude Code 接入指南 | 当前已有 Claude Key 映射；通用连接、鉴权 scheme 和角色模型路由尚需独立适配与验证 |
| CodeBuddy CLI | CodeBuddy 平台 Key、站点与平台模型 | CLI 官方文档明确第三方 Key + Base URL；`models.json` 可定义自定义模型和协议 | 当前工具 Key、站点、`--model` 已接入；模型连接与跨工具 Key 复用尚未形成产品能力 |
| Trae CLI | Trae PAT，平台及企业控制台配置的模型 | 企业文档明确适用客户端包含 CLI；未找到 CLI 本地直接填写任意供应商 Key/协议的公开契约 | 仓库有意安装 1.x；当前不能把 2.0 配置与 ACP 参数直接套入，也不能把 `TRAECLI_HOST` 当任意 LLM 网关 |
| Codex CLI | 当前仓库只接受 ChatGPT 登录文件 | 官方配置提供自定义 `model_provider`、`base_url`、`env_key`；当前 `wire_api` 使用 Responses | CLI 能力不等于 Grasp 能力；本仓库现有登录文件限制下，自定义连接待适配 |
| OpenCode | 本设计主要使用独立模型连接 | 当前已经存在供应商、Base URL、模型配置链路 | 当前 Key 的 `provider=opencode` 和 `metadata.model` 是历史耦合，需要拆分；不是供应商凭据的新标准 |
| DSH | 无独立 DSH 平台账号；当前使用 DeepSeek 模型服务 Key | 固定版本 `0.2.1-rc.2` 已支持三个通用协议及独立 DeepSeek native 路由 | 当前启动配置和模型值强制 `deepseek-official`；此 Key 应属 `model_api`，通用连接待适配 |

### CodeBuddy CLI 的两种自定义路径

官方 CLI 文档给出环境变量路径：`CODEBUDDY_API_KEY`、`CODEBUDDY_BASE_URL` 与主模型配置；其 DeepSeek 示例明确使用兼容 Anthropic 的服务。该路径与自定义模型配置不能混用为一个无协议的“Base URL”。[环境变量参考](https://www.codebuddy.cn/docs/cli/env-vars)

另一条路径是用户级或项目级 `models.json`：模型条目包含 `id`、`apiKey`、`url`；常规自定义接口采用 OpenAI Chat Completions 完整地址，`api: "openai-responses"` 则使用 Responses 的基础地址。密钥与地址支持环境变量引用。`availableModels` 控制列表可见性。[模型配置](https://www.codebuddy.cn/docs/cli/models)

平台 Key 的站点配置与第三方模型服务是不同场景；身份文档专门给出第三方服务示例。[身份和访问管理](https://www.codebuddy.cn/docs/cli/iam) Grasp 当前默认使用 `codebuddy -p --output-format stream-json --input-format stream-json`，同时保留 `codebuddy_acp` 的 `codebuddy --acp --model <id>` 适配。官方也提供原生 ACP 接入方式；公开文档并不能替代所用镜像版本及接入方式的 BYOK 执行验证。[ACP 文档](https://www.codebuddy.cn/docs/cli/acp)

当前仓库可以透传非敏感 `CODEBUDDY_BASE_URL`，并将工作区 CodeBuddy Key 映射为 CLI Key；配置工作目录整树复制，也能承载用户编写的配置文件。但凭据选择仍按工具对应的环境键匹配，通用模型供应商凭据不会自动转换成 CodeBuddy 连接。后续应由连接适配器生成配置，不要求用户把供应商 Key 伪装成“CodeBuddy 平台授权”。

### Trae CLI 的直接接入与企业平台接入

CLI 快速开始公开的是 PAT 登录；`TRAECLI_HOST` 的自定义地址用于组织的 API 域，不能据此推断它接受 OpenAI 或 Anthropic 原始模型接口。[CLI 快速开始](https://docs.trae.cn/cli_get-started-with-trae-cli-2)、[环境变量](https://docs.trae.cn/cli_environment-variables)

企业模型配置明确列出 TRAE CLI，支持管理员在企业控制台添加第三方 Key、自定义地址、模型 ID，并选择 Chat Completions、Responses 或 Anthropic Messages。这是有来源支持的企业平台能力；成员自行添加模型的客户端范围另外列举，不能混为 CLI 的本地 BYOK 配置。[企业模型设置](https://docs.trae.cn/enterprise_model-settings-for-trae-enterprise)

仓库 `install-agent.sh` 有意保留 1.x CDN 安装路径；官方已将旧入门页改为 1.0 到 2.0 的升级说明。[升级文档](https://docs.trae.cn/cli_get-started-with-trae-cli) 当前 Grasp 调用 `traecli acp serve --model <id>`，而 2.0 的 ACP 文档使用 `traecli -c 'model="<id>"' acp serve`，并要求全局参数位于子命令之前。升级必须作为后续独立适配，不能仅替换安装脚本。[ACP 参数](https://docs.trae.cn/cli_agent-client-protocol)、[CLI 参数](https://docs.trae.cn/cli_command-line-parameters)

因此，第一阶段 Trae 的来源选项为“工具平台授权”，可选择平台返回或用户明确提供的企业模型 ID；不开放尚无 CLI 本地配置契约与 Grasp 适配的通用直连连接。

### 其他工具的来源与适配注意点

- Cursor：依据 [CLI 鉴权](https://cursor.com/docs/cli/reference/authentication)、[CLI 参数](https://cursor.com/docs/cli/reference/parameters)、[ACP](https://cursor.com/docs/cli/acp) 和 [官方论坛的 CLI BYOK 说明](https://forum.cursor.com/t/can-i-use-provider-api-keys-with-cursor-cli-agent/149158/3)。平台内包含多个模型供应商，不等于接受它们的个人 API Key。
- Claude Code：依据 [网关连接](https://code.claude.com/docs/en/llm-gateway-connect)、[协议兼容](https://code.claude.com/docs/en/llm-gateway-protocol)、[模型配置](https://code.claude.com/docs/en/model-config)。`ANTHROPIC_BASE_URL` 配合 API Key 或 Auth Token 需要明确鉴权与角色映射。第三方模型的支持证据单独标为供应商文档，例如 [DeepSeek](https://api-docs.deepseek.com/quick_start/agent_integrations/claude_code/)、[DeepSeek Anthropic 接口](https://api-docs.deepseek.com/guides/anthropic_api/)、[Z.ai](https://docs.z.ai/devpack/tool/claude)、[Kimi](https://platform.kimi.ai/docs/guide/claude-code-kimi)。不能把“Anthropic 未承诺非 Claude 模型”解释成这些连接绝对不能使用，也不能把供应商指南当作 Grasp 已验证。
- Codex：依据 [高级配置](https://learn.chatgpt.com/docs/config-file/config-advanced) 与 [配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)。自定义 provider 名、协议和秘密环境键由适配器生成；仓库当前只认登录文件的策略需要后续单独调整。
- OpenCode：[自定义 provider](https://opencode.ai/docs/providers/#custom-provider) 中不同协议使用不同 SDK；不能把全部“OpenAI 兼容”连接当成同一种请求格式。
- DSH：固定 tag [provider 配置](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/llm/llm-pi-ai/README.md#configure-provider-routes) 和 [三协议实现](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/llm/llm-pi-ai/src/provider.ts) 支持 `openai-completions`、`openai-responses`、`anthropic-messages`；通用协议名在适配器边界映射。[ACP 编码](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/acp/acp/src/model-control.ts) 使用 `JSON.stringify([routeId, upstreamModelId])`。通用 pi-ai 路由身份由连接 ID 派生，不能用品牌名代替；native 插件保留工具注册的固定路由名（如 `deepseek-official`），平台仍用独立连接 ID 区分账号和地址，每次工具进程只注入所选连接，不承诺同一进程并存多个任意命名的 native 路由。[DeepSeek native](https://github.com/deepseek-ai/deepseek-harness/blob/dsh-v0.2.1-rc.2/packages/llm/llm-deepseek/README.md#endpoint-and-wire-format) 是独立传输，其配置不接受通用 `protocol` 字段；保留专用能力，不强行归一化。

## 7. 数据示例

以下只有非敏感的逻辑数据；Key 明文不出现在文档或连接记录中。示例地址用于说明对象关系，不代表预设的可用性承诺。

```json
{
  "credential": {
    "id": "cred-deepseek-team", "name": "团队 DeepSeek Key",
    "purpose": "model_api", "issuer": "deepseek",
    "secretKind": "api_key", "hasSecret": true, "state": "active"
  },
  "connections": [
    {
      "id": "conn-deepseek-chat", "name": "DeepSeek · Chat Completions",
      "providerId": "deepseek", "protocol": "openai-chat-completions",
      "baseUrl": "https://api.deepseek.com/v1",
      "credentialId": "cred-deepseek-team",
      "auth": { "scheme": "bearer" },
      "models": [{ "id": "deepseek-flash", "source": "manual" }],
      "state": "active", "revision": 1
    },
    {
      "id": "conn-deepseek-anthropic", "name": "DeepSeek · Anthropic",
      "providerId": "deepseek", "protocol": "anthropic-messages",
      "baseUrl": "https://api.deepseek.com/anthropic",
      "credentialId": "cred-deepseek-team",
      "auth": { "scheme": "x-api-key" },
      "models": [{ "id": "deepseek-flash", "source": "manual" }],
      "state": "active", "revision": 1
    }
  ],
  "agent": {
    "backend": "claude_code",
    "modelBinding": {
      "kind": "connection", "connectionId": "conn-deepseek-anthropic",
      "modelId": "deepseek-flash",
      "toolSettings": {
        "modelRoles": {
          "main": "deepseek-flash", "fast": "deepseek-flash", "subagent": "deepseek-flash",
          "opus": "deepseek-flash", "sonnet": "deepseek-flash", "haiku": "deepseek-flash"
        }
      }
    }
  }
}
```

上例说明一把 Key 被两个协议连接引用，以及连接与编码工具分离；它不是可直接导入当前 Grasp 的配置。平台授权示例如下：

```json
{
  "credential": {
    "id": "cred-trae-team", "name": "团队 Trae 授权",
    "purpose": "tool_auth", "issuer": "trae", "toolId": "trae",
    "secretKind": "token", "hasSecret": true, "state": "active"
  },
  "agent": {
    "backend": "trae",
    "modelBinding": {
      "kind": "platform", "credentialId": "cred-trae-team",
      "modelId": "enterprise-model-id"
    }
  }
}
```

企业模型由 Trae 平台转接到第三方时，Grasp 依然只持有 Trae 授权。不能从平台返回的展示名反向构造一条声称拥有第三方 Key 的 `ModelConnection`。

## 8. 页面交互（原始设计与兼容编辑）

本节保留最初的页面设计，包括尚未实现的发现、依赖检查等目标；新建 Agent 与凭据页的当前交互以上文实现说明为准。下述 Agent 直接来源和继承入口仍用于已有 Agent 编辑及工作区默认设置，不出现在新建 Agent 页面。

### 凭据列表

默认显示真实保存的凭据，按“工具授权”“模型 API Key”“代码仓库与 SSH”分组。列表列出名称、发行方或工具、用途、掩码、引用数量、更新时间和操作。没有保存秘密的预置槽位不计入总数，也不铺成十二条未配置记录；首次使用通过空状态引导添加。

新增凭据使用向导：

1. **选择用途**：工具授权、模型 API Key、Git 或 SSH。
2. **填写信息**：别名、发行方或工具、秘密；秘密格式随所选用途变化。模型 Key 此步不要求模型、Base URL 或编码工具。
3. **确认保存**：展示用途与掩码预览。选择模型 Key 时可勾选“保存后创建模型连接”，进入连接表单；取消这个可选动作不会丢弃已经保存的 Key。

编辑 Key 时先输入新值，再显式保存，不预填旧明文。清除、撤销或删除前展示受影响的连接和 Agent；它们会变为不可用，不自动替换引用。引用中的记录可停用或清除，但永久删除应要求先解除依赖。

### 模型连接列表

模型连接有独立页面或同层标签页，不藏在某个工具的凭据编辑器里。紧凑列表展示名称、供应商、协议、地址、引用的凭据、模型数量、适用工具与验证状态。

“新增连接”先选供应商或自定义服务，再选择产品/区域、协议、地址与已有模型 Key。没有 Key 时可在此打开新增凭据向导，保存后回到原表单；用户仍能看清这是两条独立记录。

模型区域提供“获取模型”和“手动添加”。列表请求失败时保留已填地址、Key 引用和手填模型，并展示实际原因；不能把获取失败泛化成“Key 无效”。保存连接不要求立刻绑定 Agent。

“适用工具”逐项显示“可选择”“待适配”“协议不兼容”“版本不支持”或“未验证”。详情可查看对应版本、文档来源和验证范围，而不是统一打一个已支持勾选。

### Agent 配置

先选择编码工具，再配置模型来源：

1. “继承此工具的工作区默认配置”，展示解析后的整条绑定摘要。
2. “工具平台授权”，选择当前工具的授权与平台模型；允许使用工具默认模型。
3. “模型连接”，只展示能力规则允许的连接，随后选择模型及该工具要求的角色映射。

Cursor 不出现可执行的通用 BYOK 选项；Trae 企业模型放在平台来源中。CodeBuddy 可在后续适配完成后同时提供平台来源和连接来源。DSH、Codex 等工具的文档能力在适配完成前显示为说明，不能让用户保存成声称可运行的配置。

切换工具时，先保留旧配置为未提交草稿并显示影响，再由用户完成来源与模型选择。取消切换恢复原配置。切换成功时只保存新 `backend` 与完整 `modelBinding`；不同时维护“旧环境变量 + 新绑定字段”两套真值。

## 9. 现有数据与后续实施阶段

本轮不改数据、不执行迁移。后续兼容设计可以保留旧数据读取器，但必须在解析边界一次转换到新对象；不能让页面持续双写旧字段和新字段。

1. **设计确认**：确定用途、协议、产品/区域、整条继承、能力状态及列表/向导原型；确认每个工具在当前部署版本的状态。
2. **持久化与只读映射**：新增对象与引用关系，制定可逆的数据映射和冲突报告。旧 `opencode` 凭据拆出模型 Key 与连接，旧 `metadata.model` 作为模型候选和绑定迁移依据，不继续留在秘密对象中。
3. **选定工具子集适配**：优先接已有可运行能力，仅为选定且有证据的组合实现连接解析、鉴权 scheme 和角色映射；按固定版本、transport 和协议记录验证。不支持的工具保留平台来源或禁用相应组合。Trae 升级可另立任务，不是首期前置要求。
4. **切换写入源**：页面/API 只写新模型，运行时只使用新对象解析；完成旧字段盘点后再移除兼容读取。遇到不明确的旧配置，展示待确认项，不猜测平台 Key、供应商 Key 或模型所属连接。

旧空槽位不会自动变成“已保存凭据”。已清除的选择也不能通过迁移重新继承其他 Key。旧模型字符串含 `/` 时必须结合旧适配器规则解释一次，不能用同一套拆分规则污染所有新模型 ID。

## 10. 验收标准

- 模型 Key 可独立创建且不要求选择模型；一条 Key 可被多个模型连接引用。
- 同一个连接可被多个适配完成、协议兼容的工具引用；不会因为连接名称或 `providerId` 相同而误用另一账号。
- 平台模型与直接 BYOK 来源明确区分。Trae 企业模型不会错误显示为 Grasp 持有的直连 Key；Cursor CLI 不提供虚假的 BYOK 功能。
- Agent 的工具与绑定分别保存；继承只读取当前工具的整条默认绑定，缺少默认时明确失败，不切工具或回退秘密。
- 清除、撤销、停用、删除或无效引用均失败关闭；不替换为其他 Key、连接或模型。
- 模型 ID 保持上游原值，包括包含 `/` 的 ID；OpenCode 组合值和 DSH ACP 二元组只在适配器边界生成。
- DSH 通用 pi-ai 路由 ID 从连接身份派生；native 插件保留固定原生路由名。平台可以保存同供应商不同账号、地址或协议的连接，运行时仅注入本次选择；固定版本证据与当前 Grasp 的官方服务强制逻辑分别记录。
- Claude/CodeBuddy 等工具所需的主、小模型、推理与子代理映射完整可检查，不发生辅助任务无意访问旧服务的情况。
- 切换工具、连接或模型前能看到影响；取消保留旧配置，保存后只有一套生效配置，不双写秘密环境变量。
- 模型目录不可发现时可手填；列表发现、连接检查、工具握手和真实执行结果有独立状态。
- 能力列表展示准确的 CLI 版本、transport、证据来源、Grasp 适配状态与验证范围；文档支持不会自动显示成已实测。
- 不支持的工具或协议允许保持不支持；没有引入通用网关或协议代理的首期要求，也不为全工具覆盖阻塞已确认支持的子集。
- 凭据和模型连接列表以真实保存记录为主，移动端可查看名称、状态和操作；新增向导中可以返回、取消并保留非敏感草稿，秘密按明确的生命周期清理。
- 后续迁移有映射报告、冲突处理和回滚方案；本轮不执行迁移，也不改变现有运行时行为。
