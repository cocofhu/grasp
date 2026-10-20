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
