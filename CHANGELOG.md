# Changelog

All notable public-release changes are documented here.

## Unreleased

- 沙箱列表页重新整理为带运行概览、状态筛选和分层操作的管理台布局，保留详情、控制台、停止、销毁和空闲回收行为，并适配移动端。

- 沙箱列表页改为连续的边框优先布局：统计指标、状态筛选和数据表格不再各自套独立容器，减少页面分隔感并保持信息层级。

- 沙箱列表页进一步收敛线条密度：移除指标区、筛选区和表格外框的多余分隔线，只保留表头和数据行所需的边界。

- 沙箱列表页补回单一内容边界：统计、筛选和表格共享一块工作区容器，内部保持低密度留白。

- 沙箱列表页将整块工作区边界细化为“概览区 + 列表区”的连续框架，保留整页归属感并让表格操作区域更聚焦。

- 沙箱列表页的状态筛选改用共享 `SlidingTabs` 组件，移除选中胶囊背景并收紧列表区顶部留白。

- 沙箱列表页状态筛选补回轻量底部 accent 线，让 tab 选中态更明确，同时保持低密度布局。

- 沙箱列表页状态筛选调整为分段式 `SlidingTabs`，使用灰色轨道和白色选中项，统一项目内 tab 视觉。

- 沙箱列表页在概览区与状态 tab 之间补充小间距，让分段控件与上方内容保持清晰层次。

- Agent 会话更稳：
  - 停止或看门狗中断回合后，平台会等 Agent 真正停下再发送下一条消息；Agent 15 秒内不响应中断时重启它，并接回原会话。
  - 沙箱后端重启后，Agent 接回重启前的会话。
  - 凭据、模型连接或 MCP 配置变化后，Agent 在下一轮前重启以加载新配置，并接回原会话。
  - Codex 和 OpenCode 派生的子 Agent 仍在运行时，回合不会提前结束，工作流节点也不会提前判定完成。
  - 工作流对话需要在新会话里补发对话记录时（沙箱重建、原会话接不回、`/compact`），记录按约 12000 token 的预算挑选：优先保留最新的用户消息、最新的 Agent 回复和最初的需求，再从新到旧补充其余记录。放不下的记录会注明条数和编号。记录会标明只作参考、不是新指令。上一次补发没能确认送达时，会先开新会话再补发，避免 Agent 看到重复的历史。

- DeepSeek Harness、Kimi Code 和 Trae 也能继续原来的会话：Agent 进程意外退出、沙箱休眠恢复后重建或切换模型设置时，Agent 仍记得之前的对话，不再从空白会话开始。需要沙箱运行时更新后生效。

- 自托管沙箱的 `AGENT_PROVIDER=cursor_acp` / `claude_code_acp` / `codebuddy_acp` 改为 `cursor` / `claude_code` / `codebuddy` 的别名：已部署的配置继续使用原来的 CLI 和配置目录，新配置请直接写不带 `_acp` 的名称。平台创建的沙箱从未使用这些取值，不受影响。沙箱环境变量 `ACP_CODEBUDDY_REGION` 同时移除，CodeBuddy 区域请用服务端的 `GRASP_CODEBUDDY_REGION` 或官方变量 `CODEBUDDY_INTERNET_ENVIRONMENT`。

- 切换模型、思考强度或速度档位后，Codex 和 Claude Code 等编码工具会继续原来的会话，Agent 记得之前的对话和工作，不用再补发截断的聊天记录。常驻的 Agent 进程意外退出时自动重启并接回原会话，排队消息继续执行。沙箱休眠恢复后，或进程运行超过 6 小时（`SANDBOX_AGENT_MAX_AGE`），会在下一轮开始前重建 Agent 进程。接不回原会话时，平台照旧补发对话记录。Claude Code 同一会话的后续回合不再把之前回合的用量重复计入。需要沙箱运行时更新后生效。

- Agent 回合进行中可以「立即引导」：在 Solo、Agent Studio、工作流澄清 / 复审 / 审批和访客分享链接的排队消息上点闪电按钮，消息直接进入正在进行的回合，Agent 接着当前工作处理，不用等这一轮结束。Codex、Claude Code、CodeBuddy、OpenCode 和 Pi 原生支持；其他 Agent 会先停下当前回合，再把这条消息作为下一轮发送。引导内容显示在对话中，也会保留在历史记录和上下文里；只带附件的引导会列出附件名。Codex 改为常驻 `app-server` 运行，OpenCode 改为常驻 `opencode serve`，Pi 改为常驻 `pi --mode rpc`，需要沙箱运行时更新后生效。

- 不支持原生「立即引导」的 Agent 不再显示排队消息上的 steer 按钮；Solo、Agent Studio、工作流对话、审批和访客会话按当前沙箱会话能力更新显示。

- Agent 对话中的「Steer」引导气泡与普通用户气泡统一尺寸和圆角，中文前缀显示为「引导」，支持 Markdown、Markdown 图片和图片附件预览。

- 修复对话中行首的 `#1304` 这类 issue / PR 编号被显示成粗体大标题、`#` 被吞掉的问题。Codex 工具行里的命令和文件路径不再被逐词首字母大写（如 `/Bin/Bash -Lc 'Gh Pr View'`），按原样显示。

- 回复下方的时间和「统计信息」改为显示完成时间，不再显示开始时间。用时达到 1 小时或跨天的回复会额外显示「开始时间」。

- 修复 Solo 沙箱暂停后切换到该会话时右侧资源面板一片空白的问题。上次停在「应用」预览、但当前没有可用预览时，面板会回到资源主页，打开 IDE、终端等即可看到暂停提示和恢复按钮。

- 移动端对话左侧的定位条支持按住切换：按住会弹出提问列表，手指上下滑动可切换高亮的提问并预览回复，松手后跳转到该提问。快速轻点仍然直接跳转。

- 打开云电脑或沙箱浏览器预览时只连接画面，不再把某个 Chromium 窗口提到最前、取消最小化或最大化、拉伸铺满。桌面上原来的窗口布局保持不变；只有进入点选元素时才会调整浏览器窗口。

- 云电脑上 Agent 还在运行时接管电脑（点「接管」或点击画面），会先弹窗确认，提示接管期间 Agent 的键鼠操作会被拒绝；取消后保持仅观看。Agent 空闲时直接接管。

- 桌面镜像的沙箱只显示「电脑」，不再同时显示「浏览器」。修复沙箱启动期间打开资源面板时检测不到桌面、启动完成后也不出现「电脑」的问题：Agent 连上后会自动重新检测，检测失败时每 3 秒重试。已打开的「浏览器」标签会换成「电脑」，打开预览和聊天里的 localhost 链接也在「电脑」里打开。沙箱控制台同样适用。

- 修复移动端模型选择器列表无法滑动、看不到下方模型的问题。触屏设备打开选择器时不再自动弹出键盘，面板高度不超出屏幕可见区域。Agent、项目等其他 Solo 选择器同样适用。

- 侧栏「历史执行」标题整行改为折叠开关：点图标、文字或箭头都只折叠或展开列表，不再跳转到运行记录页。运行记录页移到「设置 › 工作区工具」，排在「看板」后面，从设置进入时保留设置侧栏。

- E2B 沙箱运行期间持续把内存还给主机：沙箱内常驻服务把页面缓存压在约 512 MiB，并整理碎片化的空闲内存。它与下文模型回合结束后的空闲清理（`sandbox.memory_trim_*`）同时生效，互不替代。实测客户机已用约 1.5 GiB 的 16 GiB 沙箱，写读 3 GiB 文件后主机占用 30 秒内从 6.2 GiB 回落到 3.4 GiB。不动进程内存，重复读同一批文件时可能多读几次磁盘。可用 `GRASP_MEMORY_CAP_MIB` 调整（`0` 关闭）；Docker/K8s 不执行。运行中的沙箱在服务端推送新运行时包后生效。

- 修复 Codex 对话在模型连接重试期间被提前终止的问题。连接恢复后可以继续当前回合，重试耗尽时会显示最终错误。

- 云电脑和沙箱浏览器预览支持声音。预览工具栏、控制台工具栏和「电脑」底栏新增喇叭按钮，点击后播放沙箱桌面里 Chromium 和桌面应用发出的声音，再点一次关闭。需要沙箱镜像带 `pulseaudio`（新镜像已预装），旧镜像点击后会提示声音不可用。公开分享链接暂不提供声音。

- 模型选择按钮省略分组名，展开菜单可查看分组。

- Agent 对话支持 `/` 命令，适用于 Solo、工作流澄清、复审和审批预览、Agent Studio、沙箱控制台及独立访客对话。`/new`（也可输入 `/new chat`）在当前工作区开启新对话，旧消息仍可查看，聊天记录中显示分隔线。`/compact` 用限长的历史摘录继续当前任务，并显示压缩状态。`/skill <名称> [任务]` 为本轮选用 Agent 或共享 Agent 已配置的 skill，消息里显示所选技能。斜杠菜单带图标、中文标题和键盘选中状态，可读取已配置技能的入口提供技能列表。

- 修复云电脑底栏的「我的电脑」图标点击无响应的问题。点击后会在当前 Chromium 实例中打开启动台窗口。

- 侧栏「历史执行」中，已取消的工作流运行和 Solo 会话统一使用灰色圆角停止方块。

- 侧栏「历史执行」里点开已结束的运行，进入只读对话（最后一个节点，之前的节点折叠在上方，底部显示运行结果），不再进入画布。右键（触屏长按）工作流运行可选「工作流视图」打开画布。

- Token 用量改为按每轮实际使用的模型统计。修复 Solo 对话（Codex 等不在用量里上报模型名的编码工具）全部记成「未知模型」，以及在对话中切换到其他模型后用量仍记到主模型上的问题。分享预览中访客对话消耗的 Token 现在也计入统计，记到对应的 Solo 会话或工作流节点，访客回复的统计信息也会显示所用模型。删除工作流、运行或 Solo 会话不会影响已记录的用量。已经记下的历史用量不会改写；服务端升级后即生效。

- 开启 Fast 时，闪电按图标原色自上而下填满，关闭时反向收回。点击恢复时，箭头绕中心顺时针旋转一圈。开启减弱动态效果后，图标直接显示最终状态。

- 支持 `max` 的模型默认使用该推理强度，思考强度弹层的恢复按钮也会回到 `max`。手动选择的 `ultra` 或其他可用档位仍会保留。

- Fast 开关使用透明底色，关闭时显示灰色描边闪电，开启时显示实心闪电。

- 「电脑」页签在桌面画面出来之前显示加载动画和「桌面启动中…」，不再整块空白；沙箱还没就绪时也显示加载提示。

- 简化沙箱空闲回收（[#1285](https://github.com/cocofhu/grasp/issues/1285)）：
  - 默认空闲 **6 小时**（`sandbox.idle_ttl_minutes: 360`）后回收，只从模型回合结束且没有排队请求时开始计时，计时持久化，平台重启后继续有效。
  - 不再扫描沙箱内的进程、终端和后台命令；打开页面、IDE、预览和终端也不再延长计时。回合结束后遗留的开发服务器或后台命令会随环境在到期后保存并释放。
  - 侧栏「正在执行」的会话和运行右键菜单新增「永不自动回收」开关，位于「停止环境」上方；取消勾选后重新计时，手动停止、取消运行和关闭会话不受影响。

- 侧栏右键菜单的「永不自动回收」显示无穷符号，开启时在右侧显示勾选标记。

- E2B 沙箱空闲时自动把内存还给主机：模型回合结束 10 分钟后清理沙箱页面缓存并整理内存碎片，之后每 30 分钟、缓存超过 1 GiB 才再次清理。实测一台 8 GiB 沙箱的主机占用从 7.9 GiB 降到 4.6 GiB。不丢数据、不中断进程，下一次读盘第一次会稍慢。可用 `sandbox.memory_trim_idle_minutes`（负数关闭）、`memory_trim_interval_minutes`、`memory_trim_min_cache_mb` 调整；Docker/K8s 不执行。

- 放大模型选择器中的思考强度闪电图标，并为支持加速的 Codex 官方模型新增独立 Fast 开关。Fast 与思考强度分别设置，提示用量增加；选择会随 Solo 会话、工作流对话及预览保存和恢复，并在下一轮生效。

- Codex 模型选择新增 GPT-6.2 Sol、GPT-6.2 Astra 和 GPT-6.1 Terra，同步各模型的上下文窗口与推理档位。

- 沙箱镜像不再安装任何 MongoDB 工具（`mongocli`、`mongosh`、`mongodump` 等 `mongodb-database-tools`），构建时不再访问 MongoDB 的 apt 源和 `fastdl.mongodb.org`。需要时可在沙箱内自行安装。

- 桌面沙箱镜像构建时，Blender、Godot、draw.io 和 3D Slicer 的下载在连接中途断开后也会自动重试；Slicer 安装包会断点续传，不再因网络抖动导致构建失败。

- 沙箱镜像改由 sandbox-gateway 统一决定，Grasp 不再配置镜像（破坏性变更）：
  - 删除 `sandbox.image`、`sandbox.images.*`、`GRASP_SANDBOX_IMAGE` 和 `GRASP_SANDBOX_IMAGE_<BACKEND>`。仍配置时启动打印 warning 并忽略，不会启动失败。请把镜像迁到网关：Docker/K8s 用 `image` 或 `profiles`，E2B 用 `defaultTemplate` 或 `templates`，然后从 Grasp 配置中删除。
  - Grasp 创建沙箱时不再传镜像，只在命名网关连接配置了 `profile` 时传环境名。不再支持按编码工具使用不同镜像；所有编码工具共用网关的同一环境，切换模型分组或编码工具时也不再因镜像不同而拒绝原地切换。
  - 网关新增 `GET /api/v1/capabilities`，返回默认镜像（`defaultImage`）、`profiles`、驱动、端口和停止/磁盘语义，使用 v1 鉴权，未配置 API key 的本地网关也可调用；`GET /api/v2/info` 同样返回 `defaultImage`。Grasp 启动日志和 `doctor` 会显示网关的镜像；对接旧版网关时自动回退到 `/api/v2/info`。
  - 升级顺序：先升级网关，再升级 Grasp。网关必须与本版本一起升级：3.3.0 的网关不识别 `SBGW_DOCKER_IMAGE`，新版 Grasp 配旧网关时沙箱会退回网关内置的 `universal-sandbox:local` 而无法创建。

- 桌面镜像换上 Grasp 外观：启动台改为浏览器新标签页（地址栏不再显示 127.0.0.1），页面带 Grasp 吉祥物、时钟和应用图标；Dock 图标不再空白，「我的电脑」用 Grasp 标志，浏览器窗口归到浏览器图标下；紫色壁纸、半透明圆角 Dock 和窗口阴影；ParaView、3D Slicer 显示真实图标。
- 桌面镜像新增「电脑」页签：桌面画面以圆角卡片居中显示，画面下方的控制胶囊显示谁在控制、Agent 是否在运行，并提供取点、全屏、接管 / 交还；原来的「浏览器」页签保持不变。舞台和沙箱控制台的页签放大；「浏览器」「应用预览」资源卡片换成各自的插画（取点标注的网页 / 运行中的应用），不再共用一个地球图标。
- 新增「云桌面」镜像，并统一 MCP 配置入口（破坏性变更）：
  - 删除沙箱环境变量 `VNC_PREVIEW` 和 `BROWSER_MCP`，代码不再读取；初始化向导和设置里对应的两个开关一并移除，能力接口去掉 `preview.enableEnv`。沙箱现在始终启动预览栈（Xvfb + Chromium + x11vnc + `grasp-desktop vnc-ws`），每个沙箱常驻内存约增加 430 MiB（实测 PSS：Chromium 360、Xvfb 61、x11vnc 13，另加 vnc-ws 转发进程），与直连预览共存。
  - 所有 MCP 只通过 Agent 的 MCP 配置生效：共享 Agent 为底，单个 Agent 可同名覆盖。新工作区初始化时，共享 Agent 默认带浏览器 MCP（`chrome-devtools`）。**已有工作区不会自动迁移**：升级后请在共享 Agent 的 MCP 页签点「快捷添加 → 浏览器」，否则 Agent 不再有浏览器工具。
  - MCP 页签新增快捷添加：浏览器、桌面控制、Blender、产物存储。
  - 新增桌面镜像 `ghcr.io/cocofhu/universal-sandbox:<版本>-desktop`（滚动 tag `desktop`）：在默认镜像上叠加 Openbox 桌面、底部 Dock、启动台、中文输入法，以及 Blender、GIMP、Inkscape、Kdenlive、FreeCAD、KiCad、QGIS、Godot、draw.io、3D Slicer 等应用。默认镜像不变。桌面镜像在「浏览器」旁多一个「电脑」页签，显示当前由谁控制、Agent 是否在操作。桌面预览栈常驻内存约 680 MiB（实测 PSS，不含打开的应用），建议沙箱规格不低于 4 vCPU / 8 GiB 内存 / 30 GiB 磁盘。
  - 桌面 MCP（截图、窗口与应用列表、点击、输入、按键、启动应用）和 Blender 官方 MCP 需在 MCP 页签快捷添加，不会自动注册。
  - 用户在「电脑」里接管画面时，Agent 的桌面键鼠操作会被拒绝并提示「用户正在控制」，截图与列表照常可用；归还、离开页面或 30 秒未续期后自动交还 Agent。Blender MCP 不受影响。只有登录且有权访问该沙箱的用户可以接管，公开分享链接不能。
  - E2B：`prepare-template.py --desktop` 可构建并校验桌面模板，`gateway.example.yaml` 增加桌面模板示例，部署文档附验收清单。

- 修复页面空闲时 CPU 占用高的问题：侧边栏小猫标志之前一直在循环摇摆、呼吸、眨眼，浏览器每秒要重绘 60 次，任何页面开着都会占用大量 CPU/GPU。现在小猫平时静止，只在鼠标悬停时循环动画，每隔 8–14 秒做一次张望或歪头并眨眼；空闲标签页的 CPU 占用约降低 90%。

- 修复工作流自动过程（无人接管的自动执行）的回复统计信息显示「未提供」：现在显示该次执行的 Agent、编码工具、模型、用时和 Token 用量。收件箱里正在观看的自动过程、刷新后的历史记录、运行详情页和接管后的开场消息都会显示；历史记录里早先节点的澄清、复审对话也会显示每条回复的统计。升级前已结束的执行没有记录 Agent、编码工具和模型，这三项仍显示「未提供」。

- 移除 worktree 部署镜像和本地执行模式（破坏性变更）：不再发布 `ghcr.io/cocofhu/grasp-worktree`，`GRASP_EXECUTION_MODE`、`GRASP_WORKTREE_ROOT`、`GRASP_WORKTREE_BACKEND` 不再生效，Agent 只在沙箱中运行。设置接口去掉 `execution_mode`，设置页去掉「执行方式」。使用 worktree 镜像的部署需改用 `ghcr.io/cocofhu/grasp` 并配置沙箱网关；`server.mcp_advertise` 默认值统一为 `http://host.docker.internal:<port>`。

- Solo 会话里 Agent 提出的选择题和表单改为与工作流对话相同的卡片：一次一题、带进度和「上一题 / 下一个」、整行选项、「采用推荐」，不再把所有题目挤成一排小标签。与工作流一样，「其他」需要勾选后填写的内容才会随答案发送。

- 移除「工作区助手」「助手设置」「外部 MCP」「通知策略」「Channel 接入」「定时任务」和 Agent 记忆，产品聚焦云端 Agent 与工作流（破坏性变更）：
  - 删除助手对话页、助手设置页、外部 MCP 页、通知策略页、Channel 接入页、定时任务页和 Agent Studio 的「数据」标签页，以及 `/api/workspace/pm-leader`、`/api/workspace/pm/*`、`/api/workspace/external-mcp*`、`/api/workspace/channels*`、`/api/workspace/cron-jobs*`、`/api/workspace/notify-receipts`、`/api/workflows/:id/notify-policy`、Agent 的 `memories`/`threads`/`cron-jobs` 接口，以及 `/mcp/pm/*`、`/mcp/workspace*`、`/mcp/memory-store`、`/mcp/context-store`、`/mcp/task-scheduler`。已签发的工作区 MCP Key 随之失效。
  - QQ / 企业微信 / 飞书 / 钉钉渠道不再收发消息，运行等待人工、失败或完成时不再外发通知。站内通知不受影响。
  - 工作区和工作流去掉 `notifyPolicy`，工作流默认设置去掉通知与「门禁自动调用助手」（`gateAuto`），运行触发方式去掉 `pm_mcp`。Agent 配置里已删除的平台 MCP（`memory-store`、`context-store`、`task-scheduler`、`pm-*`）不再注入。
  - 「创建团队」只创建工程师，不再创建 PM，也不再选择仓库或填写背景。内置角色包去掉 `PMAgent`。
  - 旧地址 `/assistant`、`/settings/assistant`、`/settings/external-mcp`、`/settings/channels`、`/settings/schedules`、`/settings/notification-policy` 会回到首页。
  - 升级后首次启动会清理数据库：删除上述功能的旧列与旧表（含渠道、Agent 会话、记忆、定时任务和通知回执），销毁遗留的 Agent 会话沙箱，并把旧取值改为新名字——审计调用者 `pm` 改为 `member`，历史运行的触发方式 `pm_mcp` 改为 `api`。
  - 工作区助手和 Agent 会话产生的 Token 用量记录（来源 `pm`、`chat`）随之删除。接口去掉 `pmTokens`、`pmTotal` 和 `source=pm`。

- 修复首页输入框弹出位置：「+」菜单现在出现在「+」按钮正下方，@ 引用面板像 IDE 补全一样在输入的 @ 旁边弹出（下方放不下时改到上方），不再落到整个输入框下面（手机端之前会离按钮很远或被键盘挡住）；窄屏下引用面板的标签不再折行。

- 修复产物、通知、Agent、沙箱、设置等设置式页面里，内层列表滚动区在 Chromium 121 及以上版本显示带箭头的原生宽滚动条的问题，恢复为 4px、悬停才显示的细滚动条。

- 修复首页输入框加了一行图片附件（带文件名和大小）后整列出现滚动条的问题：底部留白现在和顶部留白一样，会先收缩再滚动；没有附件时的布局不变（#800 复发）。

- Solo 会话启动沙箱时的进度改为与工作流节点启动相同的样式（启动沙箱 / 准备工作区 / 连接 Agent，显示已用时间），并按沙箱的真实状态推进，不再按固定时间自动跳到下一步。

- 修复工作流对话的沙箱在空闲回收后恢复、或平台重启后重连时丢失所选模型的问题。之前回复信息里的「模型」会显示 Not reported，Token 统计也会把这部分用量记成「未知模型」（E2B 沙箱上尤其明显）。修复后会读取沙箱实际使用的模型。已经记下的历史用量不会改写。

- 优化通知页「全部已读」按钮：桌面端高度更紧凑并与筛选标签对齐（手机端保留 44px 触控高度）；没有未读时按钮悬停不再变色。

- 修复 Solo 会话输出超长回复时，切到别的会话再切回会把已输出内容重新逐段“播放”一遍、页面卡顿很久的问题；切回后直接显示已输出的内容，只有新内容继续流式输出。
- E2B 保存、恢复和备份淘汰改用官方接口，不再要求自建 provider 的私有快照状态接口。默认保留一份使用原生暂停，保留多份使用独立快照；恢复和删除失败保留重试记录，计算容量与磁盘清理状态分开处理。现有空闲时长、容量和 `solo_backup.keep` 配置不变。

- 沙箱回收与容量统一：所有用途及 Docker/Kubernetes/E2B 共用空闲时长（默认 6 小时）和同时运行上限（默认 15）。空闲后先保存再释放资源，等待人工的工作流可回收并在回复时恢复；运行任务不被空闲策略中断。Docker/K8s 保存工作区并删除容器/PVC，E2B 保存完整快照。备份共用 `solo_backup.keep`（默认 1），只恢复最新成功版本，移除运行中定时备份、版本选择及分类 TTL/上限/手动续期。取消运行清理全部恢复点；清理失败持久重试。自建 E2B 需先升级支持快照持久化确认和引用安全回收的 provider。

- 移除「需求草稿」和「工作区共享变量」功能，产品聚焦云端 Agent 与工作流（破坏性变更）：
  - 需求草稿：删除设置里的页面、`/api/workspace/requirement-drafts` 接口、工作区助手的 `pm-prd-manager` MCP，以及 output 节点的「自动写入遗留需求草稿」开关。已保存的 `pm-prd-manager` 选择会被自动忽略。
  - 工作区共享变量：删除设置里的页面、`GET/PUT /api/workspace` 中的 `variables` 字段，以及工作流设置里的「继承工作区共享变量」开关。工作流运行不再注入工作区变量，只使用工作流自己的「默认变量」和画布变量；原先依赖工作区变量的工作流，请把变量补到工作流的默认变量里。
  - 旧地址 `/requirements`、`/settings/variables` 会回到首页。数据库中已有的需求草稿和工作区变量数据保留但不再使用。

- 修复 Agent 在 E2B 等远端沙箱中用 `send_file`、`upload_file_artifact` 发送或导入文件时总是失败、提示 `argv[2]: shell argument contains disallowed characters` 的问题。zip 等任意类型的文件现在都能直接发给用户，文件名含中文、空格或括号也可以。导出范围不变，仍只允许工作区和 `/tmp` 内的普通文件。

- 新创建的 Solo Agent 默认可以读取全部产物（`list_artifacts` / `read_artifact`），不用再到能力页手动勾选「全部产物」。已有的 Solo Agent 不变，需要的话请手动勾选。

- Solo 会话的沙箱暂停后直接发消息，服务端会等沙箱里的会话服务真正可用后再发送，不再在端口刚能连通时就开始；唤醒偶尔失败（如恢复较慢导致超时）会自动重试最多 3 次，不再让这条消息直接失败。

- Solo 会话的 E2B 沙箱因长时间空闲自动暂停后，「停止环境」按钮会原位变为「恢复环境」，浏览器/终端/IDE 标签页显示暂停提示和恢复按钮；直接发消息也会自动恢复。新增接口 `POST /api/solo/sessions/:id/wake`，会话详情的 `sandbox.paused` 表示沙箱已暂停。工作流对话在沙箱失联时不再误报模型切换失败、也不回滚所选模型；右侧终端/IDE/浏览器面板在沙箱重建后自动重连。

- Solo / 沙箱对话在输出一部分内容后连接意外断开时，保留已收到的回复并明确标记本轮失败，不再把未完成的回合显示为成功；正常完成后关闭连接和主动取消仍按原有逻辑处理。

- E2B 模板构建支持用 `--min-free-disk-mb` 向 provider 申请构建后的可用磁盘空间；仍以临时沙箱实测容量为准，空间不足时不输出可发布模板配置。

- 修改 `auth.users` 中账号的 `password_hash` 后，该账号用旧密码登录的会话随即失效；此前旧会话要到原定到期时间才失效。升级后所有用户需要重新登录一次。

- E2B 沙箱高负载或 IDE 无响应时，打开 Solo 会话不再一直转圈：会话详情的沙箱实时探测最多等 3 秒，超时就先显示已保存的会话和历史，沙箱状态显示为「未知」；网关对已运行的沙箱查询状态时只做一次最长 2 秒的探测，不再等待 SSH、会话服务和 IDE 全部就绪（#1223）。

- 管理员通过登录页登录后，品牌设置、Token 单价等管理员设置立即可编辑，不再需要刷新页面。登录接口的响应新增 `is_admin`，与 `/api/auth/me` 一致（#1137）。

- 登录 Cookie 的有效期改为跟随 `auth.session_ttl`（`GRASP_AUTH_SESSION_TTL`），不再固定 7 天。此前配置超过 7 天时，浏览器会在第 7 天删除 Cookie 而提前登出；配置更短时 Cookie 也会随会话一起到期。已登录的浏览器在下次登录后生效（#1138）。

- 修复 Markdown 中文粗体/斜体有时不生效、原样显示 `**` 的问题：`**结论：**正文`、`**注意（重要）**这里`、`是**“关键”**步骤` 这类中文标点紧贴强调符号的写法，现在在对话回复、产物预览和节点输出里都会正常显示为粗体或斜体。只在标准 Markdown 规则因中文标点不成立时才放宽，英文、代码和本来就能正确显示的写法不受影响。

- 人工门禁只接受该门禁配置过的动作：提交未配置或空的动作会返回错误，门禁保持待审批、运行保持等待人工；此前会消费这次审批，没有匹配分支时还会把运行误标为完成（#1135）。待处理消息重排拒绝重复的消息 ID，失败时队列保持原顺序；此前重复 ID 会让另一条待处理消息丢失（#1136）。

- 从 `auth.users` 移除账号后，该账号已登录的会话随即失效，不能再访问需要登录的页面和接口；此前旧会话要到原定到期时间才失效（#1134）。

- 刷新或重连回到正在输出的对话时，已经生成的回复直接完整显示，只有之后新到的内容逐字出现；此前超长的一轮会从头逐字重放，要等很久且页面卡顿。积压过多（例如切回后台标签页）时也只逐字显示最后一小段。

- Solo 切换模型失败的提示统一措辞：保存切换状态失败且恢复原配置也失败时，提示改为「保存切换状态失败，恢复原配置也失败」；原配置已失效时，两种失败都提示「请重新选择模型重试」（此前保存失败时只说「请重试」）。

- 沙箱里的 `gh auth status` 显示 GitHub 凭据的真实账号，不再一律显示 `grasp-selected`。手动填写 github.com 的 Token 时，服务端会查询一次账号（最多 5 秒，失败照常保存），凭据列表也会显示 `@账号`；已有的手动 Token 需重新填写一次才会记录账号。GitHub Enterprise 等其他主机的 Token 不查询，仍显示 `grasp-selected`，认证不受影响。

- 平台产物 MCP 的 token 改为跟随所属对象：工作流运行处于排队、运行或等待人工时有效，结束后失效，续跑后自动恢复；Solo 会话在启动、运行和停止中有效；测试沙箱在沙箱存在且未出错时有效。回收同一运行里的任何沙箱、重试和服务重启都不再让正在执行的节点断开 MCP；服务重启或请求落到其他副本时，`node_complete` 仍要求填写交接说明。运行结束后保留调试的沙箱仍可打开终端和 IDE，但不能再调用平台 MCP（#797）。

- Cursor 模型列表与 Cursor CLI 2026.11.11 对齐：新增 Gemini 4 Carbon、Gemini 4 Hydrogen、Claude Fable 5.2（含 Fast 版）和 DeepSeek V4.1 Flash 1017 各档位；移除 CLI 已下线的 Gemini 3.6 Flash 与 Gemini 3.7 Flash。已选用这些旧模型的 Agent 仍会把原 ID 交给 CLI。

- E2B 沙箱到达最长时长后改为暂停并保留内存快照，不再被直接销毁（网关 `e2b.autoPause`，默认开启）；续期被 E2B 时长上限截断时网关记录警告。Solo 终端和 IDE 打开时会自动恢复已暂停的沙箱，后台会清理已在 E2B 侧消失的沙箱；网关状态探测失败显示为「未知」，不再当作沙箱不存在（#1217）。

- **不兼容变更**：沙箱网关配置按 Docker、Kubernetes、E2B 分离，拒绝混合后端和未知字段；Docker/K8s 的镜像、资源和 profiles 归各自后端，端口使用顶层 `ports`。E2B 使用具名 `templates` 与可选 `defaultTemplate`，显式配置 API 地址，移除 `domain` 和重复 image 映射。Grasp 的沙箱内存 `0` 跟随网关/选中模板；设置页支持清除内存覆盖。`SBGW_IMAGE` 改为 `SBGW_DOCKER_IMAGE` / `SBGW_K8S_IMAGE`，`SBGW_E2B_DOMAIN` 改为显式 `SBGW_E2B_API_URL`（可选 `SBGW_E2B_SANDBOX_URL`），旧变量即使为空也会使启动失败；`compose.e2b.yaml` 新增必填 `GRASP_CONFIG`（参照 `sandbox-gateway/deploy/e2b/grasp.example.yaml`）。请使用新的独立配置示例并同时更新 Grasp 与 gateway。

- **不兼容变更**：Git 授权改由仓库配置。「设置 → 仓库」里每个仓库可以选择一个访问凭据（GitHub / GitLab Token 或 SSH 密钥，须与仓库地址的协议和主机一致），并填写提交用户名和邮箱；工作流在设置里绑定要克隆的仓库，启动运行时可以取消部分仓库或改分支，所选仓库固定到这次运行。Agent 上的 Git 授权、默认配置里的 Git 授权和「继承 Git」开关已删除，工作流变量的 `repos` 类型和 Agent 环境变量 `GIT_REPOS: ${vars.repos}` 不再生效（Agent 环境变量也不能再设置 `GIT_REPOS`），旧数据不迁移。升级后请：在「设置 → 仓库」登记仓库并选择凭据和提交身份；在每个工作流的设置里绑定仓库；删除输入节点里的 `repos` 变量和 Agent 环境变量里的 `GIT_REPOS`。导入的工作流不带仓库绑定，需要在本工作区重新绑定。平台不再把环境变量里的 `GITHUB_TOKEN`、`GITLAB_TOKEN`、`GIT_SSH_PRIVATE_KEY` 等 Git 身份交给沙箱，没有为仓库配置凭据时只能匿名克隆；已有的共享沙箱升级后会重新准备一次。单独运行沙箱镜像时仍可用这些变量。

- Solo 支持提问和表单：Agent 授予了「提问 ask_question」或「表单收集 ask_form」时，在 Solo 里调用会在这条回复下方显示可点选的卡片，推荐项默认选中；提交后作为下一条消息发出，格式与工作流澄清一致。引导新建的 Solo Agent 默认授予提问、表单、产物预览和应用预览，`AGENTS.md` 改为使用平台的 `ask_question`。已有的 Solo Agent 不会被改写，可在 Agent 的「能力」里勾选这些工具。

- 工作流运行一律使用共享沙箱：同一次运行的节点复用沙箱工作区、已安装依赖和未提交修改。「工作流默认配置」里的「共享沙箱」开关已移除，已有工作流保存的 `defaults.sandbox.mode`（包括 `per_node`）不再生效，升级后新启动的运行和进行中运行的后续节点都使用共享沙箱。共享沙箱被其他会话占用或工作目录不兼容时，该节点仍单独使用一个沙箱。

## 3.3.0 — 2026-11-11

> 此前 GHCR 上发布过的 `3.3.0` / `3.4.0` 镜像来自已回滚的失败版本，已撤下；本版本重新使用 `3.3.0`。拉取过旧 `3.3.0` 镜像的机器请执行 `./start.sh pull` 更新本地镜像。

- 删除不再被调用的代码：工作流节点的自动推送与建 MR、GitLab 仓库判断、沙箱能力与变更探测、SSH 注入打包、旧的公开分享消息清洗、未挂载的顶栏组件，以及多个开发期预览页面和配置；功能与接口不变。
- E2B 模板准备直接校验并使用固定 digest 环境镜像中的 bootstrap 和 `IMAGE_LEVEL`，移除重复文件上传；构建文件上传接口不可用时不再因此失败，镜像内容与源码不一致仍会拒绝构建。
- Solo 会话可以「停止环境」：先把工作区打包备份（配置了 `solo_backup.s3` 时存到 S3 兼容存储，否则存在服务端本地 `data/solo-backups`），再停止沙箱，停止后不占 Solo 配额，对话保留。点「恢复」时原沙箱还在就直接启动，E2B 沙箱连进程一起恢复；原沙箱已不存在则新建沙箱并从备份还原工作区。`node_modules`、`.venv` 等依赖与缓存目录不备份，单个备份默认上限 4 GiB（`GRASP_SOLO_BACKUP_MAX_BYTES`）。
- 引导创建的 Solo Agent 自带一份 `AGENTS.md`，要求 Agent 需要用户选择时用文字列出选项并等待回复，不再调用 Solo 无法应答的 AskQuestion 工具（此前会显示为失败的工具调用）。已有的 Solo Agent 不受影响，可在 Agent 设置里自行加入同样的说明。
- Solo 沙箱名额已满的弹窗显示整个工作区已用名额，列出仍占名额的失败会话，并说明其余由其他成员占用，可直接停止或关闭自己的会话（#1201）。
- 私有网络（`sandbox.private_endpoints`）下的预览不再需要通配子域名和 `GRASP_PREVIEW_ADVERTISE`：预览统一走 `/preview/...` 路径代理，并继续校验预览端口归属当前沙箱（#1198）。
- 移除 v3.3 / v3.4 的 Solo 迁移、存储认证与外部执行控制体系，回到 v3.2 的数据格式；保留其后的通用修复（见下）。
- 修复首页 Solo「全部会话」点击后白屏：现在在首页弹窗列出全部 Solo 会话，直接访问 `/solo` 也显示会话列表（#1200）。
- 修复沙箱 Web 终端里 `agent`、`claude` 等 Agent CLI 提示 `command not found` 的问题：SSH 会话现在拿到完整 PATH（#1195）。
- 看板与状态栏把正在回复中的「等待人工」运行计入执行中（#1180）；侧栏历史保留失败的运行，不会被更新的 Solo 会话挤掉（#1181）。
- 修复回复仍在流式输出时思考尾部被收起（#1187）、手机上聊天输入框异常变高（#1178），以及 Agent 测试对话、PM 对话和评论输入框在输入法确认候选词时按回车直接发送的问题。
- 引导页的团队模型选择框不再溢出，并显示继承模型的提供方图标（#1190、#1192）；英文界面下公开分享页与审批、运行等接口报错显示英文说明（#1163）。
- Kubernetes 沙箱停止前确认进程已退出；网关清理孤儿工作负载前再次核对记录，数据库出错或请求取消时停止清理。服务启动恢复期间先接管终止信号。
- `./start.sh dev` / `sandbox` 构建时 npm 镜像站失败会回退官方源（#1158），Docker CE 在清华源 403 时依次回退其他镜像（#1188）。

- E2B 暂停期间保留原有本地端口并关闭转发连接；网关重启先预留全部持久化端口，再连接 provider，避免自身 HTTP 请求抢占端口而导致恢复偶发失败。

- Solo 首条消息等待沙箱启动时，定时恢复不再把创建或拉取镜像中的沙箱误判为恢复失败；启动或模型准备阶段的失败会保留为聊天中的可见回复，刷新后仍能查看。

- Sandbox startup reuses one authenticated SSH connection to install helper files and permissions. The optional MCP proxy releases inherited SSH pipes when daemonizing and does nothing without host mappings. Runtime startup rejects empty-password SSH probes even when an imported template enables them, avoiding PAM delays before public-key authentication while retaining password fallback.

- Kubernetes gateway connections preserve ports published after sandbox creation, so opening a preview channel no longer removes its Service port.

- Grasp can route Docker, Kubernetes and E2B sandbox connections through an authenticated gateway, keeping chat, SSH/files, terminal, IDE and browser access on the existing Grasp entry points. Named gateways retain existing sandbox bindings when the default changes; optional environment profiles resolve provider images in the gateway. Legacy direct mode remains the default.

- Solo 和工作流聊天切换工具或模型组时沿用现有沙箱，不再补克隆仓库或重写 Git/SSH 配置；仓库缺失或网络不可用不再阻断切换，失败回滚也保留工作区。

- 聊天支持拖拽添加多个附件，展示文件名称、类型和大小；图片、音频、视频使用对应类型图标并支持在线查看或播放，保留下载与失败提示。文件分段读取沿用现有登录、分享和嵌入权限校验。

- 修复 CodeBuddy 用量快照延迟造成的漏计：以原生会话累计量核对每轮消耗，扣除已入账的历史和失败轮次，并从输入总量中扣除缓存；缺少模型归属的部分明确标记为未完整核验。

- 修复 Token 计量的故障恢复：日志目录仍不可写时，也会在 SQL 恢复后将内存中的待处理用量写入账本。

- 修复 Token 计量与汇总：状态栏、工作区和统计页统一纳入 Solo / Studio；校正缓存与恢复会话的重复计数，保留重试和中断时已报告的消耗，并通过持久化重放避免数据库短暂故障造成丢失或重复。未完整上报、待入库和无法核验的历史记录会明确提示；执行中计数包含启动中与运行中的 Solo 会话。

- Solo sandboxes retain their workspace after host or Docker restarts. Startup reconciliation resumes the original stopped container, preserves data on status or recovery failures, retries recovery and queued messages, and reports genuinely missing sandboxes clearly. Existing Docker containers receive the updated bootstrap during recovery.

- Mobile workflow conversations use the existing chat interface directly, including deep links and reloads, and keep the chat mounted when resizing. The old pending-card page and return-to-list navigation are removed. Approval sharing remains available through the conversation menu and preview toolbar.

- Agents can send workspace files as versioned chat attachments, with original downloads and previews for common images, audio, video, PDF and text. Artifact bodies and message payloads now use persistent file references instead of SQL blobs/base64; existing data remains readable during a resumable, verified migration. Back up the database together with the file directory; imported files remain private to authenticated WebUI users.

- 移动端表单控件在窄屏保持至少 16px，避免 iOS Safari 聚焦输入框时触发整页自动放大；新增 Chromium/WebKit 的全路由缩放与溢出回归验证。

- **Optional E2B sandbox deployment:** a separate `compose.e2b.yaml` and native Go gateway driver connect Grasp to prepared E2B Cloud/self-hosted templates through private endpoints. Includes persistent connection recovery, lease renewal, deletion retries and isolated preview origins for root paths, WebSockets and page tools. Enable it explicitly; Docker and Kubernetes defaults are unchanged. See the E2B deployment guide for template resource constraints and pause/reinstall limitations.

- E2B preview switching republishes and probes displaced application ports before changing the shared injector, keeping earlier previews reachable. Its deadline cleanup regression test now follows the provider's actual create count and no longer depends on a fixed wall-clock stage.

- SSH commands retain stdout when an empty stderr stream closes concurrently, preventing successful sandbox scripts from occasionally returning empty results.

- Agent model settings distinguish the default model configuration from the configurations allowed for switching. "All active configurations" explicitly includes configurations across coding tools and those added later, with matching Chinese and English labels, hints, and inherited summaries.

- Run artifacts can be edited by hand while the run waits for a person with an idle session, or after it fails. Structured artifacts open in a form with a JSON view, other text artifacts in a code editor, and `page.html` can switch to a live preview. Each changed save becomes a new version under the original node. Concurrent edits are detected, and unsaved edits are guarded when switching views, leaving the page or sending a clarification message. Shared, embedded and Solo views stay read-only.

- Feedback rounds no longer show the "Agent summary" block, and confirming a dialogue no longer runs the hidden summary turn. Older rounds that carry a summary display without it.

- Codex device-code sign-in consistently reports upstream connection loss and truncated responses as retryable sign-in service errors.

- Codex ChatGPT sessions continue after platform renewal or re-login. Credential edits preserve concurrent token refreshes, and graceful shutdown waits for renewals to be saved. Device-code sign-in follows the shared UI controls and themes, fits narrow screens, and supports keyboard navigation and a direct fallback to pasting a login file.

- Grasp now renews Codex ChatGPT logins itself. Codex credentials can be added or re-logged with a ChatGPT device code, and pasted `auth.json` files must contain a `refresh_token`. Sandboxes receive a copy without the `refresh_token`, updated whenever Grasp renews; a turn rejected for its login is renewed and retried once. The credential list shows the account email, last renewal and renewal status. Configure with `codex_login.refresh_lead`, `codex_login.check_interval` and `codex_login.auth_base_url`.

- Shared main-chat Stop targets the turn the visitor saw and rejects stale requests without changing the queue. Sharing hints explain that Solo and review Stop clear everyone's queued messages, while clarification keeps them.

- Shared main-chat connections keep their retry backoff when a snapshot is immediately followed by an error, and reset it after five seconds of healthy streaming.

- Shared main and independent preview chats can stop the active reply. Interrupted replies keep their partial content, drafts survive stopping, and independent chats can continue on the same connection.

- Preview chat composers offer Pick an element in the "+" menu. Picking returns the selected element to chat, and Escape cancels while preserving the draft and attachments.

- Preview frames apply the host theme before the loading screen is painted and retain it on reload without changing the account theme preference.

- Shared main chats show the live pending-send queue and recorded reply statistics. Image previews open above the full Page Harness card, inherit the sharing language, and close with Escape or the backdrop.

- Shared previews keep the opening language within that preview instead of changing the recipient's Grasp preference. Main-chat reconnections back off and stop when the conversation is unavailable; viewers share one live snapshot and receive only changed transcript tails.

- Shared app chats stream thoughts, tool activity and replies with the regular chat timeline and elapsed time. Shared links and direct previews carry the opening language into the app toolbar and chat, including after a reload.

- Preview frames load only preview views. Preview proxies filter workspace session cookies, and workspace endpoints check the origin of cookie-authenticated requests. A request that carries two valid session cookies is signed out instead of picking one.

- Workspace pages refuse to load in an iframe. Set `GRASP_FRAME_ANCESTORS` to the origins that may embed them.

- `/preview/` pages run in a sandbox without the Grasp origin. Popups, dialogs, downloads, and cross-origin requests to the app's own paths keep working, app cookies and `Authorization` still reach the app, and Web Storage falls back to per-page memory.

- Solo chat in a new-tab app preview streams replies and updates turn status without a page reload.

- Model and provider icons follow the preview drawer's current light or dark theme. Monochrome marks stay legible and color marks retain their original colors.

- **Preview chat messages:** Direct and shared app previews open with the same floating chat. The composer shares attachment, model, and @ controls. Guests can reference public GitHub issues and pull requests. Picked elements appear as annotation chips while the Agent receives their context.

- **Preview chat:** Solo direct previews use the full Solo chat. Workflow previews and both anonymous sharing modes use the regular chat composer and model picker. Shared main chats apply model choices to the original conversation. Independent chats switch within the app sandbox's active model group. Drag the floating toolbar or move it with the arrow keys, and it keeps its position when the page reloads.

- **Sandbox preview:** The back, forward, and reload icons render in the app preview and sandbox browser toolbars, and the Vue console stops repeating missing-icon warnings.

- **App preview sharing:** Solo and workflow previews can copy a link that lets guests open the app and chat without signing in. Choose a separate guest chat or explicitly share the main conversation's text history and message queue. Solo's signed-in direct preview Chat now opens its existing conversation.

- **Direct shared previews:** Shared app links open the full app with the same floating Chat and Pick controls as a direct preview. Solo's preview can resolve chat tickets in sandboxes where artifact MCP credentials are only configured inside the Agent.

- Direct app previews can render the floating toolbar and open chat on pages with restrictive style, connection, and frame policies. App CSP protections, including sandboxing and frame-ancestor restrictions, stay in force.

- **Solo previews:** new preview-enabled Solo sandboxes can register an app on a separate port and open it from Resources. Solo artifact pins appear in the Artifacts drawer, and clicking an HTML thumbnail opens its preview. The Agent's Solo tool list hides actions that require a workflow, and failed preview registration identifies missing port mappings.

- **Live review previews:** the chat stays in sync with the selected candidate when a WebSocket update arrives ahead of the request acknowledgement.

- The chat message navigation rail has a clear gap from the transcript in narrow panels and preview drawers.

- **Chat references and links:** a sent message shows each `@` reference as a chip where it was typed instead of only above the bubble, and a message starting with `@#815` no longer turns into a large heading. Links in chat open in a new window. Links to `localhost` or `127.0.0.1` open the resources panel and load in a new tab of the sandbox browser, since those addresses only resolve inside the sandbox; without a sandbox they open in a new window.

- **Workflow clarification:** A message sent while a clarification node is opening is processed once. Resuming an interrupted visit still retries its unanswered message.

## 3.2.0 — 2026-11-02

Solo and workflow chats support `@` references to Solo sessions and GitHub issues/PRs, clarify and review chats render directly from server-side dialogue state, queued chat turns persist across server restarts, Git credentials add GitHub device-flow login and unified SSH keys, and settings and Agent Studio layouts are refreshed.

- **@ references in workflows:** the home page Workflow composer, clarification and review chats inside a run, and gate hot revisions now accept the same `@` references as Solo: your own Solo sessions, and GitHub issues and PRs from the workflow's or run's `repos` repositories. The agent of that node reads them as in Solo. A Workflow launch can carry only references, and switching workflows clears them. The reference picker and chips also look and move differently, in Solo too: the highlight and tab underline slide, rows keep their place while results refresh, icons are tinted by state, and motion is reduced when the system asks for it. Typing `@` after Chinese text or IME input also opens the picker directly. The anonymous shared approval page does not offer references.

- **Clarification and review chats follow the server:** in Inbox, run details and shared approval links, the conversation now shows exactly the state the server reports. A turn that is still running, was stopped, or is waiting in the queue is no longer shown as "no output this turn", and reconnecting or reloading no longer leaves duplicate or stuck turns. A stopped turn with no reply reads "(已中断)", and sending a new message replaces the retry button on a failed reply.

- **Sign in with GitHub for Git credentials:** when the deployment sets `GRASP_GITHUB_OAUTH_CLIENT_ID` (the Client ID of its own GitHub OAuth App with Enable Device Flow checked), adding a github.com GitHub credential offers "Sign in with GitHub". The page shows a one-time code to enter at github.com/login/device; after you authorize, the token is saved as a regular GitHub credential with scopes `repo read:org workflow`, and the row shows the GitHub account. Existing github.com GitHub credentials get "Sign in again" to replace the token without changing bindings. The credential form's Host field is now "Site", shows the scheme, strips a pasted `https://` and marks official, self-hosted or invalid values. Self-hosted GitHub Enterprise and deployments without the client ID keep pasting tokens.

- **SSH key credential (incompatible change):** the separate "SSH private key" and "SSH known hosts" credentials are merged into one "SSH key" credential with a required private key and optional known hosts, editable later on their own. An Agent can bind several SSH keys, and each key brings its own known hosts into the sandbox; a key without known hosts accepts new host keys on first connect as before. Existing standalone known-hosts credentials and the hosts choice on Agent Git bindings are removed on upgrade, so re-enter your known hosts on the matching SSH key.

- **Settings pages redesign:** settings, credentials, model groups, external MCP, notifications, projects, variables, schedules and audit pages share a new header and card layout, and most show a summary strip (for example configured credentials, enabled model groups, running runs or sandbox usage) built from data the page already loads. Model group rows show their coding tool and whether they are available, disabled or need attention, and the credentials tabs show counts. When the workspace assistant is not set up, it now shows a short setup guide with the steps and what the assistant can do.

- **Built-in agents run straight through:** newly created Implement and Test & review agents (from onboarding, the PM or Agent studio templates) now have human review off, so after you confirm the clarified requirement the default workflow runs implementation, test & review and delivery without stopping. Turn review on in an agent's capabilities page if you want it to wait for you. Existing agents keep their current setting.

- **Shared sandbox on for new workflows:** workflows created in the editor, by onboarding, or with "Create from default baseline" now start with Shared sandbox turned on, so nodes in the same run share one sandbox and workspace without changing Workflow defaults first. You can still turn it off there. Existing workflows keep their current setting, and imported or copied workflows keep the defaults they came with.

- **Model groups in Agent settings:** the option that includes every active model group is now called "Use all model groups" (「使用全部模型组」) instead of an "inherit" label, so it is no longer confused with "Inherit defaults". The model section now asks for the default startup model group first, then lets you choose between all model groups and selected groups with a segmented control. Switching only changes the panel below it. In selected mode, the default group appears as a fixed row. Reset now sits next to the section title. Saved settings are unchanged.

- **@ references in Solo:** type `@` in a Solo message, including the home page composer when starting a new Solo session (or use the "+" menu), to reference another of your Solo sessions or a GitHub issue or pull request from the session's repositories. Typing `#number` or pasting an issue or PR link works too. The agent gets a summary of a referenced session and can read its full conversation as it was when the turn began. GitHub items are checked with the sandbox's Git credential and read by the agent with `gh`. A message can carry up to 5 references, shown as chips in the message and the queue.

- **Solo sandbox limit and running order:** admins can change how many Solo sandboxes run at once under Settings → Capacity, applied immediately and kept across restarts. Solo sessions in the running list now stay in start order, newest first, instead of jumping to the top on every reply.

- **Sidebar queued group:** queued runs now sit in their own collapsible 排队中 / Queued group below 自动过程 instead of crowding the top of 正在执行; 正在执行 no longer counts them.

- **Queued messages survive a server restart:** messages waiting behind a running reply in Solo, clarification, review and gate revisions are stored, and after a restart they run in their original order instead of being lost. A reply the restart cut off is marked as interrupted, and workflow conversations offer a retry. Queued Live requests are marked failed so their card offers a retry; share-link visitor conversations are not covered yet.

- **Long agent turns:** a turn is no longer stopped while the agent is still working. Solo turns were cut off at 60 minutes, PM, channel and scheduled-task turns at about 10 minutes (scheduled tasks gave up waiting at 30 minutes), and Agent test chats at 10 minutes; none of them has a default total time limit now. A turn that goes silent is still stopped by the no-activity limit. Limits you set yourself still apply: the workflow node time limit, a channel's turn timeout and `SANDBOX_TURN_MAX_DURATION`.

- **Agent studio layout:** the agent list no longer takes a column of its own. Click the agent name in the top bar to search, switch agents, open Agent defaults, create, import, export, rename or delete, which leaves more room for the file tree and editor. Switching agents keeps the current tab instead of jumping back to the workspace directory. Agent and Agent defaults tabs are larger, show counts as small badges instead of "(n)", and use a sliding underline.

- **Workflow and approval queued messages:** in clarification, run details, review gates and shared approval links, a message sent while a reply is still running now appears only in the queue panel and enters the conversation when its own reply starts, matching Solo and the page after a reload.

- **Conversation side panel memory:** each chat, including Solo conversations, remembers whether its right-hand panel was open and whether it showed Resources or Artifacts, and restores that when you return, also after a page reload. A chat you have not opened before follows the last state you used.

- **Solo queued messages:** a message sent while a reply is still running now waits only in the queue and enters the conversation when its turn begins, instead of also showing an early bubble with an empty reply that disappeared on reload. Solo also shares the workflow pending queue panel, supporting drag reorder, withdraw-to-edit (preserving text, images and `@` references) and removal.

- **Onboarding and credential management:** closing the first-run wizard remembers dismissal so it no longer reopens on every login, and splits Git credentials into a dedicated step before repository setup. Every workspace credential can now be deleted directly from its row.

- **Token analytics cache hit rate:** the KPI cache hit rate now includes cache writes in its denominator (matching the trend chart) and caps ratios below 100% at `99.9%` so partial misses never display as a false `100.0%`.

- **Light theme, sidebar and mascot polish:** light-theme secondary text, placeholders, borders, sidebar rows and modal hover labels have stronger contrast on white surfaces; sidebar status metrics are labeled; and the home mascot and brand mark add idle, tap-combo and composer-reactive animations (respecting `prefers-reduced-motion`).

- **Chat turn safety:** an unexpected failure inside one chat turn now ends that turn with an error and lets queued messages continue, instead of stopping the server. A Solo conversation that stays busy without updates checks the session and recovers a missed completion without a page reload; long silent tool calls keep their connection.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:3.2.0`.

## 3.1.0 — 2026-10-30

Agents can run Kimi Code, keep several model groups, and execute in a worktree image. Conversations stay on the same visit through reconnects, queues and workflow switches.

- **Run list spacing:** restore horizontal padding inside desktop rows so titles and actions have room within the hover highlight.

- **Workflow chat synchronization:** clarification, run details and live conversations share one message processing loop for queue state and recovery. Missed startup, output and completion updates recover without a page reload, while newer replies, queued messages and drafts stay intact. Tool-first replies and growing tool details remain visible across watchdog updates and healthy automatic observations. ACP bursts preserve their output, errors and completion notifications. Saved tool summaries count only their own reply; empty replies no longer reuse earlier timeline steps. Late send, confirmation and artifact responses stay with their originating visit when switching workflows.

- **Page Harness icon:** CoCo now uses the Grasp character with gentle sway, breathing, blinking and hover feedback, respecting reduced-motion preferences.

- **All configured model groups:** Agent defaults and individual Agents have a switch to inherit every active model group, including groups added later. This mode uses a generic group icon and a short explanation; the default startup group is selected separately. Inheriting Agents follow this setting, including workflow launch validation, and unavailable groups still require valid authorization before use.

- **Translation dependency security:** upgrade Vue I18n to a supported release with fixes for translation HTML and parameter escaping.

- **Workflow confirmation:** the current node stays visible while a clarification or review is being confirmed and its successor is still undecided, including when confirmation arrives before the run finishes entering its waiting state.

- **Kimi Code subscription and reasoning:** Kimi Code now uses the official Coding Plan endpoints and model IDs with subscription-console API keys. Supported models offer `low`, `high` and `max` thinking strengths, confirmed by the native CLI before the next turn. Parallel chats keep separate model and thinking settings, and previously saved K3 model IDs map to their subscription equivalents.

- **OpenCode model availability:** official backends synchronize the native model catalog at startup in sandboxes and worktrees, so manual refresh commands are no longer needed. Ordinary chat messages do not trigger catalog checks or refreshes. Missing or retired selections fail with a clear startup error, and deprecated entries are excluded from the provider's model catalog.

- **Switching workflow conversations:** selecting another running workflow clears the previous reply while the selected conversation loads. Delayed snapshots and old connection events cannot restore another conversation's thought, tools or task, including when both runs use the same node name or the user switches back quickly.

- **Concurrent workflow chat:** delayed queue frames cannot restore completed messages or stop a newer reply. Reconnecting windows receive their own session snapshot; message identities remain attached through queues, streamed replies and saved history, including repeated text. Concurrent session cleanup and sends share one processing queue, and stopping a reply cannot cancel the next reply. A rejected WebSocket request reports an error only to its sender without ending another window's active reply.

- **Workflow chat recovery:** stalled connections reconnect automatically and restore the active reply, queue and completed token usage. Failed handshakes keep retrying; recovery preserves subsequent replies and their streamed segments. Saving an intermediate reply segment no longer hides its still-running thought or reply when another message is queued. Starting an annotated message removes its own pending row, including when the host adds a preview selection or the opening frame is replayed. Waiting for the next node to start no longer ends the live conversation.

- **Chat process folding:** opening a finished reply's elapsed-time row now keeps each thought in its own collapsed “思考过程” row alongside tool groups. Thoughts expand independently, and “expand all” still opens them together.

- **Solo chat display:** an empty running chat opens directly to the composer. Model and reasoning changes apply from the next message without an informational banner. Completed thought/tool details collapse at the end of a turn and can be opened again, while the final reply remains visible.

- **Shared chat details and theme-aware logos:** Solo, workflow, Agent test and PM replies use the same timeline, time hint and information popup. Each new reply keeps its actual Agent, coding tool, model, reasoning and per-turn token/duration snapshot; later selections do not relabel old replies. Missing historical fields remain unavailable. Colored model logos no longer receive a forced white background in dark mode.

- **Native CLI reasoning and history:** OpenCode variants now reach the model API with the requested reasoning strength, declared reasoning capabilities are preserved, and thoughts returned by the model appear in chat. Cross-group DSH settings use the target group's choices. Codex loads native MCP configuration and recognizes read-only history access; recoverable model-metadata notices no longer fail successful replies. Result-only stream-json replies retain their final text, and terminal usage totals replace incremental samples to avoid counting tokens twice.

- **Worktree conversation switching:** changing coding tools keeps the local worktree and its files, replaces the backend process and updates its connection port. OpenCode and CodeBuddy load artifact-store MCP from the selected configuration directory. Saved Solo model choices also update other open views immediately.

- **Multiple model groups per Agent:** bind groups across coding tools and select an explicit default. Solo selectors include models from every bound group and keep identical model IDs from different accounts distinct. Model and tool changes apply at the next turn, preserving the workspace and handing prior conversation to the new tool. Unavailable selections require an explicit replacement. Successful configuration changes refresh open selectors in the same browser, including other tabs. The artifact-store `get_history` tool provides current-session history with scoped, snapshot-based pagination.

- **Accepted chat messages:** Workflow, Solo and gate chats show each accepted send once while waiting for live frames and saved history. Repeated text and image-only sends retain separate turns and queue order; rejected, edited or cancelled pending messages are removed correctly. Queuing another message preserves the current answer's streamed segments. Gate replies stay attached to their own request without duplicating saved preview feedback, and the LLM transcript receives live frames and reconnect recovery.

- **Worktree Git authentication:** Agents now receive their selected Git credentials for commands after checkout, including `git push` and GitHub/GitLab CLI operations. Each backend uses private credential files and Git configuration, so concurrent nodes keep their own authorization and an empty selection cannot inherit the server's credentials.

- **Reliable live message delivery:** repeated identical clarify replies stay in separate turns, including when the new reply text matches the previous one and the saved snapshot has not arrived yet. Completion refreshes are no longer lost, and stale detail or preview-feedback responses cannot overwrite newer context. PM retries a failed cold-start history read on the next idle frame of the same connection and still isolates back-to-back turns. Agent Studio reconnects without deleting an older same-text turn or duplicating the turn that started while the socket was down. When a reconnect flush delivers new output before `turn_begin`, the restored open reply keeps both the event-log prefix and that output. Slow chat subscribers close the WebSocket even when a data write is blocked, so Solo, PM, and Agent Studio can reconnect. A Solo reconnect snapshot restores session status and model state that is not kept in the replay buffer, and a slower session read started before that snapshot cannot overwrite it.

- **Repository settings:** a repository now stores only its name, description, URL, default branch and directory. Access methods, credential bindings and environment variables are removed from the repository form, API and Solo snapshots. New sessions and restarts use the current Agent's environment, credential bindings and Git authorization, including its existing default inheritance. Upgrade deletes those historical fields and does not copy them onto any Agent; a private repository that relied on them needs the Agent configured again. Public anonymous clones, repository selection, branch and directory checks, and platform or system variables stay as they are. Running sandboxes are not interrupted; the next restart follows the new rules.

- **DeepSeek Harness token usage:** workflow, React and Solo turns now record per-turn input, output and cache tokens from DSH's session projection, so usage analytics, cost estimates and the token ledger include DeepSeek Harness. Cached input is counted once in its own bucket; turns without a model call, unavailable caches and subagent sessions keep the previous omission behavior.

- **Clarify timeline alignment:** the in-progress 「思考中…」 indicator and label now line up with thought and tool summaries in current turns, follow-up nodes and run history. First-token placeholders, 「输出中」, other busy indicators and reduced-motion behavior are unchanged.

- **Agent chat failures:** OpenCode passes reasoning variants separately from model IDs. Gemini chats now display native streamed text, tools and failure details, with cached input counted once. Solo and Agent test chats show provider failures and refusals, including errors recovered from older empty replies, while retaining partial output and usage.

- **API model image input:** OpenCode, CodeBuddy and DeepSeek Harness model groups can declare text-only or text-and-image input for primary and auxiliary models. Saving fills missing model-directory metadata without overwriting explicit choices. Native adapters consistently honor those declarations, preventing image-capable custom models from being treated as text-only when their API directory omits capabilities. Claude Code and Codex retain their native image handling; platform-authorized tools retain their own model catalogs.

- **CLI chat activity:** Solo and Agent test chats keep an animated processing indicator visible between tool calls and reply text until the turn ends, including OpenCode, Claude Code and DeepSeek Harness. Completed tools retain their completed status, and stopped or failed replies clear the activity indicator.

- **Home scrolling:** hide the scrollbar beside the home composer while keeping overflow content accessible by mouse wheel and touch in Workflow and Solo modes.

- **Sandbox Agent recovery:** a broken ACP output stream now closes the failed connection, releases pending calls and marks the Agent disconnected. Reconnecting creates a usable session instead of leaving later messages busy forever; delayed exit handling cannot disconnect a replacement session.

- **Solo app previews:** preview registration now resolves the current Solo sandbox and its Agent's preview permissions. Existing Solo sessions also recover their MCP authorization after a server restart, while closed sessions and replaced sandbox generations remain rejected.

- **Solo message delivery:** fixed subscription races that could discard a sent message's opening frame when the queue snapshot shared its sequence number. Reconnects now resume after the last processed real frame rather than the latest snapshot, without mistaking legal replay gaps for lost frames; identical replays remain deduplicated, and the live transcript no longer requires a refresh.

- **Coding-tool credentials:** OpenCode CLI and DeepSeek Harness now save their own vendor keys as coding-tool authorizations, alongside Cursor and CodeBuddy. General model service keys remain in API accounts. Both tools can use their vendor authorization or an explicitly selected API account when configuring a model.

- **API account model selection:** model configuration reads the selected API account's model directory for searchable primary and auxiliary choices. Saved models stay available, failed reads can be retried, and original model IDs can still be entered manually. Only selected models are added to the account when saving.

- **Model brand icons:** API model options and Solo selectors show their own brand marks, including GPT, Kimi, GLM and Hunyuan, even when accessed through one shared API account. Unknown models retain the account provider mark, and color logos remain readable in dark mode.

- **Default Solo Agent:** first-time setup also creates an empty `Solo` Agent using the selected model and Git settings. Solo conversations select it by default when no valid saved choice exists; existing choices and an already customized Solo Agent are preserved.

- **First-time setup:** the existing setup window guides users through credentials, coding-tool configuration and Git. Empty steps open their setup forms directly; saving credentials or a model advances to the next step without a completed-items list. Forms stay inside the current window, and API address settings use the shared switch component. Existing credentials and models can be reused, and edits stay protected while saving.

- **Solo model controls:** startup selectors have a border, while active conversations use a borderless model control. Both show the actual model name and open a compact name-only model list with lighter shadows, small brand marks and a clear selected check. Light mode uses a white panel. Supported models offer their available reasoning levels in a small slider panel; a single-model configuration opens that panel directly. Session changes apply to the next turn, with rollback if the coding tool rejects the change.

- **Model groups and model names:** workspace model management now uses “Model groups” and “New model group” (「模型组」/「新建模型组」), including the edit dialog and group-name field. Solo selectors show each concrete model's catalog name or upstream ID instead of a configuration or API service name such as TokenHub; automatic tool selection keeps its existing label.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:3.1.0`. First publish of `ghcr.io/cocofhu/grasp-worktree`.

## 3.0.0 — 2026-10-26

- **Clarify conversation recovery:** delayed queue snapshots and old temporary replies no longer leave later questions read-only after submitting choices. Reconnecting after a completed turn restores the saved questions and releases the stale busy state without reloading the page.

- **Clearer token analytics:** usage structure now uses readable ranked rows with drill-down details. Model and workflow breakdowns share a full-width tabbed table with usage bars, and the busiest runs show eight rows by default with an option to expand; CSV exports still include the complete list.

- **Home default subtitle:** when the home subtitle is left blank, the line under the mascot and the settings placeholder now read 「我们应该做些什么？」 in Chinese and “What should we do?” in English. Saved subtitles stay as written.

- **Workflow shared sandbox switch:** enable Shared sandbox in the workflow editor’s default settings and save. New runs use that selection directly; the separate platform-wide enable flag has been removed. Workflow settings now use consistent groups and switches, with searchable variable-type and Agent selectors.

- **Simpler account and inheritance settings:** Credentials separates coding-tool authorizations under Agent CLI from model service API accounts, both with provider logos. Removed the separate connections list, add-connection entry and model catalog. New Agents inherit by default; Agent metadata collapses inherited settings into one card; switching inheritance off restores local choices. The card keeps its size, and save feedback transitions smoothly. Removed automatic credential slots and old-format inference; only explicitly created current credentials are listed and used.

- **Credential hosts and Agent defaults:** GitHub and GitLab credentials now store their own Host, defaulting to the official service, instead of a separate GitLab URL credential. Coding-tool and Git credentials appear in their own sections. Agent defaults can select a named model and several Git authorizations; each Agent can independently inherit either selection. Selecting a model determines the coding CLI without a duplicate tool picker.

- **Unified model bindings (breaking configuration change):** Agent creation, editing, onboarding, team creation and execution now use only a saved model configuration: coding tool + credential + upstream model. Removed legacy Agent credential slots, direct bindings and automatic migrations. Existing Agents must select a model configuration or explicitly inherit the configured default before execution. Missing or inactive references fail explicitly; changing a shared model configuration affects all referencing Agents.

- **Sandbox binding validation:** long-lived sandboxes validate applied model and Git bindings before reuse or another chat turn; changed authorization requires reopening. Messages adapters preserve gateway prefixes and avoid duplicate API-version paths. CodeBuddy model connections support Chat Completions only.

- **Conversation header alignment:** Solo sessions now use the same workspace frame as workflow conversations, aligning header spacing and resource/artifact controls. Mobile approval chats place their back and share buttons in the shared header, removing the repeated title and separate share row.

- **Multiple Git credentials per Agent:** Agents can select several saved GitHub, GitLab and SSH credentials, mark a preferred identity and optionally scope accounts to a site or repository. Selected credentials are staged in the sandbox; workflow nodes apply their Agent's bindings and shared sandboxes replace credentials when nodes change. Missing or revoked selections stop execution without trying another account. Empty custom bindings grant no Git access when inheritance is disabled; inherited Agents use the selected default authorizations. Old single-slot bindings are removed.

- **Explicit Agent model selection:** creating an Agent requires a valid selected or inherited model configuration. Turn inheritance off to choose a custom configuration; missing or unavailable configurations block progression and creation. Both Credentials and Models tabs now share one add menu for credentials and models.

- **Agent configuration controls:** the create wizard combines coding tools and models on one page, followed by Git and review. Coding tools use a compact logo list with an integrated service-region selector across create and edit. Model, credential, connection, auxiliary-model and MCP dropdowns use the shared searchable picker, with explicit unavailable-reference states and keyboard navigation.

- **Model setup wizard:** coding tools use logo cards, and one searchable credential picker automatically selects the matching authorization or service. OpenCode CLI and DeepSeek Harness shortcuts create reusable model API credentials. Creating a missing credential returns to the current model draft; back navigation preserves selections. Advanced API settings are collapsed, and dialogs support keyboard focus and narrow screens.

- **Credentials and models UI:** simplified rows show a logo, name and short subtitle, with icon actions and model enable switches. Service details stay in the editor. Provider selection now supports search, logos and 15 presets, alongside custom services.

- **Reusable model configurations:** create credentials first, then save a named combination of coding tool, credential and selected model in the Models tab. Agents bind these configurations by reference. API accounts store service addresses and keys; Added protocol adapters for Claude Code, CodeBuddy Chat Completions, OpenCode, Codex Responses and DeepSeek Harness; Cursor and Trae retain platform authorization. Missing or revoked references fail explicitly without falling back to another key.

- **Workspace credentials:** credentials now appear in compact lists grouped by coding tools, model API keys, repositories and SSH. Status, masked values and actions stay together; adding a credential uses a three-step wizard with a visible step indicator. Configure provider presets or custom API bases and keys here, then combine a coding tool, credential and model in the separate Models tab.

- **Icon motion and shapes:** functional icons across the app, sandbox chat and preview tools now share single-play hover and keyboard feedback, with reduced-motion support. Labels, logout, workflow, copy, delete and terminal glyphs are corrected; completed checks draw left to right without a dot. Only active loading states keep moving.

- **Coding tool picker:** product icons now appear before tool names in Agent, team and first-time setup. The picker hides internal configuration paths.

- **DeepSeek Harness backend:** choose DeepSeek Harness alongside the other coding tools, select Flash or Pro, and run agents with their instructions, skills and MCP tools through native dsh ACP. Authenticate with a dedicated DeepSeek API key. Requires the updated sandbox image with dsh installed.

- **Settings navigation:** clicking the selected settings icon again returns to the previous workspace page, preserving its filters and URL fragment; a direct settings visit returns to Home.

- **Sidebar consistency:** settings and workspace pages now use the same inset, rounded sidebar frame, keeping the left navigation icons and account avatar in the same positions when switching pages.

- **Segmented tabs:** run status, usage filters, editor modes and other segmented controls now share the Home switch's smooth moving highlight. Run counts travel with the highlight and reuse the matching filter's count instead of briefly showing the previous tab's number.

- **Home empty state:** removed the explanatory paragraph and view-all shortcut when no workflows are available on Home, keeping the create-workflow card without an empty spacer row.

- **Home mode highlight:** Workflow and Solo now share one background highlight that slides and resizes with a soft spring easing. The previous option stays transparent, including during rapid switching; reduced-motion preferences are respected.

- **Home mode switching:** switching between Workflow and Solo keeps the hero and mode switch anchored, and fades and slides the mode-specific controls and panels into place instead of shifting abruptly.

- **Home workflow selector:** the inline workflow picker now uses the same flat, transparent treatment as the Solo selectors instead of appearing as a raised card.

- **Workflow and Solo conversation headers:** Solo sessions now use the shared workflow conversation header sizing and compact drawer controls so both startup views have the same top bar.

- **Solo home selectors:** the Agent and repository selectors now use a flat transparent treatment in the composer toolbar, with a subtle hover/open tint instead of a raised gradient and glow.

- **Agent files editor:** fewer columns. Markdown files open in a single editor with an Edit / Split / Preview switch in the toolbar (no minimap), and version history opens from a toolbar button instead of taking a column on the right.

- **Shared run sandbox (opt-in, off by default):** a workflow can run every node of a run in one sandbox, so the workspace, installed dependencies and uncommitted work carry over between nodes. Enable Shared sandbox in the workflow editor’s default settings (`defaults.sandbox.mode = shared`). When the next node uses a different agent or different credentials, the sandbox's env and credentials are re-initialized before that node starts.

- **Loading skeletons:** the shimmer is now a soft neutral sheen with a pause between sweeps instead of a fast purple wave, blocks have rounded corners, and the skeleton fades in after a short delay so quick loads no longer flash it.

- **Conversation navigation:** hovering the message rail now opens a compact outline of every question, highlights the current one, previews the hovered reply and can load earlier messages; touch screens jump straight from the marks.

- **Artifact drawer preview:** artifacts opened in the drawer no longer repeat the name and file-name row above the content; the tab already names them.

- **Artifact preview:** the file header no longer repeats the type (e.g. “JSON”) next to a filename that already shows its extension.

- **Artifact drawer workflow:** the workflow list now matches the resource and artifact cards — node icons form the timeline, the duplicate status dots are gone, and the current step is a soft highlight with the same badge style.

- **Sidebar history:** “历史执行” rows drop the repeated clock icon and use the same text-only rhythm as “正在执行”: titles align with the section heading, a failed run shows a small red ⓘ-style mark at the row end and a cancelled run a grey dot. The “正在执行” and “历史执行” headings now carry a small run / history icon that wakes up on hover, and their rows hang off the heading with a thin tree rail to show the hierarchy.

- **Resource/artifact drawer:** the `+` button now opens a closable “New tab” next to the existing tabs instead of hiding the tab strip, and open tabs stay side by side; the redundant “Resources / Artifacts ×” heading row is gone on desktop (the toolbar toggles open and close the drawer).

- **Solo resources:** Solo now uses the same conversation shell and artifact stage as workflow runs, including shared drawer state, card sizing, previews and a new-tab action; resources include IDE, Terminal, browser and log cards.

- **Execution navigation:** active Solo sandboxes now appear with workflow runs in the shared “正在执行” sidebar, including shared row context actions. Solo conversations use the workflow conversation shell, toolbar, resource/artifact switcher and full-screen layout instead of a separate session list; their sandbox MCP now exposes the selected Agent’s artifact read/write capabilities.

- **Clarify chat:** parent classes, styles, accessibility attributes and native
  events now reach the chat column, resolving the Vue attribute warning in the
  review composer.

- **Conversation actions:** uses one readable purple for submissions and flow
  confirmation, neutral secondary/disabled buttons and quieter recommendation
  labels, including compact approval layouts.

- **Home background:** removes the separate gray-purple backdrop and aligns
  the home page, workflow edge fades and controls with the app theme.

- **Chat design:** unifies readable message typography, neutral user messages,
  composer actions, line icons, tool/thought states and artifact controls across
  chats. Motion is restrained and honors reduced-motion preferences; icon
  changes now update their shapes and sizes correctly during live replies.

- **Inbox conversation:** removes the redundant run/node heading and run-detail
  shortcut above the chat, including the desktop loading heading.

- **Conversation navigation:** previews only the last nonempty assistant reply
  for each question, preserving its paragraphs and Markdown text while leaving
  earlier progress messages out of the summary.

- **Notifications:** uses a clean inbox with small unread indicators, quiet
  status labels and a separate filter/action toolbar. Mobile rows show time and
  their destination, while long titles and run identifiers stay within the list.

- **Audit workspace:** separates view controls from filters, uses quiet inline
  counts and compact groups on the page surface, and removes repeated visible
  table headings. Event details and filter menus support keyboard navigation,
  with theme-aware mobile records and bounded dropdown positioning.

- **Usage statistics:** detail tables expand with their rows, leaving one main
  vertical scroll area. Wide tables scroll horizontally within their own bounds
  on narrow screens without stretching the page. Long model names retain a
  readable prefix and show their full name on hover.

- **Workflow notification selector:** replaces the native dropdown with a
  compact themed menu, a checkmark and soft accent selection. Keyboard focus,
  dismissal and viewport placement work across desktop and mobile layouts.

- **Run board:** status columns have subtle outlines and tinted headings, with
  inset task content and fine row dividers for clearer grouping in both themes.
  Long unbroken titles wrap without overlapping status or priority badges.

- **Inbox conversation:** detail views use a flat surface without an outer
  rounded border or card shadow on desktop and mobile, including initial loading.

- **Conversation navigation:** hover summaries display readable Markdown text
  across chats, preserving filenames and code symbols while removing formatting
  markers. Summaries are shortened after parsing; full replies keep their layout.

- **Chat theme:** conversations share the app's light and dark palette, with
  lavender user messages, clearer prose and compact metadata. Composer focus,
  artifact references and message navigation use the existing brand accent.
  Code blocks and wide tables scroll within their messages on narrow screens.

- **Conversation workspace:** chats use a shared reading column with question
  navigation, hover previews and stable jumps through loaded history. Artifact
  references open in a resizable right pane; narrow canvases use a returnable
  preview while preserving drafts and reading position. Version menus dismiss
  with their pane, filenames retain explicit run provenance, and public approval
  links keep their scoped APIs and reset on credential changes.

- **Application background:** all app routes use a clean, solid canvas in light
  and dark themes, including trailing-slash and case-insensitive workflow links.
  Removes the route-specific background whitelist and decorative shell dot grid.

- **Artifact library and settings tools:** artifacts use a compact file directory
  and wide preview, with searchable source filtering on demand and a list/preview
  switch on smaller canvases. Structured reports use document sections. Settings
  lists and Agent editors remove repeated card frames; narrow Agent layouts retain
  their configuration tabs and file history. Failed source changes never display
  another source's cached files, and deleting a final paged file returns to the list.

- **Browser icon:** uses the Grasp character in a transparent multi-resolution
  favicon, served through the existing ICO endpoint with an updated cache key.

- **Workflow, drafts and assistant layout:** workflows use a flat directory with
  compact notification controls and responsive rows. Requirement drafts use a
  document workspace with an on-demand planning inspector and distinct empty,
  filtered and error states. Assistant setup and conversations use an open canvas
  with a single composer and lightweight suggestions instead of large panels.
  Sidebar directories now use neutral surfaces, and narrower editors adapt to
  their actual available width; mobile chat controls stay clear of floating navigation.
  Failed history loading keeps its retry state until a deliberate retry.

- **Settings layout:** the narrow navigation rail stays beside a 248px category
  directory, with workspace tools first, then execution and platform settings.
  Centered forms use flat groups and fine separators, aligned labels and controls,
  and natural page scrolling. General settings scroll correctly; assistant,
  notification and external MCP forms share the layout without sidebar search.

- **Workspace tools:** board focuses on current runs and human attention;
  Token, model and cost analysis lives in Usage, with clearer filters and
  chart sections. Assistant setup and first-chat screens replace the empty
  split view and repeated settings buttons; paused conversations remain
  readable. Agent lists show full titles and group secondary actions under More.
  The duplicate board usage endpoint and unused charts are removed.

- **Settings navigation:** switching to board, workflows, drafts, or assistant
  keeps the settings directory open. The navigation rail and mobile drawer
  share this context; returning home or selecting a main section exits it.

- **Single workspace:** removes project selection and project management. The
  compact sidebar lists every workflow without search; workflow search stays on
  the management page. Board, drafts, assistant, and shared configuration have
  dedicated pages and Settings entries. Project APIs, `projectId` parameters
  and project MCP addresses are removed in favour of workspace endpoints
  (`/api/workspace`, `/mcp/workspace`); the PM tool is now
  `pm_list_workspace_agents` and Agent bundles use `workspace.json`.
  Requires a fresh install with an empty database and an empty data directory.

- **HTML preview toolbar:** removes the repeated file-information row and puts
  copy and download immediately before the enlarge control. Version selection
  and historical read-only controls remain available, including on narrow panels.
- **Unified chat actions:** Confirm & continue now sits inside the input panel
  alongside send and attachments, with wrapping for narrow panels. Draft text,
  attachments, annotations and pending sends hide confirmation with a smooth
  collapse and reveal. Sending shows progress inside its button, and reduced
  motion switches immediately. Unsent content remains protected from confirmation.
- **Yume motion:** the home mascot's tail stays attached throughout its wag,
  press and jump. Home and sidebar characters gain gentle breathing, double
  blinks and hover greetings; reduced-motion mode keeps both still.
- **Two-level navigation:** a compact desktop icon rail sits beside the
  workflow directory and recent runs. New request, pinning, contextual actions,
  account controls, and a single-column mobile drawer remain available.
- **Priority and chat controls:** the home priority chooser gains a compact
  lightning icon and a layered three-level panel; keyboard opening and short
  windows scroll the active option into view. Chat avatars, bubbles, input and actions
  share a softer purple palette with a thinner pane separator.
- **Resource card icons:** IDE, terminal, browser and logs share upright SVG
  windows with consistent size, strokes and neutral surfaces, replacing the
  mixed paper, dark terminal and planet illustrations.
- **Home and artifact visuals:** the home page uses a quieter deep-purple
  palette, layered workflow illustrations and subtle hover feedback while
  keeping its mascot, layout and controls. Resource and artifact cards gain
  softer surfaces and animated illustrations, with static reduced-motion states.
- **Agent wizard service address:** the OpenCode API Key step now hints the
  picked vendor's real default base URL (from the model catalog) as the service
  address placeholder, instead of a fixed `https://api.example.com/v1`. A vendor
  with no published address, or a self-hosted gateway, falls back to neutral
  copy. The field stays optional and blank by default.
- **Control-plane restarts no longer wait for agents:** on SIGTERM Grasp now
  stops scheduling, finishes in-flight HTTP requests (at most 10s) and exits,
  instead of draining agent runs for up to 10 minutes (which outlasted
  `kubectl rollout status` timeouts). Sandboxes keep running. On startup, an
  autonomous agent node whose sandbox is still alive is reattached and its
  current visit continues: a turn still running is awaited, a finished one is
  read back from the sandbox event log, and the main prompt is not resent.
  Queued runs stay queued. Other in-flight nodes (clarify, human-facing,
  non-agent) re-enter at the same node. If reattach fails, the node retries in
  a fresh sandbox as before.
- **Live request card while generating:** in the preview chat drawer, a Live
  request (for example「趣味细节」from the element card) now shows its card
  right away with a loading animation (skeleton rows and a sweep; static
  under reduced motion) instead of an empty「我」bubble. The full card with
  switch / adopt controls still appears under the agent reply once the
  candidates are ready (#813).
- **Candidate switcher clicks:** the in-page switcher no longer rebuilds its
  buttons when nothing changed, so ‹ / › and「原版」clicks are no longer lost
  on pages that keep changing (animated candidates, HMR), and switching no
  longer cuts off the candidate fade (#813).
- **Preview pick on any port:** with IP-direct preview, `set_preview(port=X)`
  now gives the reviewer's new-tab link (`IP:$PREVIEW_PORT`) the page on port X
  with pick injected, even when the app does not listen on `$PREVIEW_PORT`
  (e.g. Vite on 5173). Only the last registered port per sandbox has pick;
  if `$PREVIEW_PORT` already serves another app it keeps the entry. Sandbox
  service ports (SSH, VNC, IDE, CDP, sandbox backend) are rejected. Agent
  prompts now say not to register backends and to self-test on
  `127.0.0.1:<app port>`. Sandboxes on an older runtime bundle keep the old
  behavior until the bundle updates.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:3.0.0`.

## 2.2.0 — 2026-10-15

<img src="media/yume-mark.png" alt="Black-and-white ghost cat, the Grasp mascot" width="120" />

- **Home mascot:** the home page, the desktop sidebar, and the login page now
  use this black-and-white ghost cat (yume). White body, ink outline, grey
  cheeks, a small tail. The home hero is the cropped cat (240×200) with no
  wordmark and no logo-mark. It floats, blinks, and wags its tail; hovering
  makes the eyes follow the pointer; pressing squishes it and it springs
  back. Reduced motion keeps a still cat. Light theme drops the sparkles and
  uses a light shadow. The sidebar shows it at 28px and the login page at
  48px (#794, #798).
- **Home subtitle:** the sentence under the home mascot now shows the brand
  setting「首页副标题」(`home_subtitle`) and falls back to the locale default
  (zh「先说清，再动手」/ en「Clarify first, then act」) when it is blank. The copy
  is one uniform color and no longer highlights only its first half.
- **Run canvas handoff:** a node with post-run review (e.g. the 实现 node) no
  longer loses its handoff when the reviewer clicks「确认并流转」. The handoff
  and its source are preserved through review confirm, revise refresh, and
  clarify/react resume, and the platform fallback still writes one when a node
  with a downstream left none. The「实现 → 测试评审」edge shows the handoff chip
  again and the downstream prompt gets the upstream handoff, including after a
  test-fail loop-back (#796 follow-up).
- **App preview:** the "may be stuck" warning now appears after 60 seconds of
  connecting instead of 20, matching the advertised 30–60 second first launch.
  The "please wait" hint is hidden once the warning shows (#802).
- **Run canvas handoff:** traversed-edge handoffs are now a compact "⇄ 交接"
  icon chip instead of a truncated slice of the text. Opening it shows a card
  above the nodes with the "source → target" route, the handoff rendered as
  Markdown (headings, lists, inline code; sanitized), and a source footer.
  Platform-written handoffs are dashed with a warning icon and a "平台代写 ·
  上游未提供交接" tag. Empty handoffs show no chip.
- **Home attachments:** adding an image on the home composer at a window height
  that just fits the page no longer brings up a scrollbar. The 120px top gap
  shrinks first (down to 48px) and the column only scrolls after that (#800).
- **Codex login failures:** when Codex rejects or cannot refresh the ChatGPT
  login (CLI `401 Unauthorized` from `api.openai.com`), the Agent node now fails
  on the first failed turn with "Codex 登录已失效或无法刷新，请重新粘贴登录文件"
  instead of re-prompting, reporting "MCP 工具面为空", and auto-retrying. The
  run failure banner links to the project credentials tab.
- **CLI models:** Agent settings, the project shared Agent config, the create
  wizard, and the workflow node inspector pick a model from a dropdown
  (vendor marks, recommended cards, search). Codex also picks a reasoning
  effort. The lists are the CLI's real model IDs. A shared default applies
  only to Agents on the same CLI; an Agent that has not picked its own model
  says it follows the project model (#789, #790).
- **Sandbox lifetime:** a run sandbox stays up by lease (debug after the node
  ends, activity while the terminal, IDE, chat, or preview is in use, preview
  after `set_preview`, and a manual extension) instead of a fixed 30 minutes.
  The sandbox list and console show time left and offer +30 minutes, +2 hours,
  and keep until the run ends. The cap is **Settings → Sandbox recycle →
  Run sandbox max retention** (default 24 hours, 1–168) (#796).
- **Test node:** destroying one node's sandbox no longer revokes MCP for the
  rest of the run, and the test node follows the implement branch instead of
  falling back to `main` (#796).
- **Page Harness images:** an image opened from the Page Harness chat is a
  floating window next to the artifact window, not inside the chat card.
  Opening the artifact after the image offsets it by 36px so both title bars
  stay visible (#795).
- **Project agents:** a workflow created from the baseline binds Agents in
  this project (reuse one with the same name, or create it from the shared
  Agent config). Saving or starting a run rejects an Agent that belongs to
  another project. If the clarify node's sandbox fails to start, the run is
  marked failed. A sandbox config or credential error is not retried. The
  start-run dialog accepts an optional first message (#787).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:2.2.0`.

## 2.1.0 — 2026-10-12

- **MCP tool-list refresh on confirmation:** run-scoped HTTP MCP sessions now
  receive `notifications/tools/list_changed` over SSE when clarification
  confirmation exposes or hides `node_complete`. Reconnecting refreshes the
  current tool list; deleting a session or unregistering a run closes its streams
  (#781).
- **Run canvas:** the run page and version preview always auto-layout and ignore
  saved coordinates. Crowded forward edges stay on a smooth step; only a
  topological back edge whose target sits to the left uses the lower lane, and
  that lane drops below every card it passes. Passed and failed edges keep
  green and red (#782).
- **Project credentials:** a saved row collapses to status, edit, and clear; an
  empty row shows fill. Edit opens the input, save, and cancel on one line, and
  an empty value cannot be saved (#783).
- **App preview:** clicking the noVNC picture takes over. That first gesture,
  including right click, middle click, and touch, is not sent to the remote
  page (#784).
- **Artifacts:** the workflow artifact grid badges fixed sandbox tools apart
  from this run's artifacts, and explains the grid when there are no artifacts
  yet (#785).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:2.1.0`.

## 2.0.0 — 2026-10-09

- **Cursor turns no longer hang on background tasks:** `cursor-agent -p`
  waits for every background shell it started, so a preview server launched
  as a background task kept the turn busy forever. Once the Agent has been
  quiet for `SANDBOX_BG_TASK_GRACE` (default `2m`, `0` disables) with only
  background shells left, the sandbox ends those shells' process groups (only
  ones it can prove belong to the turn's CLI) and the CLI wraps up. A CLI that
  reported its result but has not exited 5s later is killed and the turn still
  succeeds. The liveness watchdog ignores the CLI's CPU/IO while it only waits
  on background tasks. Claude Code, which waits up to 10 minutes on its own,
  gets the same limit through `CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS` (an
  explicit value wins). The base Agent rule now says to start long-running
  services with `setsid nohup … &`.
- **One VNC desktop per sandbox — breaking route change:** the per-port
  `/preview-vnc/:runId/:nodeId/:port/ws` WebSocket is removed; every viewer
  uses `/sandbox-vnc/:sandboxId/ws`, and the external share page uses one
  desktop ticket per share (`preview-ticket` for `vnc` no longer needs a port).
  The desktop is the Agent's own screen: the platform takes over the browser
  page chrome-devtools MCP drives instead of opening its own tab, and
  connecting never navigates it. `set_preview` switches the desktop to the
  registered port, and port tabs navigate the same desktop. The run page shows
  the desktop as soon as the node has a sandbox, before any port is
  registered. Agents that can `set_preview` now default to `VNC_PREVIEW=1` and
  `BROWSER_MCP=1` (an explicit value wins). Direct-IP preview, the `/preview/`
  HTTP proxy and `preview-api` are unchanged.

- **Agent time limits — breaking config change:** an Agent turn no longer has
  a wall-clock limit. Two limits remain: the node's total time (the canvas
  node **Timeout** field; empty means the platform cap,
  `sandbox.agent_node_hard_cap_hours` / `GRASP_AGENT_NODE_HARD_CAP_HOURS`,
  24 h by default) and the no-activity limit per turn (20 minutes by default,
  **Settings → Sandbox resources**, pinned by `GRASP_CHAT_IDLE_SEC`).
  `sandbox.agent_chat_timeout_seconds` / `GRASP_AGENT_TIMEOUT_SEC` is ignored
  with a startup warning. A running command counts as activity while its
  processes use CPU or do IO, so long builds and test runs are not cut off. An
  Agent that goes quiet or repeats the same tool call 8 times is resumed once
  with a hint, then stopped and retried once in a fresh sandbox.
- **Test review gate:** a test result passes only when every plan coverage
  item cites a case that actually passed; skipped cases need a reason.
  Re-prompts ("nudges") ask the Agent to record what it did not run instead
  of writing placeholder results. The number of nudges is configurable per
  node (**Nudge retries**, default 3).
- **Sandbox exit reason:** when a sandbox dies mid-turn, the node error and
  the retry notice say why, e.g. `沙箱 OOM 被杀(8192MiB)`, instead of only
  `acp connection closed`. Needs the updated sandbox gateway
  (`GET /api/v1/sandboxes/:id/exit`).
- **Work branch names:** implement Agents name branches
  `<type>/<topic>-<run short ID>`, e.g. `fix/agent-liveness-224eb8c7`.

- **Sandbox memory limit setting:** every sandbox the platform creates now
  requests a memory limit from the gateway, 8192 MiB by default (previously
  the gateway default, usually 4096 MiB). Change it under **Settings →
  Sandbox resources** or pin it with `GRASP_SANDBOX_MEMORY_MB` /
  `sandbox.sandbox_memory_mb`; it applies to newly created sandboxes and must
  not exceed the gateway `maxMemoryMB`. Agent node prompts now state the limit
  and ask the agent to run heavy build/test/lint commands one at a time, since
  running them in parallel used to OOM-kill the sandbox and restart the node
  from scratch (seen as `acp connection closed: not connected`).
- **Breaking — one Agent node:** workflows now have seven node types:
  input, output, set variable, branch, Agent, human gate and proposal select.
  The old Agent-like types (Grasp, approve, react, plan, preflight, research,
  proposal, test, review, submit MR, visual, app preview, conditional prompt)
  are removed. An Agent node only picks an Agent, a goal and a timeout; what
  the Agent may do is declared once in its `capabilities` (interaction, review,
  tools, readable artifacts, written products) and edited on the new
  **Capabilities** tab in Agent Studio. Agents that write a test result or a
  review get **Pass** / **Fail** outlets. Workflows that still contain a
  removed type fail validation with "未知节点类型 X", and Agents without
  capabilities fail with "Agent X 未声明能力"; there is no migration, so
  delete and recreate them.
- **Breaking — platform rule overrides removed:** the platform rules settings
  page, the per-Agent platform rules tab and the `/api/platform-rules` APIs are
  gone. Behaviour now lives in each Agent's `AGENTS.md` and skills; the
  platform keeps only its fixed common protocol.
- **Built-in templates:** Clarify, Implement and Test & review replace the
  previous engineer templates. All three can start a preview with
  `set_preview`. Onboarding has four steps (connect, team, workflow preview,
  done) and generates input → Clarify → Implement → Test & review, with Fail
  looping back to Implement.
- **Workflow canvas:** redesigned with autosave, a collapsible palette, a
  slide-out inspector, labelled outlets, inline edge conditions, quick add
  (`/`, Ctrl/Cmd+K, dragging an outlet to empty space, or the + on an edge),
  undo/redo, keyboard shortcuts, 8px grid snapping with alignment guides and
  automatic layout. Run details use the same canvas read-only, follow the
  running node and lay out workflows that have no saved positions.
- **Wording:** "流水线" / "pipeline" is now "工作流" / "workflow" everywhere in
  the product.
- **Direct preview toolbar:** refresh the Pick / Artifact / Chat controls with consistent icons, spacing, states, responsive wrapping, and an accessible custom hint for the original-page preview action.
- **Project credentials UI:** reorganize credentials into API key, Git, SSH, and other sections with responsive cards, configuration summary, and a structured add-credential form.
- **Project credentials:** add a model-provider API Key card with provider/model/API Base/vision settings; keys remain write-only and masked, while routing metadata is injected into OpenCode Runs.
- **Project credentials:** ACP and Git credentials can be managed in project
  credential settings, which take precedence at runtime; compatible
  project/Agent environment variables remain a fallback. Platform service
  configuration keeps its explicit environment > file > defaults precedence.
- **Gate approval chat:** a message that starts while an earlier queued item
  was already trimmed no longer removes the wrong waiting message from the
  queue panel.
- **App preview:** noVNC now keeps one persistent page per sandbox. Reopening
  the preview resumes the same screen without reloading, switching ports
  navigates within that page, and browser logins survive a Chromium restart.
  The viewer starts watch-only, with **Take over** / **Return control**. With
  IP direct preview on, the panel still uses noVNC and adds an "open directly in
  new tab" button. Public share pages behave the same way. The new
  `desktop_idle_ttl_seconds` setting (default 0) can close idle pages.
- **App preview:** the noVNC window shows the browser tab strip and address bar
  again and no longer clips the top of the page. **Cancel annotation** now
  leaves pick mode, and the panel shows a tip if the page did not leave it. The
  watch-only hint is easier to read.
- **App preview:** a watched preview no longer drops after five minutes without
  input. If the connection is closed for inactivity or lost, the panel shows a
  readable reason and reconnects on its own with backoff, waiting until the tab
  is visible again. It does not reconnect when the preview was opened in another
  window.
- **App preview:** `set_preview` now waits briefly for a port that is still
  starting. When the port is unreachable, the error tells the agent what to
  fix, including the address the app actually listens on (for example
  `127.0.0.1` instead of `0.0.0.0`), instead of `Process exited with status 1`.
- **Runs:** a paused node's agent session no longer floods the server log with
  `acp event channel full` warnings, and the platform stops reconnecting to the
  sandbox every two seconds while no turn is running.
- **PM chat:** a reply no longer fails with "connection lost" when the socket
  drops, the tab is refreshed, or a turn runs longer than 90 seconds. The turn
  keeps running on the server; reopening the thread replays it and follows it
  live. Messages sent while PM is busy wait in line, and **Stop** also clears
  the line. A turn cut short by a server restart is marked **Interrupted by
  restart** and can be retried.
- **Agent Studio:** the chat tester and the sandbox console's chat tab keep
  their queue and running reply when the page is refreshed or the connection
  drops. The panel reconnects on its own and picks the reply up where it is,
  and destroying the sandbox stops its queued messages.

## 1.2.1 — 2026-09-28

- **Preview drawer:** compact Page Harness CoCo title bar (grip + PH badge +
  ellipsis name, 48px toolbar) (#672).
- **Direct preview:** keep the drawer ticket across a full-page redirect
  (#670).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.2.1`.

## 1.2.0 — 2026-09-28

- **Agent page control:** server bridge, drawer relay, and page executor so
  the agent can drive the preview (#656–#658); prompt and docs (#659);
  page-agent pointer (#660); Pick and Chat stay grey without a live drawer
  ticket (#661); custom no-ticket tooltip (#662).
- **Direct preview:** in-page Pick bar (#646); drawer tickets and chat tokens
  (#649); chat-only drawer (#650); chat drawer opened in a new tab (#651);
  floating chat (#654).
- **Requirement leftovers:** auto-write test/review leftovers to requirement
  drafts (#633); root-cause JSON (#634); leftover drafts are self-contained
  specs (#635).
- **Preview:** noVNC and direct iframes go through the pick proxy (#640);
  share polling no longer trips the public rate limit (#655); app preview
  refits when the viewport grows (#666).
- **Chat:** clear stale busy state so the last turn is not replayed into a
  new bubble (#652); ignore the previous-turn ACP snapshot in the next live
  bubble (#653); confirm-and-advance no longer hides behind a hung sandbox
  turn (#663).
- **Sandbox:** resume a silent turn once before timing out (#667).
- **Inbox:** stop the false pending-update banner when the list is already
  current (#632).
- **Run detail:** stage and Agent sidebar lay flush instead of floating
  cards; artifact tiles fill the stage without rounded corners (#668).
- Floating nav on all routes (#641).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.2.0`.

## 1.1.0 — 2026-09-20

- **Preflight node:** env-confirmation Agent node with `ask_form` and vars
  export (#604); create-wizard template dropdown plus PreflightAgent pack
  (#611).
- **Approve identity:** rename the Approve node type to `grasp` so confirm-flow
  uses the canonical type (#607). Grasp/approve Phase1 hides `node_complete`
  until the user confirms (#628).
- **Artifacts:** auto-pin visible products to preview tabs on create/update
  (#602); show agent products, sniff image uploads, and version overwrites
  (#603).
- **Confirm flow:** success overlay with a single-path check draw (#613);
  page-level host and click-overlay fixes (#614, #616).
- **Fix:** one-shot turns end on process exit, not pipe EOF — leftover child
  processes no longer hang the session (#619).
- **Fix:** Plan mermaid diagrams render serially (#622).
- Home pipeline rail rise-on-settle (#621); StatusMetrics run zone routes to
  `/runs` (#612).
- Web polish: artifact version menu, clarify labels, inbox deep-links,
  skip-round placeholder, new-workflow plus centering, brand-purple button
  hover (#608–#610, #615, #617, #618).
- README brand banner / Trendshift badge / QQ community QR (#623, #625);
  ignore the whole `data/` runtime root (#620).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.1.0`.

## 1.0.1 — 2026-09-14

- **Fix:** ReAct empty-fail Retry was a dead click — wrappers never forwarded
  `retry-last`; also stop duplicating the optimistic retry slot (#600).
- **Fix:** Persist ACP `error_text` / `prompt_done{failed}` on clarify, open,
  and revise turns so the failure card shows provider errors (quota / 4xx)
  instead of a blank “no output” bubble (#600).
- **Fix:** Keep AppButton slotted inline icons on one row (#599).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.0.1`.

## 1.0.0 — 2026-09-14

- First stable public release (not a beta / prerelease tag).
- Hover-ink landing cover on nav and buttons (#593); keep icon/label from
  shifting after left-edge overflow, leave, or Vue class patch (#595–#597).
- Split status bar into Token / run-zone KPI cards (#594).
- Artifacts: pack by run (#586) and page loading states (#588).
- README: demo video, screenshots, star history (#584–#587, #590, #591).
- Replace private/internal host fixtures with example.com (#592).
- Freeze Approve opening hint copy (#589).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.0.0`.

## 0.5.4 — 2026-09-13

- Revert high-contrast origin-fill button hover (#576 / #581); restore prior
  button hover styling.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.4`.

## 0.5.3 — 2026-09-13

- **Fix:** `publish-image` can build again. Web `npm run build` runs the brand
  guard at `../scripts/assert-no-approving-brand.mjs`; the image now keeps
  `web/` and `scripts/` under `/src` so the walk root is the project, not `/`
  (which 404'd on `v0.5.2`, then OOM'd when the script sat at `/scripts`).
- CodeQL on default runners frees unused SDK disk before analyze so incremental
  analysis does not fail the `security` job.
- README (EN / zh-CN) leads with the FSM differentiator: designed
  success / fail / rollback, visual clarify, parallel human gates.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.3`.

## 0.5.2 — 2026-09-13

- Clear remaining product Approving brand identifiers (storage keys, MIME,
  preview-pick path, doctor header, sandbox prefixes, fixtures) to grasp with
  one-shot read-old-write-new migration; add a CI brand guard. GitHub repo URLs
  are unchanged.
- Clarify composer: unanswered ask_question cards send text/images as a fixed
  three-line skip envelope instead of applying recommended choices.
- ReAct empty/failed idle slots stay visible with a trailing-turn retry; later
  turns hide retry so the API is not called on a buried failure.
- Restore the connecting preview tab with HardLoadLayer; connecting→ready no
  longer flashes HardLoadLayer or the pipeline grid.
- Keep the Run detail canvas/timeline switcher in the left pane (no overlay on
  right-side node tabs).
- Agent Studio: move import / new agent / create-team into the AGENTS list
  header; reclaim vertical space below the org list.
- Settings general no longer duplicates platform rules and integrations cards
  (still reachable via settings subnav).
- High-contrast origin-fill hover on buttons; login tab title waits for locale
  so it shows 登录/Login · Grasp instead of raw i18n keys; zh-CN shared agent
  project tab label is「共享Agent配置」.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.2`.

## 0.5.1 — 2026-09-13

- **Fix:** Grasp now injects `AGENT_PROVIDER` (and no longer `ACP_BACKEND`) so
  the five backends select the right CLI. 0.5.0 baked `ENV AGENT_PROVIDER=cursor`
  into `universal-sandbox` and only set `ACP_BACKEND`, so claude_code /
  codebuddy / trae / opencode all started Cursor CLI. **The server-side fix
  also works against the already-published `universal-sandbox:0.5.0` image**;
  you do not have to swap the image first.
- The sandbox image no longer bakes `AGENT_PROVIDER=cursor`. Runtime selection
  is `AGENT_PROVIDER` only; the `ACP_BACKEND` env alias is gone.
- Auth chain no longer fails silently: empty keys warn (and generated OpenCode
  `{env:OPENCODE_API_KEY}` without a value errors); dropping a platform-level
  official CLI key logs a WARN; Agent-session and workflow Token merge both
  keep the shared Token when both sides set one; Agent sessions also read the
  project shared workspace auth files; invalid `settings.json` errors instead
  of being overwritten.
- Remove leftover `APPROVING_*` recognition from server and web (no compat
  layer). `git grep APPROVING_` should only hit CHANGELOG and the agent-pack
  guard test. Remove `skill_profile` dual-read / migrate, `cursor/` workdir
  fallback, and gateway `SBGW_IMAGE_TEMPLATE` / `SBGW_IMAGE_MAP` / byProvider
  image resolution.
- Startup logs the SQLite path and whether the file already existed.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.1`.

## 0.5.0 — 2026-09-12

- **Breaking:** publish one sandbox image `ghcr.io/cocofhu/universal-sandbox`
  instead of per-backend `universal-sandbox-{cursor,claude_code,codebuddy,trae,opencode}`.
  The image preinstalls the five CLIs; runtime `AGENT_PROVIDER` / `ACP_BACKEND`
  selects the live backend. Upgrade pins in `.env` / compose from the old
  per-provider tags to `universal-sandbox:<tag>`. Existing `0.4.0` split tags
  stay on GHCR. `GRASP_SANDBOX_IMAGE_*` remains an optional per-backend
  override.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.0`.

## 0.4.0 — 2026-09-12

- **Breaking:** remove the approving → grasp compatibility window. The
  control plane no longer reads `APPROVING_*`, rewrites `.env`, migrates
  `approving.db` / `.approving`, aliases `approving-local-demo`, or folds
  leftover Agent / MCP `APPROVING_*` keys and interpolations. Upgrade to
  0.3.17-beta first if you still have old names, then set `GRASP_*` only.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.4.0`.

## 0.3.17-beta — 2026-09-12

- Fold leftover `${APPROVING_*}` interpolations in Agent / shared-Agent MCP
  url / headers / env on boot, read, and save. Runtime template vars keep
  `APPROVING_*` aliases so unsaved old templates still resolve until the next
  minor. Run-start sandbox env also denies leftover `APPROVING_*` auth /
  artifact keys.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.17-beta`.

## 0.3.16-beta — 2026-09-12

- Product wordmark, login/home splash, favicon, default notify prefix, DingTalk
  card title, run-log export header, and docs site brand are Grasp. A stored
  product name of `Approving` falls back to Grasp so upgraded instances do not
  keep the old logo. The Approve node display name is Grasp (type stays
  `approve`).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.16-beta`.

## 0.3.15-beta — 2026-09-12

- Fold stored Agent / shared-Agent `APPROVING_*` env keys to `GRASP_*` on
  read, save, and boot (the 0.3.14-beta rename left existing
  `APPROVING_CURSOR_API_KEY` rows in Studio). Runtime still accepts the old
  names until the next minor.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.15-beta`.

## 0.3.14-beta — 2026-09-12

- **Breaking:** rename the public package, image, and env prefix from Approving
  / `APPROVING_*` to Grasp / `GRASP_*`. Go module is now
  `github.com/cocofhu/grasp`; the app image is `ghcr.io/cocofhu/grasp`; compose
  service / binaries are `grasp` / `/app/grasp` / `/app/grasp-server`; default
  SQLite file is `grasp.db`.
- This release auto-migrates old config: the control-plane server rewrites
  `APPROVING_*` keys in nearby `.env` files on boot and still reads
  `APPROVING_*` (GRASP wins when both are set). `./start.sh` only exports
  aliases for compose interpolation. Default `approving.db` / `.approving`
  are renamed when the new path is free. The local-demo gateway key is
  `grasp-local-demo` and still accepts `approving-local-demo`.
- **Next release removes this compatibility.** Update compose, agent env
  templates, and secrets to `GRASP_*` now. Custom agent env that still
  injects `APPROVING_*` will not be dual-emitted into sandboxes.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.14-beta`.

## 0.3.13-beta — 2026-09-11

- OpenCode models that models.dev does not describe can opt into image input
  with `APPROVING_OPENCODE_MODEL_VISION=1`, so vision models on a gateway are
  not treated as text-only.
- Onboarding, Agent create, Agent meta, and shared Agent show a Vision switch
  for custom vendors and for catalog vendors with a typed-in model.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.13-beta`.

## 0.3.12-beta — 2026-09-11

- OpenCode's bridge model override now receives the normalized
  `provider/model` value, preventing `--model` from routing slashed upstream
  model IDs to the wrong provider.
- Agent, platform, and browser MCP servers are translated into OpenCode's
  native `opencode.json` `mcp` block; `mcp.json` remains available to the other
  ACP backends.
- Existing user-authored OpenCode configuration is merged without replacing
  user values, and malformed configuration is rejected instead of overwritten.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.12-beta`.

## 0.3.11-beta — 2026-09-11

- OpenCode custom providers can be named directly (for example
  `tencent-tokenhub`) instead of exposing a `custom/` routing prefix to users.
- Model IDs containing `/` are preserved end to end, so gateway IDs such as
  `deepseek/deepseek-flash` reach the upstream request unchanged.
- Provider and model membership use the same models.dev snapshot as the UI.
  Missing providers receive an OpenAI-compatible adapter; missing models are
  declared without replacing a catalog provider's native adapter.
- Catalog-absent providers require an API Base URL in every save flow.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.11-beta`.

## 0.3.10-beta — 2026-09-11

- Public beta follow-up on [`v0.3.10-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.10-beta)
  (relative to `v0.3.9-beta`: PRs #542–#551). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.10-beta`, including `universal-sandbox-opencode` (tag publish does not
  rewrite these files).
- Highlights: Agent create wizards align with onboarding start paths; two-tone
  page slogans; micro-interactions / motion tokens; project Agents onboarding
  embed; global Agents sidebar + `/agents` Studio; home pipeline create entry;
  Agent Git step always shows provider choices.

## 0.3.9-beta — 2026-09-10

- Public beta follow-up on [`v0.3.9-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.9-beta)
  (relative to `v0.3.8-beta`: PRs #538–#541). Full notes on the GitHub Release.
- Default GHCR pins stayed at `*:0.3.8-beta` until `0.3.10-beta` (OpenCode
  sandbox image was first published on this tag).
- Highlights: OpenCode as fifth ACP backend; Visual `page.html` fidelity;
  from-baseline workflows on home; sidebar ZH copy + chrome motion.

## 0.3.8-beta — 2026-09-10

- Public beta follow-up on [`v0.3.8-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.8-beta)
  (relative to `v0.3.7-beta`: PRs #531–#536). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.8-beta` (tag publish does not rewrite these files).
- Highlights: StatusMetrics opens `/stats`; home baseline-workflow modal;
  ReAct connecting loader; live run-detail chrome during clarify; public
  approval inbox parity; token analytics stacked-bar dimensions.

## 0.3.7-beta — 2026-09-10

- Public beta follow-up on [`v0.3.7-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.7-beta)
  (relative to `v0.3.6-beta`: PRs #526–#529). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.7-beta` (tag publish does not rewrite these files).
- Highlights: home pipeline cards show project name; baseline workflow create
  requires a name; Prompts tab no longer false-dirties Agent Studio; unified
  embedded composer toolbar.

## 0.3.6-beta — 2026-09-09

- Public beta follow-up on [`v0.3.6-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.6-beta)
  (relative to `v0.3.5-beta`: PRs #501–#524). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.6-beta` (tag publish does not rewrite these files).
- Highlights: home Composer run priority; instance brand settings; dashboard
  pipeline menu; workflow create from scratch/baseline; Runs in workspace nav;
  today's tokens in the topbar.

## 0.3.5-beta — 2026-09-09

- Public beta follow-up on [`v0.3.5-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.5-beta)
  (relative to `v0.3.4-beta`: PRs #494–#499). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.5-beta` (tag publish does not rewrite these files).
- Highlights: first-install agents clone `vars.repos` on every node; default
  workflow shown on Home; floating workspace nav; ReAct stream while still
  replying.

## 0.3.4-beta — 2026-09-08

- Public beta follow-up on [`v0.3.4-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.4-beta)
  (relative to `v0.3.3-beta`: PRs #485–#492). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.4-beta` (tag publish does not rewrite these files).
- Highlights: Approve `set_preview` live app preview; first-install wizard
  (default team + workflow); integrations in Settings modal; mobile ReviewShell
  drawer fill; pending-queue annotation re-edit.

## 0.3.3-beta — 2026-09-04

- Public beta follow-up on [`v0.3.3-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.3-beta)
  (relative to `v0.3.2-beta`: PRs #441–#483). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.3-beta` (tag publish does not rewrite these files).
- Highlights: global Token analytics; Agent workspace VCS + file history;
  SSH credentials in Agent meta with pre-clone inject; Plan multi-diagram tabs
  and mermaid validate; composer drafts in IndexedDB; `skill_profile` →
  `agent_profile` with legacy compat.

## 0.3.2-beta — 2026-08-26

- Public beta follow-up on [`v0.3.2-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.2-beta)
  (relative to `v0.3.1-beta`: PRs #415–#439). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.2-beta` (tag publish does not rewrite these files).
- Highlights: project shared Agent config (+ sandboxEnv migration); project external MCP;
  clarify queue cancel/reorder/edit; notification read prefs / per-run reads; mobile UX;
  Plan data_design hard gate; ACP timeline snapshot; home composer draft.

## 0.3.1-beta — 2026-08-23

- Public beta follow-up on [`v0.3.1-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.1-beta)
  (relative to `v0.3.0-beta`: CI flake fix #414). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.1-beta` (tag publish does not rewrite these files).

## 0.3.0-beta — 2026-08-23

- Public beta follow-up on [`v0.3.0-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.0-beta)
  (relative to `v0.2.2-beta`: PRs #385–#412). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.0-beta` (tag publish does not rewrite these files).

## 0.2.2-beta — 2026-08-21

- Public beta follow-up on [`v0.2.2-beta`](https://github.com/cocofhu/approving/releases/tag/v0.2.2-beta)
  (relative to `v0.2.1-beta`: PRs #327–#384). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.2.2-beta` (tag publish does not rewrite these files).

## 0.2.1-beta — 2026-08-14

- Public beta follow-up on [`v0.2.1-beta`](https://github.com/cocofhu/approving/releases/tag/v0.2.1-beta)
  (relative to `v0.2.0-beta`: PRs #311–#325). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.2.1-beta` (tag publish does not rewrite these files).
- Highlights: 反馈账本与 Agent 总结；GateShare 权限预设；应用预览 IP 直连/取点；
  公开审批滚动与确认态；MCP CAPA 误杀修复；若干 Web UX 修复。

## 0.2.0-beta — 2026-08-13

- Public beta follow-up on [`v0.2.0-beta`](https://github.com/cocofhu/approving/releases/tag/v0.2.0-beta)
  (relative to `v0.1.2-beta`: ~385 commits, PRs through #309). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.2.0-beta` (tag publish does not rewrite these files).
- Highlights: QQ/企微/飞书/钉钉等多渠道与 live 接话；公开审批与分享链路；需求草稿/PRD；
  通知中心与顶栏指标；应用预览/noVNC；工作流收藏与团队模板；大量 Web Loading/布局打磨。

## 0.1.2-beta — 2026-07-31

- Public beta follow-up on [`v0.1.2-beta`](https://github.com/cocofhu/approving/releases/tag/v0.1.2-beta)
  (relative to `v0.1.1-beta`: PRs #143–#151). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.1.2-beta` (tag publish does not rewrite these files).
- Highlights: Gates Inbox 纳入 app_preview 待审批；Inbox 布局/预览预算修复；inspect 可取消；
  revise 失败不再显示 Done；PreviewIssues 冷会话退回；sandbox `/tmp` PVC；Token 实心饼图；
  ArtifactPreview 图片 padding；输出节点 goto 邻接选项。

## 0.1.1-beta — 2026-07-29

- Public beta follow-up on [`v0.1.1-beta`](https://github.com/cocofhu/approving/releases/tag/v0.1.1-beta)
  (relative to `v0.1.0-beta`: PRs #136–#141). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.1.1-beta` (tag publish does not rewrite these files).
- Highlights: Token 按模型统计；审计分页升级；HTML 主产物放大；沙箱环境变量单行启用开关；
  human_gate 冷会话静默；Run 筛选触发文案左对齐。

## 0.1.0-beta — 2026-07-29

- Public beta on [`v0.1.0-beta`](https://github.com/cocofhu/approving/releases/tag/v0.1.0-beta)
  (relative to `v0.0.4-beta`: PRs #113–#134). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.1.0-beta` (tag publish does not rewrite these files).
- Highlights: 评审统一「确认并流转」；澄清/预览流式与刷新续传；桌面 HTML 预览 fillParent；
  项目管理文案；app_preview 纯 ReAct；同项目 agent_profile 约束。

## 0.0.4-beta — 2026-07-28

- Public beta follow-up on [`v0.0.4-beta`](https://github.com/cocofhu/approving/releases/tag/v0.0.4-beta)
  (relative to `v0.0.3-beta`: onboarding bootstrap #109 + pin). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.0.4-beta` (tag publish does not rewrite these files).
- Highlights: 空项目首次上手引导（五步向导 + bootstrap 五 Agent +「快速上手·轻量」；默认 well-known Heroku git）。

## 0.0.3-beta — 2026-07-28

- Public beta follow-up on [`v0.0.3-beta`](https://github.com/cocofhu/approving/releases/tag/v0.0.3-beta)
  (relative to `v0.0.2-beta`: ~43 commits, PRs #62–#106). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.0.3-beta` (tag publish does not rewrite these files).
- Highlights: PM MCP artifact/react/fs；QQ 通知与模板；审计轨迹；TagFilter；澄清/审批流式续传；
  release 持久化 `/app/data` + 按 acpBackend 多镜像；安全/CI（CodeQL、x/net、gateway Dockerfile）。

## 0.0.2-beta — 2026-07-26

- Public beta follow-up on [`v0.0.2-beta`](https://github.com/cocofhu/approving/releases/tag/v0.0.2-beta)
  (relative to `v0.0.1-beta`: ~161 commits, PRs #1–#61). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.0.2-beta` (tag publish does not rewrite these files).
- Highlights: Run delete/cancel/sort + Token KPIs; Board/project Token stats;
  Agent 5-step wizard; PM IM QQ egress + cron UTC fix; Docker/K8s sandbox logs;
  docs `/en` + marketing site; CI coverage gates, e2e, CodeQL/security cleanup.

## 0.0.1-beta — 2026-07-25

- Initial public beta of Approving (MIT, Copyright 2026 cocofhu).
- Strip private registry, Apollo, and k3s preview hosts; default sandbox images
  to `universal-sandbox-*:local` (keep public `github.com/cocofhu` / `ghcr.io/cocofhu`).
- Adopt `APPROVING_*` environment variables, `.approving/` workspace paths, and
  matching binary/image names.
- Remove repository GitLab CI, VitePress docs site, showcases, deploy previews,
  and root selftest scripts. Keep sandbox Git host authorization for GitLab and
  GitHub (`GITLAB_*` / `GITHUB_*` / SSH on Agent meta env).
- Vendor `sandbox-gateway` (control plane + universal sandbox image) into
  `sandbox-gateway/` so a single clone runs via `./start.sh` / `docker compose up`.
- Add GitHub Actions CI, release-smoke, and GHCR publish workflows; root
  community policy files; `GATEWAY.md` and generated `server/CONFIGURATION.md`.
- Chinese README (`README.zh-CN.md`).
