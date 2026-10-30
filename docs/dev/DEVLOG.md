# 开发日志

仓库内部流水，不发布到公开站点。最新条目在上。不回填 git 历史，不写排期。

对外行为变化仍按原有惯例另记仓库根目录 `CHANGELOG.md` 的 `Unreleased`。没有对外行为时不要改 `CHANGELOG.md`。

## 条目模板

复制下面这一块，填完后放在本文件「记录」一节的最上方。

```text
### YYYY-MM-DD

- 日期：YYYY-MM-DD（写下这条的当天，不是排期）
- 范围：路径
- 做了什么：
- 为什么：
- 如何验证：
```

## 记录

### 2026-10-30（PR #1040 本机执行二次 review 修复）

- 日期：2026-10-30（UTC）。
- 范围：`server/cmd/server/main.go`、`server/internal/{handlers/settings.go,execution/tools.go,services/model_configurations*,sandbox/{confighome_local,worktree_create,worktree_git_local,worktree_git_test}.go}`、`start.sh`、`scripts/test-start-local.sh`、`server/README.md`、`CHANGELOG.md`。
- 做了什么：本机模式服务只监听 `127.0.0.1`。默认模型组按 CLI 记录已创建过的工具（`settings` 表 `local_model_defaults_seeded`），删除后不再自动恢复，`Get` 不再写库。本机 Git 只复制 SSH `config`/`known_hosts`，私钥原位使用。`/api/settings` 的宿主 Git 身份只返回给管理员。`start.sh` 停止前校验 PID 属于 grasp-server，重复启动报错，新增 `--local down|restart`。快照失败、预览工具缺失与启动期绑定失败改为记录警告。README 说明 OAuth 刷新不写回的限制。
- 为什么：服务原先监听所有网卡并使用公开的演示密码，本机模式下等同把宿主用户的执行权限、CLI 登录与 SSH agent 开放到局域网。默认组在每次读取时补种导致删除无效；私钥副本在进程崩溃时会残留在 worktree 目录；PID 复用可能误杀无关进程。
- 如何验证：server execution、handlers、services、modelrouting、runtime、sandbox 全量单测通过，新增删除后不复活回归；`ssh -G` 校验身份文件指向宿主私钥、known_hosts 指向私有副本；`scripts/test-start-local.sh` 新增 PID 复用用例通过。

### 2026-10-29（PR #1040 同步主线并解决记录冲突）

- 日期：2026-10-29（Asia/Hong_Kong）。
- 范围：`CHANGELOG.md`、`docs/dev/DEVLOG.md`、`web/audit-allowlist.json`，合并主线 `87f12cf0`。
- 做了什么：完整保留本机执行模式的发布说明与开发记录，同时保留主线的确认竞态修复、回归测试和 Vue I18n 11.4.16 升级；本机执行功能源码不变。移除已由升级修复的 Vue I18n 漏洞例外，保留原有 braces 例外。
- 为什么：PR #1041 合入主线后，两个分支都在发布说明和开发日志顶部追加记录，导致 PR #1040 无法合并。直接取任一侧会丢失另一侧记录；继续保留旧版依赖的审计例外也已无必要。
- 如何验证：逐文件核对本机功能源码与合并前相同、主线六个修复文件与 main 相同，双方记录完整且不重复，无冲突标记。Server 全量 lint、vet、配置文档、单测、runtime 专项及核心覆盖率通过（90.5%），确认/当前节点回归各重复 25 次 race 通过；sandbox lint、vet、完整单测与核心覆盖率通过（96.6%）。Web 542 文件、5,836 单测通过，Lines 91.74%，完整关键浏览器 260 项通过；lint、类型检查、构建及官方源审计通过。本机关闭、46 项 Node 合约、12 项 Python 合约、文档构建/审计和根目录检查通过。Python 合约在本机 Python 3.10 下使用已有 tomli 兼容加载，CI 原始命令保持不变。

### 2026-10-29（PR #1040 本机配置路径来源隔离）

- 日期：2026-10-29（Asia/Shanghai）。
- 范围：`server/internal/sandbox/worktree_git{,_local,_test}.go`。
- 做了什么：本机 CLI 配置根改从服务端 Spec 的独立字段传入，私有 XDG 目标从 HOME 的固定相对目录计算；环境 map 仅接收最终路径输出。Trae 使用 HOME 的 `os.Root` 创建固定 `.trae` 链接，继续只指向 Grasp 自有配置目录。
- 为什么：推送后的 CodeQL 报告四条路径注入告警，数据流均从 PM 请求字段进入 Agent 环境 map，再经 `CONFIG_ROOT` / `XDG_DATA_HOME` 回读抵达文件操作。将文件路径来源与模型环境分开，保留已有目录访问边界，不添加扫描豁免。
- 如何验证：新增 Codex / Trae / OpenCode 恶意环境路径回归，确认无法改写配置来源、私有布局或外部目录；完整 sandbox 包单测及针对性 race 通过。Server 完整 lint、vet、配置文档、单测、90.5% 核心覆盖率与 runtime 专项通过；三个工具的本机切换 smoke 7 轮通过，该 smoke 是完整 8 工具矩阵之外的针对性复查。远端 CodeQL 在新提交上复查。

### 2026-10-29（PR #1040 本机执行 review 修复）

- 日期：2026-10-29（Asia/Shanghai）。
- 范围：`server/internal/{sandbox,services,modelrouting,runtime,execution}`、`sandbox-gateway/sandbox/internal/provider/{codex,modelconnection,kimicode}`、`sandbox-gateway/scripts/{test-agent-switching.py,agent-platform-wire-fixture*}` 及其合约测试、`start.sh`、`scripts/test-start-local.sh`、`web/src/components/workspace/WorkspaceCredentialsPanel*`、`web/src/lib/agent/modelRouting.ts`、`server/README.md`、`CHANGELOG.md`。
- 做了什么：本机执行仍过滤服务环境中的密钥与 Git 覆盖；所选 CLI 登录按文件白名单复制，跳过历史与 worktrees，防止目标嵌套导致自复制，保留 hooks 的执行位。Git/SSH 配置使用私有副本，子进程使用独立 HOME/XDG 并保留 local 模式，仅显式继承 SSH agent。Trae 的 HOME 路径只允许链接 Grasp 自有 ConfigHome，以保留切换后的历史。移除 Codex 本机模式的 API Key 放行与外部沙箱绕过参数；OpenCode 使用配置文件路径与无密钥的运行期覆盖，官方登录文件复制并清理已移除的授权。Kimi 最终隔离配置保留匹配站点的已保存 API Key 或文件 OAuth，凭据刷新不写回来源。`down` 在 Docker 检查之前停止本地 PID。默认模型组只为已安装 CLI 创建，发现路径同步到子进程 PATH，具体绑定类型必须明确，Solo 严格限定 Agent 的绑定组。补充本机模式中英文提示；E2E 授权改用原生配置文件，并让 local 模式覆盖全部 56 个有向工具切换。
- 为什么：已有 review 的三个必须修复项分别会扩大宿主机密钥传播、通过软链接写回登录状态，以及阻止无 Docker 环境关闭本地服务。继续核查发现模式传播、OpenCode 内联密钥、未绑定模型组回退和 E2E 依赖宿主环境变量也会掩盖实际边界问题。
- 如何验证：新增密钥过滤、配置副本写回、HOME/SSH、绑定校验、OpenCode 登录及无 Docker 生命周期回归；Server / Sandbox 全量 lint、vet、单测、配置文档与 runtime 专项通过，核心覆盖率 90.5% / 96.6%，相关 race 通过。Web lint、vue-tsc、5,836 单测（Lines 91.75%）、生产构建及 260 项关键浏览器用例通过。最终本机构建通过完整 worktree 84 项与 local 57 轮矩阵（覆盖全部 56 个有向切换），46 项 Node 和 12 项 Python 合约通过。真实 Kimi Code 2.4.0 在无环境密钥情况下使用保存的 synthetic API Key 向 localhost 完成原生 ACP 请求，模型与强度正确；云模式原生模型／强度专项也通过。沙箱 smoke、Git、bootstrap、preview 检查，actionlint、error-level shellcheck、品牌断言、文档构建及 npm high/critical audit 通过。完整切换矩阵中的 Cursor / Trae / Kimi 仍明确标注协议夹具，Kimi 原生授权另测；未调用厂商 API 或读取真实密钥。

### 2026-10-29（PR #1041 的 Vue I18n 安全审计修复）

- 日期：2026-10-29（Asia/Hong_Kong）。
- 范围：`web/package{,-lock}.json`、`web/src/locales/locales.compile.test.ts`、`CHANGELOG.md`。
- 做了什么：Vue I18n 与 Intlify 依赖从 10.0.8 升级到受支持的 11.4.16，保留现有 Composition API 配置。编译探针使用导出的 `INVALID_TOKEN_IN_PLACEHOLDER` 常量，继续要求非法占位符被拒绝；全量语言包编译与环境变量字面量的中英文实际渲染断言保持完整。
- 为什么：原确认竞态修复的 server、Go race 与汇总检查已通过，但独立安全流程因 [GHSA-83rm-6c8r-g728](https://github.com/advisories/GHSA-83rm-6c8r-g728) 拒绝 `vue-i18n 10.0.8`。上游 v10 已停止支持，修复从 11.4.14 开始。升级后编译器对原非法表达式报告更精确的错误类型，旧数字 7 断言需同步到具名错误常量。
- 如何验证：官方源完整 npm audit 报告所有严重级别均为 0，`audit:check` 通过；没有新增审计例外。Web 542 文件、5,833 项单测通过，Lines 91.75%；完整关键路径浏览器 260 项通过，覆盖真实中英文界面。lint、vue-tsc、生产构建及品牌、安全上下文断言通过，根目录 actionlint 和 error-level shellcheck 通过；更新同一 PR 后继续核对最新提交的远端 CI。

### 2026-10-29（确认早于暂停时保留当前节点）

- 日期：2026-10-29（Asia/Hong_Kong）。
- 范围：`server/internal/services/run_current{,_pending_test}.go`、`server/internal/engine/pending_auto_test.go`、`CHANGELOG.md`。
- 做了什么：运行仍为 `running`、没有运行中的节点或有效的确定后继时，当前节点查询回退到最新的等待节点；真实运行节点和确定后继继续优先。原失败测试通过数据库提交后的同步屏障覆盖确认先于、晚于运行暂停两种交错，并在清理前等待确认实际到达后继审批节点。
- 为什么：CI run `40768511138` 的 `TestConfirmDoesNotMarkAnUnsettledSuccessor` 读取到空当前节点。确认提前写入对话 `Done` 时，初始执行器可以跳过运行的 `waiting_human` 转换，而节点自身已经在等待；后继条件尚未确定，没有 pending-auto 标记，原查询遗漏了这个合法中间态。单纯增加测试等待或重跑 CI 无法修复刷新时的节点消失。
- 如何验证：新增服务回归在旧实现上确定失败；同步屏障也确定复现原引擎断言失败。修复后两种交错各重复 100 次 race 通过，engine/services 完整 race 与 server 全量单测通过；服务回归覆盖澄清、复审、接管、审批、旧标记、运行后继优先、终态和无活动节点。Go 1.26.10 下 lint、vet、配置文档检查、runtime 专项及核心覆盖率检查通过（90.5%，门槛 90%）；actionlint、error-level shellcheck、docs 构建和官方源 high/critical audit 通过。文档生成目录移至临时目录后，最终品牌断言与 diff 检查通过，生成物未入库。

### 2026-10-29（Kimi Code 订阅接入与思考强度）

- 日期：2026-10-29（Asia/Hong_Kong）。
- 范围：`server/internal/{clicatalog,modelrouting,runtime,services}`、`sandbox-gateway/sandbox/internal/{provider/kimicode,service}`、`sandbox-gateway/scripts/agent-*`、`sandbox-gateway/scripts/{mock-agent-model*,test-agent-switching.py,test_agent_switching_test.py}`、`web/src/{components/agent,lib,locales}`、`server/README.md`、`CHANGELOG.md`。
- 做了什么：Kimi Code 使用订阅控制台 API Key，国内／国际站分别连接官方 `api.kimi.com/coding/v1` 与 `api.kimi.ai/coding/v1`。目录改用六个订阅模型 ID，以精确别名兼容已有 K3 配置；五个可调模型公开 `low/high/max`，K2.7 Highspeed 保持布尔思考开启。配置、聊天选择、服务端校验和原生 ACP 确认贯通至真实请求的 `thinking.effort`。每个站点／模型／强度组合复用有界、原子发布的独立配置，同时共享原生历史、索引、锁及用户扩展；清除继承的 `KIMI_MODEL_*` 覆盖，拒绝未知 ID 和不支持的强度。 配置根从原始进程环境独立读取，目录、链接、索引及配置发布使用同一个 `os.Root` 边界，防止模型派生环境污染路径或符号链接替换造成越界写入。
- 为什么：原接入把 Kimi Code 订阅 Key 发给 Moonshot 开放平台导致 401，界面也没有思考强度。仅添加选项无法证明原生请求生效；共用可变配置还会让并行聊天互相覆盖，不声明 Highspeed 的布尔思考能力会触发厂商的跨模型回退。
- 如何验证：使用用户授权的订阅 Key，在真实 Grasp Solo API → worktree runtime → 原生 Kimi Code 2.4.0 → 官方 Coding API 链路分别测试国内／国际站的 low、high、max、空值恢复 max，八次请求均 200，实际 wire 字段与回复执行快照一致，非法值拒绝且历史快照不被后续选择改写。原生 CLI 本地模型回归另覆盖并存进程、同路由复用、旧别名、各模型默认值和 Highspeed enabled/no-effort。 远端 CodeQL 首轮发现的路径告警通过配置根来源隔离和文件操作边界收紧处理，另以外部符号链接、目录替换和外部配置目标保护回归验证；没有添加扫描豁免。免费 CI 仍明确使用 Kimi platform-wire-fixture；完整八工具矩阵 84 项、Node 合约 45 项、Python 合约 9 项通过。Server／Sandbox 全量 lint、vet、单测与核心覆盖率通过（90.5%／96.6%），runtime 专项与 provider/service race 通过；Web 541 文件、5,810 单测通过，Lines 91.73%，260 项关键浏览器用例、lint、vue-tsc、生产构建通过。文档构建和 high/critical audit、actionlint、error-level shellcheck、三模块 govulncheck、沙箱 smoke 与运行时包字节复现检查通过。密钥未进入仓库、PR 或测试日志；远端 CI 在 PR 最新提交上单独核对。

### 2026-10-29（OpenCode 目录缓存读取边界）

- 日期：2026-10-29（Asia/Hong_Kong）。
- 范围：`sandbox-gateway/sandbox/internal/provider/modelconnection/opencode_catalog{,_test}.go`。
- 做了什么：以原生缓存目录为 `os.Root`，仅打开固定的 `opencode/models.json`；检查和读取使用同一文件句柄，限制读取大小，非阻塞打开后拒绝 FIFO 等非普通文件。
- 为什么：PR #1037 的 CodeQL 检查指出环境变量派生路径直接传给 `os.Stat` / `os.ReadFile`，需要明确文件访问边界，防止子路径符号链接跳出缓存目录，同时避免检查后替换文件及读取增长文件的问题。
- 如何验证：新增目录和文件符号链接越界回归在旧实现上确定失败，修复后通过；缓存目录内的符号链接仍可使用，超大文件和 FIFO 立即拒绝。完整 sandbox lint、vet、单测、核心覆盖率及相关包 race 检查通过；提交后继续核对远端 CodeQL 与完整 CI。

### 2026-10-29（OpenCode 官方模型目录自动同步）

- 日期：2026-10-29（Asia/Hong_Kong）。
- 范围：`server/internal/opencodecatalog/`、`sandbox-gateway/sandbox/internal/provider/{modelconnection,oneshot}/`、`sandbox-gateway/sandbox/internal/agents/{platform_auth,opencode_variant}_test.go`、`sandbox-gateway/sandbox/README.md`、`CHANGELOG.md`。
- 做了什么：OpenCode 官方模式仅在启动或重建后端会话时同步原生模型目录并校验选择，沙箱与 worktree 共用入口。启动阶段的重复握手复用原生五分钟有效缓存；普通消息不执行目录预检或按时间刷新。启动时过期或新模型缺失会合并刷新，更新失败短暂抑制重复尝试并保留仍包含所选模型的旧缓存。检查真实缓存文件，避免原生 CLI 吞掉下载失败却输出成功提示时误判。在添加自定义推理变体之前校验目录，避免模型覆盖项伪造可用性。服务端列表同步过滤 deprecated/alpha，不自动更换已保存的模型或账号。
- 为什么：新容器沿用镜像预装 CLI 的目录快照，原生后台更新不保证在短命的单轮进程退出前完成。模型目录更新后，旧 Contributor 已标记 deprecated，但平台列表和已保存选择仍可能继续使用，分别触发本地模型找不到和上游模型不可用。官方文档把 `models --refresh` 作为目录更新命令，而非每轮对话要求；集成层应自动处理。
- 如何验证：回归覆盖冷启动、有效缓存复用、过期/缺失模型更新、并发合并、更新失败与成功退出却未更新缓存、取消后立即重试、缓存文件变更、弃用拦截及推理变体不可绕过。启动策略专项验证缓存过期后当前会话连续三轮不刷新，新 worktree 会话启动才更新；普通与 race 测试通过。真实 OpenCode CLI 1.19.5 在隔离缓存下通过冷启动、缓存复用和弃用拦截，未调用 Prompt，推理请求为零。Server 与 sandbox 全量 lint、vet、单测及规定 smoke 通过，核心覆盖率分别 90.5% / 96.6%；配置文档、runtime 专项、docs 构建和官方源审计通过。最终运行时在 worktree 模式通过完整 8 工具、76 项 Agent 切换/模型/推理验证，付费模型请求为零；可配置工具使用原生 CLI，Cursor、Trae、Kimi Code 仍按既有契约明确标注平台 CLI-wire fixtures。品牌断言、actionlint、error-level shellcheck 与 diff 检查通过；运行时逻辑不要求重建沙箱镜像。

### 2026-10-28（工作流自动过程切换时的对话隔离）

- 日期：2026-10-28（UTC）。
- 范围：`web/src/lib/run/useRunLive*`、`web/e2e/run-live-continue*`、`web/src/components/run/conversationToolbarTitle.test.ts`、`CHANGELOG.md`。
- 为什么：PR #1031 覆盖同一运行的断线恢复、排队与连续节点，却没有覆盖侧栏在两个不同运行间切换。两个运行使用相同节点 ID 时，旧 `lastEvents` 在新组件挂载时被重放，新的初始化标记又阻止正确历史接管；旧连接在新详情返回前仍可写入正在显示的页面。仅比较 run/node 字符串也无法隔离 A→B→A 中前一次访问的延迟请求。
- 做了什么：切换访问时立即关闭旧连接与计时器，清理节点、任务、当前回复缓存和初始化状态，以单调递增的访问代次隔离详情、历史种子、对话链及异步操作结果。新内容只投递给当前访问对应的组件；同一运行内自动流转仍沿用原对话。先设置当前执行轮次再加载链，保留第 2 次及以后执行的历史。标题测试改用实际留在 live 页面的自动节点，避免依赖已跳转到人工聊天后的旧页面。
- 如何验证：同名节点切换、A→B→A 迟到历史、旧连接回调与异步操作用例先在旧实现复现失败；修复后控制器 41 项通过，真实组件切换用例通过，并覆盖终态历史、计时器清理及第 2 次执行。合入最新主线后，Web 全量 541 文件、5,799 项单测通过，Lines 91.72%；按 CI 默认端口确认服务属于本工作树后，完整关键路径浏览器 260 项通过。完整 lint、vue-tsc、生产构建及品牌/安全上下文断言、actionlint、error-level shellcheck、文档构建与 high/critical audit 均通过；提交后继续核对远端 CI。

### 2026-10-28（取消队列清理与当前轮结束落库顺序）

- 日期：2026-10-28（Asia/Hong_Kong）。
- 范围：`server/internal/chatsession/{session,session_test}.go`。
- 做了什么：取消时在回合切换锁内、状态锁外先完成 `OnDropped`，再通知当前上下文与原生 CLI 取消；广播仍在取消之后。恢复待发消息清理先于取消触发的当前轮结束处理，同时继续保护原生取消不会误中下一轮，清理回调仍可读取会话快照。
- 为什么：PR #1031 的远端核心竞态检查发现 `TestPmTurnCancelStopsAndDropsQueue` 中 PM 队列取消落库与当前轮取消落库重叠，测试 SQLite 报表锁并留下错误的消息状态。没有将其当作偶发失败重跑，也没有修改数据库测试设置或放宽断言；生产 SQLite 的单连接设置没有变动。
- 如何验证：新增确定性用例先在旧顺序下以“队列清理前当前轮已取消”复现失败。修复后新增用例与原生取消目标回归各重复 50 次 race 通过，原失败 PM 用例重复 50 次 race 通过；chatsession/services 完整 race 通过。Server 全量 lint（0 issues）、vet、配置文档检查、单测、runtime 专项通过，核心覆盖率 90.5%，diff 检查通过；提交后继续核对最新提交的远端 CI。

### 2026-10-28（聊天恢复 PR 的主线集成检查）

- 日期：2026-10-28（Asia/Hong_Kong）。
- 范围：`web/src/components/agent/CliModelSelect.test.ts`、`web/src/components/workspace/ModelConfigurationManager.test.ts`。
- 做了什么：将未知 OpenCode 模型家族的旧字母占位断言更新为精确的官方 OpenCode SVG 资产，继续分别核对 Gemini、Claude 家族图标以及明暗主题规则。
- 为什么：聊天恢复 PR #1031 创建期间主线合入官方 OpenCode 模型支持，GitHub 合并结果中的两项旧测试仍要求 monogram，但实际新契约使用服务商图标回退。没有改变产品图标实现或放宽家族区分，也没有修改 CI 契约。
- 如何验证：两文件 70 项测试、全 Web 类型检查及定向 ESLint 通过；合入最新主线后全量 536 文件、5,767 项单测通过，Lines 91.77%，完整 lint 与生产构建及品牌/安全上下文断言通过。更新后的 GitHub CI 继续核对。

### 2026-10-28（并发聊天状态一致性与历史复核）

- 日期：2026-10-28（Asia/Hong_Kong）。
- 范围：`server/internal/chatsession/`、`server/internal/engine/{review_session,resume,chat_identity_test}*`、`server/internal/models/models.go`、`server/internal/handlers/{ws,dto,inbox_context,react_sessions_dto}*`；`web/src/lib/{chat,inbox,run,shared,solo,pm}/`、聊天宿主与消息可靠性浏览器用例、`web/src/components/agent/AgentChatTester*`、`CHANGELOG.md`。
- 为什么：复核 `d26f472a`、`2882fbea`、`752dea81`、`8a96dc01`、`f4ac5af9`、`4ca6c74b` 等历史修复后，确定已有修补分别解决丢帧、恢复文本与乐观消息，却没有定义队列状态跨发布协程的顺序。确定性复现 A/B/C 全部完成后 B 的早期队列快照最后发布，连接健康时也会恢复残影；同时复现空闲会话回收与入队竞争产生两个执行队列，以及取消发布被阻塞后原生取消误中下一轮。
- 做了什么（后端）：状态锁内分配会话代次与递增 revision，队列、开场和结束帧都带完整队列与当前请求身份；发布仍在状态锁外执行。Run/Inbox 快照在读取会话表前记录代次水位，阻止新连接接受已退休会话的迟到帧；重连头部只发给该连接。共享流序号与完整订阅者投递串行化。注册表租约保护查找至入队，孤立取消同样占有租约；原生取消与切换当前轮共享边界锁。WebSocket 请求拒绝只回复来源连接的非终结事件。
- 做了什么（前端与历史）：发送 UUID 贯穿实际组件、HTTP/WS、当前项、等待项与持久化人类/Agent/交接分段，重复文字按身份区分。工作流宿主在修改 busy 前过滤旧版本，对话终结帧按完整队列对账，重连按新连接快照重置版本。历史没有身份时保留兼容匹配。审批、Solo、PM 和 Agent 测试入口同步处理共享控制器的版本顺序，避免迟到的取消队列覆盖结束状态。
- 如何验证：新增 Go 用例在旧实现上复现并发旧快照与投递倒序，实际多协程队列/取消/订阅者、相同文本持久化与多窗口拒绝隔离用例通过重复竞态检测。Server 全量 lint、vet、配置文档校验、单测、runtime 专项与 race 通过，核心覆盖率 90.5%。Gate 的终结先于取消队列、新当前项先于开场帧三项回归先复现失败再修复通过；Solo/PM/Studio 同步验证迟到队列、服务端重启及序号复用。Web 最终 536 文件、5,761 项单测全部通过，Lines 91.76%；lint 0 errors（12 条既有 warnings）、类型检查、生产构建和品牌/安全上下文断言通过。完整关键浏览器回归 259/259（5.5 分钟）及额外可靠性 5/5 通过，覆盖同文 UUID、不同代次旧帧、终结丢失、无 close 的半开连接、恢复最终用量和不重载续聊。旧 Inbox/Panel 测试桩改用真实的独立 finish 事件与明确 UUID，保留强制流转和消息身份断言。文档构建/审计、actionlint、error-level shellcheck 和 diff 检查通过；生成物未入库。
- 验证边界：浏览器用例使用本地模型/事件夹具，没有给线上工作流发送、取消、确认或重试，也没有触发付费推理。变更尚未部署。测试覆盖明确的并发交错，不能据此承诺所有网络故障下恰好一次执行；请求已接收但全部确认丢失后的人工重发仍需另行设计幂等策略。

### 2026-10-28（工作流聊天断线恢复、队列与用量对齐）

- 日期：2026-10-28（Asia/Hong_Kong）。
- 范围：`server/internal/handlers/{ws,gate_share_public_events,ws_heartbeat_test}.go`；`web/src/lib/run/{wsHeartbeat,wsReconnect,useRunLive,useRunDetailWs}*`、`web/src/lib/inbox/{useGatesInbox,useClarifyChat,mergeClarifyLiveTurns}*`、`web/src/components/run/ClarifyChat.test.ts`、`web/src/views/PublicGateApprovalView.vue`、`web/e2e/{run-live-continue.spec,message-reliability.spec,message-reliability-main}.ts`、`web/LIB_DOMAIN_MAP.json` 与 `CHANGELOG.md`。
- 做了什么：工作流与公开审批连接每 25 秒发送可被浏览器观察的心跳；服务端以 pong 续期，客户端在 60 秒无任何帧、联网恢复或挂起后唤醒时检测失效连接。关闭旧连接前先解绑身份，重建连接显式清除主动关闭标记而保留重试退避，修复握手失败后不再重试。忙碌续聊仍建立连接，重连按平台队列恢复正在执行与真正等待的消息，并重新读取当前输出；较早 HTTP 读取不能覆盖新实时轮次。续聊接收 ACP 正文，下一节点仍在 entering 时继续等待，完成后按原始逐轮事件恢复用量，不把节点累计 token 附在单条回复上。
- 做了什么（对话合并）：持久化历史接管较早轮次后，保留随后由服务端启动的完整人类/Agent 回合及连续的全部 Agent 分段，避免只剩停止按钮、后续内容被合并逻辑吞掉。已封口但仍标记 handoff 的中间分段不能证明整轮完成；裁剪历史时等待该人类请求的最终分段，保留其仍在生成的思考以及后续排队用户。宿主追加暂存预览标注时按原始标注前缀关联已发送消息；重复开场帧先移除属于自己的待发送项，再保留已经收到的正文，真实的后续同文消息继续排队。
- 为什么：仅依赖浏览器 close 事件、聊天忙时跳过连接、握手失败消费主动关闭标记，以及只保留本地乐观回合，均可能让工作流聊天需要整页刷新才能恢复。线上只读核对同时区分了两种现象：已完成且保存用量的消息仍显示本地队列残影；真实忙碌时下一条确实排队，但当前轮思考内容没有显示。没有向线上工作流发送、取消、重试或确认消息，没有改动沙箱镜像和发布契约。
- 如何验证：队列及分段裁剪回归先复现后修复。Server 全量 lint、vet、配置文档校验、测试、runtime 专项及心跳/事件 handler race 通过，核心覆盖率 90.4%。Web 全量 536 文件、5,722 项单测通过，Lines 91.73%；lint 0 errors（12 条既有 warnings）、类型检查、生产构建及品牌/安全上下文断言通过。最终完整关键浏览器回归 259/259（5.5 分钟），额外消息可靠性 4/4，覆盖真实 UI 排队、持久化中间分段、继续思考与无需重载的后续输出。首轮既有凭据向导出现导航超时，未改其源码或断言，独立重复 3/3 与最终完整回归均通过。文档构建、官方源高/严重审计、actionlint、error-level shellcheck 和 diff 检查通过；文档生成目录清理后品牌断言通过，生成物未入库。没有向用户工作流插入请求，也没有触发厂商模型费用。

### 2026-10-28（Chat 展开过程后仍按段折叠思考）

- 日期：2026-10-28（Asia/Hong_Kong）
- 范围：`web/src/components/run/AgentTimeline.vue`、`web/src/components/run/{AgentTimeline,ClarifyChat,collapsedReplySpacing}.test.ts`、`web/TimelineHarness.vue`、`web/e2e/{agent-tool-timeline,chat-message-info}.spec.ts`、`CHANGELOG.md`。
- 做了什么：展开已结束回复的「用时」后，每段思考改为默认收起的「思考过程」行，复用现有思考折叠样式；各段独立展开，外层收起再打开保留选择，「全部展开」切换会重新同步。完成后的展开状态与流式状态分开，开始下一条流式回复时清除旧选择，最终回复继续显示在过程外。保留最新共享消息元数据、紧凑间距与长 URL 换行；移除不再使用的平铺思考样式及对应断言，并同步现有布局测量页的选择器。
- 为什么：外层过程展开后，思考全文直接铺开会与工具摘要挤在一起，长聊天难以浏览；需要保留逐段查看的层次。
- 如何验证：基于最新主分支，Web 全量 535 个文件、5,694 项测试通过，Lines 91.71%；回归覆盖默认收起、逐段切换、外层重开、新旧传输数据的流式切换、全部展开和长文本完整保留。完整关键浏览器套件初跑 255 项通过，剩余 1 条旧平铺断言调整为逐段折叠后，两个相关 spec 共 7 项复验通过，覆盖 Solo/工作流、明暗主题、点击、Enter/Space、390px 窄屏及工具/产物交互，并核对截图。ESLint 0 errors（12 个既有 warnings）、Vue 类型检查、生产构建、Secure Context/品牌断言、actionlint、error-level shellcheck、docs 构建及官方源高/严重漏洞审计通过。

### 2026-10-28（OpenCode 思考输出与 PR 验证修复）

- 日期：2026-10-28（Asia/Shanghai）。
- 范围：`web/src/components/{solo,run}/`、`web/src/lib/solo/useSoloChat*`、中英文文案、共享聊天浏览器回归、`server/internal/sandbox/worktree_create.go`、`server/internal/engine/clarify_visit_ended_test.go`、`sandbox-gateway/sandbox/internal/{agents,provider/modelconnection}/`、`sandbox-gateway/scripts/agent-platform-wire-fixture*`、免费 Agent CI、`AGENTS.md` 与 `CHANGELOG.md`。
- 做了什么：移除空 Solo 的欢迎头像、就绪说明和仓库占位，以及模型/思考强度的下一轮生效提示；切换保存、跨标签同步、逐轮执行快照和真实错误继续保留。共享 timeline 在流式状态边界清除展开状态，完成后收起过程详情，仍能手动展开且最终回复始终可见。浏览器时间断言分别核对两个已接受的用户请求和未完成的 Agent 回复，兼容排队消息可见性。平台 wire fixture 只按白名单真实读写固定的 checkpoint 文件，完全移除模型字符串的 shell 执行；Go 环境收集去掉不必要的长度加法预分配，保留非 nil 空切片，避免环境继承语义变化。
- 做了什么（OpenCode）：JSON 模式显式添加原生 `--thinking`，让模型已经返回的思考通过 parser 进入聊天事件；模型声明的 reasoning true/false 原样写入原生配置，未知能力仍省略。没有额外开启模型推理模式或修改请求强度。
- 做了什么（取消回归）：两个 turn-start drop 测试等待第一条消息真正进入 held provider 后再排队、取消，避免只观察 busy 状态时第一条也被丢弃、误取其无 clientMessageId 的通知。保留排队消息身份和错误契约断言，没有增加固定延时或放宽超时。
- 为什么：处理本地体验反馈及 GitHub 浏览器/CodeQL 首轮失败，保持相同工具切换、上下文与安全测试要求，不屏蔽告警、不放宽断言和覆盖率门槛。
- 如何验证：Web 相关 7 文件、284 项和共享聊天/时间浏览器 14 项（两次重复）通过，类型检查通过；只读检查真实 DSH 的 1m31s 回复，21 个思考块和 22 次工具调用均在可折叠过程内，最终正文独立，首次加载和重载均收起。Server 全量 lint、vet、配置文档检查、测试、运行时专项通过，核心覆盖率 90.4%，相关 worktree/SoloRoute race 通过。Mock 协议与新增安全用例 39 项、矩阵契约 7 项通过；禁外网的 Cursor→Trae→Cursor 三轮真实服务/历史 MCP/文件续接通过。GitHub 首轮实际 artifact 确认完整矩阵 62/62、42 个有向切换、9 次强度、10 次模型切换及零付费调用；server、免费 Agent suite 和包含完整镜像的 sandbox CI 均成功。actionlint、error-level shellcheck 和差异检查通过。
- 如何验证（OpenCode）：已安装的真实 OpenCode 1.19.2 对本地免费 SSE，reasoning 能力缺省/true/false 三组分别对比：原先均为 0 个 thought，加参数后均收到正确的思考事件，实际 reasoning_effort=max 不变，未新增 thinking/enable_thinking 参数。原生 provider 子进程到 `agent_thought_chunk` 的六路回归与 race 通过；Sandbox 全量 Go、lint、vet、核心覆盖率 96.6% 通过。未向用户会话插入消息或调用付费模型。
- 如何验证（取消回归）：两个测试各重复 200 次、合计 400 次 race 通过；修正测试同步而非放宽 clientMessageId 断言。

### 2026-10-28（免费全工具切换 CI 与共享聊天详情）

- 日期：2026-10-28（Asia/Shanghai）。
- 范围：`.github/workflows/ci-agent-e2e.yml`、`AGENTS.md`、`sandbox-gateway/scripts/agent-*` / `test-agent-switching.py` / `mock-agent-model*`、server 的 MCP、运行时与聊天记录、sandbox provider、共享聊天组件及 Solo/工作流/Agent 测试/PM 调用方、中英文帮助与 `CHANGELOG.md`。
- 做了什么：新增不读取厂商 Key 的真实服务端、运行时和原生 CLI 集成 CI，使用本地 Chat Completions、Responses、Anthropic Messages mock；七个产品后端按能力 API 严格核对测试清单，覆盖全部 42 个有向切换、9 次推理强度切换、10 次主/辅助模型切换及初始轮，共 62 轮同会话、同工作区。每次续聊必须收到先前原始要求、调用真实授权的历史 MCP、读取上轮文件进度；历史分页根据真实游标回到原始 user 消息，不接受模型回显或嵌套工具结果充当原文。请求侧核对实际模型和强度，并检查重载后的逐条执行信息。五个可配置工具使用真实 CLI；Cursor/Trae 因平台私有协议使用明确标记的 CLI wire fixture，不声称原生厂商推理通过。未来新增/删除产品后端必须同步矩阵、安装、MCP 和推理覆盖，缺项不能跳过为成功。
- 做了什么（聊天）：Solo、工作流、Agent 测试与 PM 统一到 `AgentTimeline`、`ChatUserMessage` 和共享 `ChatMessageMeta`，恢复时间悬停并提供支持鼠标、键盘、触屏的信息浮层；逐条保存实际 Agent、编码工具、模型组、模型、强度、开始时间、本轮用量和耗时，保留服务商/估算来源，未知历史不按当前选择补猜。模型和凭据图标共用透明背景规则，检查 26 个品牌的明暗主题与交互尺寸。
- 为什么：仅检查下拉选中、CLI 启动或模拟回复不能证明实际模型参数、原生 MCP 与上下文接续。真实测试发现并修复 OpenCode variant 参数层级导致上游始终 medium、跨组 DSH 强度校验误读旧工具、Codex 原生 MCP 配置与只读注解缺失、可恢复诊断提前触发失败、stream-json 最终结果正文丢失，以及增量用量与最终总量重复累计；共享聊天入口原先存在时间、用量和布局分叉。
- 如何验证：本地隔离、禁止外网的运行环境中，62 场景完整通过，其中真实 CLI 之间的全部 20 个有向切换完成；模型、思考强度、默认恢复、实际 MCP 事件、原始历史分页、文件和执行快照均有请求与 API 证据。另以真实 CodeBuddy 比对 mock 请求数、CLI 最终计数和持久化用量，三轮完全一致；OpenCode 的 OpenAI 和 TokenHub 身份均以本地 mock 验证默认→low→high→默认的实际请求。五个 CLI 的独立临时安装通过。Server 全量 lint、vet、配置文档检查、Go 测试、运行时专项与相关 race 通过，核心覆盖率 90.4%；Sandbox 全量 lint、vet、Go 测试、相关 race 通过，核心覆盖率 96.6%。Web 全量 535 文件、5,622 项通过，Lines 91.57%，lint 0 errors（12 条既有 warnings）、类型检查、生产构建、品牌与安全上下文断言通过；最终完整关键浏览器回归 255/255（9.1 分钟），26 品牌双主题与共享消息鼠标/键盘/触屏 12/12 通过。较早全量中的既有配置页偶发超时，经不改源码的独立重复 6/6 与最终安静环境全量重跑通过，未放宽断言或阈值。Mock 协议新旧 37 项与矩阵防漏/清理契约 7 项通过。docs 构建与官方源审计、actionlint、error-level shellcheck、diff 检查通过。本轮没有真实厂商推理费用，没有提交凭据或改动镜像发布契约。

### 2026-10-28（worktree 真实工具接续与原生历史 MCP）

- 日期：2026-10-28（Asia/Shanghai）。
- 范围：`server/internal/{sandbox,services}/`、`sandbox-gateway/sandbox/internal/provider/{streamjson,modelconnection}/`、`web/src/lib/solo/useSoloChat.ts` 及回归、模型路由设计与 `CHANGELOG.md`。
- 做了什么：本地 worktree 模式切换工具时只替换 backend 进程，保留原目录、文件和 launch 持有关系，成功或回滚后保存实际 ACP 端口；最终关闭才释放目录并停止最新 CLI 子进程，宿主其他模型账号不会被继承为所选授权。CodeBuddy/Claude 的严格 MCP 参数读取实际 `CONFIG_ROOT/mcp.json`；OpenCode 连接配置保留当前目录的受管 MCP 与 instructions，继续隔离其他 provider、plugin 和账号设置。Solo 保存模型组、模型或推理强度后向已连接视图广播完整选择，不打断当前轮；忙时保留下一轮生效提示，较早发起的详情请求不能覆盖通知。
- 为什么：真实本地测试暴露出容器路径假设、重启 backend 误释放 worktree、原生 MCP 读取默认目录和其他标签页选择滞后的问题。模型能复述交接内容仍不足以证明原生历史工具已经加载，需要同时验证文件、实际工具事件与多个浏览器视图。
- 如何验证：使用单独的本地数据库、临时数据目录、真实运行时包和 TokenHub 连接，第一阶段 OpenCode → DeepSeek Harness → CodeBuddy → OpenCode/辅助模型四轮真实回复成功，始终使用同一 Solo 会话、沙箱记录和工作目录，保留暗号文件、功能约束及中途新增的按钮圆角要求。定位并修复原生 MCP 缺口后，最终同一会话中 OpenCode → CodeBuddy → DeepSeek Harness 三轮全部成功；原生 `get_history` 分别完成 1、1、3 页，末轮回查至首条用户消息，未使用 curl 或读取凭据替代，原文件保留四行暗号和接续标记。真实双标签页选择主/辅助模型立即同步，模型组名称保存与恢复也无需刷新；浏览器与 API 无错误。Server 全量 lint、vet、配置文档检查、Go 测试、运行时专项通过，核心覆盖率 90.4%；sandbox 全量 lint、vet、Go 测试与专项 race 通过，核心覆盖率 96.6%；规定 shell/node 语法、inject、Git 授权、runtime bootstrap、preview-inject 与 mock chat smoke 通过。Web 全量 534 文件、5,613 项通过，Lines 91.55%，关键 Playwright 249/249（9.2 分钟），lint（0 errors、12 条既有 warnings）、类型检查及生产构建通过；选择通知、忙时提示和旧详情请求覆盖的专项回归通过。docs 构建、官方源审计、actionlint、error-level shellcheck 与 diff 检查通过。真实凭据仅进入本地测试数据目录，未写入仓库；未变更镜像、IMAGE_LEVEL 或工作流发布契约。

### 2026-10-28（Agent 多模型组与跨工具上下文接续）

- 日期：2026-10-28（Asia/Shanghai）。
- 范围：`server/internal/{modelrouting,models,services,handlers,mcp,runtime,sandbox}/`、`server/cmd/server/`、`web/src/components/{agent,solo,canvas/inspector}/`、`web/src/lib/{agent,api,solo}/`、中英文文案、模型路由浏览器回归、配置帮助与 `CHANGELOG.md`。
- 做了什么：Agent 的模型绑定保留默认 `configurationId`，增加包含默认组的有序 `configurationIds`，支持不同 CLI 的多组选择；默认配置、继承、创建、团队、初始化和导入导出保留完整绑定。Solo 初始、请求与实际应用的路由分别保存模型组 ID，同名模型按组区分账号。每轮边界在原沙箱内重载工具、配置与授权，保留仓库快照、工作目录和未提交改动；组内辅助模型目录也参与指纹，失效路由要求重新选择。切换前持久化历史交接标记，取消、响应丢失或回滚后的重试仍补入上下文；历史读取失败阻止空白续聊。artifact-store 新增只读 `get_history`，从当前沙箱代次定位原始 Solo 消息，提供固定快照分页、附件引用和可见工具结果；交接携带覆盖会话的用户要求与更正记录、近期对话，并标明预算省略范围。历史查询自身的结果正文不再嵌入下一次历史，避免递归膨胀。成功保存模型、账号或 Agent 绑定后通过页内版本和跨标签通知重读目录，失败读取保留选择，其他用户刷新后读取最新配置。
- 为什么：原来 Agent 只引用一个组，选择器与沙箱白名单无法覆盖其他工具或账号；原生 CLI 会话重建后需要已持久化的任务记录接续。模型组 ID、执行环境和上下文交接必须一起处理，避免界面选中与实际账号不一致，或使用过期需求继续执行。
- 如何验证：模型组、实际路由、原工作区保留、分阶段失败回滚、旧会话固定默认组、忙时切换、模糊响应重试、历史读取失败、分页授权、中段需求更正与历史查询防递归专项通过；相关服务、运行时和沙箱竞态测试通过。Server golangci-lint 0 issues、go vet、配置文档检查、隔离 Git 配置及 ASKPASS 的 `go test ./... -p 2`、`TestRunAgent|TestReact` 通过，核心覆盖率 90.4%。Web 全量 534 文件、5,610 项测试通过，Lines 91.55%，关键 Playwright 249/249 通过；类型检查、ESLint（0 errors、12 条既有 warnings）、生产构建与安全上下文/品牌断言通过。首次浏览器全量中一项既有移动端弹层断言失败，单独重跑与最终完整重跑均通过，未改无关逻辑。docs 构建与官方源高/严重漏洞审计、actionlint、error-level shellcheck、diff 检查通过。未调用真实厂商账号，未修改沙箱镜像或工作流发布契约。

### 2026-10-28（修复已接受消息的排队与实时对账）

- 日期：2026-10-28（Asia/Shanghai）。
- 范围：`web/src/lib/inbox/useClarifyChat.ts`、`mergeClarifyLiveTurns.ts`、`useGateApproval.ts`、`web/src/lib/solo/useSoloChat.ts`、RunDetail/LLM 实时接线、Gate 对话组件及相关回归；`CHANGELOG.md`。
- 做了什么：每次发送保留独立本地身份并只绑定一次服务端回合；处理 HTTP 确认晚于实时帧、同文及纯图片排队、删除/编辑/取消与拒收。当前回答的 ACP 分段在待发用户行之前插入。Gate 按回合保存用户与回答，预览意见持久化后去掉本地重复用户行。LLM 视图接入真实组件引用，并沿用快照恢复、缓冲与重连种子链路。成功重试可覆盖较长的历史错误提示。
- 为什么：PR #1021 的乐观消息仅以正文查重，造成同文串轮、重复消息、被取消消息残留、输出分段丢失和 Gate 回复错配；新增 LLM 引用没有绑定页面组件。
- 如何验证：Node 24 下 Web ESLint 0 errors（12 条既有 warnings）、vue-tsc、生产构建及 Secure Context/品牌断言通过；全量 530 个文件、5,660 项单测通过，Lines 91.67%（37660/41078），高于 85% 门槛。源码冻结后 critical Playwright 248/248 通过；早先开发中运行受 Vite 热更新影响的导航/拖拽失败未在最终重跑复现。docs build/audit、根 actionlint 与 error-level shellcheck 通过；生成的 docs/public 已清理，未入库。

### 2026-10-28（补齐工作树 Agent 的 Git 授权）

- 日期：2026-10-28（Asia/Shanghai）。
- 范围：`server/internal/sandbox/{worktree_create,worktree_git,inject_bundle}.go`、`server/internal/execution/auth.go`、`server/internal/runtime/acp_run_agent.go` 及相关回归；`CHANGELOG.md`。
- 做了什么：工作树 backend 启动前安装当前 Agent 选中的 Git catalog，把 Git、SSH、gh/glab 指向每个 backend 独立的私有配置目录，平台本地命令也使用同一环境；启动失败或 backend 关闭时清理凭据文件。自动 GitLab MR 操作读取会话内的 helper 路径，沙箱仍使用原默认路径；克隆凭据缺失时不调用宿主 askpass。沙箱打包与工作树安装共用固定文件名的 catalog 生成逻辑，工作树直接写入文件，不再解包 tar；Git 配置命令直接使用私有目录生成的路径。
- 为什么：工作树模式原先只在 `execution.PrepareAuth` 的 clone/fetch 期间临时使用所选凭据，实际 Agent 执行 `git push` 时未获得这些凭据；工作区已有 GitHub 授权仍会失败。不能通过修改宿主全局配置修复，否则并发节点会共享身份。
- 如何验证：真实 HTTP 认证 clone → Agent `git push` → 平台本地 `git push` 通过；同一 run 的不同节点与空授权不串用，仓库范围限制、gh/glab 私有配置、legacy 授权、启动失败/结束清理和仓库本地作者优先级均有回归。宿主 askpass 即使能提供有效凭据，空授权 clone 仍失败且不调用它。GitLab MR 回归实际执行 shell，覆盖默认、空值和含空格/单引号的 helper 路径；catalog 回归确认含 `../` 的账号元数据不影响生成路径，本地文件与沙箱打包内容、换行和权限一致。两路独立 review 复核通过；基于最新 main 用 Go 1.26.10 重跑 Server 全量 `go test ./...`、`go vet ./...`、golangci-lint（0 issues）、配置文档检查、核心覆盖率 90.4%（门槛 90%）、`TestRunAgent|TestReact` 通过；最终工作树回归 `-race` 通过。actionlint、error-level shellcheck、docs Node 24 build/audit 与 `git diff --check` 通过。

### 2026-10-28（保留重连尾轮正文与订阅快照）

- 日期：2026-10-28（Asia/Shanghai）。
- 范围：`web/src/components/agent/AgentChatTester.vue`、`web/src/lib/solo/useSoloChat.ts` 及相关回归；`CHANGELOG.md`。
- 做了什么：Studio 在同一次重连 flush 里，若开放尾轮已经收到实时输出，同 id 的 `turn_begin` 复用该尾轮，不再把回复槽换成空正文。Solo 在应用订阅快照时推进会话代际，更早发起的 `getSoloSession` 返回后不再整行覆盖状态和模型。
- 为什么：事件日志已恢复的开放尾轮，会在 `queue_state`、输出帧和 `turn_begin` 同批刷出时被 `turn_begin` 清空；连接打开时发起的较慢会话读取也会把已应用的关闭状态和模型盖回旧行。
- 如何验证：同一次 held flush（`queue_state`、ACP「续写」、`turn_begin`）之后，B 同时保留「B 部分回复」和「续写」，A 的最终回复仍在且不重复。订阅快照应用后，第二次 `getSoloSession` 以 running/old 完成，status 仍为 closed、模型仍为 model-b。消息链路 11 个文件、444 项通过。Server golangci-lint 0 issues、go vet、配置文档检查、`go test ./... -count=1 -p 2`、核心覆盖率 90.4%、`TestRunAgent|TestReact`、chatsession/handlers `-race` 通过。Web ESLint 0 errors（12 条既有 warnings）、vue-tsc、全量 528 个文件、5,584 项单测通过，Lines 91.48%（37222/40687）；生产构建含 Secure Context 与品牌断言通过。message-reliability Playwright 3/3 与 critical 248/248（6.1 分钟）通过。

### 2026-10-27（补齐同文回复、重连尾轮与阻塞写）

- 日期：2026-10-27（Asia/Shanghai）。
- 范围：`web/src/lib/inbox/useClarifyChat.ts`、`web/src/lib/pm/usePmLeaderChat.ts`、`web/src/lib/solo/useSoloChat.ts`、`web/src/components/agent/AgentChatTester.vue` 及相关回归；`server/internal/handlers/chat_subscription.go`、`pm.go`、`solo.go`、`sandbox.go`、`server/internal/services/solo.go`；`CHANGELOG.md`。
- 做了什么：Clarify 只把不同提问上的旧回复快照视为外来内容，同文同回复在流式、turn_done 和快照未到的 idle 期间都保留；历史对账只看当前锚点之后的后缀。Studio 按问题 ID 删除待回放尾轮，空闲重连时把游标对齐事件日志里仍开放的尾轮，并拆开重连与更早分页的代际。共享 WebSocket 写泵在订阅关闭时不再等待阻塞写返回，数据写有截止时间，PM、Solo 和 Studio 都先退订并关闭连接再等待写泵。Solo 订阅首帧带上状态和模型快照，重连时再拉一次会话。PM 在同一连接上重试失败的冷启动 idle 对账。
- 为什么：评审探针证明同文同回复会被当成旧快照丢掉，Studio 会按文本截掉上一轮或把断线期间新开始的回合画两次，而写阻塞时关闭订阅 channel 并不能让连接进入重连。
- 如何验证：评审四项探针与三项中等缺口的回归已补上并通过。Clarify 同文同回复在 idle 先于快照时保留两轮；Studio 同文滞后不截掉旧轮，断线后 A 完成、B 开放且 ACP 先于 turn_begin 时不写到 A。该轮断言停在 `turn_begin` 之前，也把第二次 `getSoloSession` 挂起，因此没有覆盖同一次 flush 清空正文和较慢会话读取覆盖快照，这两项见 2026-10-28 条目。写阻塞时关闭订阅，泵在 250ms 内退出并关闭连接；Solo 订阅首帧恢复状态与模型；PM 同一连接上冷启动失败后下一帧 idle 重试；Studio 重连与更早分页使用独立代际。定向 Vitest 5 个文件、293 项通过。Server golangci-lint 0 issues、go vet、配置文档检查、`go test ./... -count=1 -p 2`、核心覆盖率 90.4%、`TestRunAgent|TestReact` 及 chatsession/handlers `-race` 通过。Web ESLint 0 errors（12 条既有 warnings）、vue-tsc、全量 528 个文件、5,583 项单测通过，Lines 91.48%（37208/40672）；生产构建含 Secure Context 与品牌断言通过（未跟踪的 `docs/public` 残留已删除，未入库）。message-reliability Playwright 3/3 与 critical 248/248（6.3 分钟）通过。

### 2026-10-27（修复全入口消息末尾可靠性）

- 日期：2026-10-27（Asia/Shanghai）。
- 范围：`web/src/lib/inbox/`、`src/lib/run/`、`src/lib/pm/`、`src/components/run/`、`src/components/agent/`、`web/e2e/message-reliability*`；`server/internal/chatsession/`、`server/internal/handlers/`；`CHANGELOG.md`。
- 做了什么：Clarify 以当前历史锚点限制同文轮次合并，并覆盖无 `turn_begin` 的 busy 恢复；Inbox 在单飞期间合并保留一次末尾刷新；RunDetail 以请求代际隔离快照、loading、异步对话投影和 artifacts，PreviewFeedback 隔离旧响应并按 ID upsert 已落库 issue。PM 在冷启动 idle 时有界重读历史并隔离跨轮 finalizing。Studio 在重连后按事件历史恢复 idle 完成态，以稳定 opId 复用迟到的活跃尾轮，同时保留 earlier 前缀与非空旧日志、重定位活动游标，并兼容无 opId/尚无输出的尾轮。慢订阅无法在有界等待内交付时关闭订阅及 WebSocket，由现有断线恢复收敛，不再静默丢弃尾帧。
- 为什么：发送成功但实时帧、刷新或请求响应发生竞态时，界面会漏消息、重复同文回合或被旧快照覆盖，只能整页刷新才能看到持久化结果。
- 如何验证：新增回归先复现同文合并、末尾刷新、旧响应覆盖、PM/Studio idle 与慢订阅尾帧失败；消息链路定向 Vitest、Go 专项及 targeted race 通过。新增 3 项浏览器竞态与 45 项既有消息链路 Playwright 通过；Server 全量 lint/vet/configdoc/test/coverage/runtime 通过（核心覆盖率 90.4%）。Web lint/typecheck/build 通过，全量 528 个文件、5,577 项单测通过，Lines 91.50%；完整 critical Playwright 248/248 通过，docs build/audit 与根 actionlint/error-level shellcheck 通过。

### 2026-10-27（仓库只保留克隆配置）

- 日期：2026-10-27。
- 范围：`server/internal/models/solo.go`、`server/internal/services/{project.go,solo_repos.go,sandbox_solo.go,sandbox_agent.go}`、`server/internal/database/project_runtime.go`、`server/internal/sandbox`、`sandbox-gateway/sandbox/scripts/{startup.sh,reprime.sh,git-credentials.py}`、`web/src/components/solo/ProjectForm.vue`、`web/src/views/ProjectsView.vue`、`web/src/lib/{solo,api}`、`docs/content/{help,en/help}/configuration.md`、`docs/dev/agent-model-routing-design.md`、`CHANGELOG.md`。
- 做了什么：仓库登记和 Solo 单仓/多仓快照只保留名称、描述、地址、分支、目录及原有元信息。启动迁移幂等删除 projects 旧列，并清掉两类会话 JSON 里的访问方式、凭据引用、凭据绑定和环境变量，不写入任何 Agent。Solo 新建和重启只注入当前有效 Agent 的环境、通用绑定和 Git catalog，以及 GIT_REPOS 克隆清单。沙箱不再打包或消费 ssh/repos 显式授权，仍会清掉旧的 repo-credentials 与 grasp-repo 文件。仓库表单和列表去掉这三个区域，地址占位只保留 `https://github.com/acme/app.git`。
- 为什么：权限和运行环境应由 Agent 统一维护。旧仓库配置如果留在快照里，重启会话时会重新盖过 Agent。
- 如何验证：迁移回归覆盖旧列、单仓/多仓快照、空值和重复执行；原始 HTTP JSON 提交旧字段后不回显、不落库；Solo 新建与重启断言仓库来源环境为空且 GIT_REPOS 仍在；bundle 不再包含 repos/；startup/reprime 与 git-credentials.py 回归确认旧显式授权被清掉且 Agent catalog 仍可用。server：golangci-lint 2.12.2 无问题，go vet、gen-configdoc -check、`go test ./...` 与 runtime `TestRunAgent|TestReact` 通过，核心覆盖率 90.4%。web：528 个测试文件、5556 项通过，Lines 91.55%；ESLint 0 errors（12 条既有 warnings），vue-tsc 与生产构建通过。sandbox：lint/vet 无问题，核心覆盖率 96.6%；bash -n、inject、git-auth、runtime-bootstrap、preview-inject 与 mock chat 8 项通过。docs 构建与高/严重漏洞审计通过。actionlint 与 error-level shellcheck 通过。未跑完整 Playwright 套件。

### 2026-10-28（DSH 每轮 token 用量接入统计）

- 日期：2026-10-28（Asia/Shanghai）。
- 范围：`sandbox-gateway/sandbox/internal/provider/deepseek/{deepseek.go,usage.go,usage_test.go,deepseek_test.go}`、`sandbox-gateway/sandbox/internal/acp/panel.go`、`server/README.md`、`sandbox-gateway/sandbox/README.md`、`CHANGELOG.md`。
- 做了什么：给 ACP `Panel` 增加可选的 `PromptUsage` 钩子，在 `session/prompt` 返回后把本轮用量写进 `prompt_done.usage`；DSH provider 读取固定版本 dsh 的 `storages/session_projcache/sessions/<sessionID>.json` 中 `tokenUsage` 投影（uncachedInput/output/cacheRead/cacheWrite），按轮前后累计值差分后上报，并对 `turn/end` 的异步落盘做短重试；缓存缺失、回退重建、畸形 JSON 或非法 session id 时保持省略 `usage` 的旧帧结构。`ReportsUsage` 改为 true，`CumulativeUsage` 仍返回空（投影不区分模型，跨模型复用会话无法正确归属）。
- 为什么：dsh 的 ACP 响应只带 `stopReason`，`usage_update` 只是上下文占用，因此 Grasp 的 token ledger 对 DSH 一直是 0；而 dsh 自身已经把精确的四桶累计写入投影缓存，应直接复用，不能再让用量分析漏掉 DeepSeek Harness。
- 如何验证：新增 usage tracker 单测覆盖按轮差分、异步落盘重试、无变化、缓存回退、畸形/缺行/负值、非法 session id 与空模型回退；DSH 子进程夹具按真实缓存路径写入累计用量，断言每轮 `TurnResult.Usage` 与 `prompt_done` 帧；另用当前会话的真实缓存跑过一次解析，发现并修复了“其他投影行的 `val` 是字符串/null 时整块反序列化失败”的问题，夹具已加入同类兄弟行。`go test ./...`（`sandbox-gateway/sandbox`）全绿，`go test ./internal/provider/deepseek ./internal/acp` 通过。`server/README.md`、`sandbox-gateway/sandbox/README.md` 与 `CHANGELOG.md` 已同步说明统计口径及子代理限制。

### 2026-10-27（修复尾部「思考中…」横向对齐）

- 日期：2026-10-27。
- 范围：`web/src/components/run/ClarifyThinkingRow.vue`、`web/src/components/run/ThinkingBusyCue.test.ts`、`web/src/components/run/collapsedReplySpacing.test.ts`、`CHANGELOG.md`。
- 做了什么：仅为 `variant=status` 且非输出中的尾部思考行增加 4px 水平内边距，并把该行内部 12px 灯槽与文案的间距改为 6px；共用 `ThinkingBusyCue` 的 8px 默认间距、首内容占位、「输出中」及其他使用方均保持不变。新增真实 Chromium 几何回归，在连续动画帧分别比较尾部状态与思考、工具摘要的图标槽及文案左缘，并覆盖非目标状态的间距与位置。
- 为什么：过程行原有 4px 水平内边距和 6px 图标—文案间距，尾部思考行却从容器左缘直接排列且间距为 8px，导致灯槽左偏 4px、文案左偏 2px，破坏同一 Agent 回合的时间线对齐。
- 如何验证：相关 Vitest 与真实浏览器回归 13 项通过，图标槽和文案左缘差均不超过 1px，占位、输出中和共享指示灯默认间距不回归。Web 全量 528 个测试文件、5,559 项测试通过，Lines 91.5%；关键 Playwright 套件 248 项通过（6.2 分钟）。ESLint（0 errors、12 条既有 warnings）、vue-tsc、生产构建及 Secure Context/品牌断言、actionlint 和 error-level shellcheck 通过。

### 2026-10-27（修复 CLI 失败后空回复与 OpenCode 变体参数）

- 日期：2026-10-27。
- 范围：`sandbox-gateway/sandbox/internal/provider/modelconnection/`、`internal/provider/streamjson/`、`internal/agents/`；`server/internal/services/`、`internal/sandbox/`、`internal/runtime/`；`web/src/lib/run/`、`src/lib/solo/`、`src/components/solo/`、`src/components/agent/`、`web/LIB_DOMAIN_MAP.json`；`CHANGELOG.md`。
- 做了什么：OpenCode 保留原始模型 ID，以独立 `--variant` 参数选择推理强度，继续保留官方、自定义和连接服务的变体声明及辅助模型隔离。Gemini 按原生 stream-json 解析正文、工具、失败与用量，缓存输入只计一次。服务端把 provider 失败和 refusal 传递给对话调用方，保留已生成内容、用量及现有中断语义；Solo 与 Agent 测试聊天显示实时错误，并从历史原始事件恢复此前误存为成功的空回复，无需修改历史数据库。
- 为什么：OpenCode 1.19.4 将 `model#max` 当作字面模型 ID；桥接已回传失败，但调用边界丢弃结果中的失败标记，前端又忽略错误事件，导致有用户消息却无回复或报错。交叉检查还发现 Gemini 被套用 Claude 消息格式，以及 ACP refusal 被当成正常结束的问题。
- 如何验证：OpenCode 参数子进程回归覆盖官方默认、原生、自定义与连接服务三种协议；Gemini 原生消息、工具、失败、警告和缓存计数回归；真实 WebSocket 验证 Solo/Studio 失败落库与部分输出、用量和取消/超时语义，React 覆盖首次及续聊 refusal，桥接测试验证迟到错误归属原轮。server/sandbox 完整 lint、vet、Go 测试、配置文档检查及 RunAgent/React 专项通过，核心覆盖率分别为 90.4% / 96.6%；规定脚本 smoke、actionlint 和 error-level shellcheck 通过。前端 527 个测试文件、5,533 项测试通过，Lines 91.5%，lint（0 errors）、vue-tsc 与生产构建通过；完整 critical Playwright 套件 243 项全部通过（隔离容器，10.4 分钟）。docs build/audit 通过。运行时包构建通过，无需镜像或 IMAGE_LEVEL 变更。另用隔离 HOME 和本地模拟模型尝试真实 OpenCode 1.19.4：CLI 停在初始化且未发出模型请求，超时后清理，不能计为真实回复端到端验证通过。

### 2026-10-27（API 模型图片能力声明与多工具适配）

- 日期：2026-10-27（Asia/Shanghai）
- 范围：`web/src/components/workspace/ModelConfigurationManager.vue` 及单测、`web/src/lib/agent/modelCapabilities.ts` 及单测、`web/src/lib/agent/modelRouting.ts`、`web/LIB_DOMAIN_MAP.json`、`web/e2e/model-routing.spec.ts`、`sandbox-gateway/sandbox/internal/provider/modelconnection/{configs,dsh,modalities}.go` 及适配回归、`CHANGELOG.md`。
- 做了什么：OpenCode、CodeBuddy 和 DeepSeek Harness 的 API 模型组按模型提供图片输入声明，覆盖主模型、辅助模型和独立角色模型；草稿只在保存时写入共享 API 账号，切换账号、工具或取消后不泄漏。模型目录按字段补齐已保存模型缺失的能力，保留明确的仅文字、视觉关闭及其他能力，并基于保存时最新账号记录更新实际引用的模型。原生适配统一以明确输入类型优先于旧视觉标志，修正 CodeBuddy 忽略输入列表、OpenCode attachment 与 modalities 不一致、DSH 关闭视觉声明不完整等问题；未声明能力沿用工具默认值，不按模型名称猜测。Claude Code、Codex 使用各自原生图片处理，不显示无法生效的能力控件；Cursor、Trae 保留平台授权目录。
- 为什么：Kimi K3.1 Pro 经 API 目录添加后只有模型 ID、名称和空能力对象，DSH 默认按仅文字模型运行，在本地 Read Image 检查阶段拒绝图片。OpenCode 自定义模型也会在未声明时关闭图片输入，旧界面没有补录入口，已有模型再次刷新目录也不能修复缺失 metadata。
- 如何验证：Web 全量 528 文件、5,547 项单测通过，Lines 91.51%；关键浏览器回归 248 项通过（11.4 分钟），覆盖三种适配器的图片启用/仅文字持久化及刷新保留、Claude/Codex 原生入口，并人工核对手机截图。ESLint（0 errors、12 条既有 warnings）、类型检查、生产构建与 Secure Context/品牌断言通过。Sandbox 使用 Go 1.26.10、golangci-lint 2.12.0 完成 lint（0 issues）、vet、全量 Go 测试、核心覆盖率 96.6% 和规定语法、inject/git-auth/runtime-bootstrap/preview-inject smoke，mock chat 8 项通过；新增 8 种能力组合 × 4 种适配模式回归。全部七种工具完成只读能力核查；禁外网新容器中以假接口、合成 PNG 验证 Claude 2.1.310 的 Read 和 Codex 0.164.0 的 view_image 均把图片内容带入后续请求，未访问真实厂商推理。docs 构建与官方源高/严重漏洞审计、actionlint、error-level shellcheck 和 diff 检查通过。

### 2026-10-27（CLI 对话持续显示处理状态）

- 日期：2026-10-27（Asia/Shanghai）
- 范围：`web/src/components/solo/SoloSessionPane.vue`、`web/src/components/agent/AgentChatTester.vue`、相关前端单测与浏览器回归、`CHANGELOG.md`。
- 做了什么：Solo 和 Agent 测试对话在整轮仍进行时，持续显示共用的脉冲指示灯与处理文案，覆盖首段输出、工具调用结束后的等待以及正文输出；准备沙箱文案在首段时间线输出后切换为处理状态，思考过程标题省去重复忙碌标记。正常完成、停止和报错后移除处理行，保留工具自身准确的完成状态，沿用减少动态效果设置。
- 为什么：原先的 loading 行只在尚无输出时显示，OpenCode 等 CLI 在工具调用完成后等待下一段模型输出时，界面只剩已完成工具摘要，容易被误认为卡住。Claude Code、DeepSeek Harness 等使用相同的整轮生命周期，无需修改适配器事件合同。
- 如何验证：新增 6 项单测与 3 项浏览器回归，覆盖 OpenCode、Claude Code 和 DSH 的工具完成后静默等待、思考/正文/工具切换、重连与正常/异常/停止收尾，并核对明暗主题和手机截图；只读核对全部 7 种 CLI 的进度归一化与整轮结束事件。Web 全量 527 文件、5,523 项单测通过，Lines 91.50%；关键浏览器回归 243 项通过（9.4 分钟）。ESLint（0 errors、12 条既有 warnings）、类型检查、生产构建及 Secure Context/品牌断言、docs 构建与官方源高/严重漏洞审计、actionlint、error-level shellcheck 和 diff 检查通过。

### 2026-10-27（隐藏首页内容区滚动条）

- 日期：2026-10-27（Asia/Shanghai）
- 范围：`web/src/views/DashboardView.vue`、`CHANGELOG.md`。
- 做了什么：首页内容容器增加 Firefox 与 Chromium/WebKit 的隐藏滚动条样式，工作流和 Solo 模式共用；保留原有垂直溢出滚动。
- 为什么：居中的内容容器溢出时，滚动条出现在输入框右侧，破坏首页连续背景；小屏和较长内容仍需通过滚轮或触屏访问。
- 如何验证：真实浏览器覆盖工作流/Solo、明暗主题与桌面/手机 8 种组合，确认无可见滚动条和横向溢出，滚轮与触屏仍能访问底部控件。Web 全量 527 文件、5,517 项单测通过，Lines 91.49%；关键浏览器回归 240 项通过（进程意外终止后补跑未完成用例）。ESLint（0 errors、12 条既有 warnings）、类型检查、生产构建及 Secure Context/品牌断言、docs 构建与官方源高/严重漏洞审计、actionlint、error-level shellcheck 和 diff 检查通过。

### 2026-10-27（PR #1000 同步 main 并解除文档冲突）

- 日期：2026-10-27（Asia/Hong_Kong）
- 范围：`CHANGELOG.md`、`docs/dev/DEVLOG.md` 与 `main` 的 Solo 重连修复合并。
- 做了什么：合并 `main` 的 `7c17d340`；保留本 PR 的 ACP 恢复与 Solo 预览说明，同时保留 main 更新后的 Solo 消息投递说明和开发记录。两份 Solo 前端文件按 main 原样合入，原有后端与沙箱修复保持一致。
- 为什么：原提交的远端 CI 已全部通过，main 新增 Solo 重连修复后，发布说明与日志的顶部插入产生冲突，需要保留双方内容并重新验证合并结果。
- 如何验证：Solo 定向 22 项单测、ACP/service 与 server 预览/授权恢复定向 race 回归、docs 构建与官方源高/严重漏洞审计、actionlint、error-level shellcheck、品牌及 diff 检查通过；只读复核确认无丢失说明、无额外代码变更。推送后的远端 CI 以 PR 最新检查为准。

### 2026-10-27（ACP 通信失效恢复与 Solo 应用预览）

- 日期：2026-10-27（Asia/Hong_Kong）
- 范围：`sandbox-gateway/sandbox/internal/acp/conn.go`、`internal/service/bridge*.go` 及回归测试；`server/internal/services/preview*.go`、`server/cmd/server/` 与 `CHANGELOG.md`。
- 做了什么：ACP 读取遇到非法 JSON 或 EOF 时原子关闭连接和待处理 RPC、终止对应子进程并安全回收，后续调用立即失败；会话可用性检查覆盖发送、队列和连接元数据，队列取出及回填与退出清理互斥，避免旧消息在清空后重新入队。退出监视先摘除失效会话，再回收进程，按会话代次隔离迟到通知；保留异常退出现场，重新连接沿用显式新建会话的历史清空合同。预览解析增加 Solo 专用当前会话与沙箱关联校验，兼容旧空节点记录，按真实 Agent 能力判断预览权限；MCP 持久化恢复同时支持 Solo token 和能力，拒绝关闭会话、旧沙箱代次及数据库异常。
- 为什么：非协议输出会使 ACP 读取线程退出，但存活子进程阻塞旧退出监视，后续请求继续写入无人读取的连接。Solo 沙箱又被仅允许 `purpose=run` 的预览查询排除，并且服务重启后的授权恢复只查工作流运行表，导致误报沙箱未就绪或无法调用 MCP。
- 如何验证：真实辅助子进程复现非法 stdout/EOF 后仍存活、RPC 与关闭并发、应答紧邻 EOF、延迟回收与重连隔离；定向 race 和 sandbox/server 全量 Go 测试通过。真实 MCP 调用经预览服务及 fake gateway 完成探测、注入、保活和持久化；fresh Host 覆盖 Solo 授权恢复与撤销、错误 token、数据库失败和工作流人工接管兼容。lint/vet、配置文档检查、RunAgent/React 专项、脚本 smoke、preview-inject smoke 通过；server 核心覆盖率 90.4%、sandbox 核心覆盖率 96.6%，docs build/audit、actionlint 与 error-level shellcheck 通过。同步最新 `main` 后重跑服务端与沙箱检查；旧 pump 叠加新增回归可复现失败，修复后 race 三轮通过。运行时包构建通过且两次产物字节一致；此次为运行时逻辑修复，无沙箱镜像或 IMAGE_LEVEL 改动。

### 2026-10-26（修复 Solo 快照断线后的消息开场丢失）

- 日期：2026-10-26（UTC）
- 范围：`web/src/lib/solo/useSoloChat.ts`、`web/src/lib/solo/useSoloChat.test.ts`、`CHANGELOG.md`。
- 做了什么：把订阅快照的缺口检测高水位与重连确认游标分开。每个连接的首个 `queue_state` 只更新高水位，真实帧处理后才推进 `ackedSeq`，重连从该确认位置继续回放；合法 `KeepNone` 序号空洞不再被误判为断流，重复真实帧仍按序号和事件类型去重。新增同序号快照后断线、跨合法序号空洞恢复两条跨连接回归，并让既有重连测试按真实顺序先接收快照。
- 为什么：上一版仅按事件类型去重，若连接在快照后、同序号 `turn_begin` 前断开，快照已推进的重连游标会让服务端过滤掉尚未处理的开场，消息只能等历史接口恢复。简单忽略快照序号又会误报回放中合法存在的 `KeepNone` 空洞，因此需要分别记录高水位和已处理位置。
- 如何验证：新增同序号断线用例在旧逻辑下稳定失败（错误请求 `after=7`）；修复后 Solo 定向 22 项通过，Web 全量 526 个文件、5,507 项测试通过，Lines 91.50%；Solo 真实浏览器用例 1 项通过。ESLint 0 errors（12 条既有 warnings）、Vue 类型检查、4GB Node 堆生产构建、Secure Context/品牌断言及 actionlint、error-level shellcheck 通过。

### 2026-10-26（模型组文案与真实模型名称）

- 日期：2026-10-26（UTC）
- 范围：`web/src/lib/solo/configuredModels.ts`、`web/src/lib/agent/modelRouting.ts`、中英文页面文案、相关前端测试、`CHANGELOG.md`。
- 做了什么：Solo 的具体主模型和辅助模型统一使用 CLI 或服务商目录中的模型名称，目录未提供名称时回退原始模型 ID，不再用模型配置名（如 TokenHub）替代；自动平台主模型保留既有名称逻辑。工作区模型管理标签、新建/编辑窗口与名称字段统一为「模型组」，新增入口使用「新建模型组」，并同步英文文案。
- 为什么：配置名或服务名不是模型名称，混用会让用户无法判断实际调用的模型；主模型与辅助模型组成的配置在管理入口应明确称为模型组。
- 如何验证：5 个相关 Vitest 文件共 126 项测试及 1 项 Solo 真实浏览器回归通过，覆盖 TokenHub 别名不显示、真实模型名称、原始模型 ID 与推理强度提交、自动模式和新建模型组入口。Web 全量 526 个文件、5,504 项测试通过，Lines 91.49%；ESLint（0 errors、12 个既有 warnings）、Vue 类型检查、生产构建及 Secure Context/品牌断言通过。docs 构建与官方源高/严重漏洞审计、actionlint、error-level shellcheck 和 diff 检查通过。

### 2026-10-26（修复 Solo 同序号快照吞掉消息开场）

- 日期：2026-10-26（UTC）
- 范围：`web/src/lib/solo/useSoloChat.ts`、`web/src/lib/solo/useSoloChat.test.ts`、`CHANGELOG.md`。
- 做了什么：WebSocket 帧去重由仅比较 `seq` 改为同时比较事件类型；订阅快照与真实 `turn_begin` 共用序号时仍处理开场帧，同一真实事件的重复重放仍被忽略。补上与服务器 `Subscribe` 序号语义一致的竞态回归。
- 为什么：订阅快照复用最新真实事件的 `seq`，原逻辑先记录快照后会误删同序号的消息开场，导致已发送消息偶发不上屏，只能刷新后从历史接口恢复。
- 如何验证：新增用例在修复前稳定失败（`turns=[]`），修复后通过；Solo 定向 20 项、Web 全量 526 文件 / 5,505 项测试通过，Lines 91.49%，关键浏览器测试 240 项通过（5.9 分钟）；lint（0 errors、12 条既有 warnings）、类型检查及生产构建通过，docs 构建与官方源高/严重漏洞审计、actionlint、error-level shellcheck 和 diff 检查通过。构建首次触及默认约 2GB Node 堆上限，改为 4GB 后成功。

### 2026-10-26（修复 DeepSeek Harness 无密钥 CI 握手）

- 日期：2026-10-26（Asia/Shanghai）
- 范围：`sandbox-gateway/scripts/test-agent-connect.sh`。
- 做了什么：未设置厂商密钥时，仅向 DeepSeek Harness 测试容器传入握手专用占位值；真实对话继续由调用环境中的 `DEEPSEEK_API_KEY` 决定，显式记录跳过原因。保留运行时对缺失及非法凭据的校验。
- 为什么：PR #997 的 `sandbox-images` 在 Agent connection E2E 中失败；新增官方授权校验在启动 ACP 前要求密钥，而既有无密钥握手未注入测试值。`ci-required` 随之失败。
- 如何验证：使用当前运行时包及现有 3.0.0 沙箱镜像，在独立容器中先复现相同错误；修复后无厂商密钥的六种工具连接套件通过，含 DeepSeek ACP 握手和 OpenCode mock 对话，并确认 DeepSeek 真实对话被跳过。沙箱 lint v2.12、vet、全量 Go 测试与覆盖率检查通过（核心 96.6%），规定 shell 语法、注入、Git、bootstrap、mock 与 preview smoke 通过；actionlint、error-level shellcheck、docs 构建和官方源高/严重漏洞审计通过。

### 2026-10-26（后端模型配置与接口说明）

- 日期：2026-10-26（Asia/Shanghai）
- 范围：`server/README.md`。
- 做了什么：补齐厂商授权与 API 账号的分域说明，更新 OpenCode、Codex 与 DeepSeek Harness 的具名配置用法，移除 OpenCode 旧 env 路由示例；记录只读模型目录接口与 Solo 模型/强度字段、下一轮应用及旧运行时边界。
- 为什么：新增授权、模型目录与会话设置需要与后端使用说明一致，避免继续按照旧的混合凭据方式配置。
- 如何验证：对照当前凭据域、配置解析器、HTTP 路由和 Solo 客户端合同逐项核对；模块测试、配置文档检查、docs 构建及官方源依赖审计通过。提交 PR 前，在最终代码版本重跑完整关键浏览器套件，240 项通过（8.5 分钟、零重试）；Web 全量单测 5,504 项、Lines 91.50%，lint/类型/构建及根门禁通过；Server/Sandbox 核心覆盖率分别为 90.4% / 96.6%。

### 2026-10-26（Solo 模型菜单留白与主题修正）

- 日期：2026-10-26（Asia/Shanghai）
- 范围：`web/src/components/solo/{SoloModelSelect,SoloPickerShell}.vue`、`CHANGELOG.md`。
- 做了什么：模型菜单收窄为 240px，使用更小圆角、轻阴影、清楚的行距与右侧选中勾，移除选项品牌图标描边；标题移到搜索框之前，选项悬停、键盘焦点与停用状态各自明确。修正浅色 CSS 的 `:global` 作用域，避免面板样式错误落到根元素；浅色面板使用白色背景。保留启动/会话按钮差异、强度面板和选择行为。
- 为什么：灰底、偏重阴影和图标小框让模型菜单显得臃肿，浅色面板规则也未正确命中。
- 如何验证：Vue SFC 样式编译确认浅色规则命中完整面板选择器；现有 Solo 浏览器用例通过（1 项，17.1 秒），核对明暗主题与手机截图、启动/会话边框差异、名称和原始请求值、强度键盘及重置。Web 全量 526 文件、5,504 项单测通过，Lines 91.50%；lint（0 errors、12 个既有 warnings）、类型检查、生产构建及内置品牌/Secure Context 断言通过。docs 构建、actionlint、error-level shellcheck 与 diff 检查通过。

### 2026-10-26（Solo 启动与会话模型按钮样式）

- 日期：2026-10-26（Asia/Shanghai）
- 范围：`web/src/components/solo/{SoloModelSelect,SoloPickerShell,SoloSessionPane}.vue`、`web/e2e/model-routing.spec.ts`、`CHANGELOG.md`。
- 做了什么：首页与创建 Solo 时保留模型按钮边框；进入会话后使用无框控件，并去掉触发按钮中品牌图标的描边。会话控件在悬停和打开菜单时也不出现边框，保留键盘焦点提示、品牌图标、名称和强度选择；其他选择器继续使用默认样式。
- 为什么：启动配置需要明确的选择入口，会话输入栏则需要与参考图一致的轻量控件。
- 如何验证：现有真实浏览器 Solo 用例通过（1 项，29.7 秒），比较首页与创建会话后的实际边框样式，并验证会话打开菜单时仍透明、品牌图标和强度键盘/重置操作、主/辅助模型原始请求值；核对手机截图。Web 全量 526 文件、5,504 项单测通过，Lines 91.51%；lint（0 errors、12 个既有 warnings）、类型检查、生产构建及品牌/Secure Context 断言通过。docs 构建、actionlint、error-level shellcheck 与 diff 检查通过。

### 2026-10-26（厂商授权、API 模型目录与模型品牌图标）

- 日期：2026-10-26（Asia/Shanghai）
- 范围：`web/src/components/workspace/{WorkspaceCredentialsPanel,ModelConfigurationManager}.vue`、`web/src/components/{ui/AppSelect,agent/VendorMark}.vue`、`web/src/assets/provider-logos/{README.md,kimi.svg,kimi-dark.svg}`、`web/src/components/solo/{SoloModelSelect,SoloPickerShell}.vue`、`web/src/lib/{workspace,agent,api,solo}/`、中英文页面文案与相关单测、`web/e2e/{onboarding-wizard,model-routing,workspace-credentials}.spec.ts`、`server/internal/` 厂商授权及模型目录相关代码、`sandbox-gateway/sandbox/internal/` 厂商运行路由及相关测试、中英文快速开始与 `CHANGELOG.md`。
- 做了什么：取消 OpenCode CLI、DeepSeek Harness 工具卡片到通用 API 账号的重定向，按 `tool_auth` 保存对应厂商 Key，凭据列表归入编码工具授权；通用模型 Key 及可编辑地址、协议继续放在 API 账号。两个编码工具同时保留厂商授权与显式 API 连接模式，补齐官方模型目录、授权注入及运行隔离；不迁移或改写已有 API 账号。API 连接新增只读模型目录接口，由服务端使用该账号密钥调用标准模型列表，保留路径前缀、协议认证、明确 metadata 和 Anthropic 分页，限制超时与响应规模并拒绝重定向；不返回密钥或原始错误内容。模型选择器合并发现目录和已保存模型，支持搜索、刷新与手动填写；目录加载不自动选择模型、不覆盖草稿，切换账号或关闭时丢弃迟到结果；保存只追加实际选择项及其 metadata，不写入整个目录。API 模型图标按原始 ID 优先、名称辅助识别品牌，GPT/Kimi/GLM/混元等使用现有本地 SVG，未知模型沿用账号服务商图标；彩色图标在暗色主题保留原色。图标识别仅影响显示，不参与凭据或请求路由。Kimi 使用官方品牌指南提供的 K 标 light/dark SVG。Solo 模型按钮恢复公共边框，主/辅助模型同样使用模型品牌图标并保留配置名称；弹出菜单改为紧凑名称列表，去除原始 ID 副行和大型选中卡片。思考强度改为当前档位、重置、小模型名返回和滑杆组成的窄卡片；唯一模型有明确强度档位时直接进入强度卡片，保留默认、键盘和实际 API 事件。
- 为什么：工具卡片应配置对应厂商授权，通用 API 账号应有独立入口；模型应从服务商目录选择，聚合账号的统一图标也不足以区分模型品牌。Solo 需要在紧凑输入栏中清楚呈现名称和强度，减少重复信息和额外点击。
- 如何验证：Node 24.22.0 下 Web 最终全量 526 文件、5,504 项单测通过，Lines 91.50%；ESLint（0 errors、12 个既有 warnings）、类型检查、生产构建及内置品牌/Secure Context 断言通过；完整关键浏览器套件 240 项通过（9.1 分钟、零重试）。定向回归覆盖厂商凭据创建与模型草稿续接、目录 metadata/失败/刷新/迟到响应、搜索后回车选择原始 ID、四种品牌及手填模型图标、Kimi 明暗 SVG、Solo 名称列表与单模型强度/重置/键盘。人工核对明暗、桌面/手机截图，无溢出。Server 最终 lint（0 issues）、vet、配置文档检查、全量 Go 与 runtime 专项通过，核心覆盖率 90.4%；Sandbox 最终 lint（0 issues）、vet、全量测试、覆盖率及规定 smoke 通过，核心覆盖率 96.6%。现有默认及历史 OpenCode 镜像的 CLI 1.18.30 在无网络、只读、无卷挂载环境完成 synthetic 隔离验证，稳定 data/state 保留会话，无需更新镜像。docs 构建、官方源高/严重漏洞审计、actionlint、error-level shellcheck 与 diff 检查通过；独立模拟预览更新并保留临时元数据，本次未访问真实模型服务或改写现有用户数据。

### 2026-10-26（引导直接配置、Solo 显示名称与思考强度）

- 日期：2026-10-26（Asia/Shanghai）
- 范围：`web/src/components/{onboarding,workspace,solo}/`、`web/src/lib/{solo,api,pm}/`、中英文页面文案与快速开始、`web/e2e/{onboarding-wizard,model-routing}.spec.ts`、`server/internal/{handlers,models,modelrouting,runtime,sandbox,services}/`、`sandbox-gateway/sandbox/internal/{handler,provider,service}/` 及相关测试、`CHANGELOG.md`。
- 做了什么：保留「配置凭据」步骤，凭据保存后直接进入「配置编码工具」，模型保存后直接进入 Git，不再展示配置完成后的管理列表；编码工具步骤直接展示现有三步配置表单，缺少凭据时仍可就地创建后恢复草稿。API 地址选项改用项目已有 `AppSwitch`，保留原窗口样式。Solo 主模型以具名配置名称显示，保留原始模型 ID；紧凑选择器提供受支持模型的思考强度、返回与重置，只有一个模型时也能选择强度。创建和会话 API 传递强度，运行时仅接受该模型及实际适配器公布的档位，修改在下一轮原子应用，失败恢复已确认设置；旧运行时保留模型切换兼容。无需更新沙箱镜像。
- 为什么：首次配置应该连续完成表单；显示名称应与保存的配置一致，模型和强度必须区分显示与实际请求值，避免单模型配置失去强度入口。
- 如何验证：引导与配置面板定向单测 97 项、Solo 定向单测 65 项、组合浏览器回归 9 项通过；后端 lint、vet、配置文档、全量及专项测试通过，核心覆盖率 90.4%；沙箱规定的 lint、vet、单测与 smoke 通过，核心覆盖率 96.6%。Node 24.22.0 下最终 Web 全量 525 文件、5,455 项单测通过，Lines 91.51%；lint（0 errors、12 个既有 warnings）、类型检查、生产构建（含品牌和 Secure Context 断言）、docs 构建、官方源高/严重漏洞审计、actionlint、error-level shellcheck 与 diff 检查通过。最终关键浏览器套件 236 项通过（8.9 分钟、零重试），包含最新引导与 Solo 显示名称、强度及请求值回归。

### 2026-10-26（首次引导顺序、窗口内配置与默认 Solo Agent）

- 日期：2026-10-26（Asia/Shanghai）
- 范围：`server/internal/{services,handlers}/onboarding*`、`web/src/components/{onboarding,workspace}/` 及相关单测、`web/src/lib/{pm,solo}/`、`web/src/router/unknownRoutes.test.ts`、`web/src/components/vitest4-component-coverage.test.ts`、中英文页面文案、`web/e2e/onboarding-wizard.spec.ts`、中英文快速开始与 `CHANGELOG.md`。
- 做了什么：保留原来的居中窗口、侧栏、配色与样式，在「偏好」后按「添加凭据 → 选择模型」的顺序配置；凭据为空时进入步骤即展示凭据类型选择，先保存编码工具授权或 API 账号，模型步骤仅使用已保存凭据，不再在模型向导里跳去创建凭据。两类表单与 Git 凭据均在当前步骤内展开，不叠加弹窗；新增模型和可用 Git 授权自动选中。移除编辑时底部提示文案，并隐藏空页脚及其分割线，编辑、保存和刷新期间保护草稿，保留资源重新校验、失败重试与焦点恢复；普通工作区凭据编辑保持既有行为。初始化另外创建空白 `Solo` Agent，使用所选模型与 Git 配置，不携带角色模板文件或能力；同名 Agent 重入保留，四个工作流角色与图结构不变。Solo 入口优先选择默认 Solo，保留有效的记忆选择，引导完成后刷新入口并丢弃旧加载结果。路由单测用空视图隔离页面渲染依赖，保留真实路由表与跳转断言，避免冷编译复杂页面挤占测试时限；覆盖率组件测试桩按真实 API 合同区分数组和分页对象，避免 Solo 默认选择遇到错误响应形状。
- 为什么：先选模型再补凭据会打断配置顺序，层层打开弹窗也不便返回；明确先后步骤并复用原窗口，让用户按顺序就地完成初始化；Solo 提供独立的空白起点，避免必须借用工作流角色才能开始对话。
- 如何验证：Node 24 下 Web 全量 524 文件、5,423 项单测通过，Lines 91.44%；最终完整 critical E2E 234 项通过（8.2 分钟、零重试），含 7 项引导回归，验证先凭据后模型、空态直接选类型、单一原窗口、默认 Solo、秘密遮罩与提交引用；另对先前受开发热更新影响的两组交互及引导执行 19 项定向回归，全部通过。前端 lint（0 errors、12 个既有 warnings）、类型检查、生产构建通过；Server lint v2.12、vet、配置文档检查、全量测试与 runtime 专项通过，核心覆盖率 90.3%，真实 HTTP 重启初始化契约通过。actionlint、error-level shellcheck、品牌与 diff 检查、docs 构建和官方源高/严重漏洞审计通过。核对 1280/390px 深色及 320px 浅色窗口，无嵌套弹窗，编辑时空页脚与底部分割线隐藏。独立预览在 5176 运行，使用模拟 API 和临时元数据，不连接现有后端或读取现有数据。

### 2026-10-26（澄清选择提交后卡住与重连续问恢复）

- 日期：2026-10-26（Asia/Hong_Kong）
- 范围：`web/src/lib/inbox/useClarifyChat.ts`、`useGatesInbox.ts`、`web/src/lib/run/useRunDetailWs.ts`、相关单测与 `web/e2e/clarify-session-ux*`、`CHANGELOG.md`。
- 做了什么：排队快照按服务端消息 ID 排除已开始或刚完成的回复，保留不同 ID 的真正待发消息；后续回合到达时移除已经保存的历史临时气泡，保留最新回复的增量。完整服务端快照缺少当前节点或省略整个会话映射时按空闲恢复，重连后补取已保存的问题；仅使用服务端新快照恢复，校验当前会话和事件代次，其他节点的状态不再干扰当前对话。补充连续选择与竞态回归。
- 为什么：迟到的排队快照会把正在处理的消息重新列为待发，而结束帧保留该 ID；旧临时回复被重复拼到新问题后又会将其误判为已回答。断线漏过结束帧后，服务端已清除的空闲会话也未被前端识别，导致新问题只读、停止按钮常驻，必须刷新才恢复。
- 如何验证：Web 全量单测与行覆盖率门禁、lint、类型检查、生产构建和完整关键浏览器套件通过，覆盖连续两轮确认选择、迟到快照、真实同文排队、重连缺省映射及新回合优先。同步最新 main 后保留孤立回合续流保护，补测权威空闲快照停止后台拉取；浏览器使用独立本地端口，临时测试副本仅替换端口。docs 构建与官方源高/严重漏洞审计、actionlint、error-level shellcheck、品牌与 diff 检查通过；PR 的最新 CI 结果另见检查列表。

### 2026-10-25（PR #951 同步 main 并解除合并冲突）

- 日期：2026-10-25（Asia/Hong_Kong）
- 范围：`CHANGELOG.md`、`docs/dev/DEVLOG.md` 与 PR #951 的 main 合并。
- 做了什么：合并 `main` 的 `8724f7de`；冲突仅涉及 `Unreleased` 顶部插入，完整保留用量分析改版与主页默认副标题两条发布说明，其他 main 更新按原样合入。
- 为什么：原提交的 CI 均已成功，GitHub 当前阻塞来自 main 前进后的合并冲突，需要解除冲突并重新验证合并结果。
- 如何验证：用量组件与页面专项 53 项单测、docs 安装/构建/官方源审计、actionlint、error-level shellcheck 与合并 diff 检查通过；完整前端检查及浏览器回归在合并树重跑，远端结果见 PR #951。

### 2026-10-25（用量分析结构与明细重排）

- 日期：2026-10-25（Asia/Hong_Kong）
- 范围：`web/src/views/TokenAnalyticsView.vue` 及单测、`web/src/components/token-analytics/`、图表注册、中英文页面文案、`web/e2e/token-analytics.spec.ts` 与 `CHANGELOG.md`。
- 做了什么：用可键盘操作的排行行替换极端占比下不可读的大面积矩形树图，保留工作流/工作区助手/Studio 到节点类型的组合下钻；模型和工作流明细改为全宽页签及占比条，统一表格阅读层次。高用量运行增加序号，默认显示八条，可展开完整列表；CSV 继续导出全部数据。
- 为什么：单一工作流占绝大多数用量时，树图挤压其他项目且浪费纵向空间，并列窄表和过长运行列表使比较与定位困难；改用稳定行高和全宽明细，让高低占比数据都能阅读和操作。
- 如何验证：Web lint（0 errors、12 个既有 warnings）、类型检查、生产构建与 511 文件/5,156 项全量单测通过，Lines 91.03%；最终定向 53 项单测、13 项用量浏览器回归及完整 227 项关键浏览器套件通过。以极端占比、长中文标题和 12 条运行示例核对桌面深浅主题及 390px 手机布局，小占比项目可读、表格只在卡片内横向滚动；组合下钻、页签切换、收起时全量 CSV 和展开后运行导航均通过。docs 依赖安装、构建及官方 npm 源高/严重漏洞审计、actionlint、error-level shellcheck、品牌与 diff 检查通过。

### 2026-10-25（工作流共享沙箱单开关）

- 日期：2026-10-25（Asia/Hong_Kong）
- 范围：工作流默认配置面板、前端类型与中英文文案、画布浏览器回归、`server/internal/{config,handlers,services,sandbox,engine,models}/`、沙箱 README 与帮助。
- 做了什么：工作流默认配置顶部新增「共享沙箱」开关，保存到 `defaults.sandbox.mode`；沙箱、变量、通知与自动审批统一分组、开关和间距，变量类型与 Agent 选择复用可搜索组件，收起冗余说明；删除平台级共享沙箱设置、配置项与全局开关状态，启动运行仅按工作流配置快照。关闭时仍为每节点独立沙箱；已有运行继续使用启动时的选择。
- 为什么：用户可以在前端一次选择完成配置，消除隐藏的第二层启用条件。
- 如何验证：server lint/vet、配置文档检查、全量测试、RunAgent/React 专项通过，核心覆盖率 90.4%；Web lint/types/build 与 511 文件、5,151 单测通过，Lines 91.01%；完整 227 项关键浏览器测试通过，覆盖共享模式开关保存/刷新、1440px 浅色与 1024px 深色统一表单及搜索选择；只读 review 确认通知/审批显式关闭、空值默认语义和密钥遮罩保留。docs build/audit、actionlint/shellcheck 通过；沙箱 README 同步后语法/smoke/lint/vet/preview 与核心覆盖率 96.6% 通过。本地前端与更新后控制面均返回 200，无沙箱镜像改动。

### 2026-10-25（API 账号与默认继承交互收口）

- 日期：2026-10-25（Asia/Hong_Kong）
- 范围：Agent 元信息/创建/默认配置、工作区凭据与模型组件、Studio 保存反馈、`server/internal/handlers/`、`services/`、`runtime/` 及当前配置合同回归、模型配置帮助。
- 做了什么：工具授权归入「Agent CLI」、模型服务凭据归入「API 账号」，两类显示厂商图标；移除独立模型连接列表、新增连接入口和模型目录，API 地址只在账号向导或账号编辑中维护。Agent 缺省使用默认配置，显式关闭状态原样持久化；元信息恢复单张继承卡片，开关前后保持固定尺寸，开启时收起模型/Git/路径明细，关闭后恢复本地配置；Agent 与默认配置的元信息均移除目录路径和管理凭据链接，Agent 另移除通用凭据绑定和底部能力实现说明；保存状态、按钮文字及底部反馈增加 200ms 过渡并尊重减少动态效果偏好；保留当前模型/Git 引用合同，不自动改写部分继承状态。后端删除自动内置空槽和旧格式推断，仅接受当前显式凭据合同，读取不转换旧行或写入空槽。
- 为什么：让页面遵循「先建账号、再建模型、Agent 选择模型与 Git」的流程；继承无需重复配置，避免无效旧占位和独立连接/目录混淆。
- 如何验证：server lint/vet、配置文档检查、全量测试、核心覆盖率 90.4%、RunAgent/React 专项、当前凭据与默认继承 race、重启初始化 HTTP 契约 race 均通过。Web 511 文件、5,147 单测通过，Lines 91.01%；lint（0 errors、12 个既有 warnings）、types、生产构建及完整 224 项关键浏览器测试通过。桌面继承卡片开关前后宽高一致（高度 120px），凭据与向导的 1440/390px 布局已核对。docs build/audit、actionlint、error-level shellcheck 和 diff 检查通过；本地前端及更新后控制面均返回 200，无沙箱镜像或引导契约变更。

### 2026-10-25（PR #943 合并初始化四角色保护）

- 日期：2026-10-25（Asia/Hong_Kong）
- 范围：`server/internal/services/onboarding.go`、默认继承回归与最新 `main` 的初始化四角色保护改动。
- 做了什么：合并 `main` 的中英文规范角色集合、角色锁定和基线工作流创建前置校验；基线校验先解析 Agent 显式继承的默认模型，Git 初始化继续使用带 Host 的凭据解析器。
- 为什么：PR #942 改写了基线角色校验，与本 PR 的默认模型继承命中同一函数；同时保留完整角色集合要求与有效模型校验，避免继承配置的角色被误判未配置。
- 如何验证：合并后 server lint/vet、配置文档检查、全量测试、RunAgent/React 专项及核心覆盖率 90.4% 通过；中英规范四角色与默认继承的 race 回归、重启初始化真实 HTTP 契约含 race/tagged vet 通过。Web 全量 512 文件、5,152 单测通过，Lines 91.02%；lint（0 errors，12 个既有 warnings）、types、生产构建及 223 项完整关键浏览器测试通过。actionlint/shellcheck、docs build/audit 和 diff 检查通过，无未解决冲突。

### 2026-10-25（凭据 Host 与 Agent 默认模型/Git 继承）

- 日期：2026-10-25（Asia/Hong_Kong）
- 范围：工作区凭据 API/列表/向导、Git 授权解析与沙箱调用链、Agent 默认配置和编辑/创建、相关回归、配置帮助与 Git 技能说明。
- 做了什么：GitHub/GitLab 凭据保存 Host（默认官方），移除独立 GitLab URL 列表项；凭据按编码工具和 Git 分类。默认配置可选择具名模型及多个 Git 授权，Agent 以独立开关明确继承模型或 Git；模型配置确定 CLI，元信息不再重复选择编码工具。
- 为什么：自建站点应与授权一起保存；Agent scope 只能细化同一站点，避免跨站转发。可复用的默认配置需要显式继承规则，与单独配置、空授权和失效处理保持一致。
- 如何验证：server lint/vet、配置文档、全量测试、RunAgent/React 专项及核心覆盖率 90.4% 通过；Host/API/继承/长会话/冷重连/共享沙箱回归含 race 通过。Web 全量 512 文件、5,143 单测通过，Lines 91.02%，lint（0 errors，12 个既有 warnings）、types、build、audit 通过；完整关键浏览器 223 项全过（4.3 分钟），含默认模型与多 Git 保存、首选、继承创建及刷新回读。重启初始化契约含 race/tagged vet、actionlint/shellcheck、docs build/audit 通过。桌面及 390px 页面已核对，本地前端与更新后的控制面健康检查均为 200；无线上数据写入，无沙箱镜像或引导契约变更。

### 2026-10-25（PR #940 重启端到端测试使用当前模型合同）

- 日期：2026-10-25（Asia/Hong_Kong）
- 范围：`server/e2e/restart_reattach_test.go`、对应初始化接口契约回归。
- 做了什么：重启 E2E 通过当前 API 依次创建模型服务凭据、连接及 OpenCode 具名模型配置，再将模型配置 ID 交给初始化向导；读回 Agent 验证 CLI 与模型引用。新增不依赖 Docker 或外部模型的真实 API 契约回归，密钥仅出现在创建凭据请求中。
- 为什么：PR #940 移除旧初始化字段后，独立的真实模型重启 E2E 仍发送旧字段，两个 CI 任务均在创建运行前以 400 退出。修复测试准备流程，继续验证真实的运行保留、排队和重启接回语义，不恢复旧字段兼容。
- 如何验证：服务端 lint、vet、配置文档检查、全量测试及 RunAgent/React 专项通过；核心覆盖率 90.3%。新增契约回归含 race 及 tagged vet 通过；actionlint、shellcheck、docs 构建和审计通过。真实模型与 Docker 重启接回继续由现有 `restart-reattach` CI 验证。

### 2026-10-24（PR #940 统一新配置模型与 CI 修复）

- 日期：2026-10-24（Asia/Hong_Kong）
- 范围：server 模型/凭据、Agent、初始化与团队服务及运行入口；sandbox 模型连接路径校验与 go.mod；Web 凭据/模型/Agent/初始化向导和测试；中英概念与快速开始、设计说明及 CHANGELOG。
- 做了什么：按用户最终确认统一为 Agent → 具名模型配置（CLI + 凭据 + 上游模型）以及显式多个 Git 授权。删除旧 Agent 单槽字段、工作区模型默认、直接来源/继承、读取映射与自动迁移；创建、编辑、初始化/团队向导和运行入口使用同一结构。草稿可未配置，运行必须配置；缺少基线角色时提示完成配置。CodeBuddy 配置读取用 os.OpenRoot 限制 FSRoot 内访问；yaml.v3 改为直接依赖；happy-dom 升至 21.0.4 并适配测试模拟差异。 浏览器图标短动效验收在首帧暂停后观察，避免遍历所有图标时被 RPC 与机器耗时影响。
- 为什么：用户明确不要求强行兼容旧结构；隐式派生可能把旧模型变成默认值。CI 另外发现路径边界、依赖分类漂移与测试库安全公告，需在本 PR 收尾。
- 如何验证：server 六项门禁全过，core coverage 90.3%；sandbox 语法/注入/Git/bootstrap/预览 smoke、Go lint/vet/全测与 core coverage 96.6% 通过；Web 最终全量 511 文件/5085 单测、Lines 90.98%、lint（0 errors，12 既有 warnings）、types、audit、build 通过。向导复核发现的 Git 跳过与模型失效回退另加 10 项回归，定向 62 项通过；完整关键浏览器套件最终 220/220 全过（4.1 分钟）。docs build/audit、actionlint/shellcheck、Go tidy/gofmt、runtime bundle 与控制面构建通过；远端检查以 PR 最新提交为准。

### 2026-10-24（凭据与模型改造提交前 review）

- 日期：2026-10-24（Asia/Hong_Kong）
- 范围：模型与凭据服务、Agent/共享配置与长驻沙箱生命周期、模型 CLI 适配器、Git helper、Web 配置向导与相关测试、公开概念说明及 `CHANGELOG.md`。
- 做了什么：修复切工具及切页签后旧授权/模型草稿丢失，所有新增表单下拉统一为可搜索 AppSelect；自定义服务商名称写入 issuer，保留 provider 机器身份，轮换密钥不再串入旧工具验证/迁移。长驻沙箱在复用和每次聊天前重新校验当前模型/Git 授权，将不可逆指纹持久化但不暴露到 API，兼容正常重连及服务重启；失效模型配置仍可在不改内容时停用。Git SSH 兼容 Git 传入的未加方括号 IPv6 地址。Messages 原生适配器只规范化末尾 API 版本，保留网关前缀及保存原值。固定官方 CodeBuddy CLI 实测仅走 Chat Completions，故关闭未实现的 Responses/Messages 连接能力。旧 OpenCode 保留原生供应商 SDK 和显式选择，不强转通用连接，不在原授权失效后换用其他 Key。
- 为什么：review 发现旧数据切换、长驻会话复用、供应商名称与机器标识混用、协议模拟与真实 CLI 行为不一致等回归；新增结构必须保住存量配置和明确授权边界。
- 如何验证：已同步 main 4b38890a 并完成三路 review 及关键修复交叉复核。Web 全量 519 文件、5,247 单测通过，Lines 90.85%；完整 critical E2E 220 项通过（4.7 分钟、零重试），类型检查、lint（0 errors、12 个已有 warnings）及生产构建通过。Server lint/vet、配置文档检查、全测、runtime 专项及核心覆盖率 90.3% 通过；sandbox 全语法/smoke、Python 19 项、Git/reprime、preview-inject、Go 全测/lint/vet 与核心覆盖率 96.6% 通过。固定官方 CodeBuddy 2.162.0 以干净配置对本地 mock 的请求验证协议边界，Anthropic SDK 0.129.0 验证实际路径拼接，无付费模型调用。actionlint、shellcheck、docs 构建与官方源审计通过，运行时包及 server 二进制构建成功，本地前端/控制面/网关健康检查均为 200。旧 OpenCode 8 类供应商、原 Key 失效不回落、长驻会话轮换/撤销/正常重连/服务重启，以及自定义名称创建/轮换不触发旧迁移均有回归。升级前旧沙箱可能需要关闭后重开，公开文档与 PR 明确列出此边界；未发布到线上。

### 2026-10-24（Solo 顶部栏与工作流会话对齐）

- 日期：2026-10-24（Asia/Hong_Kong）
- 范围：`web/src/router/index.ts`、`web/src/components/chat/ConversationWorkspace.vue`、`web/src/components/run/ReviewShell.vue`、`web/src/views/GatesInboxView.vue` 及相关测试、`CHANGELOG.md`。
- 做了什么：移除 Solo 入口与会话路由的独立全屏标记，使用与工作流启动后会话相同的 AppShell 内容布局。三处继续复用 ReviewShell 与 ConversationWorkspace，统一顶部栏外部间距、宽度及资源/产物按钮对齐。审批中心的移动聊天分支将原返回与分享入口放进共同顶部栏，去掉重复标题及独立分享行；普通审批与启动失败页面保留原有返回区域，返回列表仍恢复原滚动位置。
- 为什么：在线登录核对发现两页内部都是 44px 顶部栏，但 Solo 从 y=0 开始、工作流从 y=12 开始，左右也相差 24px；差异来自路由跳过统一外层间距，而不是顶部栏本身。
- 如何验证：Web 全量 515 个文件、5,148 项单测通过，Lines 90.76%；类型检查、生产构建及 lint（0 errors、12 个已有 warnings）通过。完整 critical E2E 219 项通过，最后合并移动分享入口后再跑分享、移动审批、会话面板与澄清产物专项 33 项，全过；回归覆盖唯一标题、分享目标、面板操作及返回列表恢复滚动。使用线上只读数据在本地新版核对深浅主题及 1895/1024/390/320px：Solo 与工作流审批会话顶部栏位置、宽高完全一致，长标题省略，按钮不溢出，零页面异常及零业务写请求。actionlint、shellcheck、docs 构建及官方源依赖审计通过；线上只读查看，未发布或修改线上会话。

### 2026-10-24（Agent 多 Git 授权与工作流沙箱隔离）

- 日期：2026-10-24（Asia/Hong_Kong）
- 范围：Agent API、持久化和 ZIP、共享配置与工作流/交互/测试运行入口、沙箱授权 staging/reprime、运行时脚本与打包、Web Git 选择组件及创建/编辑/共享草稿、相关测试、配置帮助与内部设计文档。
- 做了什么：Agent 以 `modelBinding` 引用模型，以 `gitBindings.items` 引用多个 Git 授权，通过 `preferredCredentialId` 指定首选；界面复用可搜索选择器与 Logo，支持移除、可选站点或仓库范围和 SSH 已知主机。所选秘密通过受限文件完整注入沙箱，按仓库、站点和首选匹配，不在失败时轮换账号。工作流节点应用各自 Agent 的绑定，共享沙箱切换授权时清理上一节点的文件与身份，MR 按同一仓库选择账号。显式空列表禁用默认 Git 注入，独立逐仓库授权保持兼容，新建 Agent 默认 `repoIds: []`；缺省保留旧绑定，Agent 显式旧授权优先于共享默认，失效引用保留并阻止执行。交互会话授权变更后拒绝复用旧沙箱，要求重新打开。
- 为什么：单个 Git 连接方式和授权槽位不能表达一个 Agent 需要多个 Git 账号，直接合并环境变量会覆盖同平台账号；工作流复用沙箱还需防止不同节点串用授权。
- 如何验证：Web 515 个文件、5,145 项单测通过，Lines 90.76%；完整 critical E2E 219 项通过（4.7 分钟、零重试），类型、lint（0 errors、12 个已有 warnings）和生产构建通过。Server 的 lint v2.12.0、vet、配置文档检查、全量测试与 runtime 专项通过，core coverage 90.3%；覆盖 API/ZIP/shared nil 与 empty、旧客户端保存、legacy 优先级、交互与测试沙箱全量 staging、失效阻止复用、MR 无秘密脚本、同一工作流沙箱 Agent A → B → 空授权的重新初始化。沙箱 Python 19 项及真实 Git credential、SSH 私钥与 gh/glab 离线验证通过；默认端口、协议/路径匹配、损坏首选不回退、具体仓库优先、MR 子进程临时配置清理与多授权 → 空授权均有回归。沙箱模块 lint/vet、smokes 与覆盖率 96.6% 通过；新增 server sandbox 打包/指纹测试核对完整授权集合、私有权限、显式空标记、旧环境清除及所有授权变更触发重建。actionlint、shellcheck、docs 构建/官方源审计通过。真实预览在深浅主题及 1440/390/320px 下使用只读授权元数据样例核对四授权、首选、scope、SSH 已知主机、确认页与返回保留，无写入用户数据、无原生下拉或溢出。运行时包包含新 helper，前端、控制面和网关保持运行。

### 2026-10-24（新建 Agent 显式选择模型与统一新增菜单）

- 日期：2026-10-24（Asia/Hong_Kong）
- 范围：`web/src/components/agent/{AgentCreateWizard,ModelBindingEditor}.vue` 及单测、`web/src/components/workspace/{WorkspaceCredentialsPanel,ModelConfigurationManager}.vue` 及单测、`web/src/components/ui/AppSelect.vue`、中英文页面文案、`web/e2e/{agent-create-wizard,model-routing,git-help-run-tags-404}.spec.ts`、`docs/dev/agent-model-routing-design.md` 与 `CHANGELOG.md`。
- 做了什么：新建 Agent 的“工具与模型”同页只允许显式选择已保存的命名模型配置，移除继承默认、直接来源、高级字段、绑定摘要和凭据管理链接；初始不代选模型，切换工具清空选择，返回保留草稿。该步不再提供跳过，配置为空或引用失效时禁用下一步与创建，并在提交入口再次检查；Git 仍可跳过。已有 Agent 编辑、工作区默认、团队模板与运行时的旧绑定兼容保持原有边界。凭据页两个页签共用右上角新增菜单，“新增凭据”和“添加模型”分别打开对应向导；模型页隐藏重复的新增按钮。内部设计文档区分当前创建流程与原始设计、兼容编辑行为。
- 为什么：用户只需要在创建 Agent 时选择工具及已保存的模型，原来的默认继承、直接配置与底部说明增加理解成本；凭据与模型的新增入口分散在不同页签，也不易发现。
- 如何验证：Web 全量 513 个文件、5,100 项单测通过，Lines 90.72%；完整 critical E2E 219 项通过（4.3 分钟、零重试），Git 帮助专项另行通过。类型检查、生产构建、lint（0 errors、12 个已有 warnings）、actionlint、shellcheck、docs 构建及官方源依赖审计通过。覆盖必选配置、空值和失效引用阻止前进、切工具清空、返回保留、站点保存、命名引用提交，以及两个页签共享新增入口。真实页面核对 1440/390/320px 和深浅主题：7 个工具 Logo、模型搜索和键盘选择正常，空模型禁用下一步，无继承、高级或管理入口；新增菜单的 12 种主题、尺寸和页签组合均可打开两个向导并取消，菜单无越界和重复模型按钮，未写入用户数据。前端、控制面与网关健康检查通过。

### 2026-10-24（Agent 工具与模型同页配置、搜索控件统一）

- 日期：2026-10-24（Asia/Hong_Kong）
- 范围：`web/src/components/agent/{CodingToolPicker,AgentCreateWizard,AgentMetaPanel,ModelBindingEditor,AgentCredentialBindings,AgentMcpPanel,CreateAgentTeamWizard}.vue`、`web/src/components/workspace/{WorkspaceSharedAgentPanel,WorkspaceModelDefaults}.vue`、`web/src/lib/agent/{agentCreateWizard,useAgentCreateWizard}.ts` 及单测、相关组件单测、`web/src/views/AgentStudioView.test.ts`、`web/e2e/{agent-create-wizard,model-routing,git-help-run-tags-404}.spec.ts`、中英文页面文案与 `CHANGELOG.md`。
- 做了什么：新建 Agent 收为“基础信息 → 工具与模型 → Git → 确认创建”四步，在同页先选择工具再选择模型配置，桌面并列、窄屏上下排列，右侧始终显示模型搜索框，可直接选择具名配置或工作区默认；跳过该页继承默认模型并清理旧授权输入，返回保留已选配置。创建、编辑及默认 Agent 共用紧凑的工具 Logo 列表，站点选择合并进列表底部；工具步骤使用明确的“编码工具”标题，去掉与现有模型配置流程不符的启动说明。窄屏收起步骤侧栏，内容和页脚可收缩换行。模型配置、平台凭据、连接、主模型、辅助模型、路由、凭据注入、默认工具及两处 MCP 传输选择均复用可搜索 AppSelect；已有模板与仓库选择控件继续复用。工具、厂商在选中值和选项中展示 Logo，装饰图像不重复读屏名称。失效引用保持明确的名称/ID 与错误状态，同值选择保留已有模型和输入；原凭据引用、站点环境键、工具切换确认/取消、特殊 CodeBuddy 站点及保存数据格式继续保持。
- 为什么：原来的工具大卡片、长说明和分离的高亮站点按钮缺少层次，创建与编辑样式分散；模型绑定中的原生系统下拉与仓库搜索控件不一致，不便定位已有模型。
- 如何验证：针对工具键盘选择、站点与特殊值、工具切换确认和草稿恢复、失效引用修复、MCP 输入保留的定向单测通过；四步合并页、模型路由及 Git 帮助专项浏览器 13 项通过。实际前端检查 7 个工具 Logo、搜索/高亮/Enter 选择、站点切换与无原生下拉，复核 1440/390/320px 和深浅主题截图，无溢出，未保存 Agent 或模型。Web 全量 513 个文件、5,091 项单测通过，Lines 90.71%；完整 critical E2E 218 项全部通过（4.5 分钟、零重试）；类型检查、lint（0 errors、12 个已有 warnings）、生产构建、actionlint、shellcheck、docs 构建及官方源依赖审计通过。

### 2026-10-24（凭据、模型与连接向导完整交互复核）

- 日期：2026-10-24（Asia/Hong_Kong）
- 范围：`web/src/components/workspace/{WorkspaceCredentialsPanel,ModelConfigurationManager,ModelConnectionManager}.vue` 及单测、`web/src/components/ui/AppModal.vue` 及单测、`web/src/lib/agent/modelRouting.ts`、`web/e2e/model-routing.spec.ts`、`docs/dev/agent-model-routing-design.md`、`CHANGELOG.md`。
- 做了什么：模型向导改为工具 Logo 卡片、单一带 Logo 的凭据选择和可搜索/手填的模型选择，自动根据真实记录解析平台授权或兼容连接，去掉来源种类的重复选择。恢复 OpenCode CLI、DeepSeek Harness 凭据入口，保存厂商 API Key 与连接后继续对应模型配置。模型内补建凭据保持同一页签与草稿，平台工具打开正确授权入口；保存后选中刚建的兼容记录，取消返回原草稿。返回或重选同一凭据保留模型/角色，实际更换来源才清空；重选同一厂商保留已改 Base、协议和鉴权。连接编辑及启停读取最新模型目录，避免用已缓存的目录覆盖新条目。高级字段收进折叠区，所有凭据选择带 Logo 和搜索，步骤条保持一致。弹窗补充局部焦点循环、步骤聚焦、关闭后焦点恢复，以及离场动画的 aria-hidden/inert，关闭按钮使用独立的本地化语义。
- 为什么：原生下拉没有品牌标识，模型来源与凭据两次选择难以理解；空态引导会丢失草稿并给 Cursor/Trae 打开不适用的 API 连接；同值选择和陈旧目录还存在误改输入的风险。切换弹窗时旧层留在可访问树中，会干扰键盘与读屏操作。
- 如何验证：模型与凭据 40 项、连接 17 项、弹窗 14 项针对性单测通过；实际前端与控制面完成 OpenCode 厂商凭据 → 模型配置 → Agent 引用持久化，全程仅用预览占位 Key，未调用付费模型。人工复核桌面与 320/390px、深浅主题截图，并检查 Logo、原始模型 ID、搜索、键盘选择、返回/取消草稿和 Tab/Escape/焦点恢复。Web 511 个文件、5,069 项单测全部通过，Lines 90.67%；完整 critical E2E 最终 217 项全部通过（4.9 分钟、零重试），包括新增的 7 项模型配置用例；类型、lint（0 errors、12 个已有 warnings）、生产构建、actionlint、shellcheck、docs 构建与官方源依赖审计通过。

### 2026-10-24（凭据与模型列表精简、厂商 Logo 选择器）

- 日期：2026-10-24（Asia/Hong_Kong）
- 范围：`web/src/components/workspace/{WorkspaceCredentialsPanel,ModelConnectionManager,ModelConfigurationManager,ModelRecordActions,ModelProviderSelect,CredentialProviderLogo}.vue`、`web/src/components/ui/AppSelect.vue`、`web/src/lib/agent/modelRouting.ts`、厂商 SVG 与来源说明、相关单测及 `web/e2e/model-routing.spec.ts`。
- 做了什么：按 Open WebUI 参考将模型和服务列表收为图标、名称与单行副标题，去掉大外框、协议/地址/修订/重复状态与解释段落，编辑中保留完整配置；右侧使用编辑图标、更多删除菜单和启停开关，菜单支持键盘、外部关闭与窄屏定位。凭据页移除统计卡和装饰说明，时间与服务入口收进编辑。厂商选择复用 AppSelect 的搜索、键盘与浮层行为，新增图标插槽；预设从 5 家扩到 15 家，可自定义，默认地址均附官方文档依据，不混用 Coding Plan 地址与通用 API。Logo 使用本地品牌素材并记录固定版本、来源和许可证，支持深浅主题。
- 为什么：列表中大量实现细节和操作按钮妨碍浏览，原生下拉缺少常见厂商及品牌识别。
- 如何验证：Web 511 个文件、5,044 项单测通过，Lines 90.65%；完整 critical E2E 213 项通过，相关 89 项组件与厂商选择专项回归通过，类型、lint（0 errors、12 个已有 warnings）与生产构建通过。真实前端核对 15 家预设加自定义、全部 Logo 加载、搜索与预设 Base 联动、深浅主题、320/390px 无横向溢出、浮层不裁切和 Escape 逐层关闭。actionlint、shellcheck、docs 构建与官方源依赖审计通过；文档构建产物保存在仓库外。仅使用占位凭据查看交互，未调用真实模型服务。

### 2026-10-23（凭据、可复用模型配置与 Agent 绑定）

- 日期：2026-10-23（UTC）
- 范围：`server/cmd/server/main.go`、`server/internal/{modelrouting,models,services,handlers,router,runtime,envauth}`、`sandbox-gateway/sandbox/internal/{provider,agents,service}`、`web/src/components/{workspace,agent,canvas}`、`web/src/lib`、`web/e2e`、`README.md`、配置说明与中英文使用文档。
- 做了什么：凭据页改为实际记录列表和「凭据 / 模型」两个标签。第一步用向导创建凭据，可选择服务商或自定义 API Base、协议与 Key；第二步把「编码工具 + 凭据 + 所选模型」保存为具名 `ModelConfiguration`，Agent 再通过 `configurationId` 绑定。配置的编码工具保存后固定，支持编辑模型、停用和删除；原始模型 ID 可含 `/`，手动输入会补入服务目录。旧直接绑定、按工具默认与平台授权保持兼容，保存与执行重新检查凭据和配置引用；失效时明确失败，不回退其他 Key。Claude Code、CodeBuddy、OpenCode、Codex Responses 和 DSH 接入对应协议，Cursor/Trae 保留平台授权。旧凭据幂等映射独立服务，目录重排不切换模型、删除服务不复活、空目录不 panic；旧待补全服务可修复后保存。Agent 创建、Studio 与工作流摘要读取同一配置，切换来源清除旧模型路由并保护运行时注入变量。按反馈移除种类选择提示行，中英文别名示例改为团队共享。
- 为什么：用户要求先保存凭据，再组合编码工具、凭据和模型，形成可复用的一条模型配置。分离秘密、服务地址、工具和 Agent 引用后，同一服务可供多个工具或模型使用，平台授权不会和第三方模型 Key 混用；紧凑列表也减少原有空槽位与重复分区占据的空间。
- 如何验证：Web 全量 509 个文件、5,034 项单测通过，Lines 90.65%（门禁 85%），类型、生产构建与 lint（0 errors、12 个已有 warnings）通过；完整 critical E2E 213 项通过，兼容性收尾后补跑凭据、模型配置和 Agent 向导 13 项全部通过，覆盖创建、编辑/停用和配置引用。Server 全量测试、lint/vet/配置文档检查、核心覆盖率 90.2% 与指定 runtime 回归通过；sandbox lint/vet/全测、相关 race、核心覆盖率 96.6% 和全部语法/注入/Git/bootstrap/preview/mock smoke 通过。协议验证使用本地 HTTP 模型服务与 CLI 子进程 fixture，含 9 个适配组合、DSH ACP 路由握手、角色固定、原始 ID 含 `/`、秘密分离与失效引用关闭；未调用真实厂商模型服务。真实前端与控制面完整验证「自定义 Base/Key → Codex 模型配置 → Agent 的 configurationId」，并核对掩码、深浅主题和 320/390px 布局。actionlint、shellcheck、文档构建与官方源依赖审计通过；本地跨模块构建产生的文档 public 副本移至临时产物目录后通过品牌门禁，未修改门禁。最新 server 与 runtime bundle 已在独立预览数据库/网关运行，不改沙箱镜像或安装合约。

### 2026-10-24（编码工具、模型连接与凭据解耦设计）

- 日期：2026-10-24
- 范围：`docs/dev/agent-model-routing-design.md`、会话内交互设计原型。
- 做了什么：核对 Cursor、Claude Code、CodeBuddy、Trae、OpenCode、Codex 和固定版本 DeepSeek Harness 的官方资料，形成凭据、模型连接与 Agent 模型绑定的数据结构草案及列表/添加向导流程；把文档支持、Grasp 适配状态和实际验证结果分别记录，区分平台多模型与自带第三方服务。
- 为什么：原有模型凭据特判 OpenCode，DSH 路由限制为 DeepSeek 官方，混淆编码工具、模型厂商和秘密，无法准确表达同一工具使用不同服务或同一 Key 复用多个模型。
- 如何验证：对照现有凭据/Agent/运行时字段与官方文档，核实固定 dsh 0.2.1-rc.2 的三协议能力；交互原型脚本、模型来源切换、协议过滤、显式绑定、新增凭据/连接向导和窄屏布局通过浏览器检查。本轮仅文档与设计，不新增运行时功能；文档构建、官方源依赖审计、JSON 示例与差异检查通过。

### 2026-10-24（工作区凭据列表与添加向导）

- 日期：2026-10-24
- 范围：`web/src/components/workspace/WorkspaceCredentialsPanel.vue` 及单测、`web/src/components/agent/OpenCodeCredentialPicker.vue`、`web/src/locales/{zh-CN,en}/pages.json`、`web/e2e/workspace-credentials.spec.ts`、`CHANGELOG.md`。
- 做了什么：凭据页改为限宽的紧凑列表，按编码工具、模型 API Key、代码仓库、SSH 与其他来源分区，合并重复种类标题；名称、掩码、更新时间、状态与操作按列对齐，窄屏按行流式排布。模型密钥管理使用同样的列表样式，Agent 内的选择模式保持原样。所有添加入口保留两步向导，补充步骤进度与已选类型提示；编辑输入增加关联标签，密钥保存/清除及来源只读契约保持一致。
- 为什么：原页面单个凭据占用大量空白，名称和操作相隔整屏，逐类重复标题不便扫读；用户要求以列表展示，并用向导添加凭据。
- 如何验证：Web lint（0 errors、12 个已有 warnings）、类型检查、生产构建与全量单测通过，Lines 90.70%（门禁 85%）；凭据专项浏览器 6 项通过，覆盖桌面纵向列表、320/390px 长别名及登录文件编辑、模型密钥替换遮码、向导前进/返回与取消后清空。人工复核浅/深主题及窄屏截图。完整 critical E2E 首轮 210 项通过，零重试；actionlint、shellcheck、docs 构建与官方源依赖审计均通过。

### 2026-10-23（PR #923 同步 main 并修复合并冲突）

- 日期：2026-10-23
- 范围：`CHANGELOG.md`、`docs/dev/DEVLOG.md`，以及自动合并的 `AgentCreateWizard.vue`、`CreateAgentTeamWizard.vue`、`VendorMark.vue`、`OnboardingWizard.vue`。
- 做了什么：合并 main 的 DeepSeek Harness 接入提交（`ad058343`），在发布说明和开发日志中保留双方新增记录；复核向导自动合并结果，同时保留编码工具产品标识、配置路径隐藏和单次图标动效。
- 为什么：两条分支在相同文档位置追加记录，产生文本冲突，需要保留完整功能说明并验证组合后的行为。
- 如何验证：Web lint、类型检查和生产构建通过；504 个文件、4986 项单测全部通过，Lines 90.70%；完整 critical E2E 205 项全部通过；server 全套 lint/vet/配置文档校验/测试/runtime 子集通过，核心覆盖率 90.2%；sandbox lint/vet、语法与 smoke 通过，核心覆盖率 96.6%，runtime bundle 的 `min_image=2`，打包图标模块与源码字节一致。actionlint、shellcheck、docs 构建与依赖审计通过。

### 2026-10-23（全系统图标与单次交互动效）

- 日期：2026-10-23
- 范围：`web/src/components/ui/Icon.vue`、`web/src/lib/icons/`、`web/src/lib/composables/useIconMotion.ts`、`web/src/styles/icon-motion.css`、主站调用组件与语义映射、`web/src/liveoverlay/`、`web/scripts/build-live-overlay.mjs`、沙箱 `web/`、三份注入脚本、`web/e2e/icon-motion*`、`web/LIB_DOMAIN_MAP.json`、`CHANGELOG.md`。
- 做了什么：将原有 103 个公共图标和独立控件收敛到 141 项可信 SVG 注册表；Vue、沙箱聊天、预览选取和 Live overlay 共用几何与部件动效。默认完整静止，最近交互宿主 hover/focus-visible 播放一次，离开后可重播；动态禁用、忙碌、画布拖拽和减少动态效果均有明确处理。只有实际启动、生成与执行状态持续运动，历史、排队和等待人工静止。完成勾移除起笔点并从左向右绘制；修正标签、退出登录、复制、桶盖、文件夹、终端等，重画技能拼图、流程分支、小地图、token/队列、图片失败和中性审批形状，拆开团队/评审/表单/通知策略语义。通知只对新增未读反馈，首次/同值轮询/重挂载不播放；连续增量合并。品牌路径、插画、图表与确认仪式维持专用体系。
- 为什么：此前功能图标来源分散，部分形状和状态语义错误，等待/完成状态仍循环，完成勾与多个手写 SVG 不能按一致规则重播；统一来源也避免主站与运行时图形漂移。
- 如何验证：基于最新 main 的独立分支验证：Web lint（0 errors）、vue-tsc、生产构建通过；503 个测试文件、4965 项单测全部通过，Lines 90.69%（门禁 85%）；完整 critical E2E 203 项全部通过，包含 7 项图标动效浏览器回归。逐项验证 141 个注册图标可见、140 个功能图标确有部件动画、QQ 品牌静止，以及单次结束、重入、键盘、动态禁用祖先、嵌套宿主双向切换、画布拖拽、忙碌生命周期与减少动态效果。检查 141 项图形边界，以及 12/16/20/24px、深浅主题的 64 项尺寸样本和 320px 窄屏。server 全套 lint/vet/配置文档校验/测试/指定 runtime 子集通过，核心覆盖率 90.2%；sandbox lint/vet/覆盖率通过，核心 96.6%；注入、Git、bootstrap、mock 模型、preview-inject smoke 与运行时包构建通过。actionlint、shellcheck、docs 构建与依赖审计通过；重建注入脚本不产生额外差异，三份副本一致，运行时包 icons 模块与源码字节一致。

### 2026-10-24（修复 DSH 镜像与重启 CI 配置）

- 日期：2026-10-24
- 范围：`.github/workflows/{ci-sandbox,publish-sandbox,e2e-restart}.yml`、`server/e2e/restart_reattach_test.go` 的手动命令说明。
- 做了什么：CI/发布镜像的显式 `AGENT_PROVIDERS` 列表补入 `deepseek_harness`。重启续接 CI 构建当前提交的沙箱镜像，将同一镜像传给 gateway 与 server，并在运行真实模型测试前检查镜像级别及 OpenCode/DSH 可执行文件；同步手动测试命令。
- 为什么：旧的显式安装列表覆盖 Dockerfile 新默认值，造成构建镜像缺少 dsh；重启测试使用发布镜像 2.2.0（级别 1），无法安装本次要求 `min_image=2` 的运行时，容器在模型调用前退出。
- 如何验证：actionlint、shellcheck、diff 检查、runtime bootstrap 回归与 tagged E2E 编译/无 Key 跳过通过。实际运行时包在级别 1 时按约定退出 4、级别 2 安装成功；Compose 验证 gateway/server 均使用当前构建镜像。完整镜像和真实重启测试由推送后的 CI 再验证，未修改超时或放宽门禁。

### 2026-10-24（DSH PR 独立验证）

- 日期：2026-10-24
- 范围：DSH 的 server、sandbox、web 变更及接入文档。
- 做了什么：基于最新 main 整理原生 DeepSeek Harness 接入与工具选择区调整，单独验证凭据隔离、模型协商、角色规则、MCP、产品图标和无路径展示；保留其他独立改动原状。
- 为什么：确保 PR 的改动范围和测试证据对应，便于独立审阅和发布。
- 如何验证：server 全量测试、vet、golangci v2.12、配置文档校验、指定 runtime 回归通过，核心覆盖率 90.2%；sandbox 全量测试、vet、lint、ACP/provider race、脚本 smoke 通过，核心覆盖率 96.6%。runtime 连续构建两次字节一致，包 hash 与此前 18 项真实 Key 验证一致，`min_image=2`。Web lint、类型和构建通过；全量 503 个文件、4976 项单测通过，Lines 90.67%；首次冷运行的一项聊天时序失败经 62 项定向复测及完整重跑通过；198 项 critical E2E 首轮全部通过，零重试。actionlint、shellcheck、docs 构建和依赖审计通过；未构建/发布完整镜像，保留 PR `sandbox-images` CI 验证。

### 2026-10-24（编码工具选择区隐藏配置路径）

- 日期：2026-10-24
- 范围：`web/src/components/agent/{WizardAcpStartPathPanel,AgentCreateWizard,CreateAgentTeamWizard}.vue`、`web/e2e/agent-create-wizard.spec.ts`、`CHANGELOG.md`。
- 做了什么：工具卡片仅展示产品图标与名称，移除名称下方的配置目录、列表末尾 `configRoot` 提示和通用 API 路径附带的目录；清理共享组件的无用属性与调用。后端选择和保存仍使用原有配置目录。
- 为什么：启动方式选择只需识别产品，内部配置路径占用界面且无助于选择。
- 如何验证：新建 Agent/团队组件 13 项测试、向导浏览器 9 项、中英文无路径展示及保存 payload 的 `layout.configRoot` 断言通过；lint、类型检查、生产构建、actionlint、shellcheck、docs 构建与依赖审计通过，保留六种工具无路径截图。

### 2026-10-24（编码工具名称前展示产品图标）

- 日期：2026-10-24
- 范围：`web/src/components/agent/WizardAcpStartPathPanel.vue`、`web/src/components/onboarding/OnboardingWizard.vue`、`CHANGELOG.md`。
- 做了什么：复用 `VendorMark`，在 Cursor、Claude Code、CodeBuddy、Trae、Codex、DeepSeek Harness 名称前展示对应产品图标；新建 Agent、团队共用的选择面板及首次配置向导同步显示，图标跟随深浅主题并作为装饰隐藏重复读屏标签。首次配置向导在窄屏使用两列，给图标、名称和选中标记留足空间。
- 为什么：让编码工具列表更容易识别，与现有模型和凭据图标保持一致。
- 如何验证：Web lint、TypeScript、生产构建通过；向导浏览器 9 项通过，最终窄屏断点调整后 onboarding 5 项重跑通过。Chromium 深浅主题与 1179px/760px 下 36 项检查确认六个真实 SVG 图标已加载、位于名称前且卡片无溢出裁切；保留选择区截图。全量带覆盖率单测命令为 4967 项通过、9 项既有主题断言失败。actionlint、shellcheck、docs 构建与依赖审计通过。

### 2026-10-24（DSH 归入编码工具入口）

- 日期：2026-10-24
- 范围：`web/src/lib/shared/startPath.ts`、Agent/团队/首次配置向导、相关文案与回归测试、README、`server/README.md`、`CHANGELOG.md`。
- 做了什么：将 DeepSeek Harness 纳入编码工具列表，右侧接入方式命名为「用编码工具」；模型厂商 API Key 路径保留 OpenCode。DSH 继续使用独立凭据、模型、`/root/.dsh` 配置根和原生 `dsh --profile acp`，切换接入方式后恢复 DSH 选择。
- 为什么：接入方式应对应选择的编码工具实现，DSH 的 API Key 认证不应改变其编码工具归属。
- 如何验证：Web lint、TypeScript 与生产构建通过；Agent 创建与 onboarding 浏览器 9 项通过，最终顶部文案更新后 DSH 中英文 2 项再次通过，1179px/760px 截图确认六种工具完整显示、DSH 选中、配置根正确且无横向溢出。全量带覆盖率单测命令为 4967 项通过、9 项既有主题断言失败，DSH 分类、切换记忆、独立凭据及模型 payload 回归通过。actionlint、shellcheck、docs 构建与依赖审计通过。原生 dsh 执行链路此前已使用真实 Key 完成 18 项集成验证。

### 2026-10-23（DeepSeek Harness backend）

- 日期：2026-10-23
- 范围：`server/internal/{runtime,services,sandbox,envauth,clicatalog,config}`、`sandbox-gateway/sandbox/internal/{provider,agents,acp}`、沙箱安装/启动脚本与 `IMAGE_LEVEL`、`web/` 的 backend/凭据/创建向导及相关测试、README、`CHANGELOG.md`。
- 做了什么：新增 `deepseek_harness`，通过原生 `dsh --profile acp` 执行；独立工作区 API Key 槽映射到 `DEEPSEEK_API_KEY`，原生键和两种 ACP 别名均纳入密钥过滤，支持 Flash/Pro 和 ACP 分组模型选项。API Key 路径保留默认 OpenCode，并允许选择/记住 DeepSeek Harness；切换后端时清除共享和逐角色模型覆盖。生成配置将角色 AGENTS 与显式 alwaysApply 规则合并，保留完整 skills 和澄清节点完成规则裁剪；stdio MCP 命令解析为绝对路径。启动 patch 不含密钥，关闭 dsh 托管凭据来源以确保所选凭据生效。镜像固定 dsh `0.2.1-rc.2`，能力版本升至 2，startup/reprime 统一配置根为 `.dsh`。
- 为什么：提供独立 Harness 的工具与会话循环，复用 Grasp 现有工作流和 MCP，同时保持凭据选择、规则、模型路由与能力声明准确。dsh ACP 只提供上下文占用，继续声明不支持计费 token；bridge 重启后的自动持久化恢复不在本次范围。
- 如何验证：server 全量 Go 测试、vet、golangci v2.12、配置文档生成校验及指定 runtime 子集通过，核心覆盖率 90.2%；sandbox 全量 Go 测试、vet、lint、针对 ACP/provider 的 race 检查及核心覆盖率 96.6% 通过。actionlint、shellcheck、Shell/Node 语法、注入/Git/运行时 bootstrap/preview-inject smoke、mock 模型测试、固定版本安装与失败退出检查通过；Git smoke 设置 `GIT_CONFIG_NOSYSTEM=1` 隔离宿主机凭据 helper。运行时包连续构建两次字节一致，MANIFEST 为 `min_image=2`。Web lint/类型/构建通过，全量 4955 项通过、9 项既有主题测试失败，Lines 90.66%；完整 critical E2E 首轮 194 项通过，修正新增英文布局溢出后重跑 3 项全部通过，共覆盖 197 项；最终向导浏览器 5 项、模型切换 66 项及密钥过滤 24 项针对性测试通过，server 密钥别名保存/运行/配置过滤回归与 lint 通过。docs 构建及依赖审计通过。使用临时进程环境中的真实 API Key，对生产 server 凭据映射与配置生成、实际 sandbox WebSocket/provider、dsh 和真实平台 artifact-store MCP 完成 18 项检查：真实回复、多轮记忆、角色/规则/skill、文件、产物写入、node_complete、取消与后续对话、重连续接、Pro 模型协商全部通过；未将密钥写入仓库或日志。未构建/发布完整沙箱镜像，部署仍须按 server → 新镜像顺序进行。

### 2026-10-23（UI 切换与侧栏 PR 验证）

- 日期：2026-10-23
- 范围：`web/src/components/ui/SlidingTabs.vue`、分段控件、`web/src/views/DashboardView.vue`、侧栏导航及对应测试。
- 做了什么：将本轮 UI 调整整理到基于 main 的独立分支；修正首页顶部锁定规则的 CSS 优先级，并新增 390×700 下 Workflow / Solo 双向切换逐帧位置回归。
- 为什么：窄屏规则原先覆盖锁定的顶部占位，切换时仍会跳动；独立验证本轮改动，避免工作区其他主题修改影响 PR 的验证结果。
- 如何验证：Web lint（0 errors）、类型检查、生产构建通过；全量 502 个测试文件、4955 项单测通过，Lines 90.66%；完整 critical E2E 加首页工作流回归 201 项通过。真实控制台窄屏双向切换各采样 37 帧，顶部位移 0px。actionlint、shellcheck、docs 构建及依赖审计通过。

### 2026-10-23（设置图标再次点击返回工作区）

- 日期：2026-10-23
- 范围：`web/src/lib/composables/useSidebarNavigation.ts`、`web/src/components/shell/AppSidebar.vue`、相关单测与 `web/e2e/workspace.spec.ts`
- 做了什么：设置齿轮在工作区进入设置，在设置模式再次点击返回最后成功访问的工作区完整地址，保留查询和锚点；直接进入设置时回到首页。选中时提示「返回工作区」，成功导航后才切换上下文，账号切换清理返回位置。
- 为什么：用户希望同一图标能来回切换工作区与设置，避免重复点击仍留在设置。
- 如何验证：Shell、Sidebar、导航及交互相关单测 95 项通过，涵盖同地址返回、拦截导航、修饰键、账号隔离和设置内编辑器；真实 `/runs` → `/settings` → 工作区浏览器检查保留筛选/锚点、共享工具中返回、键盘回车及直接设置页回首页。Lint、类型检查和构建通过。

### 2026-10-23（统一最左侧导航栏外框）

- 日期：2026-10-23
- 范围：`web/src/components/shell/AppShell.vue`、`AppSidebar.vue` 及对应测试，`web/e2e/workspace.spec.ts`
- 做了什么：设置模式与工作区共用侧栏 14px 外留白和 16px 圆角外框，移除设置模式专属的直角、裁切及边框覆盖；浮动状态仅由侧栏隐藏状态决定。保留设置目录、标题及导航上下文。
- 为什么：切入设置模式时原先外留白消失、侧栏变为通高直角，导致顶部图标和底部头像移动；用户要求最左导航栏跨页面保持一致。
- 如何验证：Shell、Sidebar、导航上下文和圆角相关单测 59 项通过；设置/工作区切换、深浅主题、长页滚动、侧栏收起恢复和移动抽屉共 13 项 Playwright 测试通过；扩充既有场景比较导航栏、首图标、头像的矩形及背景、圆角、边框。Lint、类型检查及构建通过。

### 2026-10-23（统一分段 Tab 动效与运行计数移动）

- 日期：2026-10-23
- 范围：`web/src/components/ui/SlidingTabs.vue`、首页/运行/Agent/预览/工作区/用量分段组件，`web/src/views/{RunListView,TokenAnalyticsView,SandboxConsoleView}.vue`，相关单测。
- 做了什么：提取首页同款 420ms 阻尼弹簧背景，统一运行状态、用量范围和来源、编辑器模式、预览设备、通知模板等单选分段控件；保留各控件颜色、圆角和选择逻辑，选项背景移至唯一滑块。监听内容和按钮尺寸，处理徽标、翻译、换行、滚动及弹窗缩放；无选中项时隐藏背景，减少动态效果时直接定位。运行计数使用同一个可见徽标随背景移动，按钮内透明文字提供尺寸和可访问文本；按筛选缓存成功计数，未访问分类等待接口时显示省略号，避免沿用上一分类数字，不增加请求。
- 为什么：同类控件原先切换方式不一致；数字立即插入新按钮而背景仍在移动，且接口返回前显示旧计数，造成视觉顿挫。
- 如何验证：共享组件 13 项测试、页面与组件定向回归通过；Chromium 在独立固定 API 数据下验证真实 `/runs`、`/stats`、`/dashboard` 的逐帧背景和徽标同步、双向切换、快速反向、不同位数、慢请求、缓存、语言/窄屏和减少动态效果，页面无异常。运行取消/删除、Agent 默认配置、运行详情移动端和首页工作流轨道共 21 项 Playwright 测试通过（临时配置指定已有 Chromium 路径）。Lint、类型检查和生产构建通过。全量覆盖率测试 4936 项通过、9 项失败，均为当前已有默认亮色与旧深色断言冲突，覆盖率门禁未通过。新增嵌套 ResizeObserver 后同步修正 AgentFilesPanel 旧测试的单 observer 假设，按观察目标触发父面板回调。

### 2026-10-23（移除首页查看全部工作流入口）

- 日期：2026-10-23
- 范围：`web/src/views/DashboardView.vue`、Dashboard 相关测试、`web/e2e/home-workflow-rail-enter.spec.ts`
- 做了什么：移除首页普通空状态的「查看全部工作流」按钮、空容器和专用跳转函数，直接显示「新建工作流」卡片并使用正常间距；未发布工作流提示保留。同步原有单测和浏览器用例断言。
- 为什么：按用户反馈去掉多余入口，避免删除按钮后仍留下空白行。
- 如何验证：Dashboard 两组测试 83 项通过；Chromium 在独立固定数据环境中确认按钮和空容器不存在、卡片间距为 40px、切换模式后不恢复按钮，且新建卡片仍能打开创建对话框。

### 2026-10-23（精简首页工作流空状态）

- 日期：2026-10-23
- 范围：`web/src/views/DashboardView.vue`、`web/src/views/DashboardView.test.ts`
- 做了什么：去掉首页没有可用工作流时的说明段落和对应按钮多余顶距，保留「查看全部工作流」「新建工作流」入口；同步调整原有空状态断言。
- 为什么：首页不再展示用户指定删除的说明文案。
- 如何验证：Dashboard 三组测试共 85 项、Lint、类型检查和构建通过；Chromium 加载 `/dashboard`（测试浏览器内使用固定 API 数据）确认段落消失、两个入口仍可见，并验证「查看全部工作流」可跳转到工作流列表。

### 2026-10-23（工作流 / Solo 共享背景滑块）

- 日期：2026-10-23
- 范围：`web/src/components/dashboard/HomeModeSwitch.vue`
- 做了什么：用一块持续复用的浅紫色背景跟随选中按钮的位置和尺寸移动，使用阻尼弹簧采样缓动及兼容回退；按钮本身始终透明。绑定实际容器引用，在选中态更新后测量，并监听按钮尺寸变化；首次加载直接定位，减少动态效果时关闭过渡。
- 为什么：切换时应看见同一块背景连续移动，离开的选项不能残留色块；避免未绑定容器引用导致背景固定在原位。
- 如何验证：在 Chromium 中加载真实 `/dashboard` 路由（独立测试浏览器内拦截 API 为固定数据），逐帧确认双向位移、宽度贴合、单背景、无高度跳动，并验证快速反向、中英文尺寸变化、窄视口与减少动态效果。`npm run lint`、`npx vue-tsc --noEmit`、`npm run build` 通过，Dashboard 与聊天相关复查 147 项通过。全量 `npm test -- --coverage` 为 4921 项通过、10 项失败：9 项涉及当前已有的默认主题改动，另 1 项聊天时序断言单独复查通过，因此全量覆盖率门禁未通过。

### 2026-10-23（首页工作流 / Solo 切换保持顶部稳定）

- 日期：2026-10-23
- 范围：`web/src/views/DashboardView.vue`、`web/src/components/dashboard/HomeWorkflowSelect.vue`
- 做了什么：切换首页工作流与 Solo 模式时记录当前顶部占位高度，并在模式特有控件与面板更新期间锁定该高度；工具栏和模式面板通过淡入淡出与轻微位移动画切换；工作流选择器改为和 Solo 选择器一致的扁平透明样式。窗口尺寸变化或新增附件时恢复原有自适应占位。
- 为什么：两种模式下方内容高度不同，原先可收缩的顶部占位会随切换被压缩，导致角色、文案和模式切换控件整体跳动。
- 如何验证：`npm run lint`、`npx vue-tsc --noEmit`、DashboardView 相关 Vitest（83 项）和 `npm run build` 均通过。

### 2026-10-23（Workflow / Solo 会话顶部栏统一）

- 日期：2026-10-23
- 范围：`web/src/components/solo/SoloSessionPane.vue`
- 做了什么：移除 Solo 会话对共享会话工具栏、紧凑模式按钮和面板标题的专属样式覆盖，统一使用工作流 `ConversationWorkspace` 的顶部栏。
- 为什么：Workflow 与 Solo 启动后的顶部栏高度和紧凑控件表现不一致。
- 如何验证：`npm run lint`、`npx vue-tsc --noEmit`、Solo / Workflow 相关 Vitest 和 `npm run build` 均通过。

### 2026-10-23（Solo 首页选择器改为扁平控件）

- 日期：2026-10-23
- 范围：`web/src/components/solo/SoloPickerShell.vue`
- 做了什么：Solo 首页的 Agent 与仓库选择器去掉渐变填充和悬停时向外扩散的光晕，改为透明底色；打开或悬停时只显示很轻的强调色背景，同时保留键盘焦点轮廓。
- 为什么：两个选择器在首页底部工具栏中显得像独立的凸起卡片，与工具栏层级不一致。
- 如何验证：`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`（501 个文件、4923 项测试，Lines 90.57%）和 `npm run build` 均通过。

### 2026-10-23（Agent 工作目录编辑区减栏：5 栏 → 3 栏）

- 日期：2026-10-23
- 范围：`web/src/components/agent/AgentFilesPanel.vue`、`MarkdownSplitEditor.vue`、新增 `MarkdownViewToggle.vue`；对应测试；`web/src/locales/*/pages.json`（`markdownEditor.split`）
- 做了什么：桌面端原本横向 5 栏（智能体列表 | 文件树 | 编辑器+minimap | Markdown 预览 | 版本历史竖条）。现在 Markdown 编辑改成工具栏里的「编辑 / 分栏 / 预览」三态切换，默认单栏编辑，偏好存 `agent-md-view-mode`；去掉 Markdown 的 minimap；版本历史不再占右侧一栏，桌面端统一走工具栏「版本历史」按钮弹窗。旧的 `agent-md-preview-collapsed` 不再读取。
- 为什么：用户反馈分栏太多，编辑区被挤窄。
- 如何验证：`web/` 下 `vitest run src/components/agent src/views/AgentStudioView.test.ts src/locales`、`eslint`、`vue-tsc --noEmit` 通过；未在浏览器里实测。

### 2026-10-23（单沙箱跑完整个工作流：共享运行沙箱，默认关闭）

- 日期：2026-10-23
- 范围：`server/internal/runtime/acp_shared.go`、`acp_sandbox.go`、`acp_run_agent.go`、`acp_provider.go`；`server/internal/sandbox/reprime.go`、`resources.go`；`server/internal/services/sandbox_shared.go`、`sandbox_run.go`、`preview.go`、`settings.go`、`workflow.go`；`server/internal/engine/start_run.go`、`node_req.go`；`server/internal/models/models.go`；`server/internal/config/config.go`；`server/internal/handlers/settings.go`；`sandbox-gateway/sandbox/scripts/reprime.sh`、`sandbox-gateway/scripts/test-reprime.sh`、`scripts/build-sandbox-runtime.sh`
- 做了什么：新增沙箱模式 `shared`，让一次运行的所有节点共用一个沙箱，工作区（包括未提交的改动、依赖和构建缓存）在节点间保留。
  - 开关有两层，都打开才生效：平台设置 `shared_run_sandbox_enabled`（0/1，默认 0），以及工作流 `defaults.sandbox.mode`。启动运行时把模式快照到 `Run.SandboxMode`。
  - 第一个节点照常建沙箱，同时记成该运行的共享沙箱。后续节点先比较 spec 指纹（env、配置根内容、SSH/仓库凭据）；不一致就经 SSH 执行 `reprime.sh`，换上新节点的 env、配置根和凭据并重启 backend。之后在默认 chat 上连一个新会话，并按 `vars.branches` 在工作区干净时切到对应分支。
  - 节点结束只关闭会话，运行结束（`AbortRun`）时才把沙箱交给调试租约。可重试故障只重建会话，沙箱连不上或 reprime 失败才换新容器。
  - 以下情况该节点回退成自建沙箱，和 per-node 模式一样：别的节点还挂着会话（审核、闸门等待打回、接管），或者需要的镜像或工作目录不同。
  - `run+node` 的查询（沙箱视图、日志、预览）在节点自己没有沙箱行时，回退到该运行 `shared=true` 的那一行。
- 为什么：每个节点都冷启动容器、重新克隆、重装依赖，耗时长；上游没提交的产出也带不到下游。
- 如何验证：
  - server：`golangci-lint`、`go vet`、`gen-configdoc -check`、`go test ./...`、`cover-check-server.sh 90`（90.2%）、`TestRunAgent|TestReact` 均通过。
  - 新增单测：`runtime/acp_shared_test.go`（复用、同节点不 reprime、per-node 不变、挂起会话时回退、重试复用容器、reprime 失败换容器、运行结束退役、工作目录不同时回退、重启后接管）；`sandbox/reprime_test.go`；`services/sandbox_shared_test.go`；`engine/sandbox_mode_test.go`。
  - sandbox：`test-reprime.sh`、`test-inject.sh`、`test-runtime-bootstrap.sh`、`shellcheck-error.sh` 通过；运行时包构建产物里包含 `reprime.sh`。
  - 未做：真实镜像的端到端验证（三节点、两种 Agent）；前端开关 UI（另开会话做）。

### 2026-10-23（骨架屏 loading 改为中性柔光）

- 日期：2026-10-23
- 范围：`web/src/styles/global.css`（`.app-skeleton`、`.app-skeleton__block`）
- 做了什么：扫光从 accent 紫（0.18）改成正文色 4.5% 的中性柔光，光带收窄到中间 60%；周期 1.2s ease-in-out 改为 2s，前 60% 扫过、后 40% 停顿；色块加 6px 圆角；整个骨架延迟 150ms 淡入，快的加载不再闪一下骨架。减少动效模式下淡入也关掉。
- 为什么：用户反馈骨架屏"感觉卡"：每个色块紫色光带很重、扫得又快，宽行和窄行各扫各的，像在频闪。
- 如何验证：`loadingReducedMotion.test.ts`、`AppSkeleton.test.ts` 通过；`npm run lint`、`vue-tsc`。

### 2026-10-23（对话时间轴改为悬停展开目录）

- 日期：2026-10-23
- 范围：`web/src/components/chat/ChatMessageRail.vue`、`ChatMessageRail.test.ts`、`web/src/components/run/ClarifyChat.navigation.test.ts`、`web/e2e/conversation-workspace.spec.ts`
- 做了什么：左侧刻度改得更细，配色改成中性灰阶（当前项用正文色，不再用 accent）。原来悬停只弹出单条气泡，现在展开一张目录卡片：列出全部提问，当前项加粗并带指示条，悬停的行和对应刻度联动高亮，底部预览悬停项的回复，有更早消息时可以在卡片里加载。鼠标离开后延迟 120ms 再关，从刻度移到卡片上不会断开。触屏（`hover: none`）下不展开卡片，点刻度直接跳转。props 和 emits 不变。
- 为什么：用户觉得原来的时间轴质感不够，希望做成 Codex 那样的侧边目录。
- 如何验证：`ChatMessageRail` 单测 11 条、ClarifyChat 导航单测、`e2e/conversation-workspace.spec.ts` 全部通过；用 Playwright 截了亮色和暗色两张图检查。

### 2026-10-23（舞台内产物预览去掉文件名行）

- 日期：2026-10-23
- 范围：`web/src/components/run/ArtifactPreview.vue`、`web/src/lib/run/useArtifactPreview.ts`、`web/src/components/run/ReactArtifactStage.vue`、`ArtifactPreview.mode.test.ts`。
- 做了什么：`ArtifactPreview` 新增 `hideFileRow`，舞台里打开的产物不再显示「图标 + 产物名 + 文件名」那一行，只保留结构化/原文切换和操作按钮；工具栏成为首行时补 8px 顶部间距。产物库等其他入口不受影响。
- 为什么：舞台页签已经显示产物名，这一行重复。
- 如何验证：lint 0 error、`vue-tsc`、`npm test -- --coverage`（4916 项，Lines 90.55%）。

### 2026-10-23（产物抽屉工作流列表对齐舞台卡片样式）

- 日期：2026-10-23
- 范围：`web/src/components/run/StageWorkflow.vue`、`web/src/components/run/ReactArtifactStage.test.ts`。
- 做了什么：去掉与状态胶囊重复的左侧状态点轨道，改用节点图标做时间线并用短连线串联；列表包进一张卡片，改用舞台的 `--ra-*` 令牌（面板、描边、阴影、强调色），「当前」标记改成与卡片类型角标同款；当前行只保留浅强调底色，等待中节点只淡化文字和图标。
- 为什么：原样式状态点贴边、与状态胶囊重复，当前行描边框打断轨道对齐，且用的是全局令牌，与同屏资源/产物卡片不统一。
- 如何验证：lint 0 error、`vue-tsc`、run/views 1721 项测试；临时 Playwright 页面截取亮/暗两套主题确认（截完已删）。

### 2026-10-23（侧边栏历史执行去时钟图标，参照 Codex）

- 日期：2026-10-23
- 范围：`web/src/components/shell/SidebarWorkspaceLists.vue`、`web/src/components/shell/AppSidebarNav.vue`、`web/src/components/ui/Icon.vue`、`web/src/styles/global.css`、`web/src/components/run/ArtifactPreview.vue` 及对应测试。
- 做了什么：「历史执行」去掉每行相同的 `history` 时钟图标，行改为纯文字（13px、行高 32px、左右 10px，与「正在执行」标题和行对齐）；失败改为行尾 14px 红色圆圈感叹号（`<title>` 失败），取消保留行尾灰点；标题字号与「正在执行」统一为 11.5px。随后两个区块标题前加 14px 图标（「正在执行」新增状态图标 `run-live` 圆圈播放，轮廓与「历史执行」`history-back` 一致；先试的 `play-continue` 用户认为不协调），标题挂 `grasp-status-host`，复用 `Icon.vue` 状态图标的 hover（放大、提亮、一次 wake 动效，尊重 reduced-motion）；「历史执行」可点击，hover 时文字同时提亮。行用 `styles/global.css` 的 `.sidebar-tree-item` 包一层，画从标题图标下来的竖线和每行圆角分支（行自身因 hover-ink 的 `overflow: clip` 不能画在外侧），标题文字与行文字对齐。产物预览文件头在文件名已带扩展名时不再重复显示 `JSON` 等类型。
- 为什么：时钟图标每行重复、不传达信息；参照 Codex 侧边栏：行首不放图标，状态只在需要时出现在行尾。中途试过行首状态列，用户认为不如 Codex 方案，已撤回。
- 如何验证：`npx vitest run src/components/shell src/components/run/ArtifactPreview`、`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`；用 e2e mock（`vite.e2e.config.ts`）截浅色/深色侧边栏确认对齐。

### 2026-10-23（分屏抽屉 `+` 新页签与去掉重复标题行）

- 日期：2026-10-23
- 范围：`web/src/components/run/ReactArtifactStage.vue`、`web/src/components/chat/ConversationWorkspace.vue` 及对应测试。
- 做了什么：分屏抽屉里点 `+` 不再整条标签栏消失，而是保留已开标签并新增可关闭的「新页签」显示首页卡片，选卡片后替换该占位；分屏下同一模式的已开标签全部显示，关闭标签切到相邻标签，无剩余才回首页；分屏不再在预览注册时自动挂出应用标签。桌面分屏抽屉去掉「资源 / 产物 ×」标题行（顶栏切换按钮已能命名和关闭），窄屏仍保留「返回对话」。
- 为什么：原 `+` 只是切回首页，分屏只显示当前标签，于是点 `+` 后看不到任何标签；标题行与顶栏切换按钮重复。
- 如何验证：`npm run lint`（0 error）、`npx vue-tsc --noEmit`、`npm test -- --coverage`（500 文件 / 4915 测试，Lines 90.55%）。

### 2026-10-23（Solo 资源抽屉对齐工作流启动）

- 日期：2026-10-23
- 范围：`web/src/components/solo/SoloSessionPane.vue`、`web/src/components/run/ReactArtifactStage.vue`、`web/src/components/run/SandboxStageSurface.vue`。
- 做了什么：Solo 直接复用工作流的 `ReviewShell → ReactArtifactStage` 组合，统一资源与产物抽屉的状态、卡片网格、行高和预览逻辑；资源卡片包含 IDE、Terminal、浏览器、日志，标签条支持 `+` 返回资源/产物首页，独立 Solo 沙箱日志通过沙箱日志接口加载，产物列表持续刷新，顶栏固定为工作流同款 44px 高度。
- 为什么：Solo 与工作流启动应提供一致的资源入口和调试面板，避免同一沙箱出现两套交互。
- 如何验证：相关 ESLint、`npx vue-tsc --noEmit`、ReactArtifactStage/ConversationWorkspace 78 项测试和生产构建通过。

### 2026-10-23（统一 Solo 会话执行入口）

- 日期：2026-10-23
- 范围：`server/internal/services/{sandbox_solo.go,sandbox_agent.go,sandbox_test_pool.go,solo.go}`、`web/src/components/shell/AppSidebarNav.vue`、`web/src/views/SoloView.vue`、`web/src/components/solo/SoloSessionPane.vue`、`web/src/lib/solo/useSoloChat.ts`、Solo 路由及侧栏回归测试。
- 做了什么：活动中的 Solo 沙箱现在和工作流启动/运行共用侧栏「正在执行」分组，支持同款右键置顶、重命名和关闭入口。移除 Solo 页内嵌的会话列表和重复筛选入口，聊天路由改为工作流会话同款全屏壳层、顶栏、资源/产物切换和右侧面板；启动 Solo 时注册独立 artifact-store MCP 作用域，继承 Agent 的 `tools/reads/writes` 能力，并在面板中展示 Agent 能力和已写产物。
- 为什么：Solo 会话与工作流执行被分在两套列表中，启动中的会话无法在统一的执行入口里找到，页面也重复展示活动会话。
- 如何验证：完整 Web 单测 500 个文件 / 4915 项通过，Lines 90.5%；`npx vue-tsc --noEmit`、相关 ESLint（0 error）、生产构建以及 `AppSidebarNav`、ConversationWorkspace 单测（59 项）通过；当前环境未安装 Go，未执行 Go 编译/测试。

### 2026-10-22（澄清聊天接收父级属性）

- 日期：2026-10-22
- 范围：`web/src/components/run/ClarifyChat.vue`、`ClarifyChat.footerShrink.test.ts`、CHANGELOG。
- 做了什么：显式把父级属性绑定到聊天主列，覆盖 class、style、id、ARIA 和原生事件。
- 为什么：聊天主列与图片预览构成多个根节点，父级属性无法自动继承，审批页面因此持续产生 Vue 警告（#815、#682 后续）。
- 如何验证：固定基线新增两例因属性丢失失败，修复后通过；完整 Web 单测 494 文件 / 4848 项通过，Lines 91.46%；lint 0 error、vue-tsc、生产构建、docs 构建及 actionlint / shellcheck-error 通过。Windows 关键路径 195/196，首次安装向导团队页高度断言在同一基线也以 21px 溢出失败；聊天底栏几何验收通过。

### 2026-10-20（聊天操作配色与主次层级统一）

- 日期：2026-10-20
- 范围：`web/src/styles/chat.css`、`web/src/components/run/ClarifyChat.vue`、`web/src/components/run/gateApproval/GateHotUnifiedActions.vue` 及既有预览选择/审批操作测试；CHANGELOG。
- 做了什么：发送、选择/表单提交与确认流转使用同一品牌紫实底，由全局 accent 统一加深，白色图标/文字在明暗主题及悬停时均保持可读。推荐、前后题导航、记录问题及异常中止确认使用中性次要操作；禁用提交改为中性灰实色，不再用紫色整体透明度。选中项保留轻紫底/细边，推荐标签改为中性；选项按钮用 aria-pressed 表达选择，既有预览回归改按该状态读取。移动/自适应审批布局同步统一发送/停止/确认图标与颜色，手机文字操作至少 44px，保留输入底栏的 36px 高度链。没有修改推荐立即提交、校验、禁用、排队、流转和选项切换条件。
- 为什么：用户指出同页按钮不协调。原先推荐紫描边、禁用浅紫提交、浅紫流转和部分布局的绿色确认同时出现，无法从颜色清楚区分可用主操作、辅助动作与禁用状态；旧主色白字及深色主题的亮紫悬停对比也不足。
- 如何验证：真实 AppShell/生产聊天组件在明暗、1440/390px、短屏与减少动效等 22 组场景完成 81 张截图复核，无页面错误；主按钮白字实测常态 5.23:1、悬停 6.03:1，禁用为中性灰、推荐/次要操作为中性，手机操作至少 44px。检查采用推荐立即提交、手动选择、表单、上下题、附件、发送/停止、排队、保留草稿及右侧产物打开/返回，PM/Agent 独立输入区同样继承正确操作色。既有预览选择回归改用 aria-pressed 后 12 项通过，审批操作 3 项通过；最终完整 Web 单测 467 文件 / 4344 项通过，Lines 91.16% ≥ 85。首轮覆盖率进程退出但未生成完整摘要，未计通过；源码不变独立重跑后 default 摘要、JSON success/failed=0 与覆盖率报告三者相符。关键路径、对话工作区与澄清交互 149/149 浏览器用例通过（5.1 分钟，无重试）。lint 0 error（12 条既有 warning）、末次审批文件局部 lint、vue-tsc、生产构建及品牌/安全上下文检查、根 actionlint / shellcheck-error、docs 构建与官方 registry 高危审计、git diff --check 均通过；使用合成数据，未新增样式镜像测试。

### 2026-10-20（首页背景与应用画布统一）

- 日期：2026-10-20
- 范围：`web/src/views/DashboardView.vue`、`web/src/components/dashboard/HomeParticleMeshBackground.vue`、既有首页浅色/布局样式断言；CHANGELOG。
- 做了什么：首页移除独立的灰紫色主题变量，沿用全局明暗主题。装饰粒子层改为透明，工作流条两侧渐隐使用页面 surface，消除首页与外层画布的矩形色差。输入框和工作流卡片继续使用 surface，避免移除旧变量后被浅色 elevated 灰底替代；阴影改为中性黑。粒子、吉祥物与布局/交互逻辑保留。
- 为什么：用户指出截图中的背景颜色不协调。原首页局部 base 浅色为 250/249/251、深色为 24/23/29，粒子容器实心铺满，而外层应用使用白色/中性深色 surface，形成了单独的灰紫色块。
- 如何验证：真实 App 在 1440/390px × 明暗四组检查中，首页、粒子容器和 canvas 计算背景均透明，继承的 surface/base/文字/强调色与应用一致；浅色画布及输入框为 255/255/255，深色为 20/20/23，工作流渐隐与画布一致，无全页横向溢出。切换主题保留输入草稿，减少动效时吉祥物静止；四组截图复核完成。更新两处既有旧调色断言，保留焦点优先级、scoped 编译与减少动效断言，聚焦 16 项通过。最终完整 Web 单测 467 文件 / 4344 项通过，Lines 91.16% ≥ 85；关键路径浏览器 136/136 通过（4.8 分钟，无重试）。lint 0 error（12 条既有 warning）、最终两测试局部 lint、vue-tsc、生产构建及品牌/安全上下文检查、根 actionlint / shellcheck-error、docs 构建与官方 registry 高危审计、git diff --check 均通过；未新增样式镜像测试，使用合成数据。

### 2026-10-20（聊天视觉、图标与动效统一）

- 日期：2026-10-20
- 范围：共享 `chat.css`；Clarify/Review/PM/Agent 聊天与 ComposerShell、PageCollaborationControls、AgentTimeline/AgentToolGroup/ThoughtSummaryStatus、ConversationWorkspace/ChatMessageRail、CitationCard、InboxMotionMark、Icon 与既有相关单测/浏览器测试；CHANGELOG。
- 做了什么：正文统一 15px 与宽松行距，用户消息用中性浅底，链接/主要动作/运行状态才用主题紫。三类输入区统一为上方正文、下方附件/圆形发送箭头/停止动作，确认使用轻紫文字控件；手机主要操作命中区至少 44px。精修聊天专用发送/停止/产物面板 SVG，修正共享 Icon 随 name/size/实心状态实时更新。工具和思考统一 16px 图标、紧凑行、左侧细线详情、键盘焦点和展开关联，完成态降噪、失败态保留语义色。历史选择/表单摘要移除多层框，仍保留输入和选中边界，普通附件改中性文件图标。产物入口、引用、定位浮层共同使用轻边线与清晰反馈；运行反馈仅旋转或呼吸，取消重复闪动/跳点，减少动效时静止。保留原有流式、队列、发送/停止、审批、锚点、产物并排与历史逻辑。
- 为什么：用户要求聊天 UI、SVG、颜色与动画协调。原先各入口的按钮尺寸、纸飞机/停止图形、紫绿青配色与多种动画各自独立，动态图标还有旧图形残留，需以阅读和操作为核心统一。统一桌面 36px、手机 44px 的输入动作尺寸，同时修复确认消失后的 4px 高度跳动及嵌入页协作按钮的 2px 对齐差；文本框取消重复原生焦点框，保留输入容器的可见焦点反馈。
- 如何验证：源码冻结后完整 Web 单测 467 文件 / 4344 项通过，Lines 91.16% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、生产构建及品牌/安全上下文检查通过。关键路径与对话工作区、澄清交互专项 149/149 浏览器用例通过（4.6 分钟，无重试），确认布局专项 16/16 与嵌入协作菜单专项 1/1 通过，覆盖菜单键盘操作、草稿与输入区高度稳定。真实 App/生产组件在桌面、手机、短屏、明暗主题和减少动效等 21 组场景完成 75 张截图复核，无页面错误或根容器横向溢出；最终另复核 5 组 Agent 焦点，原生白框已移除而外层焦点仍可见。检查发送/停止、附件、排队输入、工具/思考状态切换、长输出、定位最终回复以及产物并排打开保留草稿和阅读位置；手机可点击工具行与输入动作均 44px，减少动效时运行图形静止。失败小字按主题混色后实测对比度为浅色 6.75:1、深色 5.38:1；共享图标同一实例切换形状/尺寸有回归覆盖。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过；只使用合成 API/会话数据，未执行真实业务写入。

### 2026-10-20（移除待办对话顶部运行栏）

- 日期：2026-10-20
- 范围：`web/src/views/GatesInboxView.vue`、既有上下文生命周期与确认离开浏览器身份断言；CHANGELOG。
- 做了什么：删除桌面对话顶部 Run 编号、节点标识和打开运行详情整行，同时移除加载态的详情标题条。手机删除打开运行详情入口，保留原有返回列表与复制分享链接操作，可分享条目才显示分享栏。详情容器保留不可见的 Run/节点数据属性，让原有切换、确认离开及异步防串线测试继续检查实际上下文身份。
- 为什么：用户要求顶部按 Codex 的对话结构简化，移除重复的运行信息和跳转栏，给对话更多空间。
- 如何验证：原有上下文生命周期 34 项单测通过；完整 Web 单测 467 文件 / 4340 条通过，Lines 91.16% ≥ 85。首次并行全量中一个依赖 30ms 定时器的既有 PM 加载态断言未捕获中间态，独立复核通过；降低至 4 workers 后完整覆盖率重跑通过，未修改该测试。lint 0 error（12 条既有 warning）、最终相关 lint、vue-tsc、生产构建及品牌/安全上下文检查通过。源码冻结后关键路径与待办确认离开、空/加载/失败状态专项 147/147 浏览器用例通过（5.3 分钟，无重试）。真实 App 在 1440/390px × 明暗四组检查中，旧 Run/节点/详情链接和加载标题未出现、无遗留空栏；桌面对话与详情顶边一致，聊天及输入高度正常。产物侧栏/手机覆盖预览可打开并保留草稿，审批、可分享与普通门禁的分享入口条件、手机返回列表正常，无页面错误或横向溢出；观察加载过程没有旧标题闪现。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过；只使用合成数据，未新增样式镜像测试。

### 2026-10-20（聊天定位只预览最后回复）

- 日期：2026-10-20
- 范围：PM/Clarify/Agent 聊天定位预览及相关测试、`web/src/lib/chat/messagePreview.ts`；CHANGELOG。
- 做了什么：每个用户问题的定位摘要只使用最后一条有正文的助手回复。Clarify 和 Agent 优先取最后一个非空 message part/step，兼容没有时间线块的正文；PM 后续回复及非空流式正文直接替换旧摘要。跳过空白、思考和工具记录；保留最终回复内部全部段落、原始 Markdown 与提问卡提示回退，下一用户问题重新开始边界。共享预览的 Markdown 解析、长度限制和定位锚点不变。
- 为什么：用户截图中的定位浮层拼接了多次过程输出，最终答复被截断；需要只展示截图红框中的最后回复，不能按空行再裁掉该回复的前半段。
- 如何验证：补充最后非空 message 块、多段 Markdown、同轮多回复、尾随空白/工具/思考、下一用户边界、提问卡回退和流式替换/累加的回归，5 文件 / 93 项聚焦单测通过；完整 Web 单测 467 文件 / 4340 条通过，Lines 91.16% ≥ 85。lint 0 error（12 条既有 warning）、最终局部 lint、vue-tsc、生产构建与品牌/安全上下文检查通过。源码冻结后关键路径及对话工作区 144/144 浏览器用例通过（4.7 分钟，无重试）。真实 Clarify 组件在 1440/390px × 明暗四组检查中，浮层只显示最终回复的两段正文，排除独立过程回复、同 parts 较早 message、尾随工具/思考/空消息与下一用户回答；Markdown 已解析、文件名完整保留。真实 applyReviewFrame / applyAcpEvents 流式追加即时更新预览，用户锚点保持且点击定位正常，无页面错误及聊天/全页横向溢出。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过；只使用合成数据。

### 2026-10-20（通知目录与未读层级优化）

- 日期：2026-10-20
- 范围：`web/src/views/NotificationsView.vue`、双语通知说明；CHANGELOG。
- 做了什么：通知页统一为平面收件目录，筛选、全局未读计数和全部已读操作放在同一工具栏。整行紫底和左色条改为未读小圆点、字重及读屏提示；状态使用图标和文字，来源与 Run 标识为较弱的辅助信息。长标题、来源与 ID 有宽度约束及完整悬停文字，手机标题最多两行并显示时间和入口提示；悬停和键盘聚焦均显示入口，筛选用 group/aria-pressed 表达按条件查看，选中下划线显式使用主题色。同一 Run 状态改变时按状态更新图标实例，避免重试后图形仍停留在旧状态。沿用原有取数、分页、查看结果及标读行为。
- 为什么：用户指出通知页同样需要优化；重复紫色整行和有框状态标签占据主要视觉，手机隐藏时间且长标识可能撑宽。
- 如何验证：完整 Web 单测 467 文件 / 4335 条通过，Lines 91.15% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、生产构建及品牌/安全上下文检查通过。最后补充按状态更新图标实例后，相关 4 文件 / 40 项单测、局部 lint 与完整构建再次通过。最终关键路径与通知专项 145/145 浏览器用例通过（4.2 分钟，无重试）。真实 App 合成 54 条通知在 8 组尺寸/明暗布局与 6 组加载、空列表和既有请求失败状态中检查：长标题/标识不撑宽，页面仅一层实际纵滚，390×600 短屏可达分页、时间和入口；筛选、翻页、键盘、完成预览显式标读、失败详情自动标读、全账户全部已读与刷新保持均正常，内存路由返回恢复全部/第一页。另在桌面浅色与手机深色验证同一 Run 失败→完成→失败，严格保持原行及列表 DOM，只替换状态图标，图形/文字/主题色同步。所有真实 App 检查无页面错误。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过；只使用合成数据，未新增样式镜像测试。

### 2026-10-20（审计目录与操作记录重新排版）

- 日期：2026-10-20
- 范围：`web/src/components/project/{ProjectAuditPanel,AuditFilterDropdown}.vue` 与既有选择器测试；双语审计文案、半径与审计浏览器既有断言；CHANGELOG。
- 做了什么：桌面/手机共用一份工具栏与筛选模板，查看方式改为下划线导航，统计变为行内文字，导出独立对齐；搜索和筛选另起一行。分组与记录使用共同 surface、紧凑单行组头、细分隔线和一次可见列标，组内保留语义表头；摘要可通过原生按钮展开，补齐展开状态及详情关联，时间保留原始秒级值的悬停提示。长摘要/资源按自身区域截断或换行，详情与失败标记保留；手机所有文字与详情使用主题 token，筛选可收起并以两列排列，Run/caller 和 Run 模式时间独占一行。选择器保留分组/辅助说明，补键盘/焦点/ARIA、翻转定位与离屏关闭。明细脱敏标记使用主题警告色。共享样式断言取消旧胶囊导航要求，搜索/导出仍用 8px 控件圆角。
- 为什么：用户反馈审计页不够清晰；单行混排筛选、模式与统计，分组大灰底和重复表头稀释了操作摘要，手机还有硬编码浅色文字与底色。
- 如何验证：完整 Web 单测 467 文件 / 4335 条通过，Lines 91.15% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、生产构建及品牌/安全上下文检查通过。关键路径及审计专项 144/144 浏览器用例通过（4.2 分钟，无重试）。真实 App 合成 90 条日志的 6 组明暗/尺寸交互检查覆盖分组默认展开/折叠、摘要与 JSON 详情、键盘菜单、Run/节点/资源/调用者筛选、搜索恢复、分页及每页 20 条、18 次带筛选范围的真实 JSON 下载；最终 8 组布局含 390×600 短屏，无全页或面板横溢出、无第二层实际纵滚，手机展开筛选后的记录区由 62px 增至 114px。最后补回主题化脱敏高亮后，相关 62 项单测、审计 8 项浏览器用例、4 组脱敏色/字面 HTML 检查及完整构建再次通过；所有真实 App 检查无页面错误。旧胶囊样式断言已随下划线导航更新，未新增样式镜像测试。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过；只使用合成数据。

### 2026-10-20（用量统计明细统一滚动）

- 日期：2026-10-20
- 范围：`web/src/views/TokenAnalyticsView.vue`；CHANGELOG。
- 做了什么：保留既有页面主纵向滚动，模型和运行明细移除 380px 高度上限及内部 flex 高度约束，按行自然展开；补齐页面与表格宽度约束，窄屏仅表内横滚。运行表以四列的会话列宽加操作列计算固定布局宽度，避免 max-content 被长文本撑大，同时保留拖列、标题截断、下钻与导出。模型名称由单元格整体截断改为图例、可收缩文本与徽标的行布局，保留可读前缀，模型名称可悬停查看全名。
- 为什么：用户反馈用量统计运行明细有多层滚动条；表格限高与页面滚动叠加，长内容还会扩大横向滚动范围。
- 如何验证：完整 Web 单测 467 文件 / 4320 条通过，Lines 91.09% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、生产构建及品牌/安全上下文检查通过。一次并行单测运行出现既有 theme.ts 延时清理在测试环境销毁后的未处理异常，构建结束后原样完整复跑通过，未修改主题代码。真实 App 合成数据在 1440/900/390px 明暗六场景检查 40 个模型、60 条运行及长连续名称：shell 无实际纵滚，只有主区纵滚；两表无纵向溢出，滚轮可带动主区到底，无全页或主区横向溢出，窄表可横滚到尾部操作。各场景 CSV 导出完整行与长值、末行详情/事件、模型下钻参数均正常；桌面拖宽运行列至 870px 仍仅表内横滚；模型前缀可见、全名 title 完整，未知徽标保留，无页面错误。源码冻结后关键路径及统计/模型专项 149/149 浏览器用例通过（4.7 分钟，无重试）。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过；仅调整布局，未新增样式镜像测试或操作真实数据。

### 2026-10-20（工作流通知策略主题选择菜单）

- 日期：2026-10-20
- 范围：`web/src/views/WorkflowsView.vue`、`web/src/components/ui/AppSelect.vue` 与既有/交互测试；通知相关三份 E2E；CHANGELOG。
- 做了什么：通知策略复用 AppSelect 的轻量 ghost 变体，替换原生 select 和重复的自绘箭头。收起时为紧凑文本按钮，展开为主题色菜单与选中勾；通知列和触发器适当加宽，菜单提供最小阅读宽度与视口约束。沿用三种模式、通知事件 PATCH 和保存禁用/失败保留原值；重复选择当前模式不再发送 PATCH。共享选择器补齐键盘焦点关联、首尾定位、Tab 关闭与 Escape 返回，定位完成后再把活动选项滚入可见区；搜索输入保留文本编辑操作，关闭动画中的菜单退出无障碍与交互树。ghost 触发器完全滚出视口或被滚动祖先裁切后自动收起且不抢回焦点；部分可见时继续跟随并限制在视口内。
- 为什么：用户反馈系统原生下拉弹层与 Grasp 主题不协调；复用已有菜单组件可统一外观，同时保留实际鼠标、键盘和触屏选择能力。
- 如何验证：完整 Web 单测 467 文件 / 4320 条通过，Lines 91.09% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、生产构建及品牌/安全上下文检查通过。共享选择器 41 项单测及工作流 8 项测试覆盖键盘、无障碍、禁用、滚动定位、重复选择与保存失败。真实 App 合成数据在 1440/900/390px 明暗及中英文共 8 组截图、16 组操作检查和桌面/手机 2 项离屏复验中通过，三种模式仅 PATCH 通知策略，事件选择保留其他值，菜单无越界、页面错误或横向溢出。源码冻结后关键路径及通知专项 143/143 浏览器用例通过（4.8 分钟，无重试）。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过；只使用合成数据。

### 2026-10-20（看板增加轻量边界）

- 日期：2026-10-20
- 范围：`web/src/components/board/RunBoardColumn.vue`、`RunBoardCard.vue`；CHANGELOG。
- 做了什么：状态列由上下横线改为细边框、小圆角的无阴影容器，列标题增加轻量主题底色和左右留白；任务正文、截断提示与查看更多对齐，行间保留稍清晰的细分隔线和原有悬停/键盘焦点。连续英文长标题允许任意位置换行，避免在变窄的列中压到状态与优先级标签。
- 为什么：用户反馈看板过于平铺，需要适当边框区分运行状态与任务；仅为状态列补充结构边界，避免恢复多层任务卡片。
- 如何验证：完整 Web 单测 467 文件 / 4295 条通过，Lines 91.07% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、独立生产构建及品牌/安全上下文检查通过。真实 App 在 1440/1280/1024/390px 明暗八组验证 3→2→1→1 列断点、1px 边框/12px 圆角/无阴影和 35% 表头底色；每组实际列内滚动 450px，附加失败列增删、预览与列头模态开关均正常，长中文/连续英文标题与状态/优先级双标签不重叠，列体无横向溢出、页面错误或缓存 504。源码冻结后关键路径与看板专项 144/144 浏览器用例通过（5.0 分钟，无重试），包括列内滚动、状态列表分页、空态、预览互斥和手机布局。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过；仅改样式，未新增测试或改变运行逻辑。

### 2026-10-20（待办对话移除外框）

- 日期：2026-10-20
- 范围：`web/src/views/GatesInboxView.vue`；CHANGELOG。
- 做了什么：桌面详情、手机详情和桌面初次加载的对话容器移除 card 外壳，保留主题 surface，取消外圈边框、圆角和阴影。内部工具条与右侧产物分隔保留，flex 高度、裁切和确认动画定位保持不变。
- 为什么：用户指出 Run 信息与对话外侧仍有卡片边框，希望对话直接融入页面；加载态同步处理，避免进入页面时闪回旧外框。
- 如何验证：完整 Web 单测 467 文件 / 4295 条通过，Lines 91.07% ≥ 85；lint 0 error（12 条既有 warning），vue-tsc、生产构建、品牌/安全上下文检查通过。真实 App 合成数据在 1440/390px 明暗四组中确认详情四边 border-width=0、四角 radius=0、box-shadow=none；产物打开/关闭正常，手机 dialog、桌面并排，关闭后聊天可见，无页面错误、横向溢出或缓存 504。源码冻结后关键路径与对话、真实待办预览复审专项 147/147 浏览器用例通过（4.7 分钟，无重试），涵盖滚动保位、手机返回、草稿保留、确认流转与侧栏状态。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 通过。仅调整三处样式，未新增测试或改变业务逻辑。

### 2026-10-20（对话定位摘要解析 Markdown）

- 日期：2026-10-20
- 范围：`web/src/components/chat/ChatMessageRail.vue`、`web/src/lib/chat/messagePreview.ts`、`web/LIB_DOMAIN_MAP.json`；PM/Clarify/Agent 聊天摘要生产端与回归测试、对话浏览器验收；CHANGELOG。
- 做了什么：在共享定位组件中统一把 Markdown 转为纯文本摘要，复用聊天解析器和未挂载的 template 提取文字；保留段落、列表、表格单元格之间的间隔，图片仅使用说明文字。摘要与无障碍标题在解析后限长，标题按消息身份/原文缓存，回复仅在预览时解析并缓存当前原文，避免流式更新时重复解析旧回答。移除 PM 删除 Markdown 字符的正则，三个入口均传入完整原文并保留回复段落边界。
- 为什么：用户截图中澄清聊天的悬停摘要直接显示 `**修缺陷**`，定位预览应显示可读摘要；原来的不同入口分别用纯文本、删字符正则和提前截断，既暴露标记，又可能损坏文件名、代码和列表。
- 如何验证：补充截图原文、加粗/列表、链接/图片、代码与下划线、普通星号、HTML 字面量、表格间隔、解析后截断和无障碍标题的回归；澄清正文仍保留 strong 排版。完整 Web 单测 467 文件 / 4295 条通过，Lines 91.07% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、生产构建与品牌/安全上下文检查通过。真实 App 合成数据在 1440/390px 明暗四组检查中均无页面错误、横向溢出或浮层越界，摘要不露格式标记，正文加粗和文件名保持。最终浏览器关键路径与聊天、附件、历史专项 150/150 通过（4.6 分钟，无重试），覆盖悬停预览、点击定位、历史锚点、右侧产物、手机审批和公开分享；测试服务依赖预优化缓存过期后重启并完整复跑。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 均通过；仅使用合成数据。

### 2026-10-20（聊天视觉与主题统一）

- 日期：2026-10-20
- 范围：`web/src/styles/{global,chat}.css`；工作区助手与引用、Clarify/Review/Agent 聊天、工具/思考时间线、LLM 阅读器、ComposerShell、ConversationWorkspace 与 ChatMessageRail；既有高度链测试；CHANGELOG。
- 做了什么：把共同聊天视觉提取为独立样式，只引用现有深浅主题 token。用户消息使用主题淡紫背景和轻边线，助手保持无框正文；正文 14px / 1.85 行高，标题、引用、代码与表格有共同阅读层级，时间/用量/复制合为 11px 元信息。输入容器统一 16px 圆角与细边线，聚焦采用轻量紫色 ring；定位刻度和产物动作使用品牌强调色，移除硬编码蓝色、工具输出渐变和发送按钮 glow。引用以图标和轻链接展示，移除裸类型标签与空时间占位。思考/工具详情保留语义状态色和折叠逻辑；窄屏宽表保留单列最小阅读宽度并在消息内横向滚动，避免中文逐字竖排。保留现有正文列宽、输入/工具栏几何、队列、历史、锚点、产物身份与公开分享行为。
- 为什么：用户反馈聊天样式需要符合项目主题。此前各入口的字色、字号、气泡和输入边框由局部 utility/scoped CSS 分别控制，正文偏淡、元信息零散，引用还有独立蓝色，需按现有紫色主题统一层次。
- 如何验证：完整 Web 单测 466 文件 / 4286 条通过，Lines 91.06% ≥ 85；lint 0 error（12 条既有 warning），vue-tsc 与生产构建及品牌/安全上下文检查通过。10 个明暗主题 × 1440/1280/1024/768/390 页面组合验证气泡色、正文/元信息计算样式、右侧预览、草稿和 DOM 保持，均无页面错误和横向溢出；另以真实 Markdown 渲染检查桌面/手机明暗四组长文、代码、引用和宽表。既有高度链源码测试改为检查必要 token，继续约束 flex 收缩、overflow 与填满宽度。源码冻结后，浏览器关键路径及聊天、图片附件、历史分页专项 150/150 通过（4.9 分钟，无重试），包括右侧产物、定位、窄屏发送/确认、公开分享、工作流与设置导航。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计、git diff --check 均通过；仅使用合成数据。

### 2026-10-20（对话主栏、右侧产物与消息定位）

- 日期：2026-10-20
- 范围：`web/src/components/chat/`、`web/src/lib/chat/conversationArtifacts.ts`、PM/Clarify/Review/Agent 聊天与工具时间线、ReviewShell 及 Run/Gate/Public/Embed 宿主、产物版本选择器、运行详情布局预算、双语文案与域清单；组件测试、合成 fixture 与浏览器回归；单工作区方案、CHANGELOG。
- 做了什么：统一为自然聊天正文、共享阅读列与输入区，中性用户气泡和轻量工具记录。产物引用/工具入口按需打开右侧预览，桌面支持指针与键盘调宽；窄画布切换到可返回的独立预览，聊天保持挂载，展开/关闭/调宽保留阅读段落。提问刻度支持悬停/聚焦摘要、点击定位和短高亮、键盘以及加载更早消息；问题身份在历史追加和流式落库后保持稳定，导航取消待执行的自动跟随。PM 读取文件不等待可选运行元数据，裸文件名明确选择来源并与服务端大小写规则一致，处理删除末页、过时响应和精确身份校验。运行舞台及编辑器保留版本、标注、排队、确认语义和公开 token API；工具严格绑定本运行，公开凭据切换清理旧面板和聊天定位。版本菜单先 Escape 收起，面板关闭时同步清理浮层；移除原 ReviewShell 左舞台/右窄聊及移动底部抽屉分支和旧预算。
- 为什么：用户希望采用 Codex 的对话阅读方式，产物在右侧打开，并可通过左侧刻度预览与定位长会话；需覆盖各聊天入口，避免卡壳、底部抽屉和历史/流式导航互相抢位置。浏览器检查同时发现宽屏 flex 宿主未填满导致误进窄屏模式，已补齐宽度约束。
- 如何验证：完整 Web 单测 466 文件 / 4286 条通过，Lines 91.06% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、生产构建与品牌/安全上下文检查通过。共享组件、历史竞态、来源/删除/菜单与运行/公开凭据隔离均有专项回归；最后的宽度 class 调整另复跑壳布局测试与构建。源码与浏览器夹具冻结后，关键路径及受影响场景 163/163 浏览器用例通过（5.0 分钟，未启用框架重试），包括新增 8 项对话定位、来源选择、右栏伸缩保位、窄屏焦点/草稿和菜单两级 Escape 验收，以及审批发送/确认、公开访客隔离、历史分页/重试、附件、Live、运行详情、工作流画布和设置导航。明暗主题 × 1440/1280/1024/768/390 的真实 App 合成页面检查共 10 组，全部无页面错误或横向溢出，草稿与 DOM 保持，宽屏并排、窄屏独立预览符合实际可用宽度；已保存预览和刻度摘要截图。旧审批测试夹具同步到真实 App 侧栏待办与按需产物入口，空上下文、加载恢复及手机返回后确认均经过重新验证。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计通过；只使用合成数据。

### 2026-10-20（统一应用纯色背景）

- 日期：2026-10-20
- 范围：`web/src/components/shell/AppShell.vue` / 既有测试、`web/src/styles/global.css`、`web/src/lib/shared/theme.test.ts`、`web/e2e/radius-visual-main.ts`；CHANGELOG。
- 做了什么：移除外壳的装饰点阵及精确路径背景白名单，全部应用页面使用共同纯色画布；浅色使用白色 surface、深色使用现有 surface。主题过渡与减少动态选择器统一到新画布类。
- 为什么：用户反馈工作流背景仍有密集点阵。旧逻辑按 route.path 严格匹配，而路由同时接受尾斜杠、大小写变体，这些地址会误落入旧点阵分支；不再让普通页面背景依赖路径例外。
- 如何验证：完整 Web 单测 460 文件 / 4254 条通过，Lines 91.02% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、完整构建及品牌检查通过。源码与测试冻结后关键路径浏览器回归 136/136 通过（4.1 分钟，无重试），覆盖导航、主题/减少动态、流程画布编辑与明暗截图。另以真实 App 合成 fixture 验证 20 个页面组合：三种工作流路径各明暗 × 1440/390，及看板/运行/首页/设置代表组合；所有外壳 backgroundImage=none，浅色 rgb(255,255,255)、深色 rgb(20,20,23) 精确匹配 surface，主题按钮往返切换正常，无 pageerror 或页面横溢出。产物 CSS 已无旧外壳点阵规则。root actionlint / shellcheck-error、docs 构建及官方 registry 高危审计通过，git diff --check 通过；只操作合成数据。

### 2026-10-20（产物文件库与设置管理页去卡片化）

- 日期：2026-10-20
- 范围：`web/src/views/{ArtifactsView,AgentStudioView,BoardView,TokenAnalyticsView,NotificationsView,SandboxListView,WorkspaceDefaultsView}.vue`；产物目录/预览/结构化报告、Agent 配置、统计、审计、凭据、共享配置、定时任务与看板组件；双语文案、组件测试、浏览器合成 fixture 与 E2E；单工作区方案、CHANGELOG。
- 做了什么：逐项复核设置目录中的全部页面，管理列表和统计分组以自然画布、细分隔线与紧凑记录替代重复大圆角外框。产物页移除常驻来源列和全页卡壳，保留文件目录与宽预览，工作流/会话来源通过带搜索的按需选择器筛选；保留 URL/历史、运行分组/打包、检索、分页、版本、结构化/原始内容、导出和删除。来源选择支持焦点、Tab/Escape 与关闭后返回触发器。窄画布按实际内容宽度切换文件列表与预览；结构化报告采用结论、摘要、分节和列表的文档排版，保留注释路径、截图、反馈时间轴及导出根。Agent 窄画布把目录放到内容上方，配置页签、文件历史与正文保持可达；变量表单也按实际内容宽度调整。去掉产物诗句和实现细节文案，改为功能说明。
- 为什么：用户指出产物和其他设置页仍大量套卡，三层目录挤压预览，平板虽无页面溢出却把编辑字段压窄；需完整检查页面及内层报告，统一信息层级。
- 如何验证：最终完整 Web 单测 460 文件 / 4254 条通过，Lines 91.02% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、完整构建与品牌检查通过。源码冻结后关键路径及受影响场景浏览器回归 194/194 通过（5.5 分钟，无重试），包括来源搜索/键盘/会话筛选、真实空白和加载恢复、文件检索/删除邻居、按运行 ZIP、HTML 历史版本只读、结构化/原始视图、明暗 PNG/PDF 实际文件下载、手机与 768px Agent 编辑、通知/审计/凭据/统计下钻及设置目录保持。设置内 16 场景 × 1440/768/390 检查，修复变量字段被挤为 74px、看板三列窄排和 Cron 逐字换行；768px Agent 编辑区由 94px 恢复为 374px，Cron 可横向滚到行尾操作。产物整体明暗 390/768/1024/1440 与英文页面截图检查通过；9 类报告及反馈索引/单轮反馈另做桌面/手机 22 状态、41 张截图复核，长文可滚到底，表格在自身区域横滚，反馈展开与懒加载正常。独立审查发现的末页删除空预览与跨来源旧文件误标均已修正并补测试；过时悬浮卡样式断言与旧通知 fixture 的用户名/导航定位、catalog 入口已按实际界面同步，失败项先单独通过再完成整套冷启动。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计通过，git diff --check 通过；只操作合成数据。

### 2026-10-20（浏览器标签图标统一为 Grasp 角色）

- 日期：2026-10-20
- 范围：`web/index.html`、`web/public/favicon.{svg,ico}`、`web/src/assets/brand/favicon.svg`、`media/{favicon.svg,BRAND.md}`；CHANGELOG。
- 做了什么：复用侧栏 Grasp 角色的静态路径、五官与配色，保持透明背景和方形留白；同步三份 SVG 源，导出 16/24/32/48/64/128/256px 标准 ICO。页面改用服务端已有的 `/favicon.ico` 路径并加稳定版本参数，更新浏览器缓存。
- 为什么：用户指定标签页使用当前侧栏角色；原页面引用 SVG，但生产服务仅注册 ICO 静态路径，SVG 会落入 SPA fallback。
- 如何验证：完整 Web 构建（含 vue-tsc、品牌与安全上下文检查）通过；构建预览页面的 icon link 指向新版本 ICO，HTTP 200 / image/x-icon，浏览器原生解码成功。ICO 7 个尺寸均有效，16px 帧角落透明；三份 SVG SHA256 一致，dist ICO 与 public 源文件逐字节一致。git diff --check 通过；未改动应用业务逻辑。

### 2026-10-20（工作流目录、需求工作台与助手对话画布）

- 日期：2026-10-20
- 范围：`web/src/components/shell/{AppShell,AppSidebar,AppSidebarNav}.vue`；`web/src/views/WorkflowsView.vue` / 测试；`web/src/components/project/RequirementDraftsPanel.vue` / 测试与 `web/src/lib/project/useRequirementDrafts.ts`；`web/src/components/pm/PmLeaderChat.vue` / 测试与 `web/src/lib/pm/usePmLeaderChat.ts`；双语文案、`web/e2e/{workspace*,notify-inherit-label,run-notify-ui,requirement-drafts-schedule}`、移除旧 `.seg` 样式约束的 `web/src/styles/radiusZeroClearance.test.ts`；CHANGELOG。
- 做了什么：工作流目录去除表格外框与灰表头，以细线、状态点和轻量行尾操作区分层级；通知策略改为当前值下拉，自定义事件按需展开，手机按内容区宽度重排为分隔行，空工作区与搜索无结果分别提供创建/导入或清空搜索。需求草稿采用视图页签和独立搜索筛选工具条，列表、排期和 Markdown 工作台去除多层卡框；甘特检查器选中后才显示且可收起，纯未排期不画空日历；真实空白、筛选无匹配与加载失败分别处理，清空筛选后保留未保存正文。助手移除全页卡壳与能力介绍网格，未配置仅保留轻量说明和单一设置入口；欢迎问题、原输入框和轻建议列表聚合，首发后同一输入框接入消息画布；历史消息采用自然正文和中性用户气泡，中等宽度会话目录转横向短列表，手机会话输入区为悬浮导航留出空间，附件可直接点击。三类工作页直接进入时也采用纯色画布；工作区和设置统一白色目录、极浅中性灰图标轨及低对比选中项，深色沿用已有 surface/base 色阶。需求编辑按真实内容宽度调整列表、日期字段与源码/预览模式，避免平板被固定侧栏挤窄。历史分页失败后保留错误提示，重复触顶事件不自动再次请求；离开顶部再返回或主动点击重试才继续加载，短消息窗口也能恢复。
- 为什么：用户指出工作流、需求草稿和助手仍像大卡片，空白偏多、工具条层层套框。按列表管理、规划编辑和自然对话分别安排信息密度，使主要内容与操作更直接。
- 如何验证：最终完整 Web 单测 459 文件 / 4240 条通过，Lines 90.94% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、完整构建及品牌检查通过。冻结后关键路径及本次受影响场景冷启动 154/154 条浏览器回归通过（4.6 分钟，无重试），包括工作流通知下拉/事件真实载荷、空目录与搜索恢复、需求首稿创建/排期/正文保存/窄容器源码预览、历史分页失败重复触顶与再次重试、手机附件中心点击及导航。明暗主题 390/768/1024/1440/1920px 截图与真实 App 测量通过；768px 日期输入全宽、正文可到达，手机历史会话附件不被悬浮导航遮挡。首轮历史重试回归暴露重复滚动事件导致立刻重请求，已修复并补齐 77 条 PM 目标测试；通知新增用例的 fixture 按实际 notifyPolicy 请求包修正后，两失败用例先单独通过，再完成整套冷启动。root actionlint / shellcheck-error、docs 构建及官方 registry 高危审计通过；只操作合成数据。

### 2026-10-20（参考 Codex 统一设置目录与表单）

- 日期：2026-10-20
- 范围：`web/src/components/shell/{AppShell,AppSidebar,AppSidebarNav,WorkspacePage}.vue` 与测试、`web/src/data/settingsNav.ts`；新增 `web/src/components/settings/{SettingsSection,SettingsRow}.vue`；`web/src/views/{SettingsView,WorkspaceDefaultsView,WorkspaceToolView}.vue`、`web/src/components/{settings/IntegrationsPanel,pm/PmSettingsPanel,pm/PmChannelMultiPanel,project/ProjectNotifyPanel,project/ProjectExternalMcpPanel}.vue` / 测试；双语文案、`web/e2e/workspace*`、设置布局契约测试；方案、CHANGELOG。
- 做了什么：设置模式保留最外层窄图标轨，旁边为 248px 设置目录和纯色画布，按工作区工具、执行配置、平台配置依次排列；保留返回工作区、侧栏隐藏/恢复、账号菜单和手机抽屉，继续不设侧栏搜索。设置表单统一 860px 居中，使用平铺分组、细线分隔及左说明右控件的设置行，不再叠加卡片或面板外框；手机输入上下排列、开关保持右侧。常规设置退出固定高度壳，恢复自然内容高度和页面单一滚动。重排常规/品牌/资源、工作区元信息、默认助手/MCP/自动审批、通知事件与模板、外部 MCP/API Key 和集成说明；保存放在对应配置后，渠道编辑改为单列字段组并保留独立保存语义。表格、编辑器与工作区工具仍使用适合其内容的宽布局。
- 为什么：用户提供 Codex 设置参考，要求保留外侧图标轨、常用工具置顶，并让右侧摆脱卡片和面板形式；此前多层导航、品牌头、点阵、满屏空白与嵌套滚动打断连续配置。此前要求不加左侧搜索和设置内切页不退出目录继续有效。
- 如何验证：最终完整 Web 单测 459 文件 / 4223 条通过，Lines 90.92% ≥ 85；lint 0 error（12 条既有 warning）、vue-tsc、完整构建与品牌检查通过。冻结后关键路径及本次受影响场景冷启动浏览器回归 181/181 条通过（5.6 分钟，无重试），覆盖内外图标轨一致、设置目录保持/手机抽屉重建、工作区工具置顶、中英文 Agent 标题完整和更多菜单、环境锁定/真实保存、明暗主题常规页面滚轮到保存及 MCP 单一滚动。6 类设置页的明暗桌面/手机共 24 张截图与英文手机页检查无横向溢出及 pageerror，长页均可滚到底。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计通过；全部浏览器操作使用合成数据。

### 2026-10-20（看板与用量统计去重、助手页面重排）

- 日期：2026-10-20
- 范围：`web/src/views/{BoardView,TokenAnalyticsView,WorkspaceToolView}.vue`、`web/src/components/{board,token-analytics,pm/PmLeaderChat,shell/WorkspacePage,agent/AgentProjectSidebar.vue}` 与相关测试、`web/src/lib/pm/usePmLeaderChat.ts`、API client / types、双语文案、`web/e2e`、`web/package.json`；旧 Board 用量 API 与专用聚合链、`server/internal/services/pm_thread_view.go` 及 PM handler/测试；方案、CHANGELOG。
- 做了什么：看板移除整块 Token 分析与专用图表，聚焦当前执行概览、运行列和待人工事项，保留预览、所有状态、分页与更多运行；只以链接进入统一用量统计。统计重新安排时间/来源筛选、KPI、主趋势、分布排行和明细，保留费用定价、下钻、CSV 和既有来源口径。清理无调用的 Board 统计接口与 client，公共图表 helper 移至统计目录。助手按未配置/首次提问/历史会话分开呈现，只留一个设置入口；未启用时无空列表、失效输入与建议按钮，历史仍可读，默认 Agent 失效不阻断可用的旧线程；PM 列表/创建/恢复接口使用非持久化线程投影，按历史线程原 Agent 返回实际可用状态，配置丢失时仍可读历史；发送总开关暂停后仍可停止正在执行的回合。线程加载失败提供重试。修复手机首次提问创建线程后退回列表的状态切换。智能体列表完整显示标题，次要动作收进「更多」菜单，避免名称被挤成单字。
- 为什么：用户指出看板与统计重复、页面不好看，截图中未启用横条、多个设置入口和大块空会话占据界面。运行处理与用量分析应分工清楚，页面布局应匹配实际可操作状态。
- 如何验证：最终完整 Web 单测 459 文件 / 4223 条通过，Lines 90.92%；lint、vue-tsc、完整构建通过。关键路径及受影响场景冷启动浏览器回归 181/181 条通过（无重试），含看板状态/预览/分页、用量筛选/下钻/CSV、助手首次提问与手机发送、默认或历史 Agent 失效/暂停发送后历史可读等场景；Agent 标题和菜单中英文实测通过。相关服务端完整 Go 测试、vet、golangci-lint（0 issues）、configdoc、核心覆盖 90.3% 与 runtime 专项通过。根 actionlint / shellcheck-error、docs 构建与审计通过；未操作实际业务数据。

### 2026-10-20（设置目录内切换工具不退出侧栏）

- 日期：2026-10-20
- 范围：`web/src/lib/composables/useSidebarNavigation.ts` 及测试、`web/src/components/shell/{AppShell,AppSidebar,AppSidebarNav}.vue` / 测试、`web/src/data/settingsNav.ts`、`web/e2e/workspace.spec.ts`、`web/LIB_DOMAIN_MAP.json`、CHANGELOG。
- 做了什么：设置侧栏选择看板、工作流、需求草稿和工作区助手时保留目录，仅切换内容和当前项；图标轨统一高亮设置，手机抽屉重建仍保留导航上下文。返回首页或显式选择主图标轨后退出；导航被阻止或在新标签打开时不切换当前上下文，同地址图标轨点击仍可切换。全屏编辑暂用工作区导航，返回共享工具时恢复原设置目录；账号切换清理上下文。
- 为什么：用户指出点击设置中的工具入口后侧栏自动退回工作流列表，打断连续配置操作。不能把所有工作流直接访问都改成设置模式，应保留用户实际进入路径。
- 如何验证：新增真实路由测试覆盖目录保持、抽屉重建、全屏往返、同地址点击、导航拒绝、新标签点击及账号切换；工作区真实应用浏览器 6 条通过，包含桌面四个共享入口及手机重开目录。完整单测 462 文件 / 4241 条通过，Lines 90.86%；冻结代码后关键路径冷启动 119/119 条通过（无重试）；首轮一个动画等待用例被开发热更新中断，冻结后整套复跑通过。lint 0 error（12 条既有 warning）、vue-tsc、完整构建与品牌检查通过。根 actionlint / shellcheck-error、docs 构建与官方 registry 高危审计通过；同步团队向导旧测试，断言初始化复用工作区、不再发送 projectName。

### 2026-10-20（单工作区落地与一次性历史迁移）

- 日期：2026-10-20
- 范围：`server/cmd/server`、新增 `server/cmd/workspace-migrate` / `internal/workspacemigration`、`server/internal/{database,router,handlers,models,services,runtime,channels,engine}`；`web/src/{router,data,lib,components,views,locales}`、`web/e2e`、`web/LIB_DOMAIN_MAP.json`、`web/package.json`；README 中英文、用户指南、`server/WORKSPACE_MIGRATION.md`、方案、CHANGELOG。
- 做了什么：移除项目列表、选择器、项目 CRUD 和旧项目 API，统一唯一工作区与轻量工作流目录；侧栏参考 Codex 直接列全部工作流和最近运行，不设搜索，管理页保留搜索、编辑/运行及更多菜单。原配置分到设置和独立页面；保留手动 head / 首页与 v1 发布图语义。工作流普通默认覆盖保存完整变量、Secret、通知及自动审批；Agent 普通凭据绑定支持 ID / 关闭 / 继承，精确登录回写不误写其他账号。历史审计与账本全局读、旧助手线程按原 Agent 可读可续，发送总开关独立；团队初始化复用唯一工作区，账号切换隔离晚到响应和编辑草稿。废弃项目 UI / E2E 移除，有效场景改为真实工作区路由。
- 迁移：新增 SQLite / MySQL 离线 plan / apply / verify / rollback，完整类型化数据库与文件备份、来源指纹、分阶段 journal、可空 DDL 与事务数据转换、配置物化和版本保全；不保留旧项目 ACL、来源配置映射或双接口。阻断权限扩大、跨来源凭据、孤立/损坏身份、活动回合、claimed 任务、未排空会话与不安全路径。处理工作流/记忆/凭据别名重名链、内置别名正常化和数据库实际排序规则，保护大整数 ID、凭据密文与文件权限；拒绝新写入后的覆盖回滚、损坏备份以及 symlink 祖先路径重叠。合并/重归属前须保全并退休存活 Run 容器；明确重连不刷新已有 MCP/环境/凭据，持久历史走正常冷恢复。维护接口冻结新请求并排空已知队列，归档退休非 Run 容器；操作文档说明旧多项目版本的独立排空前提，不让新版兼容启动旧库。
- 为什么：用户要求移除项目维度、保留现有历史数据，除一次性迁移外减少历史包袱，并按 Codex 参考设计紧凑侧栏和完整前端。
- 如何验证：完整 Go 测试通过，最后受影响包与 runtime 专项复跑通过；核心覆盖 90.3% ≥ 90，golangci-lint 0 issues、vet、configdoc 通过。迁移包/CLI 回归覆盖备份、幂等、失败恢复、半 DDL、指纹与凭据运行等价性；真实隔离 MySQL 8.4 完成建库、合并、verify、rollback，含排序规则重名和超过 2^53 的账本 ID。Web 完整单测 461 文件 / 4237 条通过，Lines 90.86% ≥ 85；lint 0 error（12 warning）、vue-tsc 与完整构建/品牌检查通过；关键路径冷启动 117/117 条通过（3.5 分钟、无重试），最后中英文 MCP/画布文案清理再通过 14 文件 / 124 条目标测试及完整 lint/tsc/build；真实工具签名逐项保持一致。新增设置/统计等浏览器 38 场景、看板 13 场景通过。桌面、深浅主题与 390px 视图检查无脚本异常或横向溢出。根 actionlint、shellcheck-error、docs 构建和官方 registry 高危审计通过；未迁移或写入实际业务库，测试 MySQL 为临时合成数据。

### 2026-10-20（单工作区改造评估与实施方案）

- 日期：2026-10-20
- 范围：`docs/dev/single-workspace-plan.md`、`docs/dev/DEVLOG.md`
- 做了什么：梳理原十三个标签的导航去向、前后端项目依赖、十三类直接归属模型、文件与浏览器草稿、运行版本语义、配置继承、对外授权、历史查询和维护切换；形成分阶段实施、预检决策、验收与回滚方案。按用户要求保留历史数据，将旧项目处理限定为一次性迁移，长期采用单工作区与必要普通配置覆盖，明确旧入口退出标准。
- 为什么：用户希望移除项目维度、侧栏直接展示全部工作流、原项目功能归入设置，并要求先充分评估，不为旧项目长期保留兼容包袱。
- 如何验证：交叉复审前端、持久化和运行时接口；核实手动 head 与首页/v1 发布版、版本裁剪、编辑器备份、凭据选择、出站队列排空、Codex 登录回写顺序和 SQLite/MySQL 边界。文档本地链接检查、docs 构建、官方 registry 高危审计、根 actionlint 与 shellcheck-error 均通过；未修改业务代码，实际业务数据由部署前只读预检确认。

### 2026-10-20（草稿确认动效与界面变更合并检查）

- 日期：2026-10-20
- 范围：`web/src/components/run/ClarifyChat.vue` / 测试；`web/src/lib/inbox/useClarifyChat.ts` / actions 测试；新增 `web/src/lib/composables/useComposerActionMotion.ts` / 测试；`web/src/lib/run/useHomeApproveChat.ts` / 测试；`HomePrioritySelect.vue` / 测试；`web/e2e/confirm-flow-clip.spec.ts`、`web/package.json`、`web/LIB_DOMAIN_MAP.json`、`CONTRIBUTING.md`、`CHANGELOG.md`
- 做了什么：未发送文字、附件、批注和发送等待状态隐藏流转按钮，清空或发送完成后恢复。将宽度与透明度过渡调整为 280ms、起止速度为零的缓动，按钮间隙同时收起；快速反转保留当前帧，减少动态效果即时收尾，卸载清理动画与偏好监听。移除临时草稿提示及未使用的双语 key，复审保留固定说明，发送等待改为按钮内转圈和 `aria-busy`；按钮隐藏前将焦点送回输入框。异常状态的「中止当前回合并确认」也阻止草稿或未确认发送，保留忙碌中止与 cold 隐藏草稿的既有语义。
- Review 修复：短窗口优先级面板只滚动自身以完整显示键盘选中的选项；项目 compose 查询在隐藏或刷新工作流后仍持续限制项目候选，失败回滚不误跳，草稿保留。启动请求与 launch 回调使用启动时工作流快照，等待期间切换项目不会把真实运行带到其它项目/节点。基于最新主分支重新整合，保留待审批分享权限和预览加载覆盖层。将草稿动效、HTML 版本工具栏、侧栏隐藏恢复与 Yume 回归纳入关键路径入口，并登记新模块。
- 为什么：用户要求编辑内容时隐藏流转、动画更柔和、取消提示，并在提交 PR 前整理代码和完成充分 review、测试与 CI 保障。
- 如何验证：最终完整单测 470 文件 / 4358 条通过，Lines 90.84% ≥ 85%；eslint 0 error（15 条既有 warning）、`vue-tsc`、构建和品牌检查通过。`CI=1 npm run test:e2e:ci` 冷启动 113 条全过；预览、导出、hover 和圆角额外回归 9 条通过，无重试。新增测试覆盖反转时旧回调隔离、动态减少效果、卸载清理、焦点、发送反馈、普通及异常确认草稿保护、项目候选失效与异步启动切换。隔离浏览器深浅主题、390px 手机和 200/240/400px 分栏验证正常；首 16.64ms 位移由旧曲线约 33% 降至 2.81%，输入栏与发送按钮全程无垂直跳动，快速反转无尺寸残留；无脚本错误或真实业务写请求。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计通过。

### 2026-10-20（HTML 预览单行工具栏）

- 日期：2026-10-20
- 范围：`web/src/components/run/ArtifactPreview.vue` 与相关测试；`web/src/components/ui/HtmlPreview.vue` / 测试；`web/e2e/artifacts-page-version-main.ts`、`artifacts-page-version.spec.ts`；`CHANGELOG.md`
- 做了什么：移除 HTML 预览的完整身份信息行，将版本、历史只读、删除、复制及下载收进同一工具栏，复制/下载位于「窗口放大查看」左侧；为 HTML 下载提供正确标签。收紧带扩展操作的间距，411px 中文分栏保持 45px 单行，更窄时自然换行。沿用主分支始终挂载的 HTML 工具栏与画布加载/错误/空态覆盖层，不重复错误条，保留其它产物头部和导出操作；放大窗口不复制扩展操作或重挂原 iframe。版本演示夹具更新为持久 revision API，移除旧 execution 输出模拟。
- 为什么：用户批注指出文件类型、名称及路径占据重复一行，希望复制和下载与放大操作集中展示。
- 如何验证：相关四份单测 46 条通过；浏览器版本、历史只读、411px/240px 几何及预览/导出回归通过，并纳入最终 113 条关键路径。隔离浏览器验证中英文、深浅主题、手机与窄分栏，复制内容与实际下载的 `page.html` 逐字一致，设备切换、取点和放大正常，无脚本错误或业务写请求。
- 附加检查记录：额外尝试的旧 `artifacts-page-loading.spec.ts` 仍等待已移除的 `HardLoadLayer`，夹具未模拟当前 `getArtifactTree`；已核对主分支与本分支的 `ArtifactsView.vue` 无差异，属于既有演示夹具失配，保留失败记录，未扩展修改无关列表逻辑或加入关键路径。

### 2026-10-20（聊天输入与流转操作合并）

- 日期：2026-10-20
- 范围：`web/src/components/run/ClarifyChat.vue`、`ComposerShell.vue` 与对应测试；`web/src/lib/inbox/useClarifyChat.ts` / actions 测试；`web/src/locales/{zh-CN,en}/pages.json`；`web/e2e/confirm-flow-clip-main.ts`、`confirm-flow-clip.spec.ts`；`CHANGELOG.md`
- 做了什么：将活动会话的「确认并流转」收进输入面板底部，与发送/取消共用操作栏，移除原外置按钮行；工具栏可按需换行，默认共用组件布局保持兼容，提示仅在实际需要时提供 slot。未发送的文字、附件、批注以及发送待确认期间会阻止流转，并显示双语提示；不自动把发送消息与接受当前结果合成一次请求，空输入 Enter 仍不会流转。冷会话保留仅确认入口，不因隐藏遗留草稿锁死；忙碌、队列、权限、错误重试与中止行为保留。
- 为什么：用户希望输入框和下方独立流转按钮合并，减少割裂与占高；同时避免误跳过未发送内容。
- 如何验证：目标组件及草稿保护测试 174 条、完整单测 468 文件 / 4310 条通过，Lines 90.78% ≥ 85%；eslint（0 error）、`vue-tsc`、构建和品牌断言通过。Playwright 输入/流转裁切及草稿保护 11 条、关键路径与侧栏 96 条、hover 2 条、圆角视觉 1 条共 110 条全部通过，无重试。真实示例浅/深主题与 390px 手机只读检查通过；mock 400/240/200px 中英文布局完整、附件/发送/流转不重叠，另确认 busy 下取消和发送可用、冷会话例外、hideFinish、错误条、草稿和附件清空恢复均正常，无脚本错误或真实业务写请求。根 actionlint / shellcheck-error、docs 构建和官方 registry 高危审计通过。

### 2026-10-20（Yume 尾根修复与角色动效）

- 日期：2026-10-20
- 范围：`web/src/components/dashboard/HomeYumeCharacter.vue` / 测试；`web/src/components/shell/BrandCharacterMark.vue` / 测试；新增 `web/e2e/yume-motion.spec.ts`；`web/package.json`；`CHANGELOG.md`
- 做了什么：首页尾巴改为身体轮廓内的固定局部锚点，身体遮住尾根，尾巴与身体共用呼吸、倾斜和按压/跳跃祖先层。新增轻呼吸与好奇摆动、错峰双眨眼、悬停/键盘焦点招呼摆尾及表情回应，保留眼睛跟随指针、240×200 外框与黑白样貌。侧栏小角色分层实现呼吸、微摆、双眨眼、品牌区悬停回应及按压回弹，保留 28px/48px 接口和装饰语义；品牌文字保持对齐。动态启用减少动态效果时停止全部位移、重置眼睛和交互态、清理回弹定时器，卸载移除监听和定时器。三条浏览器回归加入关键路径命令，覆盖尾根连续性、减少动态效果和品牌文字不随内部角色偏移。
- 为什么：用户指出首页角色尾巴偶尔出现断口，并希望首页和侧栏角色增加动效。旧尾巴以整个 SVG 的 `(0,200)` 为旋转轴，+10°时尾根偏移约 31.86 屏幕像素、留下约 5px 可见缝。
- 如何验证：目标组件 13 条、完整单测 468 文件 / 4300 条通过，Lines 90.77% ≥ 85%；eslint（0 error）、`vue-tsc`、最终构建和品牌断言通过。真实浏览器对静止、悬停、按压、跳跃共 148 个关键帧采样，尾根与身体锚点最大误差 0.000033px，所有帧尾根都在身体填充内，外框仍为 240×200；深浅主题、390px 手机、28px/48px 标记、鼠标与键盘、动态减少效果/恢复及眼睛跟随检查通过，减少效果下包括腮红渐变在内均为 0 动画。录制 11.6 秒真实页面演示，未发业务写请求。最终关键路径加侧栏 Playwright 96 条通过，另最终角色回归 3 条、hover 2 条、圆角视觉 1 条通过；首轮取点用例在顶层页面和 iframe 重载时超时，保留 trace，源码稳定后隔离及整组复跑均通过，未改业务代码或重试配置。根 actionlint / shellcheck-error、docs 构建和官方 registry 高危审计通过。

### 2026-10-20（侧栏内容层级与新需求入口）

- 日期：2026-10-20
- 范围：`web/src/components/shell/AppSidebar.vue`、`AppSidebarNav.vue`、`SidebarWorkspaceLists.vue`、`StatusMetrics.vue`；新增 `SidebarAccountMenu.vue` / 测试；`web/src/components/ui/Icon.vue`；`web/src/lib/run/useHomeApproveChat.ts` / 测试；`web/src/locales/{zh-CN,en}/{nav,route}.json` 与相关现有测试；`web/e2e/hover-ink-scroll-shift.spec.ts`；`CHANGELOG.md`
- 做了什么：开始入口及路由标题改为「新需求」/「New request」，使用专用创建图标、无边框入口与中性选中底色。统一待审批、项目、最近列表的标题颜色、字号、行高与左右留白，待审批标题显示真实数量，澄清、复审、审批使用统一细线 SVG 与柔和语义色。项目展开内容增加缩进导线，最近运行补充会话图标，选中行使用柔和底色和短标记；展开控制移到左侧文件夹，右侧按参考增加更多和新需求动作。品牌行下移并与外轨对齐；底部仅保留用量和语言/主题/通知，账户名及登出收进外轨头像菜单。修复用量提示首次显示时，普通块级元素测量得到视口全宽、导致左侧越界的问题：在测量前即使用 fixed 定位。
- 交互：项目更多菜单提供真实的项目详情和项目运行列表链接；项目新需求使用 `projectId` 查询参数选中该项目可在首页启动的工作流，支持同页切换，保留跨项目列表和草稿正文/附件。异步草稿恢复不覆盖明确项目上下文；该项目没有可启动工作流时提示并返回项目详情。手动选择其它项目工作流后清除 URL 的项目参数，保留其它查询，确保再次点击原项目的新需求能重新生效。账户菜单与项目菜单补充键盘、外部关闭和视口定位；隐藏桌面栏或切到手机时关闭账户浮层。
- 为什么：用户要求进一步协调侧栏内部布局，并明确把「开始」改为「新需求」；按后续批注去除入口描边、整理账户展示、统一状态图标与项目操作。
- 如何验证：完整单测 468 文件 / 4296 条通过，Lines 90.75% ≥ 85%；eslint（0 error）、`vue-tsc`、构建和品牌断言通过；源码冻结后关键路径加侧栏隐藏/恢复 Playwright 93 条、hover 位移回归 2 条、圆角视觉 1 条全部通过。新增菜单与项目上下文测试覆盖键盘、外点/隐藏关闭、首次定位、同页切换、不可启动项目回退、异步草稿恢复及重复项目入口。真实浏览器在 1440×900、1183×1244 深浅主题确认品牌与外轨中心一致、新需求无边框、项目悬停动作与账户菜单正常；两真实项目入口选择对应工作流，390px 抽屉操作可达、首次用量提示完整显示，无横向溢出、脚本错误或业务写请求。根 actionlint / shellcheck-error、docs 构建和官方 registry 高危审计通过。

### 2026-10-20（两层侧栏与真实项目/最近运行列表）

- 日期：2026-10-20
- 范围：`web/src/components/shell/AppSidebar.vue`、`AppSidebarNav.vue`、`AppShell.vue` 及对应测试；新增 `SidebarWorkspaceLists.vue` / 测试；`web/src/lib/inbox/reviewLayoutBudget.ts` / 测试；`web/src/locales/{zh-CN,en}/nav.json`；`web/e2e/hover-ink-scroll-shift.spec.ts`、`radius-visual-main.ts`；`CHANGELOG.md`
- 做了什么：桌面左侧改为 56px 功能图标轨加 248px 内容列表，保留品牌角色、隐藏/恢复导航、账户和主题通知工具；开始/项目/运行/设置切换放到图标轨，内侧保留开始、收藏和待审批，新增真实项目与最近运行列表。项目默认折叠，点击标题进入项目，独立箭头按需读取该项目最近两条运行；最近列表读取全局最近记录。默认只发项目和运行两个列表请求，路由变化只额外刷新当前展开项目，取消旧请求并忽略晚到响应。待审批重命名通过标题映射即时同步到最近和已展开项目记录。补双语加载、失败重试和空态文案；移动抽屉保持单列，隐藏的桌面列表在手机端不额外取数。同步首帧宽度预算、测试替身与演示尺寸。
- 为什么：按用户提供的 Codex 侧栏参考，让功能切换与工作内容分层，项目和运行记录能直接找到，同时保留现有审批、重命名、置顶、收藏和抽屉交互。
- 如何验证：现有侧栏/布局与新增列表行为测试、双语 key/编译测试、完整单测（467 文件 / 4277 条，Lines 90.71% ≥ 85%）、关键路径加侧栏隐藏/恢复 Playwright（93 条）、eslint（0 error）、`vue-tsc` 和构建通过；hover 位移回归 2 条、深浅圆角视觉 1 条通过。浏览器实测桌面 304px（含外留白 318px）、隐藏/恢复与焦点交接、四类导航与设置分类、项目按需请求、最近运行、收藏/重命名取消及 390px 单列抽屉。根 actionlint / shellcheck-error、docs 构建和官方 registry 高危审计通过。

### 2026-10-20（左侧主导航略微加宽）

- 日期：2026-10-20
- 范围：`web/src/components/shell/AppSidebar.vue`、`AppSidebar.test.ts`、`AppShell.test.ts`、`web/src/lib/inbox/reviewLayoutBudget.ts` 及对应测试、`web/e2e/desktop-sidebar-hide.spec.ts`、`CHANGELOG.md`
- 做了什么：桌面主导航展开宽度由 232px 调整为 248px，外层占位与内卡同步；运行详情挂载前工作区估算同步扣除侧栏与外侧留白，让初始分栏预算跟随新尺寸。更新已有断言、测试替身与展开宽度注释。
- 为什么：用户希望左侧导航略宽，为图标和导航文字增加少量空间；折叠与移动端行为保持。
- 如何验证：现有侧栏与布局测试（40 条）、完整单测（466 文件 / 4265 条，Lines 90.70% ≥ 85%）、关键路径加侧栏隐藏/恢复 Playwright（93 条）、eslint（0 error）、`vue-tsc` 及构建通过；实机确认外栏与内卡 248px、含留白 262px，隐藏后 0、恢复后 248px，390px 手机视口固定侧栏隐藏且无横向溢出或脚本错误。根 actionlint / shellcheck-error、docs 构建和官方 registry 高危审计通过。

### 2026-10-20（资源卡图标统一为正面窗口 SVG）

- 日期：2026-10-20
- 范围：`web/src/components/run/ResourceCardIllustration.vue`、`ReactArtifactStage.vue`、`ReactArtifactStage.test.ts`、`CHANGELOG.md`
- 做了什么：IDE、Terminal、浏览器和日志共用 64×52 的正面圆角窗口、浅色标题栏、统一线宽及中性表面；内部用分栏代码、命令提示符、地址栏地球和列表区分入口。移除倾斜叠纸、深色立体终端、浏览器卫星轨道及额外底部阴影，悬停只轻微加深窗口描边，终端光标保留有限次闪动并尊重减少动态偏好。卡片尺寸、顺序和点击行为保持；标签页继续使用原有紧凑图标，更新原有插画必须与标签页 SVG 完全相同的过时断言。
- 为什么：此前三类资源插画在材质、角度、比例与线条上各自为一套风格，用户仍觉得不协调；统一成清晰、稳定的工具入口图标。
- 如何验证：现有产物组件测试（53 条）、完整单测（466 文件 / 4265 条，Lines 90.71% ≥ 85%）、关键路径 Playwright（90 条）、eslint（0 error）、`vue-tsc` 及构建通过；真实浏览器核对深浅主题四卡、悬停时相对中心和尺寸保持不变、减少动态效果时光标无动画，无脚本错误。根 actionlint / shellcheck-error、docs 构建和官方 registry 高危审计通过。

### 2026-10-20（优先级面板、开始图标与聊天区视觉统一）

- 日期：2026-10-20
- 范围：`web/src/components/dashboard/HomePrioritySelect.vue` 及对应测试、`web/src/components/shell/AppSidebarNav.vue`、`web/src/components/ui/Icon.vue`、`web/src/components/run/ClarifyChat.vue`、`ReviewShell.vue`、`web/src/views/DashboardView.vue`、`web/src/styles/radiusZeroClearance.test.ts`、`CHANGELOG.md`
- 做了什么：优先级保留高/普通/低三档与紧凑工具栏位置，改用闪电、强度柱、静态蓝紫分段提示和柔和圆角浮层；浮层使用独立主题色，短窗口可上翻或滚动。修复键盘打开事件继续冒泡后立即选中关闭的问题，补上真实冒泡及短窗口回归测试，并更新原有圆角断言。开始入口使用专用细线房屋图标，保持其他四格图标。聊天区统一深紫头像、柔和消息气泡、输入框聚焦柔光及动作按钮，发送禁用态保持中性，完成状态保留绿色；桌面拖拽分隔条只显示 1px 细线，保留原热区和尺寸。动态效果尊重减少动态偏好，原页面布局、消息顺序与业务事件保持。
- 为什么：原优先级列表、四格开始图标、亮紫分隔线、青色 Agent 头像和绿色确认按钮缺少统一视觉，截图中的输入框与气泡描边也过硬；按用户参考统一局部材质和图标，不改整体布局。
- 如何验证：组件及图标现有测试、优先级新增键盘/短窗口测试、完整单测（466 文件 / 4265 条，Lines 90.71% ≥ 85%）、eslint（0 error）、`vue-tsc` 及构建通过；源码稳定后关键路径 Playwright 90 条全部通过（首次并行开发触发 Vite 页面重载，画布放置用例中断，复跑无需改画布或测试）。真实浏览器检查深浅主题、400/240px 聊天区、普通消息与时间线消息、输入框聚焦及禁用发送，以及优先级 Enter/Space/方向键、矮窗口上翻与滚动。根 actionlint / shellcheck-error、docs 构建及官方 registry 高危审计通过。

### 2026-10-20（浏览器资源插画的经线与轨道修正）

- 日期：2026-10-20
- 范围：`web/src/components/ui/Icon.vue`、`web/src/components/run/ResourceCardIllustration.vue`、`CHANGELOG.md`
- 做了什么：浏览器专用图标的经线改用真实椭圆，球体与装饰轨道共用中心；将轨道拆为前后半弧，通过球体填色遮挡后弧，卫星点放在椭圆轨迹上。球体与轨道整体响应悬停，减少动态效果时保留固定倾角与静态位置。
- 为什么：原经线在两极形成尖角，外圈与球体中心偏移，且悬停时只有球体移动，造成图形变形和错位。
- 如何验证：图标与产物组件现有测试（61 条）、web eslint（0 error）、`vue-tsc`、完整单测（466 文件 / 4260 条，Lines 90.66% ≥ 85%）、关键路径 Playwright（90 条）及构建通过；真实浏览器检查深浅主题、悬停与减少动态效果，轨道前后遮挡和移动保持对齐，无脚本错误。根 actionlint / shellcheck-error、docs 构建与官方 registry 高危审计通过。

### 2026-10-20（首页与工作流产物卡片视觉优化）

- 日期：2026-10-20
- 范围：`web/src/views/DashboardView.vue`、`DashboardView.fill.test.ts`、`web/src/components/run/ReactArtifactStage.vue`、`ResourceCardIllustration.vue`、`CHANGELOG.md`
- 做了什么：首页局部使用深紫 `#53488B` 与中性卡面，降低背景粒子不透明度和输入框焦点柔光；保留 Yume 原 SVG、240×200 裁切、交互、首页文案来源、输入区和横排卡片布局。工作流封面改为分层节点与现有图标，悬停时连线流动、节点轻移；选中项悬停仍保留选中描边。产物页保持资源/文件卡片的原尺寸、排列、事件和版本选择，仅增加分层资源插画、纸角、细高光与轻阴影，支持深浅主题及减少动态效果。更新已有焦点柔光断言以匹配降低后的透明度。
- 为什么：按已确认的视觉预览优化配色、SVG 和动效，解决原页面大面积浅灰、强紫描边与图形层次不足的问题，同时保留原布局、角色和业务流程。
- 如何验证：web 依赖安装、eslint（0 error）、`vue-tsc`、完整单测（466 文件 / 4260 条，Lines 90.66% ≥ 85%）、关键路径 Playwright（90 条）、构建；根 actionlint / shellcheck-error，以及 docs 依赖安装、构建和官方 registry 高危审计。使用隔离 SQLite 的本地后端和真实 Vue 开发服务检查首页与样例产物页，不接入真实沙箱或模型供应商。

### 2026-10-16（优化中英文 README，去 AI 腔并修正首次安装说明）

- 日期：2026-10-16
- 范围：`README.md`、`README.zh-CN.md`
- 做了什么：重写中英文 README 文案与排版：去掉录屏下方的规格参数行、对偶式口号句与伪口语文案；将五处碎片化双列表格收拢为自然列表，截图表补齐 `workflow-canvas.webp` 组成 2×2 网格；把「跑通第一个工作流」修正为真实的首次安装向导与「项目凭据」配置流程（而非在 Agent Studio 填 API Key），并补充运行时凭据隔离说明。
- 为什么：#788 改写后的 README 存在明显模板化表格堆砌、AI 口号腔与伪口语表达，且首次配置 API Key 的入口写成了已拒收密钥的 Agent Studio。
- 如何验证：核对锚点与内部文档/图片相对路径存在且可达；执行 `node scripts/assert-no-approving-brand.mjs`、`./.github/scripts/actionlint.sh`、`./.github/scripts/shellcheck-error.sh` 全部通过。

### 2026-10-16（Live 请求生成中显示加载卡片；切换栏不再吞点击）

- 日期：2026-10-16
- 范围：`web/src/components/run/ClarifyChat.vue`、`web/src/components/run/LiveVariantCard.vue`、`web/src/liveoverlay/overlay.ts`、`web/src/liveoverlay/scan.ts` 及对应测试；`npm run build:live-overlay` 同步的三份 `live-overlay.js`
- 做了什么：抽屉里最新一条 Live 请求在 Agent 工作期间不再隐藏卡片，而是显示精简卡片（标题 + 状态）加骨架条和扫光的加载动画（`aria-busy`，减少动态效果时只留静态骨架），完成后照旧在 Agent 回复下方出现可操作卡片。页面切换栏 `renderSwitchers` 只在标记变化时才写 `innerHTML`；`showVariant` 跳过已处于目标显隐状态的根节点，重扫不再取消候选淡入。
- 为什么：#813。#742 让最新请求的卡片等 Agent 做完才出现，而 Live 生成的请求文字本就不展示，生成期间只剩一个空的「我」气泡。切换栏每秒定时器和整页 `MutationObserver` 触发的重扫都会重建按钮，重建落在 mousedown 与 mouseup 之间时点击丢失；候选带 JS 动效（如「趣味细节」）时重扫持续发生，切换频繁失效且淡入被打断。
- 如何验证：web `vitest run` 全绿（新增：生成中卡片带 `aria-busy` 与骨架、无控件；定时器与页面变动后 `next` 按钮仍是同一节点且可切换（去掉修复后该用例失败）；重复 `showVariant` 不取消淡入）；`live-variants.spec.ts` 16 条通过；`vue-tsc`、eslint 通过；server `go test ./internal/handlers/`、sandbox `go test ./internal/previewinject/` 通过。本地源码栈 + opencode/DeepSeek 真实跑通：取点标题选「趣味细节」生成，抽屉立即出现加载卡片、无空气泡，约 2 分钟后 3 个候选就绪、加载态消失；页面 DOM 每 30ms 变动时 8 次慢速点击（按住 250ms）全部切换成功，3 秒内切换栏重建 0 次。

### 2026-10-16（set_preview 任意端口都带取点：注入代理自适应上游）

- 日期：2026-10-16
- 范围：`sandbox-gateway/sandbox/internal/previewinject/`（`upstream.go`、`proxy.go`）、`sandbox-gateway/sandbox/cmd/preview-inject/main.go`、`sandbox-gateway/sandbox/scripts/preview-inject.sh`、`server/internal/mcp/preview.go`、`server/internal/mcp/tools.go`、`server/internal/services/preview.go`、`server/internal/models/prompts.go`、`sandbox-gateway/scripts/test-preview-inject-upstream.sh`、`sandbox-gateway/scripts/test-runtime-e2e.sh`、`.github/workflows/ci-sandbox.yml`
- 做了什么：注入代理新增 `--upstream-file`，文件里的端口在可连时覆盖默认上游（`$PREVIEW_PORT`），连不上自动回落；响应带 `X-Grasp-Upstream` 标明实际上游；沙箱自带服务端口（22/5900/6080/8744/8765/9222/17980）一律不作上游。`preview-inject.sh set-upstream <port|reset>` 写/删该文件，`$PREVIEW_PORT` 已有别的应用时返回 `busy` 不抢入口；SSH 调用时从 `/run/grasp-runtime/env` 读 `PREVIEW_PORT`；脚本头带 `# grasp:set-upstream` 标记，平台检测到才调用，旧运行时包走原路径。IP 直连下 `set_preview(port=X)`（X≠`PREVIEW_PORT`）按沙箱加锁后切换上游，只发布 `PREVIEW_PORT`，用响应头校验确实转到 X，失败则 reset 并回到原有登记；记录 `Port=X`、`Host=PREVIEW_PORT`，桌面浏览器仍打开 `127.0.0.1:X`；被挤掉的旧端口登记释放。服务端同样拒绝沙箱服务端口。`PreviewContract` 和 `set_preview` 说明改为：可用任意端口、同一时刻只有最后登记的端口带取点、不要登记后端、沙箱内自测用 `127.0.0.1:<应用端口>` 而不是 `$PREVIEW_PORT`。
- 为什么：Agent 把 Vite 起在 5173 并登记，审批人直连 `IP:$PREVIEW_PORT` 拿不到页面，新标签页取点失效（run 60f19582）。之前只能靠提示词要求起在 `$PREVIEW_PORT`，不可靠。
- 如何验证：sandbox `go test ./internal/previewinject/ ./cmd/preview-inject/`（选择器缓存/回落/拒绝端口、脚本各结果码）；server `go test ./...`（切换、探测失败 reset、busy 备注、旧版本回退、保留端口、非直连不切换、`ProbeInjectUpstream` 校验头）；新增 CI 冒烟 `scripts/test-preview-inject-upstream.sh`（真实二进制 + 脚本，无 iptables）；`test-runtime-e2e.sh` 新增 2b 在特权容器走真实 iptables 验证切换/busy/denied/reset。golangci-lint、vet、两侧覆盖率门槛（server 90.2%、sandbox 96.6%）、shellcheck-error、actionlint 通过。

### 2026-10-15（首页文案接回品牌设置「首页副标题」）

- 日期：2026-10-15
- 范围：`web/src/views/DashboardView.vue`、`web/src/locales/zh-CN/pages.json`、`web/src/locales/en/pages.json`、`web/src/views/DashboardView.test.ts`、`web/src/views/DashboardView.fill.test.ts`、`web/src/views/DashboardView.light.test.ts`、`web/src/views/SettingsView.test.ts`
- 做了什么：首页吉祥物下方文案改读 `useBrandSettings().homeSubtitle`，留空（含纯空白，经 composable trim 为空串）回退语言包默认句；`pages.dashboard.heroEm/heroRest` 合并为单一 `heroSubtitle`（zh「先说清，再动手」/ en「Clarify first, then act」）；文案整句统一配色（`rgb(var(--c-txt))` + 600），移除 `.em` 分段高亮与对应浅色规则，并补 `max-width`/换行兜底；设置页 `homeSubtitlePlaceholder` 同步为新默认句。
- 为什么：#794 首页改版移除了 `homeSubtitle` 接线，品牌设置「首页副标题」成为孤儿设置，管理员改完首页无变化。接线后该设置真正生效。
- 如何验证：DashboardView / fill / light、SettingsView、locales 相关 vitest 全绿（新增读取设置、纯空白回退、英文回退用例）；`npm run lint` 0 error、`npx vue-tsc --noEmit` 通过。

### 2026-10-15（OpenCode 接口地址占位符随所选厂商变化）

- 日期：2026-10-15
- 范围：`web/src/components/agent/OpenCodeProviderFields.vue`、`web/src/locales/{zh-CN,en}/pages.json`、`web/src/components/agent/OpenCodeProviderFields.test.ts`、`web/src/locales/openCodeCopy.test.ts`
- 做了什么：接口地址输入框的占位符改为 computed `basePlaceholder`：优先取所选厂商在模型目录中的默认地址（`OpenCodeCatalogProvider.api`），厂商无 api 或为 custom/自建时回退中性文案；移除 `basePlaceholder` 中硬编码的 `https://api.example.com/v1`，中英文新增 `basePlaceholderCustom`。字段仍选填、默认留空，切换厂商只更新占位符，不清空已输入地址，也不改必填判定与后端契约。
- 为什么：占位符原先绑定静态 i18n 文案，对所有厂商恒为示例域名，无法反映所选厂商真实使用的接口地址；而目录接口早已返回 `api` 字段，前端从未读取。
- 如何验证：单测覆盖目录有 api / 无 api / custom / 离线四场景与切换厂商占位符更新且输入值不变；文案锁断言 openCode 文案不含 example 且中英键一致。`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`、`npm run build` 全部通过。

### 2026-10-15（复审确认后节点交接 handoff 不再丢失）

- 日期：2026-10-15
- 范围：`server/internal/engine/handoff.go`、`server/internal/engine/review.go`、`server/internal/engine/outcome.go`、`server/internal/engine/handoff_test.go`
- 做了什么：新增统一 helper `retainHandoff`（及读当前 iteration StateRun 的 `priorHandoff`）：重建 outputs 前若新 outputs 缺 handoff，则把先前执行的 handoff/handoff_source 补回，再复用 `ensureHandoff` 做平台兜底。接入 review 强制确认（`reviewReply`）、复审修订刷新（`refreshProducerOutputs`）与 react/clarify resume 完成路径（`finishAgentOutcome`，覆盖 `resume.go` / `review_session.go` / `node_react.go`）。补 4 个回归用例（保留 agent 交接、平台兜底、无下游不写、回环 iteration>1）。
- 为什么：review:true 的节点（默认工作流「实现」）在 `node_complete` 产生 handoff 后进入复审；人工「确认并流转」走 `reviewReply` 用空 `runtime.NodeResult{}` 重新 `finalizeAgent` 并 `saveState` 整体覆盖 outputs，丢掉了 handoff/handoff_source，且未走 `ensureHandoff`。结果运行画布「实现→测试评审」边无交接胶囊，下游「测试评审」提示词缺上游交接，回环重跑同样（#796 遗漏的 review 路径）。
- 如何验证：`go test ./internal/engine/...`、`go test ./...`、`go vet ./...`、`golangci-lint run --config ../.golangci.yml ./...`（0 issues）、`./scripts/cover-check-server.sh 90`（90.1%）。新用例在未修复时会失败。

### 2026-10-15（应用预览「可能卡死」推迟到 60 秒，开发服务器代理 noVNC）

- 日期：2026-10-15
- 范围：`web/src/components/run/NovncPreviewPanel.vue`、`web/src/components/run/NovncPreviewPanel.test.ts`、`web/vite.config.ts`
- 做了什么：`PREVIEW_STUCK_MS` 从 20 秒改为 60 秒，警告出现后隐藏「请稍候」启动提示；Vite 开发服务器新增 `/sandbox-vnc/` 的 WebSocket 代理。
- 为什么：启动提示写的是首次启动约 30–60 秒，20 秒就报「可能卡死」与之矛盾；服务端 `OpenInSandbox` 有 90 秒时限，超时会推送错误，60 秒警告正好落在两者之间。开发环境缺 `/sandbox-vnc/` 代理时 WebSocket 无法升级，预览一直停在「连接中」。生产由 Go 服务直接处理该路径，不受影响。
- 如何验证：新增单测在连接一直未完成时断言 20 秒不出现警告、60 秒出现且启动提示隐藏；改回 20 秒时该用例失败。本地经 Vite `:5173` 访问 `/sandbox-vnc/:id/ws` 完成握手。web eslint / vue-tsc / vitest（含覆盖率）/ build 通过。

### 2026-10-15（运行画布节点交接样式优化：图标胶囊 + Markdown 悬浮卡片）

- 日期：2026-10-15
- 范围：`web/src/components/canvas/edges/FlowEdge.vue`、`web/src/components/canvas/edges/FlowEdge.test.ts`（新增）、`web/src/components/canvas/canvas.css`、`web/src/components/ui/Icon.vue`、`web/src/locales/{zh-CN,en}/canvas.json`
- 做了什么：运行画布走过边上的交接指示由「原文截断胶囊」改为「⇄ 交接」图标胶囊；点开为悬浮卡片（头部图标 +「交接说明」+「来源→目标」路线 + 关闭按钮、Markdown 正文、底部来源元信息）。正文改用 `renderMarkdown`（marked + DOMPurify）并套用 `.md` 样式，仅在展开时渲染以复用 LRU 缓存；空/纯空白交接不显示指示。抬升 `.cedge-mid.is-handoff-open` 的 z 序修复卡片被节点遮挡，卡片固定宽度、限高滚动、箭头指向胶囊。`source=platform` 用虚线边框 + 警示色图标 +「平台代写 · 上游未提供交接」。文案经 `canvas.handoff.*` 由 `t()` 取得，新增 `swap` / `arrow-right` 图标。
- 为什么：原交接与条件标签外观无异、弹层被节点卡片压住且正文是 `white-space:pre-wrap` 的裸 Markdown，用户读不清、认不出这是交接。
- 如何验证：新增 `FlowEdge.test.ts` 覆盖胶囊/展开收起/路线/Markdown 结构与缓存/平台态/空文本/aria；`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`（Lines 90.52% ≥ 85）、`npm run build` 全绿。未改后端交接生成逻辑与 `EdgeHandoff { text, source }` 契约，编辑态画布不受影响。

### 2026-10-15（首页加图片附件不再在临界高度带出滚动条）

- 日期：2026-10-15
- 范围：`web/src/views/DashboardView.vue`、`web/src/views/DashboardView.fill.test.ts`、`web/e2e/dashboard-home-chat.spec.ts`
- 做了什么：首页内容列顶部固定 `padding-top: 120px` 改为可收缩的 `::before` 占位（`flex: 0 1 120px`，下限 48px；520px 以下基准 4.25rem、下限 0.5rem）；图片芯片行加 `w-full min-w-0 pr-1.5 pt-1.5` 给删除钮留位；内容列 `overflow-x-hidden`。
- 为什么：窗口刚好一屏时加一张图片，芯片行（约 70px）把内容顶出可视区；删除钮 `-right-1.5` 还会造成横向溢出，横向滚动条再挤出纵向滚动条。72px 的可收缩余量正好吸收一行芯片，第二行或附件提示仍会正常滚动。
- 如何验证：新增 Playwright 用例把视口设为内容自然高度，加一张图片后断言内容列无纵向/横向溢出、删除钮在视口内；换回 main 的 `DashboardView.vue` 时该用例失败（纵向溢出 63px）。web eslint / vue-tsc / vitest（含覆盖率）/ build 通过。

### 2026-10-15（dev 栈 web 依赖变更后自动补装 node_modules）

- 日期：2026-10-15
- 范围：`web/Dockerfile.dev`、`web/dev-entrypoint.sh`
- 做了什么：dev 镜像在 `npm ci` 后写入 `package.json` + `package-lock.json` 的哈希戳；新增入口脚本在启动时比对 `node_modules/.grasp-deps-hash`，不一致就在卷里重新 `npm ci`。
- 为什么：`docker-compose.yml` 用 named volume `web-node-modules` 挂 `/web/node_modules`，卷只在首次创建时从镜像填充；之后 `./start.sh dev` 的 `--build` 即使重装了依赖也被旧卷遮住，新增的 `elkjs` 导致 Vite 报 `Failed to resolve import "elkjs/lib/elk.bundled.js"`。
- 如何验证：复制现有缺 `elkjs` 的 `grasp_web-node-modules` 卷，用新镜像启动后自动 `npm ci`，`node_modules/elkjs/lib/elk.bundled.js` 存在；再次启动跳过重装。shellcheck 0.11.0 无报错，`shellcheck-error.sh` 通过。

### 2026-10-14（Codex 登录失效快速失败，不再误报 MCP 工具面为空）

- 日期：2026-10-14
- 范围：`server/internal/runtime/{codex_auth.go,acp_run_agent.go,acp_ensure.go,acp_sandbox.go,acp_provider.go}`、`server/internal/engine/{auto_retry.go,node_agent.go}`、`web/src/lib/run/useRunDetail.ts`、`web/src/views/RunDetailView.vue`、`web/src/locales/*/pages.json`、对应测试
- 做了什么：Codex 回合以登录拒绝结束（`chatFailure` 文本命中 `IsCodexAuthRejectionText`）时，首轮与 `ensurePlanComplete` / `ensureStructured` / `ensureRootCauseConsistency` / `ensureOutcome` 催促轮都立即返回 `ErrCodexLoginRejected`；`settleCodexLogin` 用 `%w` 包装哨兵；沙箱级重试与引擎 auto-retry 都跳过该错误；节点错误统一为重新粘贴提示；Run 失败横幅附项目凭据链接。
- 为什么：run-64490c06 中 auth.json 失效后每个回合都 401，但失败回合 `err == nil`，平台继续催促 6 次、判成 `errMCPSurfaceEmpty`（retryable）并自动重试 3 次，用户看到的是错误原因"MCP 工具面为空"。
- 如何验证：新增 fake bridge 用例（Codex 401 只跑 1 回合、1 个沙箱；非 Codex 后端行为不变）、`asCodexLoginErr`/`settleCodexLogin` 单测、引擎不重试用例与前端横幅用例；回退修复后 fail-fast 用例失败。`go test ./...`、golangci-lint、`go vet`、configdoc、core 覆盖率 90.1%、web eslint / vue-tsc / vitest 通过。

### 2026-10-09（恢复 #629 的持续回归测试）

- 日期：2026-10-09
- 范围：`server/internal/mcp/http_test.go`、`server/internal/handlers/mcp_stream_test.go`、`docs/dev/DEVLOG.md`
- 做了什么：恢复此前移除的两个测试文件，保留通知合并、并发清理、HTTP 错误/Origin 检查、取消请求、工具列表缓存刷新、撤销放行、会话重连/删除与 run 隔离的回归覆盖。
- 为什么：保留 HTTP MCP 工具变更推送的持续验证，避免后续修改退回仅递增内存计数的行为。
- 如何验证：相关测试的 `go test -race` 通过；核心覆盖率 93.5%（门槛 90%）；golangci-lint 2.14.0 全量检查 0 issues，`go vet`、配置文档一致性、actionlint、shellcheck、品牌检查通过。`TMPDIR=/private/tmp go test ./...` 中 41 个包通过，runtime 仍有既存 macOS BSD `grep -Z` 引起的 Live 扫描失败。

### 2026-10-09（HTTP MCP 工具列表变更推送，#629）

- 日期：2026-10-09
- 范围：`server/internal/mcp/{http,host,outcome,rpc}.go`、`server/internal/handlers/health.go` 与既有 handler 测试、`server/internal/runtime/{acp_mcp,acp_live,provider_e2e}_test.go`、`CHANGELOG.md`
- 做了什么：初始化声明 `tools.listChanged` 并分配 run 绑定的 `Mcp-Session-Id`；GET 建立 SSE，`SetOutcomeAllowed` 翻转时发送真正的 `notifications/tools/list_changed`。每会话一条流，容量为 1 的通知队列合并连续变更；重连后重新失效当前列表，DELETE 只销毁指定会话，run 注销关闭所有流并清理工具面状态。保留既有无会话 POST；带 Origin 的请求校验来源，SSE 禁用代理缓冲并发送心跳。真实 Cursor 测试入口复用正式 HTTP transport。
- 为什么：原先 `toolsListGen++` 只在内存中变化，缓存 Phase1 列表的客户端无法发现 Phase2 的 `node_complete`，提示词重试不能代替协议通知。
- 如何验证：临时 HTTP 集成测试覆盖缓存 Phase1 → 收通知 → 重拉列表 → 调用 `node_complete`、撤销放行、会话替换/删除、断线重连、run 隔离和鉴权；并发清理与合并通知通过 `go test -race`。按用户要求，在验证后移除本次新增的两个测试文件，保留既有测试的适配。`go vet`、配置文档一致性、品牌检查、actionlint、shellcheck、docs build/audit 已通过。全套 server 测试使用 `TMPDIR=/private/tmp` 避开 macOS 临时目录符号链接；另有既存 Live 扫描测试因 BSD `grep -Z` 不是 NUL 分隔而失败。未执行真实 CLI 确认回合（未配置 Live MCP 开关与 Cursor 凭据）。最终状态核心覆盖率 92.3%（门槛 90%）；handler 全套与 `TestRunAgent|TestReact` 回归通过。使用本机已安装的 golangci-lint 2.14.0（CI 指定 v2.12）检查，本次涉及的 mcp/handlers/runtime 包为 0 issues；全量 lint 被既有 `channels/feishu/adapter.go:103,108` 的 SA4023 阻塞。

### 2026-10-09（cursor 后台任务卡住回合）

- 日期：2026-10-09
- 范围：`sandbox-gateway/sandbox/internal/provider/{oneshot,streamjson}`、`sandbox-gateway/sandbox/internal/service/{liveness,bridge_watchdog}.go`、`sandbox-gateway/sandbox/docs/PROTOCOL.md`、`server/internal/sandbox/skills_embed/rules/base.md`
- 做了什么：
  - cursor 编解码器从 `shellToolCall` 结果的 `isBackground` + `success.pid` 取出后台 shell 的 pid（`Msg.BackgroundPID`）。
  - oneshot 引擎按回合跟踪工具调用和后台 shell，只接管能确认属于本轮 CLI 的进程：CLI 的后代、自成进程组且与 CLI 不同组、启动时间未变（防 pid 复用）。没有前台工具在跑、只剩后台 shell、输出安静满 `SANDBOX_BG_TASK_GRACE`（默认 2m，`0` 关闭）后，对这些进程组 SIGTERM，1s 后仍在则 SIGKILL；CLI 收到任务通知后自行收尾、给出结果、退出。
  - CLI 给出结果后 5s 仍不退出，杀掉它的进程组，`end_turn` 仍按成功结束。
  - 活性：会话报告「只在等后台任务」时，这段 CPU/IO 不算活动（cursor-agent 每 30s 改写终端文件约 47KB，原来让看门狗永远判为有活动）。
  - Claude Code `-p` 自己会等后台 shell，默认最多 10 分钟（2.1.292 代码中的 `CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS`，到点杀掉再退出）；`ClaudeAuthEnv` 用同一个宽限设置它，已显式设置时不覆盖。CodeBuddy（退出时杀掉后台 shell，只等后台子 Agent）、OpenCode（Bash 无后台模式，超时杀进程组）、Codex（不等后台终端，退出时清理）读代码确认不会因后台任务卡住回合。
  - 基础规则新增「常驻服务」：用 `setsid nohup … &` 拉起，不用前台或后台任务，不占 8765。
- 为什么：沙箱 52 的 clarify Agent 用后台任务起了 `python3 -m http.server 8766` 看页面，回复写完后 cursor-agent `-p` 一直等这个后台任务，进程不退出，回合一直忙，用户后续消息没有回应，看门狗也因终端文件 IO 不触发。参考 multica（`server/pkg/agent/cursor_background*.go`）：后台 shell 计入在途、安静一段时间后只结束确认归属的后台进程组、以结果为回合边界。
- 如何验证：在沙箱里复现（后台任务存活时 CLI 不出 `result`，SIGTERM 后台进程组后 CLI 收尾退出）；sandbox `go vet`、golangci-lint、`go test -race ./...`、`cover-check-sandbox.sh 90`；新增引擎测试（假 CLI 用 setsid 起后台 shell 并等它结束，宽限后回合正常结束；宽限为 0 时不动；结果后不退出被杀仍算成功）、归属校验、`/proc/<pid>/stat` 解析、编解码与活性暂停测试；server `go test ./...`。

### 2026-10-09

- 日期：2026-10-09
- 范围：`server/internal/{browser,handlers,router,gateshare,mcp,services,runtime,models,config}`、`web/src/{components/run/{NovncPreviewPanel,AppPreviewPanel,PublicAppPreviewPanel,ReactArtifactStage}.vue,views/SandboxConsoleView.vue,lib/api/clients/settingsClient.ts,lib/inbox/gateShareLink.ts,locales}`、`web/{vite.config.ts,vite.e2e.config.ts,e2e}`、`agents/TestReviewAgent` 与 `team_embed` 同步副本、VNC 相关文档
- 做了什么：
  - 每个沙箱只剩一个 VNC 地址 `/sandbox-vnc/:id/ws`，删除按端口的 `/preview-vnc/:runId/:nodeId/:port/ws`。两条入口共用 `serveDesktopVNC`；外部分享的 VNC ticket 不再带端口，按 run/node 找到沙箱。
  - 桌面就是 Agent 的屏幕：`OpenDesktop` 接管浏览器里已有的页面（chrome-devtools MCP 操作的那页），不另开 tab，平台释放时只解绑不关闭。连上桌面不导航；`set_preview` 登记端口后 `ShowURL` 把桌面切到 `127.0.0.1:port`，直连模式也一样。
  - 能 `set_preview` 的 Agent 默认打开 `VNC_PREVIEW`/`BROWSER_MCP`（显式值优先）；提示词与 TestReviewAgent 改为用 chrome-devtools MCP 在桌面浏览器上验证，无头 Playwright 只做批量回归。
  - 前端：运行页按节点查沙箱 id，只挂一个 noVNC，端口标签改 `targetPort` 在同一桌面跳转；没登记端口也能看桌面。分享页一份分享一个 ticket。控台与产物舞台显式传 `console`。
  - 保留：IP 直连新标签页、抽屉、page_* 工具、Live、`/preview/` HTTP 反代、`preview-api`（用户澄清：直连页操作的是用户自己的浏览器，与 VNC 无关）。
- 为什么：之前每个端口一个 VNC 地址，平台自己开 tab，用户看到的不是 Agent 正在操作的画面；参考云电脑做法收成一个桌面。
- 如何验证：server `go test ./...`、golangci-lint、gen-configdoc -check、覆盖率门禁；web lint、vue-tsc、vitest coverage、build、`test:e2e:ci` 与预览相关 e2e（app-preview、preview-review-inbox、sandbox-list-isolation、clarify-empty-preview、preview-artifact-modal）。

### 2026-10-08

- 日期：2026-10-08
- 范围：`server/internal/{runtime,sandbox,nodereg,config,services,handlers,models}`、`sandbox-gateway/{sandbox/internal/{service,provider,acp},gateway/internal/{driver,service,api}}`、`web/src/{views/SettingsView.vue,components/canvas/NodeInspector.vue,components/run/LiveLogPanel.vue}`、`agents/{ImplementAgent,…}` 与 `server/internal/services/team_embed/` 同步副本、`server/internal/sandbox/skills_embed/{skills/git/SKILL.md,rules/base.md}`
- 做了什么：
  - 测试门禁：`plan_coverage` 必须引用 passed 用例，skipped 用例必须写原因，引用 skipped/failed/不存在的用例判不通过；催写提示改为「没跑就写 skipped + 原因」，不再诱导占位结论。催写次数改为节点配置 `nudgeRetries`（默认 3，上限 10，NodeInspector 可改）。
  - 时限模型：取消单轮时长；只保留节点总时限（画布「超时」，留空走 24h 平台兜底）和每轮无动作时限（默认 20 分钟，设置页可改，`GRASP_CHAT_IDLE_SEC` 锁定）。只计 Agent 回合时间。
  - 沙箱活性：bridge 每 30 秒采样 Agent 进程树 `/proc` 的 CPU 与 IO（含前台 docker 命令对应的 dockerd/containerd-shim 子树），有 CPU/IO 或事件即算活动；连续 8 次相同工具调用判为原地打转。首次无动作或打转先带提示续跑一次，再犯以 `stuck` 结束本轮；每 60 秒发 `liveness` 心跳，平台据此把失联判定改为 3 分钟无帧。`ErrAgentStuck` 换新沙箱重试一次并在提示词里说明原因，不占沙箱故障重试次数。时间线底部显示活性。
  - OOM 可观测：网关新增 `GET /sandboxes/:id/exit`（k8s 读容器 terminated/lastState 与驱逐，docker 读 inspect）；平台丢弃失联沙箱前查询，节点错误与重试提示带「沙箱 OOM 被杀(8192MiB)」。
  - `/ws` 噪音：本客户端正在流式接收的回合不再每 2 秒拨 `/ws` 拉事件日志。
  - 分支命名：Implement 提示词、实现清单、git skill 与基础规则统一为 `<type>/<topic>-${GRASP_RUN_ID#run-}`，提交时点名文件。
- 为什么：run-224eb8c7 在 OOM 修复后，test_review 主回合撞上 10 分钟单轮时限，催写让 Agent 写入未验证的占位测试结论，门禁照样放行，deliver 据此开了 PR #763；codex 建的分支 `feature/implement-requirements` 不符合规范。
- 如何验证：server `go test ./...`、`go vet ./...`、golangci-lint、`gen-configdoc -check`、`cover-check-server.sh 90`、`go test ./internal/runtime/ -run 'TestRunAgent|TestReact'`；sandbox `cover-check-sandbox.sh 90`、golangci-lint、`go vet`；gateway `go test ./...`、golangci-lint；Web `npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`、`npm run build`。回放 run-224eb8c7 的 test_result，门禁判不通过。

### 2026-10-08

- 日期：2026-10-08
- 范围：`server/internal/{config/{config,descriptor}.go,sandbox/{resources,manager}.go,services/settings.go,handlers/settings.go,runtime/acp_prompt.go}` 及对应测试、`server/{CONFIGURATION.md,config.example.yaml}`、`web/src/views/SettingsView.{vue,test.ts}`、`web/src/locales/{zh-CN,en}/pages.json`
- 做了什么：新增设置项 `sandbox_memory_mb`（默认 8192，最小 1024，`GRASP_SANDBOX_MEMORY_MB` 锁定）。设置页新增「沙箱资源」分组；值经 `sandbox.SetDefaultMemoryMB` 写入进程级默认值，`Manager.Create` 在 Spec 未指定内存时自动填入，所以工作流节点、测试与 Agent 沙箱统一生效。Agent 节点开场提示词追加「沙箱资源」段，告知内存上限、要求重型命令串行、限制单进程 Node 堆（上限的 3/8）与 vitest worker 数。
- 为什么：run-224eb8c7 的 test_review 节点中，codex 并行启动 lint、vue-tsc、vitest --coverage、vite build，超出网关默认 4096 MiB 被 OOM 杀掉；控制面只看到 `acp connection closed`，按可重试错误换新沙箱从头 `codex exec`，循环直至 4 次节点重试耗尽。
- 如何验证：`go test ./...`、`go vet ./...`、`golangci-lint run`（v2.12，0 issues）、`go run ./cmd/gen-configdoc -out CONFIGURATION.md -check`、`./scripts/cover-check-server.sh 90`（91.0%）、`go test ./internal/runtime/ -run 'TestRunAgent|TestReact'`；Web `npm run lint`（0 errors）、`npx vue-tsc --noEmit`、`npm test -- --coverage`（3976 passed，Lines 90.58%）、`npm run build` 通过。未做 OOM 终止原因上报与 OOM 不重试，留待后续。

### 2026-10-05

- 日期：2026-10-05
- 范围：`web/src/components/{agent/OpenCodeProviderFields.vue,project/ProjectCredentialsPanel.{vue,test.ts}}`、`web/src/locales/{zh-CN,en}/pages.json`、`server/internal/services/project_credentials.{go,test.go}`
- 做了什么：在项目凭据页增加模型厂商 API Key 专用卡片，复用 OpenCode 的厂商、模型、API Base 和视觉能力选择器；Key 仍为仅写入并掩码展示，路由配置保存为非敏感元数据。修正 OpenCode 凭据模型元数据注入到实际读取的 `ACP_BRIDGE_MODEL`，并支持视觉能力环境变量。
- 为什么：模型 API Key 需要把密钥与模型路由放在同一个项目凭据入口管理，避免用户继续在 Agent 环境变量里手工拼接配置；原有模型元数据环境键不会被运行时读取。
- 如何验证：`npm test -- --coverage`（3983 passed，8 skipped，Lines 90.98%）、`npm run lint`、`npx vue-tsc --noEmit`、`npm run build`、`go test ./...`、`go vet ./...`、`go run ./cmd/gen-configdoc -out CONFIGURATION.md -check`、`./scripts/cover-check-server.sh 90`（91.7%）通过。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/{nodereg,engine,runtime,pmmcp,mcp,models,services,handlers,gateshare,sandbox}`、`agents/`、`web/src/{components/canvas,components/agent,lib/workflow,lib/inbox,lib/run,views,locales}`、`web/e2e/workflow-canvas*`、`sandbox-gateway/sandbox/docs/PROTOCOL.md`
- 做了什么：把 13 种 Agent 类节点合并为一个 `agent` 节点，节点类型收敛为 input / output / set_var / branch / agent / human_gate / proposal_select 7 种；Agent 行为由 `capabilities`（interaction clarify|auto、review、tools、reads、writes、maxRounds）一次声明，节点保存能力快照，引擎只有一个按 interaction 分流的执行器和统一收尾（必填产物、schema 钩子、review 出口路由）。删除平台规则覆盖（页面、Agent 标签页、`/api/platform-rules`、嵌入规则读取）和全部旧类型字符串判断；收件箱 kind 只剩 clarify / review，公开分享预览 productKind 改为 `app` 并带 `interaction`。内置模板改为需求澄清 / 实现 / 测试评审，向导四步。工作流画布重做：自动保存、可折叠节点面板、滑出检查器、带标签出口、边上条件编辑、快速添加、撤销重做、8px 网格吸附和对齐参考线、ELK 自动布局；运行页复用只读画布并跟随运行节点，快照无坐标时只在显示层自动布局。移除 `nodeColorHex`，节点颜色改用 `--c-hue-*` 主题色。
- 为什么：旧模型每种 Agent 类型各有一套执行器、提示词契约和前端分支，组合新能力需要新增类型并改动全栈；能力声明化后同一节点即可组合澄清、评审、预览与产物契约，代码路径和配置面都显著收敛。
- 如何验证：新增 `web/e2e/workflow-canvas.spec.ts` 覆盖面板拖入、出口拖到空白处快速添加、边中点插入、撤销重做、边条件编辑后自动保存并刷新不丢失、运行/失败状态，以及浅色/深色主题下空画布、默认工作流、运行中、失败 4 种状态截图；200 节点拖拽实测约 58 fps。修掉两个画布缺陷：Vue Flow 的 `isValidConnection` 会把已存在的边判为重复导致模板/撤销后边消失；回流边入口与正向边中点控件重叠。web：Vitest 442 文件 4056 通过、覆盖率达标，lint 0 error，`vue-tsc` 0 error，build 通过，`test:e2e:ci` 77/77；server：`go test ./...` 与 golangci-lint 0 issue。`e2e/artifacts-page-version.spec.ts`、`e2e/run-list-sort.spec.ts:230` 在 HEAD 上同样失败，不在 CI 列表内。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/handlers/preview-pick.js`、`web/public/preview-pick.js`、`sandbox-gateway/sandbox/internal/previewinject/preview-pick.js`、`web/src/lib/shared/previewPickScript.test.ts`
- 做了什么：重做直连预览底部工具条的视觉样式，统一按钮尺寸、间距、圆角、边框、阴影和浅深色主题；为取点、产物、对话和原版预览加入线性 SVG 图标；将原版预览说明从浏览器原生长 tooltip 改为可聚焦的自定义提示，并增加窄屏自动换行。
- 为什么：原工具条依赖默认按钮样式，emoji 眼睛图标在不同环境中比例和颜色不一致，长原生 tooltip 会遮挡页面并破坏层级，截图中的控件难以辨识。
- 如何验证：`previewPickScript.test.ts` 61/61 通过；`go test ./internal/handlers -run 'TestPreviewPickScript|TestLiveOverlayScript'` 通过；三份脚本副本同步且 `git diff --check` 通过。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/{envauth,services,runtime,handlers}`、`README.md`、`server/README.md`
- 做了什么：项目凭据收口安全边界。去掉沙箱从服务进程环境读取 `GITHUB_TOKEN` / `*_API_KEY` 等的回退（含 `gitToken` / `gitLabURL`）；`fallbackEnvKey` 只从项目/Agent env 取值，不再 `os.LookupEnv`。凭据 env key 必须是合法标识符，且不能是平台保留变量（新增 `envauth.IsPlatformReservedEnvKey`）；交互/测试沙箱叠加凭据时跳过保留键，工作流沙箱的 `GRASP_*` 平台变量恢复为最后写入。渠道/外部 MCP/工作流类型只作为只读视图，不能新建。未填值的内置槽位不再挡住 Run env。项目凭据中的 SSH 私钥/known_hosts 优先于 Agent 元信息。删除项目时一并删除凭据。
- 为什么：原实现会把服务端宿主机的 Token 注入所有项目的沙箱；项目用户可通过 `fallbackEnvKey`（如 `GRASP_SECRETS_KEY`）读出服务端任意环境变量；自定义凭据可覆盖 `GRASP_ARTIFACT_TOKEN` / `GRASP_PM_TOKEN` 等平台令牌；打开凭据页即生成空槽位，会让 Run 级 `GITHUB_TOKEN` 静默失效。
- 如何验证：新增服务与 runtime 用例覆盖保留键/非法键拒绝、空槽位、平台键不被覆盖、进程 env 不泄漏、删除级联；`go test ./...`、`go vet ./...`、`gen-configdoc -check` 通过。

### 2026-10-05

- 范围：`web/src/components/project/ProjectCredentialsPanel.vue`、`web/src/components/project/ProjectCredentialsPanel.test.ts`、`web/src/locales/{zh-CN,en}/pages.json`
- 做了什么：重做项目凭据页的布局，按模型 API Key、代码托管、SSH 和其他凭据分组，增加配置摘要和响应式卡片网格；新增凭据弹窗拆分为基本信息、运行时绑定和凭据值区块，并明确 API Key 与仅写入语义。
- 为什么：旧页面单列卡片在宽屏留下大量空白，API Key 与 Git/SSH 凭据混排，新增表单字段层级也不清楚，用户难以判断凭据该填在哪里。
- 如何验证：项目凭据组件 Vitest 4/4 通过；`npm run lint`（0 errors，仅仓库既有 warnings）和 `npx vue-tsc --noEmit` 通过；`git diff --check` 通过。

### 2026-10-04

- 日期：2026-10-04
- 范围：`server/internal/{models,services,handlers,runtime,router}`、`server/cmd/server/main.go`、`web/src/{components/project,lib/api,lib/project,locales,views}`
- 做了什么：新增项目凭据加密存储、掩码 CRUD、清除/撤销和项目详情凭据页；把 AI CLI、Git、SSH、MCP、自定义凭据接入统一解析器，并为渠道、外部 MCP 和工作流 Key 提供只读适配视图。UI 凭据覆盖项目共享 env、Agent env；已绑定凭据键不能被 Run env 覆盖，MCP 支持 `${credential:<id>}` 展开，SSH 材料写入文件。
- 为什么：让项目密钥有统一的高优先级管理入口，同时保留旧环境变量部署的兼容路径，避免服务端专用鉴权和沙箱运行时凭据互相泄露。
- 如何验证：`go test ./...`、`go vet ./...`、`go run ./cmd/gen-configdoc -out CONFIGURATION.md -check`、服务端覆盖率 91.4%；Web `npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --run --coverage`（3976 tests）和 `npm run build` 通过。`golangci-lint` 未运行，当前环境未安装该二进制。

### 2026-10-04

- 日期：2026-10-04
- 范围：`README.md`、`server/README.md`、`server/config.example.yaml`、`docs/content/{guide,help}`、`docs/site/{index.html,en/index.html}`、`CHANGELOG.md`
- 做了什么：把 ACP、Git 等运行时凭据的项目凭据 UI 标为首选，将兼容的项目/Agent 环境变量保留为回退；同时明确平台服务配置仍按环境变量 > YAML > 默认值解析。
- 为什么：项目凭据由专门的 UI 统一管理，避免新部署继续把运行时密钥散落在环境变量中，同时保持已有部署的兼容路径。
- 如何验证：用 `rg` 检查公开 README、服务端说明、帮助页和站点文案中的凭据优先级；`git diff --check` 通过。未重新生成 `server/CONFIGURATION.md`，因为平台配置项和生成器未改变。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/chatsession/stream{,_test}.go`、`server/internal/services/{sandbox_chat,sandbox_chat_test,pm_turn}.go`、`server/internal/handlers/{sandbox,handlers}.go`、`server/cmd/server/main.go`、`web/src/components/agent/AgentChatTester{.vue,.test.ts,.interactions.test.ts}`
- 做了什么：把 PM 里"帧编号 + 当前轮回放缓冲 + 订阅者扇出"抽成 `chatsession.Stream`，PM 改用它。Agent Studio / 沙箱控制台的对话从"每个 WS 连接一条队列"改成 `SandboxChats`：每个沙箱一个 chatsession FIFO，连接断开不影响排队和正在跑的轮次；WS 连上先发 queue_state 快照，忙时回放当前轮，帧格式和 PM 一致（`{type:'session',event}` / `{type:'acp'}`）。客户端发 chat 时带 `id`，`turn_begin` 按 id 取回本地附件预览。`AgentChatTester` 断线按 1s·2^n（封顶 15s）重连；历史恢复完成前先暂存实时帧，并去掉事件日志里正在跑的那一轮，避免回放重复。销毁沙箱时先取消它的对话。
- 为什么：统一聊天的最后一块。Studio 原来刷新页面或断线就丢队列，正在跑的轮次也看不到了；PM 和 Studio 的回放逻辑本质相同，收成一份。
- 如何验证：`go test ./...` 全绿（新增 `stream_test.go`、`sandbox_chat_test.go`，`go test -race` 通过）；`chatsession` 覆盖率 97.0%，`cover-check-server.sh 90` 为 91.7%；golangci-lint 0 issues；`npx vitest run` 全绿（新增重连回放用例）；`vue-tsc --noEmit`、eslint 无错误。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/services/{pm_turn,pm_thread,pm,sandbox_view}.go`、`server/internal/handlers/pm.go`、`server/internal/router/router.go`、`server/cmd/server/main.go`、`web/src/lib/pm/{usePmLeaderChat,pmTurnState}.ts`、`web/src/lib/api/clients/pmClient.ts`、`web/src/components/pm/PmLeaderChat.vue`、相关测试与 locales
- 做了什么：PM 对话改跑在 `chatsession` 上。每个线程一个 FIFO 会话，网页、IM 渠道、定时任务、审批自动回复都通过原有的 `Start` / `Active` / `Cancel` / `Subscribe` 入队。新增 `POST .../turns`（起轮或 `retryOf` 重试，忙时排队，返回 `waiting`）和 `POST .../turns/cancel`；线程 WS 只负责订阅：连上先发 queue_state 快照，忙时回放当前轮的 turn_begin 和 acp 帧，再接实时帧，另有 `phase` 帧报告沙箱准备进度（preparing / pulling / running）。沙箱准备从前端挪到服务端（`openPmSandbox` + `SandboxView.WaitReady`）。启动时把残留的 streaming 草稿标成新的 failKind `interrupted`。删掉 `/draft` 接口和前端的草稿续接、孤儿判定、90 秒期限。前端断线按 1s·2^n（封顶 15s）重连，页面回到前台时立即重连。
- 为什么：PM 断线、刷新、轮次超过 90 秒都会在前端被判成"连接中断"，可服务端其实还在跑。改成以服务端为准，和 ReAct 澄清走同一套排队与快照恢复。
- 如何验证：`go test ./...` 全绿（新增 `pm_turn_session_test.go`、`pm_turn_prompt_test.go`，覆盖排队、重复入队、重连回放、取消清队、准备失败、失败类型、空闲回收）；golangci-lint 0 issues；`npx vitest run` 全绿（`PmLeaderChat.test.ts`、`usePmLeaderChat.actions.test.ts` 按新协议重写）；`vue-tsc --noEmit`、eslint 无错误。

### 2026-10-05

- 日期：2026-10-05
- 范围：`web/src/lib/chat/sessionQueue{,.test}.ts`、`web/src/lib/inbox/{useClarifyChat,useGateApproval}.ts`、`web/LIB_DOMAIN_MAP.json`
- 做了什么：新建前端 `lib/chat` 域，把 ReAct 澄清和审批热修订各自维护的排队对账抽成纯函数：`reconcileQueue`（queue_state 重建队列：先按 id、再按文本匹配乐观行，无进行中轮次时最多保留一条本地领先行）、`takeTurnBeginItem`、`dropGhostItems`、`isAuthoritativeIdle` 以及附件克隆。两个 composable 改为调用这些函数，队列类型统一为 `SessionQueueItem`。
- 为什么：两处代码逐行重复，后续 PM 和 Agent Studio 迁到 chatsession 后也要用同一套对账，先收成一份。审批面板的 turn_begin 原来直接 `shift()` 队首，queue_state 先裁掉该条时会误删下一条等待消息；现在与澄清一致，按 id 匹配，id 已不在队列时不按文本回退。
- 如何验证：`npx vitest run`（新增 `sessionQueue.test.ts`；`ClarifyChat`、`useGateApproval`、`PublicGateApprovalView` 等既有用例全部通过）；`vue-tsc --noEmit`、eslint 无新增问题。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/chatsession/{session,registry,session_test}.go`、`server/internal/engine/{review_session,visitor_lane,live,engine}.go`、`server/internal/engine/{resume_review_external,clarify_session}_test.go`、`server/scripts/cover-check-server.sh`
- 做了什么：新建 `chatsession` 包，把 ReAct 澄清、复审 / 预览审批、分享页访客通道共用的排队、单 pump、Cancel（只停当前轮或连队列一起清）、删除 / 重排、快照，以及 queue_state / turn_begin / turn_done / error 的发布抽成泛型 `Session[T]` 和 `Registry`。engine 的 `reviewSession` 改为包一层 `chatsession.Session`，执行、落库、Live、page session、反馈台账仍留在 engine，通过 Config 回调接入。`chatsession` 加入服务端覆盖率门禁。顺手修了 `TestClarifyReactReplyEnqueues` 不加锁改 `reactHold` 的数据竞争（main 上 `-race` 已失败）。
- 为什么：统一聊天的第一步。平台上有好几套"排队 + 一次跑一轮 + 断线后靠快照恢复"的实现，PM 和 Agent Studio 各写了一份，PM 断线就判失败。先把 ReAct 这套已经验证过的逻辑抽出来、行为不变，后面 PM、Studio 接同一个包。
- 如何验证：`go test ./...` 全绿；`go test -race ./internal/engine/ ./internal/chatsession/` 通过；`chatsession` 覆盖率 96.5%，`cover-check-server.sh 90` 为 91.6%；golangci-lint 0 issues。行为差异只有一处：turn_begin 和快照里的 images / annotations 为空时统一给空数组（原来有的路径给 null）。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/sandbox/{acp,acp_turn,acp_turn_test}.go`、`server/internal/runtime/{acp_timeline,acp_timeline_test,acp_sandbox,acp_react}.go`、`server/internal/engine/approve_first_message_test.go`
- 做了什么：ACP 客户端记录当前有几个调用方在读事件通道（连接握手、一轮对话、等待取消确认）；没有读取方时 `readLoop` 只更新 queue_state 镜像，不再把帧塞进通道。真正丢帧时的告警限为每分钟一次，并带丢弃计数。时间线的事件日志轮询只在这一轮正在执行（本客户端有轮次在跑，或 bridge 报 busy）时每 2 秒拉一次，轮次结束后再拉最后一次；拿不到 ACP 客户端时保持原来的行为。补了一个测试，确认审批节点暂停后投递首条消息的那一轮在会话快照里显示为 busy。
- 为什么：run 3f471c4b 从 03:10 起持续打印 `acp event channel full, dropping message`。原因是 grasp 节点暂停后 ACP 连接一直开着，但两轮之间没人读通道，bridge 的广播很快把 512 的缓冲占满。同时时间线每 2 秒拨一次 `/ws` 又断开，沙箱日志里刷出大量连接和 broken pipe，每次断开还会触发 bridge 再广播一次 queue_state。
- 如何验证：`go test ./internal/sandbox/ ./internal/runtime/ ./internal/engine/`（新增：暂停的连接被灌 2000 条 queue_state 不缓冲、不告警且镜像正确；告警限频；读取方计数不泄漏；空闲不轮询、busy 时轮询、结束后停；首条消息那一轮 busy）；`-race` 通过；golangci-lint 0 issues。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/mcp/preview{,_test,_more_test}.go`、`server/internal/services/preview_keepalive{,_test}.go`
- 做了什么：`set_preview` 改为先探测端口（最多 5 次、间隔 500ms），能访问后再保活，保活后再探测一次确认，整体超时 60 秒。端口不可达时，按沙箱里 `ss` 看到的监听地址给出提示：没有进程监听、只监听回环地址（附实际地址，要求改为 0.0.0.0）、或已监听但无响应。保活失败时错误里带上脚本自己的 `keepalive: …` 原因。
- 为什么：run 3f471c4b 里 Agent 在服务还没监听时调用 `set_preview`，先跑的保活脚本以 `no listener` 退出 1，但输出被丢弃，Agent 只看到 `Process exited with status 1`，看不出该怎么修。探测只请求一次，也不说服务实际监听在哪里。
- 如何验证：`go test ./internal/mcp/ ./internal/services/`（新增先探测再保活、不可达不保活、三种监听地址提示、保活原因透传）；golangci-lint 0 issues。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/handlers/{preview_vnc,sandbox_vnc,gate_share_public_preview}.go`、`server/internal/handlers/preview_vnc_test.go`、`web/src/lib/shared/vncReconnect{,.test}.ts`、`web/src/components/run/NovncPreviewPanel{.vue,.test.ts}`、`web/src/locales/{zh-CN,en}/pages.json`、`web/LIB_DOMAIN_MAP.json`
- 做了什么：三个 VNC 代理收到客户端任何消息（含二进制 RFB 和新增的 `ping`）都刷新会话活跃时间，限频 15 秒一次。预览面板在页面可见且已连接时每 60 秒发一次 `ping`。服务端 `closed` 和意外断线改为本地化提示；`idle`、`desktop-closed` 和断线按 1s、2s、4s… 退避自动重连（最长 30s，最多 6 次），页面在后台时等切回前台再连；`superseded`、`evicted` 不自动重连，保留手动按钮。公开分享页自动重连同样走 `reconnect-request` 换新票据。
- 为什么：活跃时间只在文本控制消息时刷新，只看不操作的观看者在 `TabIdleTTL`（300 秒）后被 sweep 以 `idle` 断开，面板直接显示原文 `idle`，只能手动重连。
- 如何验证：`go test ./internal/handlers/`、golangci-lint 0 issues；`vue-tsc --noEmit`、eslint、全量 vitest 通过（新增 idle 自动重连、后台等待、superseded 不重连、重试用完出按钮、心跳只在可见时发送）。

### 2026-10-05

- 日期：2026-10-05
- 范围：`server/internal/browser/rod.go`、`server/internal/browser/rod_desktop{,_live}_test.go`、`server/internal/handlers/preview_vnc{,_test}.go`、`sandbox-gateway/sandbox/scripts/vnc-preview.sh`、`web/src/components/run/NovncPreviewPanel{.vue,.test.ts}`、`web/src/locales/{zh-CN,en}/pages.json`
- 做了什么：noVNC 预览窗口固定在 0,0、1920x1080 正常态，标签栏和地址栏留在屏幕上，视口锁定为工具栏下方的内容区；每次 setWindowBounds 后轮询到外框匹配且连续三次读数一致再判定，失败也不再移动窗口。`SetInspect(false)` 关闭时也带 highlightConfig，失败退回 `Overlay.disable`；关闭失败推送 `inspect-off-failed`，面板显示提示。「仅观看」提示改为 `text-txt2`、11px、带描边。
- 为什么：#724 把工具栏移出屏幕，但调整窗口后立刻读 innerHeight 读到旧值，线上算出工具栏 263px（实际约 88px），窗口被多推上去约 175px，页面顶部导航被裁，且失败后窗口停在错误位置。Chromium 的 `Overlay.setInspectMode` 在 mode none 时也要求 highlightConfig，否则报 "highlight configuration parameter is missing"，取点模式一直开着，「取消标注」无效。提示文字在浅色主题下对比度约 2.3:1。
- 如何验证：`go test ./internal/browser/ ./internal/handlers/`；`PREVIEW_DESKTOP_LIVE=1` 实机 Xvfb 测试通过（内容区 1919x992、工具栏像素为浏览器 UI、进出全屏后取点命中、取点开关均返回 nil），同一 Chromium 上不带 highlightConfig 的 none 请求复现了线上报错；golangci-lint 0 issues；`vue-tsc --noEmit`、eslint、`NovncPreviewPanel.test.ts` 通过。

### 2026-10-05

- 日期：2026-10-05
- 范围：`.github/scripts/{actionlint,shellcheck-error,govulncheck-check}.*`、`.github/workflows/{ci,ci-web,ci-sandbox,security}.yml`、`.golangci.yml`、`govulncheck-allowlist.json`、`docs/scripts/audit-check.mjs`、`docs/audit-allowlist.json`、`AGENTS.md`、`CONTRIBUTING.md`，以及 server / gateway / sandbox 里为通过 errcheck、unused 做的机械修改
- 做了什么：始终执行的 ci 工作流加上 actionlint 和 error 级 shellcheck，根目录按一层 `*.sh` 通配收集。security 工作流对三个 Go 模块跑 govulncheck，过期豁免会失败；docs 也像 web 一样拦 high/critical npm 漏洞。共享 golangci 对非测试代码启用 errcheck 和 unused，sandbox-go 补上 `go vet`。
- 为什么：这些检查仓库文档里已经点名过，但一直没有接进 CI；工作流和 shell 脚本出错要等真正跑到才会发现。
- 如何验证：rebase 到最新 main 后，shellcheck-error 和 actionlint 退出 0；三个模块 golangci-lint 0 issues，`go vet` 通过；`cover-check-server.sh 90` 为 91.4%。本地 npm 镜像不支持 audit 接口，govulncheck 需要 Go 1.25.14，这两项以 PR CI 为准。待办：`golang.org/x/crypto` 两条豁免（GO-2026-6354/6355）2026-12-31 到期，补丁要求 Go 1.26，到期前复查，不要静默续期。

### 2026-10-05

- 日期：2026-10-05
- 范围：`sandbox-gateway/scripts/{mock-chat-model.mjs,mock-chat-model.test.mjs,test-agent-connect.sh,agent-ws-check.mjs}`、`.github/workflows/ci-sandbox.yml`、`AGENTS.md`、`CONTRIBUTING.md`
- 做了什么：宿主机起一个不出网的 OpenAI 兼容 mock chat model（夹具 `ci-e2e`，回复 `GRASP_AGENT_E2E_OK`），`test-agent-connect.sh` 让 opencode 通过 `host.docker.internal` 每次都跑一轮对话；`scripts` 作业跑 mock 契约测试。是否打到替身改为看本轮对话前后 hits 的差值。
- 为什么：以前没有 `CURSOR_API_KEY`（fork PR）时整段对话断言都被跳过，镜像里 Agent 能不能真正对话没人验证。
- 如何验证：`node --test scripts/mock-chat-model.test.mjs` 8/8 通过；`bash -n` 通过；`sandbox-images` CI 日志显示 opencode 经 mock 跑完一轮，mock 收到补全请求。

### 2026-10-04

- 日期：2026-10-04
- 范围：`server/internal/browser/`、`server/internal/handlers/{preview_vnc,sandbox_vnc,gate_share_public_preview}.go`、`server/internal/config/config.go`、`server/cmd/server/main.go`、`server/config.example.yaml`、`sandbox-gateway/sandbox/scripts/vnc-preview.sh`、`web/src/components/run/{AppPreviewPanel,PublicAppPreviewPanel,NovncPreviewPanel,DirectPreviewLauncher}.vue`、`web/src/locales/*/{pages,nodes}.json`、`docs/content/{,en/}guide/concepts.md`
- 做了什么：每个沙箱只保留一个常驻「桌面页」，noVNC 断开只解绑观看者，不再关标签页；再次进入时只要已经在同一 scheme+host 就不 Goto，换端口就在同一页跳转；页面被关掉时探活重建；新增 `desktop_idle_ttl_seconds`（默认 0，一直保留）。修掉 rod `Browser.Close()` 会发 `Browser.close` 杀掉 Chromium 的问题：引擎自己持有 CDP WebSocket，关闭只断连。NewTab 改用默认 context，以前 rod 的 `Browser.Page()` 本来就会覆盖 context。Chromium 使用固定的 `--user-data-dir`，登录态在重启后保留。ready 消息带上页面当前 URL。IP 直连端口也走 noVNC，直连只是工具栏上一个「新标签页直连打开」按钮；公开分享页的直连端口也会签 VNC ticket。noVNC 默认只看，点「接管」才把鼠标键盘交给远端，「交还」后回到只看，重连也回到只看。公开分享页的 `goto` 只能去沙箱回环地址（127.0.0.1 / localhost / ::1）的 http(s) 页面：页面共用 owner 的浏览器配置，跳去外部站点就会用上别人的登录态。
- 为什么：用户要的是 Grok Bot 云桌面和 dots 那样的常驻电脑：每次打开预览不重载，保留屏幕和登录态，默认观看、显式接管；直连和 VNC 不应该是两套互斥的界面。
- 如何验证：`server/` 下 golangci-lint 0 issues，`go vet`、`gen-configdoc -check`、`go test ./...` 全部通过，`cover-check-server.sh 90` 为 91.4%，browser 包加 `-race` 通过。`web/` 下 lint 0 error，`vue-tsc` 通过，vitest 431 个文件全部通过，行覆盖率 90.9%，`npm run build` 通过。`vnc-preview.sh` 做了 `bash -n`；需要 Docker 的 runtime e2e 没跑。已知限制：server 重启后内存里的桌面表清空，会新开一个标签页，旧窗口留在原处。没有接管已有标签页，是因为 chrome-devtools-mcp 也连在同一个 Chromium 上。

### 2026-10-04

- 日期：2026-10-04
- 范围：`docs/agent/README.md`、`docs/dev/DEVLOG.md`、`docs/README.md`、`AGENTS.md`
- 做了什么：新增英文文档边界手册；在根 `AGENTS.md` 文首增加「读哪份文档」表和开发日志硬规则，命令、门禁数字、六条已知坑和勿碰清单保持原样；建立本日志并写入这一条；在 `docs/README.md` 布局表标明 `agent/` 与 `dev/` 不进入 `public/`。
- 为什么：编码 Agent 需要从根短入口找到命令、文档边界和日志写法，同时不把角色包使命、公开帮助站和对外 CHANGELOG 混在一起。
- 如何验证：根 `AGENTS.md` 到 `docs/agent/README.md` 与 `docs/dev/DEVLOG.md` 的相对链接可打开；`CHANGELOG.md` 与 `agents/*/workspace/AGENTS.md` 无改动；在 `docs/` 执行 `npm run build` 后，`public/` 中不出现 `agent/` 或 `dev/` 页面。
