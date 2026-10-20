# 升级到单工作区

新版本只有一个工作区。工作流直接出现在侧栏；看板、需求草稿、助手和原项目配置位于独立页面或设置。旧 `/projects` 页面、`/api/projects` 和项目级 MCP 地址退出使用，不提供双接口。数据库的 `project_id` 和原工作区 ID 继续作为内部关联字段；运行时不读取迁移报告或按原项目选择配置。

## 升级前核对

- 保留现有加密主密钥、数据库、Agent profiles、同级 `project-shared`、沙箱归档及部署配置。工具不解密或轮换密钥。备份含凭据密文、消息及可能明文保存的工作流变量，应按原业务数据保管。
- 单项目保留原 ID；多项目必须明确选择保留哪一个。名称、默认变量、通知、默认助手和外部 MCP 设置以该工作区为基础。其他工作流的变量（含 Secret 标记）、完整通知策略和自动审批策略转换成普通工作流默认覆盖；其他 Agent 的共享文件、环境和 MCP 配置物化到 Agent 自身。
- 合并后所有用户在同一工作区协作。原项目隔离不再存在。PM / 渠道工具扩大访问范围必须在预检中明确授权；不接受扩大时，应在旧系统禁用后重新预检。原 PM 定时任务若失去原权限语义，必须重配或明确暂停。
- 工具保留工作流、版本、Run 快照与 MCP token、消息、产物、内存、定时任务、通知回执、审计和 Token 账本。原审计与账本归属快照不重写。重名工作流增加一次名称版本，保留发布指针与草稿关系，不裁剪旧版本。
- 明确选中的凭据保留原 ID；原隐式凭据选择转换为 Agent 的普通凭据绑定。缺失和禁用使用显式关闭，不回退到其他身份。合并后有多个默认槽时，登录回写必须有可验证的精确凭据身份；无法确定时停止回写。
- 多项目合并撤销全部原项目级外部 MCP key；工作流 `/v1` key 保留。外部客户端需改用 `/mcp/workspace`，重新发放所需权限。自定义 Agent / 共享 MCP 配置里的旧项目地址必须先调整，工具会阻断检测到的内嵌旧地址。

## 先进入安全点，再停写

先停止用户新请求、Webhook 和渠道新消息进入，并停用定时触发；让正在执行的回合和出站发送完成。保留出站连接直至推送、FIFO、流式发送和回执处理排空。检查已领取的定时任务，不应直接清空 claim。

在旧账号绑定仍有效时，将 PM / Studio / 测试沙箱的最新登录文件回写到原精确凭据；保留会话、日志与归档后退休这些非 Run 沙箱。迁移后按新的工作区地址重新创建它们。运行中的 Run 应先到达等待人工、排队或终态等安全点，不能在 running/cancelling 中迁移。涉及项目合并或孤立归属修复时，仍存活的 Run 沙箱也必须先保全工作树、日志、配置和精确登录回写，再由源部署正常退休；预检不会把其状态强行改成已销毁。重连不会刷新容器内已有 MCP、环境或凭据。等待人工的 Run 保留 ID、图、变量、消息、产物及 token，升级后按引擎正常冷恢复流程重建所需交互沙箱；未保存的容器工作树不能靠数据库备份恢复。

当前单工作区服务提供管理员 `POST /api/workspace/maintenance`（空 JSON），冻结新启动、调度、PM 和渠道入站，返回待排空计数；`GET` 读取进度。待回合与出站均空后再次 `POST` 才回写登录并归档、退休空闲非 Run 沙箱，最终返回 `safeToStop=true`。归档位于 profiles 同级 `maintenance-archives`，也应备份。维护状态不跨重启保存，关闭服务后离线迁移。

旧多项目版本没有这个新端点，新服务也拒绝直接打开未合并的多项目数据库。因此，旧部署必须使用其已有的关闭入站/调度、会话导出与沙箱退休手段完成上述安全点，不能先启动新服务“帮忙排空”。离线工具无法读取旧进程内的出站队列；`--offline` 是操作人对排空和全部写入进程已停止的明确声明。不能确认时不要执行 apply。

## 工具流程

以下示例在 `server/` 下运行，路径请换成实际部署路径。工具不会创建不存在的数据库，也不会在 plan 阶段 AutoMigrate。SQLite 的数据库文件必须在 profiles / shared / backup 树以外。

```bash
install -d -m 700 /srv/grasp-upgrade
go build -o /tmp/workspace-migrate ./cmd/workspace-migrate
/tmp/workspace-migrate plan \
  --database /srv/grasp/data/grasp.db \
  --profiles /srv/grasp/agents \
  --shared /srv/grasp/project-shared \
  --workspace EXISTING_ID \
  --plan /srv/grasp-upgrade/plan.json
```

单项目可省略 `--workspace`；多项目不能由排序自动选择。`--profiles` 必须填写实际根目录（即使为空）。`--shared` 用于显式核对路径，必须与服务端实际使用的 profiles 同级 `project-shared` 相同（包括解析符号链接后）；默认取该目录。计划文件以 0600 新建，已有文件不会覆盖。计划仅包含资源名称/ID、数量、冲突与指纹，不含配置值、密钥、消息或图内容；有阻断项仍写计划并以非零退出。

需要明确决策时准备 JSON，再用新的计划文件运行 plan：

```json
{
  "reauthorizeWorkspaceTools": true,
  "pauseFormerPMJobs": true,
  "repairOrphans": false,
  "workflowNames": {"WORKFLOW_ID": "调整后的工作流名称"},
  "memoryTitles": {"MEMORY_ID": "调整后的记忆标题"},
  "credentialNames": {"CREDENTIAL_ID": "账号别名"}
}
```

通过 `--decisions /srv/grasp-upgrade/decisions.json` 提供。不要盲目启用全部布尔项。`repairOrphans` 只确认孤立数据重新归属；损坏的 agent.json、符号链接、运行中会话和危险的跨来源凭据引用仍须人工修复。所有一级 profile 目录都需合法 agent.json，孤立共享基线需确认处理。不要把备份、临时目录或其他文件放进运行中的 profiles / shared 树。

同名工作流与同 Agent 同名记忆按稳定 ID 加后缀；凭据同种类重名按普通别名规则（去首尾空格、忽略大小写）增加后缀，兼顾旧内置别名的正常化，保证合并后仍可区分与编辑；ID 和密文保持不变。计划列出最终名称。MySQL 使用实际列排序规则检测冲突，索引中的工作流/记忆名称字段最长 191 个字符。重名或显式改名都会体现在计划中。修复源数据或改决策后必须重新生成计划，旧指纹不可继续 apply。

确认计划无 blockers，服务及其他写入程序停止后：

```bash
/tmp/workspace-migrate apply \
  --database /srv/grasp/data/grasp.db \
  --plan /srv/grasp-upgrade/plan.json \
  --backup /srv/grasp-upgrade/backup-001 --offline
/tmp/workspace-migrate verify \
  --database /srv/grasp/data/grasp.db \
  --plan /srv/grasp-upgrade/plan.json \
  --backup /srv/grasp-upgrade/backup-001
```

backup 必须为空且不与运行数据目录重叠。工具先写完整的类型化数据库快照、profiles/shared 文件与 journal，再分阶段添加两列、事务转换数据、发布文件目录、校验历史与指纹。数据库整数按有符号/无符号文本编码，避免 JSON 浮点损失；工作文件保留权限，重写的配置使用 0600；原工作目录及已有 VCS 历史保留。多项目共享配置物化后，原共享根退出运行路径，仅保留迁移备份中的副本。

完成后的重复 apply 会转为 verify。未完成 journal 不会自动重跑写入，先回滚或调查。结构添加与数据事务分开记账，兼容 MySQL 隐式 DDL 提交；中断在两个 ALTER 之间也能在原单元格完全未变时恢复。备份、原目录保留副本及 `.workspace-*.stage` 都属于本次迁移材料；回滚会校验并清理本工具持有的临时目录，不会删除预先存在的同名目录。

MySQL 使用相同命令，只替换数据库参数；DSN 从环境变量读取，不放到参数或报告中：

```bash
# GRASP_MIGRATION_DSN 由部署环境提供，含 parseTime=true 与原连接参数。
/tmp/workspace-migrate plan --driver mysql \
  --profiles /srv/grasp/agents --workspace EXISTING_ID \
  --plan /srv/grasp-upgrade/mysql-plan.json
/tmp/workspace-migrate apply --driver mysql \
  --plan /srv/grasp-upgrade/mysql-plan.json \
  --backup /srv/grasp-upgrade/mysql-backup-001 --offline
```

可用 `--dsn-env VARIABLE_NAME` 指定环境变量。SQLite/MySQL 均应另保留原生数据库备份；迁移快照用于本工具的受限恢复，不代替已有备份制度。

## 验证与回滚边界

verify 完成后启动新版本，检查侧栏工作流总数、历史运行/产物、记忆、助手旧线程、通知、Token 总量，以及需要恢复的等待人工 Run；检查每种 Agent 的实际模型与凭据身份。新助手默认绑定只影响新线程，旧线程按其原 Agent 身份继续；关闭助手发送不应阻止读取历史。

对外客户端改用新工作区接口，重新配置已撤销的外部 MCP key。工作流默认配置属于当前运行设置，与旧项目默认配置一样不随图版本冻结；首页及 `/v1` 的图使用发布版本，手动编辑器/列表启动使用最新保存图。便携工作流导出保留覆盖结构但不包含 Secret 值，导入后需补齐。

在开放新写入前可以回滚：

```bash
/tmp/workspace-migrate rollback \
  --database /srv/grasp/data/grasp.db \
  --plan /srv/grasp-upgrade/plan.json \
  --backup /srv/grasp-upgrade/backup-001 --offline
```

工具检查备份完整性、当前数据库和文件是否仍属于迁移前/后状态。发现新业务写入会拒绝自动覆盖；不能在升级后已接收新数据时拿旧备份直接覆盖。回滚恢复源记录、配置和目录，新增的可空列保留以供旧版本读取；重启原版本后再开放写入。迁移材料不参与新服务运行，验证和保留期结束后可离线归档整个备份及保留目录，不需要永久保留旧项目 API、映射表或运行时兼容开关。
