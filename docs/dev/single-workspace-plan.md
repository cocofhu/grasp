> 实施说明：代码与一次性离线工具已按本方案落地，操作步骤见 [迁移说明](../../server/WORKSPACE_MIGRATION.md)。最终复核收紧了存活容器规则：合并/重归属时必须先保全并退休 Run 沙箱；重连不刷新已有 MCP、环境和凭据，不能作为配置迁移机制。持久 Run 历史与正常冷恢复保留。

# 单工作区改造评估与实施方案

日期：2026-10-20。代码基线：`d536ee6dac4acd15a67957e886134322f683dfb9`。

用户目标：取消项目维度，系统只有一个工作区；侧栏直接列出全部工作流，原项目页的能力在设置中完整列出。采用升级现有系统的方式，保留全部历史数据；除迁移工具外，不长期保留旧项目兼容包袱。

推荐方案是把旧项目差异在迁移时转换成正常的工作区、工作流和 Agent 配置。迁移完成后只有一套模型、接口和执行路径，不保留按旧项目分流的运行时、历史权限映射或两套导航。历史记录保留，不等于长期保留旧项目的运行规则。

本方案以代码为依据。实际项目数量、配置冲突、孤儿数据与活动任务须由部署前的只读预检确定。下文新增接口、字段与工具均为拟实施设计。

## 1 目标与边界

| 范围 | 目标 |
|---|---|
| 产品层级 | 取消项目列表、新建、切换、筛选和删除。工作流、运行和所有工具属于同一工作区 |
| 历史数据 | 保留工作流版本、运行、变量、审批、会话、附件、产物版本、记忆、定时任务、审计和 Token 账本 |
| 配置 | 共享默认成为工作区默认；差异转成正常工作流覆盖或 Agent 显式配置 |
| 权限 | 保留用户、Agent、会话所有者、管理员、只读渠道和公开分享边界。旧项目级外部授权需重新处理 |
| 代码仓库 | 仍可对多个代码仓库运行任务，保留 `repos` 输入与每次运行的仓库参数 |
| 内部存储 | 可复用现有单例配置记录和原 ID；不为了文字统一而迁移无关 ID。业务接口不再要求调用者选择项目 |
| 兼容边界 | 迁移报告、源快照、journal 与回滚备份离线保留；正常服务不读取它们来决定业务行为 |

不同时重写 FSM、沙箱协议或引入新的团队层级。普通业务代码不散落 `if legacyProject`、旧 ID 回退或来源授权分支。无法等价转换的配置必须在迁移前解决或明确接受变更，不能上线后靠隐藏兼容层兜底。

## 2 页面与导航

### 侧栏工作流目录

将截图中的 `Projects / 默认项目` 替换为“工作流”及完整目录，保留新需求、待处理事项、最近运行和设置入口。

- 列出所有草稿和已发布工作流。收藏用于置顶；`showOnHome` 仍只控制新需求首页推荐和搜索，不过滤完整目录。
- 点击名称进入画布，运行使用独立按钮。**保留现有运行版本语义**：编辑器、工作流管理和侧栏手动运行最新保存 head，可测试未发布流程；首页与 `/v1` 执行 publishedSnapshot。表单字段必须来自实际执行的同一版本，不能混用。
- 新增轻量目录投影，只返回 ID、名称、状态、版本和必要展示字段，打开编辑或运行时再加载详情。现有 `/workflows` 虽有“不含重图”的服务注释，实际 DTO 仍携带 nodes、edges、variables 和 publishedSnapshot，不能据注释假定传输很轻。
- 不沿用当前项目列表前六项截断，也不拿最多八项的收藏列表替代目录。侧栏不设搜索框，采用参考图中的紧凑行和浅色选中态，操作悬停显示；搜索仅放在工作流管理页。目录支持独立滚动、新建、空态和失败重试；实际数量需要时分页加载，但全部条目必须可达。
- 创建、导入、复制、改名、删除和发布后同步侧栏、管理页和首页。复用请求取消与过期响应隔离，避免慢响应恢复已删除条目。
- 收藏保留个人顺序，其他条目稳定排序；同名发布快照用短 ID 辅助辨认，不重新引入项目分组。
- 保留移动端抽屉、桌面侧栏隐藏、键盘焦点、触摸操作和长名称处理。

### 原有标签的完整去向

设置导航列出全部原能力，分为工作区工具、执行配置和平台设置。看板、需求草稿、PM 对话也在首页提供快捷入口。入口位置与功能 URL 分开，不必把日常工具的地址都命名为 settings。

设置参考 Codex 保留最外层窄图标轨，旁边为 248px 设置目录，顺序为工作区工具 → 执行配置 → 平台配置，不设搜索。表单在纯色画布上居中、限制宽度，以平铺分组和细线分隔对齐说明与控件，不叠加卡片或面板外框；常规设置和其他表单采用自然内容高度与页面单一滚动，避免内容被固定高度裁切。进入设置后切换共享工具仍保留目录，明确返回工作区后恢复主导航。数据表格和编辑器保留宽布局，避免统一缩成窄表单。智能体列表完整显示标题，次要操作收进「更多」菜单。

| 原标签 | 设置中的入口 | 拟用路由 | 需要保留的能力 |
|---|---|---|---|
| 看板 | 工作区工具 → 看板 | `/board` | 当前运行、待处理事项、最近完成、标签、预览与分页 |
| 统计 | 工作区工具 → 用量统计 | `/stats` | Token、模型、费用、趋势、下钻与导出，统一用量分析入口 |
| 工作流 | 主侧栏及设置管理链接 | `/workflows` | 管理表格、基线创建、导入、复制、发布、删除、首页可见性 |
| 需求草稿 | 工作区工具 → 需求草稿 | `/requirements` | 归档、里程碑、日期、进度、父子关系、未保存离开确认 |
| 通知 | 执行配置 → 通知策略 | `/settings/notification-policy` | 模板、事件、目标渠道、回执，与 `/notifications` 消息中心区分 |
| 项目管理 | 工作区工具 → 工作区助手 | `/assistant` | PM 对话、线程、记忆、附件、恢复、渠道会话只读 |
| 项目管理中的设置 | 执行配置 → 助手设置 | `/settings/assistant` | 默认 PM、MCP、自动审批、渠道 |
| 外部 MCP | 执行配置 → 外部 MCP | `/settings/external-mcp` | 接入地址、能力包、开关、密钥，与内置 MCP 说明目录区分 |
| 定时任务 | 工作区工具 → 定时任务 | `/settings/schedules` | 所有 Agent 的任务、计划、执行记录与投递 |
| 智能体 | 执行配置 → 智能体 | 现有 `/agents` | 单 Agent、团队创建、测试、文件、记忆、任务、导入导出 |
| 共享 Agent 配置 | 执行配置 → Agent 默认配置 | `/settings/shared-agent` | 共享文件、MCP、环境、模型与凭据选择、布局、测试 |
| 项目凭据 | 执行配置 → 凭据 | `/settings/credentials` | AI、Codex 登录文件、Git、SSH、自定义与专用服务凭据适配入口 |
| 工作流变量 | 执行配置 → 共享变量 | `/settings/variables` | 类型、默认值、Secret 打码和原有元数据 |
| 审计 | 工作区工具 → 审计 | `/settings/audit` | 列表、筛选、facets、统计和导出 |
| 项目信息 | 平台设置 → 工作区信息 | `/settings/workspace` | 名称、描述、未知模型显示别名，移除项目创建和删除 |

产物采用文件目录与宽预览两栏，来源按需筛选，不再并列放一整列工作区目录。按运行分组保留文件检索、打包、版本、结构化/原始内容及导出；文件使用紧凑目录行，结构化报告采用正文和细线分组。窄内容区切换文件列表与预览，依据实际剩余宽度适配，设置目录不能挤压编辑内容。智能体窄内容区将目录移至上方，保留编辑、测试、文件历史及全部配置页签。设置内各管理列表、统计分区和配置表单去除重复大卡壳，保留输入控件、状态和弹层的必要边界。

现有通知消息、统计、产物、沙箱、常规设置和集成说明继续存在。迁入设置不自动改变权限，保留业务接口的 403、管理员和只读判断。

把 ProjectDetailView 中的工作流列表、变量与元信息拆出；看板、需求草稿、助手和设置面板使用独立页面包装，继续复用现有组件，不把整个项目详情页嵌进设置。Agent Studio 的工作区范围与 embedded 展示模式解耦，避免固定传入 ID 后意外隐藏团队创建。

### 对话与产物的统一工作区

聊天以提问分段，正文与输入保持共同阅读宽度；助手回答直接排版，工具和思考以轻量记录展示。工作区助手、运行中的澄清与复审、审批页和智能体测试使用相同的左侧定位刻度：悬停或聚焦预览问题与回答摘要，点击定位并短暂高亮；定位期间暂停自动追随生成内容，滚回底部后恢复。刻度仅覆盖已加载历史，提供明确的更早消息入口；用户问题身份在历史追加、流式落库与重连后保持稳定，不把数组下标作为定位书签。

产物通过引用或工具入口在聊天右侧打开，关闭后恢复聊天宽度；桌面可拖拽或用键盘调整预览宽度，按实际内容宽度决定是否并排。窄画布进入可返回的预览页，底层聊天与草稿保持挂载；展开、关闭与调整宽度保留阅读位置，切换会话、运行或审批上下文后关闭旧面板。版本菜单随面板关闭，并支持先 Escape 关闭菜单、再次 Escape 返回聊天。

运行内的产物选择沿用既有舞台、版本、标注和公开分享 API。工作区助手的精确产物 ID 可直接预览，裸文件名先列出运行来源供用户选择；按后端引用验证规则匹配大小写，不跨运行猜测同名文件。文件内容读取成功后立即展示，运行元数据可以随后补齐；失败、删除、来源分页和过时异步响应均有明确处理。智能体测试没有真实产物运行身份时不推断产物按钮。

### 上下文与浏览器存储

新增单例 `useWorkspaceContext`，认证完成后加载工作区元数据，并合并并发请求。退出或换用户时清理、取消请求并丢弃旧用户晚到响应；失败允许重试。登录、公开审批和 embed 页面不等待工作区初始化。

所有组件停止从项目列表、URL 和 localStorage 推测当前项目。`usePendingGates` 直接读取 query 的旁路同步移除；TagFilter、看板及运行弹窗仍获得有效工作区上下文，不能因去掉选择器而停止加载。

| 浏览器数据 | 处理 |
|---|---|
| `grasp-project-context` | 一次性读取导航偏好后移除，不再恢复项目筛选；不调用 localStorage.clear |
| 收藏、首页流程记忆、运行草稿 | 按用户或 workflow ID 保存，原 ID 保留即可，不重写也不清空 |
| 首页正文与附件 | 保留 IDB 与 fallback，沿用 savedAt 比较；流程不可运行时保留内容并提示重新选择 |
| 编辑器 `grasp.workflowDraft.{workflowId}` | 必须保留。不同内容的备份不能因 server.updatedAt 较新被隐藏，迁移改名会触发此问题 |
| PM 当前会话 | 从旧项目键一次性选取合法线程，写入工作区键；不合并聊天正文、不覆盖其他原始键 |
| 安装向导抑制、折叠偏好 | 根据实际资源迁移，旧系统不能因新入口重新初始化；与项目无关的偏好原样保留 |

编辑器备份使用通用冲突恢复机制：新备份记录 baseVersion 或内容 hash；内容不同即可提供比较，savedAt 仅用于排序展示。旧备份缺基础信息按需比较，恢复不自动保存或发布；默认保留迁移后的合法名称，只在用户丢弃或恢复成功保存后删除备份。这是正常编辑冲突处理，不新增永久迁移时间戳字段。

PM 恢复顺序：显式 thread → 旧 PM 链接对应的合法来源线程 → 已建立的工作区线程记忆 → 旧项目偏好对应的线程 → 服务端最近可见线程。本地旧 PM 键没有时间戳，不能猜哪个最近；校验 Thread 自身的用户和 Agent 身份，不能强制等于新默认 PM。

## 3 迁移后的单一模型

### 工作区与接口

保留唯一配置容器，单项目升级沿用原 ID，不能写死 `proj-default`；真正空库才创建默认 ID。无需另建常驻的 canonical 绑定表或来源映射表：初始化事务、唯一约束和服务入口共同保证只有一个工作区，启动校验数量，初始化失败阻止业务服务启动。

关闭所有再建项目路径，包括 HTTP CRUD、团队 bootstrap/retry、安装向导、基线创建、工作流导入、Agent 单包和整包导入、PM 写工具及内部直接调用 Projects.Create。团队名称只用于资源命名，不重置已有全局 PM、凭据和共享配置。

新业务接口统一为 `/api/workspace` 及其配置子资源；工作流、运行、产物等继续使用自身 ID，不带项目筛选。前后端类型、客户端和文档只维护新接口。内部模型和字段随实际调用清理，不机械改写与代码仓库含义有关的“项目”。旧列可留到迁移核对完成后的 schema 收尾阶段，届时删除；append-only 历史中的来源字段保留为记录内容，不参与分流。

### 正常配置承接旧差异

| 配置 | 转换与长期表达 |
|---|---|
| 共享 Agent 文件、MCP、env、布局、后端 | 计算每个 Agent 的有效配置，迁移到其普通配置和 workspace。多项目首选空共享基线或共同部分，避免再次继承额外内容 |
| 共享基线继承 | 当前只有增加和覆盖，没有“不继承此项”。若保留任意非空全局基线，需要普通 Agent 的继承开关；它必须是通用设置，不是 legacy 标记。可以空基线等价表达时不新增该字段 |
| 工作流共享变量 | 增加普通的工作流默认覆盖，保留完整类型、Ask、Required、Editable、Options、Secret 等元数据；需要时可关闭全局默认继承 |
| 变量执行顺序 | 工作区默认 → 工作流默认覆盖 → Graph 变量 → 对 Ask 字段提交的值。已存在 RunVariable 不重算 |
| 非密钥沙箱 env | 工作区 Agent 默认 → Agent → 运行快照，继续服从认证与平台保留字段的最终约束 |
| 凭据 | 保留 ID、密文、状态和已有 AI/OpenCode/Git/SSH 选择。确有差异时增加普通 `credentialBindings`，表达继承、指定 ID、明确不注入，禁止无效选择自动回落 |
| 默认凭据槽位 | 旧 ID 当作不透明 ID；用正常槽位属性或明确的新默认规则解析，不再靠旧项目 ID 拼接判断。覆盖自定义/MCP 密钥、已清空项、credential 引用与 Codex 回写 |
| 通知 | 扩展普通工作流通知覆盖，必要时承接总开关、事件、目标渠道和模板。现有 WorkflowNotifyPolicy 只有 mode/events，不足以保存各旧项目差异 |
| 自动审批 | 原 PM、开关变量、提示与能力需要明确转换为普通工作流策略或统一重配，不能用全局 PM 偷换旧语义 |

Graph.Variable 没有 Secret，不能把原项目密钥变量直接复制进 Graph。默认覆盖应与当前非版本化的默认配置语义一致，并在所有读取接口正确打码，不能泄漏到工作流导出或公开产物。

凭据转换必须比较实际注入结果：同 EnvKey 多条有效凭据、额外清空槽位会抑制自动注入；迁移后不能按“最新一条”选择。原先无效或属于其他项目的显式引用不能因合并后同属工作区而突然有效。无法用普通绑定表达的情况列为迁移阻断或重配项。

保留用户工作区原文件及 VCS，物化共享文件时追加可追踪的迁移变更，不覆盖旧历史。结构化平台 MCP 地址可由迁移工具转换；自定义模板中的旧项目地址逐项报告处理，不能全局替换历史文件。

### 运行与会话

普通运行使用唯一的 Agent 和 Workflow 配置解析路径。迁移前后逐个比较可续跑任务的有效配置；差异必须等价转换，或明确选择新配置后再切换。不默认建设 legacy execution context、resource origin ACL 或旧记忆 namespace。

Run.Graph、RunVariable、Run.SandboxEnv、McpToken 已持久化，原值保留；后端选择、Agent 文件、共享目录、凭据、通知与自动审批会实时解析，必须检查整条链路，不能只保留 Graph 就宣称恢复一致。现有系统没有保存最初启动时的完整配置，比较基准是升级前有效配置。

旧 PM 会话继续使用 Thread.AgentName，新默认 PM 只决定新会话。记忆归到同一工作区并按 Agent 隔离；同 Agent 同标题冲突在迁移前明确改名，保留 ID 与正文。合并后同 Agent 使用统一记忆，这是移除项目边界后的目标行为，不维护旧逻辑范围。

若同一 Agent 在不同可续跑任务中必须维持互不相同的旧配置，先完成任务、明确重配或单独评估通用执行快照；这一例外不是默认迁移路径，也不能靠隐藏的旧项目分支处理。

## 4 数据与文件清单

除 projects 自身外，当前十三类数据库 Model 直接含 ProjectID。目标是活动数据属于同一工作区，历史事实直接全局查询。

| 表 | 保留与转换 | 约束 |
|---|---|---|
| `projects` | 留一份工作区配置；其他原配置离线归档 | 不通过普通 Delete 清理旧来源 |
| `workflow_defs` | 保留 ID、图与版本链，统一归属 | 原 `(project_id,name)` 唯一，先处理重名 |
| `project_credentials` | 保留 ID、密文、撤销/清空状态和选择关系 | 别名、默认槽位、同 EnvKey 注入冲突 |
| `artifacts` | 保留内容、ID、revision、ArtifactVersion 与 blob | 空归属、已删工作流和会话产物均纳入 |
| `sandboxes` | 保留运行恢复信息和日志，按容器类型处理 | 容器内旧 MCP URL 不会随 DB 更新 |
| `project_memory_items` | 归并为 Agent 的统一记忆，重名先处理 | 原 `(project_id,agent_name,title)` 唯一；不覆盖或拼接正文 |
| `chat_threads` | 保留 Thread、User、Agent、Kind、消息和草稿 | 私聊权限、渠道会话只读和所有权不变 |
| `agent_cron_jobs` | 保留 Job/Thread/Agent、计划、时区、nextRunAt 与执行链 | 处理 claimed 状态和原 PM 的工具权限 |
| `channel_configs` | 保留所有渠道与 Agent 绑定，选择唯一 primary | 每项目一个 primary，每非空 Agent 全局一个渠道 |
| `requirement_drafts` | 保留状态、里程碑、进度、日期和父子 ID | 孤儿父项报告，不删除子项或伪造父项 |
| `project_external_mcp_settings` | 转成一份明确的工作区接入策略 | project_id 为主键，不能直接批量改成同值 |
| `project_mcp_api_keys` | 保留记录与撤销历史，重新处理有效授权 | 多项目 key 默认撤销重签 |
| `project_audit_events` | 原事件、来源、操作者、时间、payload 不改写 | append-only，全局查询，不加运行时来源映射 |
| `token_usage_events` | 原事件和名称快照不改写 | append-only，全局账本聚合，包含已删项目和空归属 |

间接关联一并核对：WorkflowVersion、Run、StateRun、RunVariable、Gate、React/clarify/review 会话、ArtifactVersion、ChatMessage、ChatTurnDraft、CronRun、Workflow API key、预览/live session、公开分享 nonce、embed、通知 receipt、Sandbox 日志。ID 不变，不能以重新导入代替迁移，也不能依赖数据库自动级联处理这些标量引用。

同一备份点覆盖 Agent 的 agent.json、workspace 与 .workspace-vcs，所有 project-shared 目录、附件/blob、沙箱恢复所需目录与配置。普通 Agent 更新和 ZIP 覆盖会在改变项目时调用 PurgeAgentProjectData 删除会话、记忆和 Cron；迁移禁止走此路径。Agent Save 还会整树重写 workspace，不能用它只改归属。

### 历史查询与统计

产物列表去项目过滤，产物树去对 projects 的 INNER JOIN，按工作流/会话分组，已删工作流保留历史桶。审计的列表、facets、统计和导出改为工作区全量查询。历史来源字段可在记录详情保留，不再需要映射到 canonical 才能被看见。

Token 以现有 ledger 为事实源，去掉 SQL 和内存层的项目过滤；不能再先枚举现存 projects 才求和，也不能通过可变工作流或线程重算历史。**保留统计 source 口径**：平台 KPI 为 workflow + pm，用量统计默认包含 Studio，可按来源筛选。看板只负责执行状态，不重复用量图表。汇总、趋势、未知模型、成本、下钻、分页和 CSV 使用相同规则。

校验事件数和各 source/model 桶，分别比较 input/output/cache read/cache write 四项总和及相同价格表下的成本。未知模型显示别名仅影响新展示，不改变模型 key、Token 数或历史名称快照。

## 5 对外接口与授权

| 类型 | 处理 |
|---|---|
| 应用 API | 唯一 workspace 接口与单套客户端；移除项目 CRUD、选择参数和旧 fallback |
| 工作流 `/v1` | 保留 workflow/run ID、key 和 URL，继续限制只访问原工作流，不升级成全局 key |
| 外部 MCP | 提供明确的工作区接入地址，例如 `/mcp/workspace[/:mcpId]`；客户端在切换时更新，旧项目 URL 不常驻 |
| 单项目 MCP key | 如权限未扩大，可保留 key/hash，更新接入 URL；迁移报告列出变更 |
| 多项目 MCP key | 默认撤销有效旧 key、保留记录，明确签发新工作区 key；不维护旧来源 ACL 来自动兼容 |
| 旧渠道、PM 与 Cron | 原项目级工作流/草稿/Agent FS 能力重新确认：明确授予工作区范围，或关闭受影响能力、暂停任务。无确认不能静默扩大 |
| 用户和 Agent 私有权限 | 保留线程 UserID、AgentName、只读渠道、memory/context-store 边界和公开分享权限 |

如果部署要求旧项目授权范围保持原样，则与取消项目范围并避免长期兼容存在冲突，迁移前必须解决，不能伪称只改 ID 就保持授权等价。渠道和 Cron 与外部 key 同样需要检查。

既有稳定 MCP 工具名可以作为唯一协议名继续使用，但实现统一工作区语义；不同时维护新旧两套别名。旧 Agent 包继续识别既有 project-agents/project.json 格式，由导入器转换目标工作区，不保留运行时项目分支。是否另发新格式应独立版本化，不能破坏旧包读取。

平台提示与新示例更新术语；不批量修改用户已保存的 Agent 提示、规则和历史内容。README 中英文、帮助概念/快速开始和 API 示例同步改，不能全局替换所有“项目”一词。

## 6 入口迁移与删除标准

不长期保留旧 `/projects` 页面、ProjectContext、项目树或项目筛选。迁移报告输出新链接；确实需要短期书签转换时，只使用一个集中升级入口，验收结束后的下一功能版本删除。服务端不保留旧项目 API 的平行实现。

转换清单供迁移工具和短期入口使用：

| 旧入口 | 新入口 |
|---|---|
| `/projects` | `/workflows` |
| `/projects/:id` 无 tab 或非法 tab | `/board` |
| 原各 tab | 按第二节映射，保留目标识别的 thread/agent/studioTab/sub 等参数 |
| `/runs`、`/gates`、`/dashboard` 的 projectId | 移除项目筛选，保留 run/node/workflow/tag/status/sort |
| `/artifacts?project=p&workflow=w` | `/artifacts?workflow=w` |
| `/artifacts?project=p&session=1` | `/artifacts?session=1` |
| `/workflows/new/edit?projectId=p` | `/workflows/new/edit` |
| 看板与用量统计 | `/board` 负责执行状态，`/stats` 负责用量分析；不向看板传递已无用途的 Token 时间参数 |

稳定的工作流、运行、产物与公开分享 ID 链接继续保留。补齐 named route project-detail 的跳转，包括 Codex 登录修复、模型凭据选择器、初始化完成、首页无可运行流程和 Token 下钻。产物使用 project 而非 projectId，解析和 URL 写回要一起更新。

一次性浏览器迁移只处理项目偏好和 PM 当前线程等受影响数据，完成后记录迁移版本。收藏与草稿按稳定 ID 原样使用。旧备份格式可在正常备份解析器中识别为“需要比较”，无需长期旧项目模型；未恢复的用户内容不按版本期限删除。

清理验收必须搜索路由字符串、named routes、query key、storage key、API 类型和动态跳转，不能只删主菜单。现有字段名如因外部稳定格式保留，登记用途，不能继续承担项目选择。

## 7 迁移与切换流程

### 预检和冲突决策

| 数据状态或冲突 | 处理 |
|---|---|
| 0 项目且无任何相关资源 | 全新安装，创建唯一工作区 |
| 0 项目但有历史或文件 | 孤儿修复预检，不当作空库 |
| 1 项目且 ID 任意 | 保留原 ID，走简化路径 |
| 多项目 | 专用迁移，不能在普通启动时静默选择最早项目 |
| canonical 与默认配置 | 明确选一份现有 ID 和目标默认 PM、primary 渠道、变量、通知及共享基线 |
| 工作流重名 | 明确新名称，保留 ID，专用追加版本且禁止 prune；原历史版本不改写 |
| 改名发布关系 | 原 head 已发布则保持同图发布；存在未发布改动的绝不顺带发布。核对 head graph 与 published graph，不强求版本总数不变 |
| 记忆同名 | 按实际数据库排序规则检测，确定新标题，保留原 ID/正文；旧标题记入离线迁移清单 |
| 变量、共享与凭据 | 转成普通覆盖和绑定，比对有效结果；无法表达先解决或重配，不留隐形回退 |
| 多个主渠道和 PM | 选全局默认，其他渠道、Agent、历史会话和任务保留；权限变化逐项明确 |
| 孤儿与已删资源 | 可推断则登记修复，不能推断也保留为来源未知；不伪造工作流、图、版本或父草稿 |
| 可续跑、活动任务 | 比较升级前后配置、身份、通知和工具权限；无法达成安全点或一致性则阻断切换 |

现有 appendVersion 会裁剪超过 200 个未发布且无 Run 引用的版本，迁移改名不能直接走会触发 prune 的普通 Save。必须有保持原历史的专用追加路径，并测试裁剪边界。后续正常版本保留策略属于现有业务行为，不由迁移工具暗改。

预检报告不含秘密值，记录数据库类型与版本、项目/资源数、孤儿、名称冲突、配置差异、槽位、授权、可续跑 Run、活动会话、Cron claim、文件与附件校验。报告包含源指纹，执行前重新核对，不能使用过期计划。

### 维护窗口

1. 冻结新运行、PM 回合、外部写工具、配置与初始化；停止 Cron 和渠道新消息准入。
2. 等活动回合落盘，排空 PM runner、渠道入站 FIFO、自动审批队列、独立 pushQueues 和出站发送，确认活动 Cron/通知投递归零。排空期间保持渠道可发送，不能先断连接。现有 shutdown 不具备完整排空保证，需要新增受控维护流程。
3. 保留通知 receipt，测试“receipt 已 claim、消息尚在推送队列”的场景；不能删除 receipt 来重发。维护期入站消息按各渠道实际缓冲/上游重试能力处理，不能承诺不存在的持久队列。
4. 旧进程及原绑定仍有效时，采集活沙箱认证状态，按核对过的准确 credential ID 完成 Codex 登录回写，归档必要文件与日志；不能在凭据归并后按 projectId + EnvKey 猜目标账号。现有采集依赖进程内 live 信息，必须早于停进程。受控退休空闲的旧 PM、渠道、Cron 和测试容器并清理线程绑定，保留 Sandbox 历史元数据和日志，不能直接调用会连带删除数据的清理函数。工作流容器保留。非运行容器注入了旧项目 MCP 地址，切换后按正常配置重建；历史可续聊，不承诺原 ACP 连接不断线。
5. 停止所有写实例，建立数据库、Agent/VCS、共享目录、附件和必要沙箱文件的同一备份点，验证可恢复。SQLite 处理 WAL；MySQL 用一致性备份，并保全现有解密依赖。仓库现有 SQLite 自动备份不能代替此步骤。
6. 先执行受 journal 记录的 schema 阶段，再做数据事务与文件发布。MySQL DDL 可能隐式提交，不能声称与数据事务一起回滚。记录 schema 已完成、数据未提交、数据已提交、文件部分发布和完成等状态。
7. 文件暂存并校验，事务转换归属、配置、记忆与主渠道，追加必要版本。数据库不能原子提交文件系统，启动前先按 journal 恢复或完成发布，不能读取半迁移状态；此后不再按旧默认槽位执行登录回写。
8. 合并/重归属的存活工作流沙箱先保全工作树、配置、日志与精确登录回写，再由源部署正常退休；预检阻断未退休容器。保留稳定 Run ID/token 和持久状态，测试引擎正常冷恢复；不依赖重连刷新 MCP 或凭据，不保留旧来源地址映射。
9. 正常服务仅加载新工作区模型，校验数据、查询、配置、权限与恢复；只读和旧任务验证通过后，恢复渠道、调度和新任务准入。

迁移不会额外承诺现有引擎不具备的外部副作用 exactly-once 保证。验证不能新增重复节点访问、Cron claim 或通知；无法排空的回合应推迟切换，不能用强杀代表无损。

### 回滚与工具退出

迁移工具提供 plan/apply/verify 阶段、稳定版本和 journal。重复执行已完成迁移只验证，不重复改名、追加版本、撤销 key 或复制文件。离线报告保留旧到新 ID/地址的映射，正常服务不依赖此文件。

恢复准入前失败，可恢复同一备份点或完成 journal 中的暂存发布。恢复准入后已有新数据，不能直接覆盖旧快照；需重新停写并核对新增数据，采用前向修复或可验证回迁。

结束标准是新模型可独立启动、所有资源有正常归属、旧项目入口和查询无业务调用、迁移工具不参与服务启动。短期入口转换器在下一功能版本删除；旧业务代码与不再使用的列纳入本次收尾，不能标成无期限“以后清理”。离线备份按运维保留策略管理，不随代码清理删除用户历史。

## 8 实施拆分与验收

| 阶段 | 交付与完成条件 |
|---|---|
| A 表征与预检 | 完整数据矩阵、只读工具、关键原行为测试；区分空库、单项目、多项目和孤儿 |
| B 新模型与接口 | 唯一工作区、统一客户端、关闭所有再建入口、必要普通配置覆盖；无平行旧运行时 |
| C 一次性迁移 | 配置固化、授权决策、文件与数据库 journal、无 prune 追加、回滚和幂等 |
| D 前端拆分 | 全部工作流、设置与工具页面、统一上下文、草稿保留、移除项目树/筛选/跳转 |
| E 集成与副本演练 | 数据、恢复、权限、统计、布局和全树门禁通过；SQLite/MySQL 都验证 |
| F 切换与收尾 | 实际预检决策、备份、维护窗、迁移报告、清理验收和短期入口删除计划 |

| 验收域 | 必测样本与判定 |
|---|---|
| 数据完整性 | 零项目有残留、任意 ID 单项目、多项目、已删项目、未知来源；原内容与 ID 链不丢 |
| 版本和本地编辑 | 超 200 未固定版本、改名后的发布/草稿关系、本地未保存图备份；不裁剪、不自动覆盖或发布 |
| 文件与附件 | 每份共享目录、Agent workspace/VCS、blob、全部 Artifact revision；引用与校验和正确 |
| 变量与凭据 | Secret、同 EnvKey 多条、空槽位、已撤销/失效/错误归属选择、custom/MCP、OpenCode/Codex/Git/SSH；不误注入或回落 |
| 运行 | 手动 head 与首页/v1 发布版分开，queued、waiting_human、clarify/review/react、failed续跑、运行重连 |
| PM 与记忆 | 原 Agent 续聊、用户隔离、同标题转换、渠道会话只读、重建沙箱后上下文正确 |
| Cron | 原 Agent/Thread、普通和原 PM 任务、at/every/cron、IANA 时区、UTC nextRunAt、过期 claim、维护期不重复补跑 |
| 通知与自动审批 | 原渠道/模板/事件与显式新配置一致、不误投、不新增自动批准、receipt 与出站队列排空 |
| 对外授权 | workflow key 跨流程拒绝，MCP 撤销和新 URL，旧渠道/PM/Cron 工具重新授权或停用，无静默全局化 |
| 统计与产物 | 全历史可查，workflow+pm KPI 与含 Studio 分析各自一致，四类 Token/成本/下钻/CSV 口径不变 |
| CRUD 与初始化 | 新建/基线/导入/复制/改名/发布/删除、团队创建、安装重试，不再创建第二工作区或覆盖已有默认 |
| 导航与存储 | 十三个原标签、named route、project/projectId query 移除、收藏/IDB/PM 线程保留、无请求乱序恢复旧条目 |
| 上下文生命周期 | 并发加载合并、A 用户慢请求后退出登录 B、失败重试、公开分享/embed 不依赖工作区 |
| 布局 | 深浅主题、中英文、移动端/窄分栏、抽屉返回、桌面收起、键盘与大量长名称 |
| 可靠性与清理 | 并发初始化、schema 后数据前失败、DB 后文件部分发布、幂等、回滚、正常服务不读取旧映射或 journal |

扩展现有 server project/workflow/credentials/PM/cron/external MCP/v1/reattach/上下文隔离测试；前端对应单测和 E2E，包括 workflow-menu/favorites、home-workflow-select-create、board-token-stats、project 各设置面板、pm-channel-multi、onboarding、artifacts-page-version、desktop-sidebar-hide，并新增升级数据和浏览器备份场景。关键路径 E2E 子集不覆盖所有迁移功能，必须额外运行相关场景。

实施门禁采用 [AGENTS.md](../../AGENTS.md) 的既有命令与阈值：server 完整套件和核心覆盖率至少 90；web 完整套件和 Lines 至少 85、关键路径 E2E 及三个 web 检查；全仓 actionlint/shellcheck；docs 构建和高危审计。实际修改 sandbox/runtime 路径时追加相应套件，不默认重建沙箱镜像。行为实现阶段更新 CHANGELOG，本次方案只记 DEVLOG。

## 9 实施前需要实际数据落定的选择

1. 实际项目数、孤儿与活动任务，以及保留哪个现有 ID。
2. 新默认 PM、primary 渠道、变量、共享基线和通知策略；哪些差异转换成普通覆盖，哪些明确重配。
3. 重名工作流与记忆的新名称、追加版本清单及发布关系。
4. MCP 客户端 URL/key 更新安排，旧渠道和 Cron 的工作区级授权或停用决定。
5. 维护窗口、消息上游重试能力、活动回合排空条件、备份和恢复验证。

这些选择由只读预检产出具体清单后用于实际迁移。可以先完成新模型、前端、迁移工具与测试的可复审实现；不需要为未知数据提前建设长期兼容系统。

## 10 代码依据与实施落点

| 主题 | 主要文件与符号 |
|---|---|
| 工作区生命周期 | [database.go](../../server/internal/database/database.go) finalize/ensureDefaultProject/createChannelIndexes；[project.go](../../server/internal/services/project.go) DefaultProjectID/Delete；[team.go](../../server/internal/services/team.go) runBootstrap/writeProjectAuth |
| 工作流和版本 | [workflow.go](../../server/internal/services/workflow.go) List/SaveAs/appendVersion/pruneWorkflowVersions；[run.go](../../server/internal/handlers/run.go) publishedSnapshot；[workflow_import.go](../../server/internal/services/workflow_import.go) |
| 变量和运行状态 | [start_run.go](../../server/internal/engine/start_run.go) resolveStartVars；[graph.go](../../server/internal/models/graph.go) Variable；[models.go](../../server/internal/models/models.go) ProjectVariable/Run；[notify.go](../../server/internal/models/notify.go) |
| 执行配置 | [registry.go](../../server/internal/runtime/registry.go)；[acp_config.go](../../server/internal/runtime/acp_config.go)；[acp_sandbox.go](../../server/internal/runtime/acp_sandbox.go)；[shared_agent.go](../../server/internal/services/shared_agent.go) |
| 凭据与文件 | [project_credentials.go](../../server/internal/services/project_credentials.go) ResolveEnv/isDefaultCredentialID；[credential_selection.go](../../server/internal/runtime/credential_selection.go)；[agent.go](../../server/internal/services/agent.go) Save；[workspace_vcs.go](../../server/internal/services/workspace_vcs.go) |
| 数据清理陷阱 | [agent.go](../../server/internal/handlers/agent.go) 更新和 ZIP 覆盖；[pm_memory.go](../../server/internal/services/pm_memory.go) PurgeAgentProjectData |
| PM 与授权 | [pm.go](../../server/internal/handlers/pm.go)；[sandbox_agent.go](../../server/internal/services/sandbox_agent.go)；[mcp_wiring.go](../../server/cmd/server/mcp_wiring.go)；[external_mcp.go](../../server/internal/handlers/external_mcp.go)；[v1_handlers.go](../../server/internal/handlers/v1_handlers.go) |
| 通知与停机 | [run_notify.go](../../server/internal/services/run_notify.go)；[gate_auto_invoke.go](../../server/internal/services/gate_auto_invoke.go)；[push_queue.go](../../server/internal/channels/push_queue.go)；[main.go](../../server/cmd/server/main.go) shutdown |
| 查询统计 | [artifact.go](../../server/internal/services/artifact.go) allQuery/Tree；[audit.go](../../server/internal/services/audit.go) applyFilter；[token_ledger_read.go](../../server/internal/services/token_ledger_read.go)；global_token_stats/project_token_stats/token_usage_events_query |
| 导航与页面拆分 | [SidebarWorkspaceLists.vue](../../web/src/components/shell/SidebarWorkspaceLists.vue)；[settingsNav.ts](../../web/src/data/settingsNav.ts)；[router/index.ts](../../web/src/router/index.ts)；[useProjectDetail.ts](../../web/src/lib/project/useProjectDetail.ts) |
| 前端上下文 | [useProjectContext.ts](../../web/src/lib/composables/useProjectContext.ts)；[usePendingGates.ts](../../web/src/lib/inbox/usePendingGates.ts)；[TagFilter.vue](../../web/src/components/ui/TagFilter.vue)；[WorkflowEditorView.vue](../../web/src/views/WorkflowEditorView.vue) |
| 本地备份与运行 | [useWorkflowSave.ts](../../web/src/components/canvas/composables/useWorkflowSave.ts)；[useWorkflowRunLaunch.ts](../../web/src/lib/run/useWorkflowRunLaunch.ts)；[useHomeApproveChat.ts](../../web/src/lib/run/useHomeApproveChat.ts)；runDraft/homeComposerDraft/firstInstall |
| 其他前端影响 | AgentStudioView、ArtifactsView、TokenAnalyticsView、CreateAgentTeamWizard、ModelCredentialSelect、useRunDetail，以及 Project 相关面板与测试 |

