package models

import (
	"strconv"
	"strings"
)

// AgentPrompts holds a per-Agent override of the platform-injected prompt text
// and sandbox rule files. It is persisted in the Agent's agent.json under the
// "prompts" key and read back by the runtime when a workflow node references
// that Agent (via agent_profile). Every field is optional: an empty field falls
// back to the built-in default (the Default* constants below), so an Agent that
// omits "prompts" behaves exactly like before this became configurable.
//
// The templated fields (ProducesContract, ProducesRetry) use a `{name}`
// placeholder for the declared produces file name, substituted at use time.
// A named placeholder (not fmt %s) keeps a misconfigured value from breaking
// formatting.
type AgentPrompts struct {
	// UpstreamArtifactsHeader is prepended before the list of upstream produces
	// files seeded into the agent prompt. Emitted only when upstream artifacts
	// exist.
	UpstreamArtifactsHeader string `json:"upstreamArtifactsHeader,omitempty"`
	// ProducesContract is the mandatory-produces clause appended when the node
	// declares `produces`. Supports the `{name}` placeholder.
	ProducesContract string `json:"producesContract,omitempty"`
	// ReactOpenSuffix is appended to the opening turn of a ReAct clarify node.
	ReactOpenSuffix string `json:"reactOpenSuffix,omitempty"`
	// ProducesRetry is the re-prompt sent when a react node finished without
	// writing its declared produces. Supports the `{name}` placeholder.
	ProducesRetry string `json:"producesRetry,omitempty"`
	// PlanContract is appended to a plan node's prompt: the plan node's sole
	// deliverable is calling set_plan.
	PlanContract string `json:"planContract,omitempty"`
	// ImplementContract is appended to an implement node's prompt: read the plan
	// and mark per-item progress via update_plan_status.
	ImplementContract string `json:"implementContract,omitempty"`
	// PlanIncompleteRetry is the re-prompt sent to an implement node that
	// finished with plan items still not done. Supports the `{items}`
	// placeholder for the incomplete-item list.
	PlanIncompleteRetry string `json:"planIncompleteRetry,omitempty"`
	// ClarifiedRequirementContract is appended to a react (clarify) node: its
	// deliverable is calling set_clarified_requirement.
	ClarifiedRequirementContract string `json:"clarifiedRequirementContract,omitempty"`
	// ClarifiedOpenQuestionsRetry is the re-prompt sent to a react node that
	// tried to finish while unresolved open_questions remain: it must raise them
	// as ask_question so the user resolves every one. Supports the `{items}`
	// placeholder for the unresolved-question list.
	ClarifiedOpenQuestionsRetry string `json:"clarifiedOpenQuestionsRetry,omitempty"`
	// PreflightContract is appended to a preflight node: env checklist via
	// set_preflight; passwords plaintext; ask_form/ask_question for gaps.
	PreflightContract string `json:"preflightContract,omitempty"`
	// PreflightRetry is the re-prompt when preflight.json is missing or not
	// confirmed. Supports `{reason}`.
	PreflightRetry string `json:"preflightRetry,omitempty"`
	// ImplementResultContract is appended to an implement node: after finishing
	// the plan it must summarize the work via set_implementation_result.
	ImplementResultContract string `json:"implementResultContract,omitempty"`
	// ResearchContract / TestContract / ReviewContract / ProposalContract are
	// appended to the respective framework-card nodes; each names the sole
	// structured deliverable that node's set_* tool writes.
	ResearchContract string `json:"researchContract,omitempty"`
	TestContract     string `json:"testContract,omitempty"`
	ReviewContract   string `json:"reviewContract,omitempty"`
	ProposalContract string `json:"proposalContract,omitempty"`
	// MRContract is appended to a submit_mr node: resolve conflicts against the
	// target branch, push the source branch, and open a merge request. Supports
	// the `{source}` and `{target}` branch placeholders.
	MRContract string `json:"mrContract,omitempty"`
	// VisualContract is appended to a visual node: its sole deliverable is a
	// single self-contained HTML page (inline CSS/JS, no external resources).
	VisualContract string `json:"visualContract,omitempty"`
	// ApproveContract is appended to an approve node: two required deliveries
	// (clarified requirement + plan) plus optional research/visual/proposal tools.
	// Ending is phased: forbid node_complete until human「确认并流转」, then require it.
	ApproveContract string `json:"approveContract,omitempty"`
	// PreviewContract is appended to an app_preview node: register a preview
	// via set_preview(port) or set_preview(url) (exactly one).
	PreviewContract string `json:"previewContract,omitempty"`
	// PreviewRetry is the re-prompt when app_preview finished without set_preview.
	PreviewRetry string `json:"previewRetry,omitempty"`
	// StructuredRetry is the generic re-prompt sent when a framework node
	// finished without writing its reserved structured product. Supports the
	// `{name}` (artifact) and `{tool}` (set_* tool) placeholders.
	StructuredRetry string `json:"structuredRetry,omitempty"`
	// OutcomeContract is appended to every Agent-class node: must call
	// node_complete before finishing.
	OutcomeContract string `json:"outcomeContract,omitempty"`
	// OutcomeRetry is the re-prompt when the agent finished without node_complete.
	OutcomeRetry string `json:"outcomeRetry,omitempty"`
	// ReviewCommitWrapUp is sent on ReAct「确认并流转」when the parked sandbox
	// still has uncommitted working-tree changes. Supports `{files}`.
	ReviewCommitWrapUp string `json:"reviewCommitWrapUp,omitempty"`
	// ReviewConfirmReconcile is sent to a review-capable producer on
	// 「确认并流转」: reconcile the structured products against the transcript
	// before the node advances. Approve uses ApproveConfirmSuffix instead.
	ReviewConfirmReconcile string `json:"reviewConfirmReconcile,omitempty"`
	// ConfirmSummaryContract is the hidden summary turn sent right after the
	// reconcile turn: induce the whole dialogue into one JSON agentSummary.
	ConfirmSummaryContract string `json:"confirmSummaryContract,omitempty"`
}

// Built-in default prompt fragments. These are the exact strings the platform
// injected before prompts became per-Agent configurable; kept here so an empty
// Agent field reproduces the original behavior.
const (
	DefaultUpstreamArtifactsHeader = "\n\n## 上游产物(只读输入)\n以下产物由上游节点产出,请用 `read_artifact` MCP 工具按名读取(它们不在工作区,不要去文件系统找):\n"
	DefaultProducesContract        = "\n## 产物契约(强制)\n完成前必须在工作目录(/root/workspace)写出文件 `{name}`,这是本节点的强制产物;未写出将判定为失败。\n"
	DefaultReactOpenSuffix         = "\n\n这是一次多轮澄清对话:先提出需要澄清的关键问题,等待我的回复后再继续,不要一次性给出最终结论。"
	DefaultApproveOpenSuffix       = "\n\n这是一次多轮 ReAct 对话:用户已发出目标,请用手上的工具阅读仓库/产物、对齐需求并写入澄清与计划。只有存在真实分歧、需要用户拍板时才调用 ask_question;禁止编造空泛开场选择题(例如「修缺陷/新功能/重构」这类为问而问)。信息充分时写入 set_* 产物并等待用户确认并流转;未点「确认并流转」前禁止 node_complete。"
	// DefaultReactConfirmSuffix is injected on classic react clarify force
	// (「确认并流转」/「结束澄清」) turns. It names no specific set_* tool because a
	// react node's deliverable comes from its own contract; the shared clause is
	// reconciling products against the transcript before wrapping up.
	DefaultReactConfirmSuffix = "【确认流转】用户已点击确认,澄清到此结束。请按顺序做两件事:\n1. 通读本节点的完整聊天记录,据此补充或修正你已写入的产物:把历次已确认的结论落进产物,清掉与对话相矛盾的旧内容。\n2. **在本回合内**按本节点契约完成收尾并调用 `node_complete`——这一步不能省略,也不能留到下一回合。\n\n禁止提问:不要再提问、不要调用 ask_question;信息不足就按对话中已有的结论定稿。"
	// DefaultApproveConfirmSuffix is injected on Approve force(「确认并流转」) turns:
	// after human confirm, reconcile the required products against the whole
	// transcript, then call node_complete.
	DefaultApproveConfirmSuffix = "【确认流转】用户已点击「确认并流转」,审批到此结束。请按顺序做两件事:\n1. 通读本节点的完整聊天记录,据此补充或修正 `set_clarified_requirement` 与 `set_plan`(`open_questions` 必须为空):把历次已确认的结论落进产物,清掉与对话相矛盾的旧内容。\n2. **在本回合内**调用 `node_complete` 结束本节点——这一步不能省略,也不能留到下一回合。\n\n禁止提问:不要再提问、不要调用 ask_question;信息不足就按对话中已有的结论定稿。"
	DefaultProducesRetry        = "【必须完成】本节点尚未写入声明的产物 `{name}`,这是唯一未完成的强制要求。现在立即调用 write_artifact 工具写入 `{name}`(内容为本次澄清得到的结论),不要再提问、不要输出其它内容、不要给出解释——只需完成这次写入。"
	DefaultPlanContract         = "\n\n## 计划契约(强制)\n你是计划节点,唯一交付是调用 `set_plan` 工具写入一份最多两级(大目标→小目标)的结构化计划;不要写代码、改仓库或写其它产物文件。\n\n**goals(强制)**:`goals[]` 大目标,每个可含 `subgoals[]` 小目标(叶子,不可再嵌套);每项 `title`(可选 `detail`);状态由平台初始化为 pending。\n\n**设计区(写入时完整性约定)**:可选字段 `architecture` / `data_design` / `interfaces` / `components` / `interaction` / `test_design`。一旦写入设计区,六节应齐全;某节无实质内容时显式写「不涉及」(summary/test_design 字符串,或 interfaces/components 用 `[{name:\"不涉及\",…}]`),禁止静默省略导致实现猜测。纯 goals-only 旧计划仍合法,不必强行带六节键。\n\n**图按需、非强制**:`architecture`/`data_design`/`interaction` 可挂 `diagrams[]`(及兼容单数 `diagram`);`interfaces`/`components` 项亦可选同结构。图对象含 `kind`/`title`/`scope`/`format?`/`source`/`fallback_artifact?`/`caption?`(有对象则 source 必填)。一等 kind:activity/flowchart/sequence/er。涉及活动/业务流/时序/数据时尽量都提供以便审批;未涉及的种类不必出;多子模块按需补图并写 scope。**禁止「必须同时提交四种图否则失败」**。缺可选图种不拒回。前端同节多图用节内小 Tab(不是左目录+右画布)。\n\n**实质 data_design 硬门禁**:当 `data_design.summary`(去空白)不是「不涉及」/「N/A」时,必须提供至少一张 ER(`diagrams[]` 中 kind=er,或兼容单数 diagram)、至少 1 个 `entities[]`,且每个实体至少 1 个结构化 `fields[]`(每项 `name`+`type` 必填;可选 `pk`/`nullable`/`fk`/`description`);仅 legacy `attributes` 不足以通过。流程:调用 set_plan → 解析与硬门禁 → 入库 → PlanView 展示。set_plan 调用成功即完成本节点。\n"
	DefaultImplementContract    = "\n\n## 实现契约(强制)\n你是实现节点:先用 `get_plan` 读取计划,按大目标→小目标逐项落地。**进度标记是硬性要求**:每开始一项先调用 `update_plan_status(id, \"in_progress\")`,该项做完立即调用 `update_plan_status(id, \"done\")`。平台仅凭这些状态判断完成度——只把代码写好却不标记,会被判为未完成并反复催促。结束前必须让所有叶子项都为 `done`。\n"
	DefaultPlanIncompleteRetry  = "以下计划项尚未标记为完成:\n{items}\n如果这些项对应的工作其实已经做完,请**立即**对每一项调用 `update_plan_status(id, \"done\")` 把状态补上,不要重复已完成的实现;若确有未完成的,先实现再标记。所有项都标记 done 前不要结束。"

	DefaultClarifiedRequirementContract = "\n\n## 需求契约(强制)\n你是需求澄清节点,唯一交付是调用 `set_clarified_requirement` 写入完整需求规格(对齐 ISO/IEC/IEEE 29148 / PRD 子集)。\n\n**必填字段**:`title`、`summary`、`background`、`goals[]`(≥1)、`in_scope[]`(≥1)、`out_of_scope[]`(≥1)、`functional_requirements[]`(≥1;每条含 `title`+`detail`+≥1 `acceptance_criteria`;`priority` 取 must|should|could,缺省 must)、`assumptions[]`/`dependencies[]`/`constraints[]`(各≥1;无实质内容时写明确「无额外…(已与用户确认)」,禁止省略键)。\n\n**可选字段**(有则写):`success_metrics`、`personas`、`user_scenarios`、`non_functional_requirements`(category: performance|security|usability|reliability|compatibility|other;可含 metric)、`external_interfaces`、`data_entities`、`business_rules`、`edge_cases`、`limitations`、`risks`、`glossary`。\n\n**禁止**:排期/里程碑/交付日期;技术选型、架构或详细 API/DB 设计(留给调研/方案节点)。不要写代码或改仓库。需求规格只能用 `set_clarified_requirement`;给人看的预览材料(页面/文案/示意图)可用 `write_artifact`,写完立刻 `set_artifact_preview` 钉到预览 Tab。\n\n**澄清是门禁:任何还不确定、需要用户拍板的点,都必须通过 `ask_question` 让用户做选择,不能把疑问塞进 `open_questions` 就结束。** 用 `ask_question` 提问时,如果你对某个选项有明确倾向,请把它的 `recommended` 置为 true(单选每题最多 1 个;多选应标记 1 个或多个),便于用户一键确认;在自动模式下平台会选中全部推荐项(未标记则回退该题首项)。调用 `set_clarified_requirement` 结束澄清时 `open_questions` 必须为空(留空或不传)。不要替用户擅自决定。\n\n## Demo 预览(可选)\n当问题涉及 **UI/交互/布局** 等视觉决策时,可为每个选项附带 `demoHtml`:以 `<!doctype html>` 开头的完整自包含 HTML 文档,前端用 iframe 并排或选中预览。**非 UI 类问题不要写 demoHtml。**\n- 每选项最多一个 Demo;允许引用 CDN(Tailwind、图标库等)。\n- ≤3 个含 Demo 的选项时界面三列并排对比;>3 时降级为选中后单预览。\n- 同一题内各选项 `label` 须唯一,便于历史轮次还原已选态。\n\n### demoHtml 运行环境(强制)\ndemoHtml 运行于 Gates HtmlPreview 的 sandbox iframe(sandbox=\"allow-scripts allow-forms\",无 allow-same-origin,文档为 opaque origin)。\n禁止:读取/写入 localStorage、sessionStorage;禁止:依赖 cookie 或同源 Web Storage 的持久化/登录态。\n需要完整 SPA、持久化或真实浏览器能力时,改走 app_preview(noVNC),不要在 srcdoc 中硬做,也不得引导恢复 allow-same-origin。\n\n## 产物舞台预览(可选)\n当需要在提问卡片之外给人看一份完整页面、文案稿或示意图时:先 `write_artifact(name, content, kind)`,再立刻 `set_artifact_preview(name)`。选项级并排对比用 `demoHtml`;独立成稿、需热更新或取点标注用产物舞台。可多次切换;同名再次 `write_artifact` 后预览会热更新。结束澄清仍必须调用 `set_clarified_requirement`(`open_questions` 为空)。\n"
	DefaultImplementResultContract      = "\n\n## 实现结果契约(强制)\n计划全部完成后:\n1. **提交并推送**:工作区根 `/root/workspace` 不是 git 仓库,每个仓库位于 `/root/workspace/<name>/`。对每个有改动的仓分别 `cd` 进其目录,各自 `git add` + `git commit`,再 `git push` 该仓的工作分支到远端(origin,多个仓可用同一分支名)。下游节点在全新克隆里工作,不推送就拿不到你的代码,不要遗漏任一改动仓。\n2. 然后调用 `set_implementation_result` 工具写入结构化的实现结果说明(概述 + 主要改动 + 测试情况 + 破坏性变更/后续),并在其中说明各仓的工作分支名。\n\n**软提示(计划贴合度)**:请按 plan 叶子逐项交付,并在实现结果/提交说明中留下便于测试阶段填写 `plan_coverage.evidence` 的可核对痕迹。implement 节点不因缺少 `plan_coverage` 而硬失败;贴合度硬门禁在 test 阶段。\n"
	DefaultResearchContract             = "\n\n## 调研契约(强制)\n你是调研节点,唯一交付是调用 `set_research` 工具写入结构化调研结论(概述 + 调研问题及结论/关键发现,可含建议与参考)。不要改仓库或写其它产物文件。\n"
	DefaultTestContract                 = "\n\n## 测试契约(强制)\n你是测试节点,唯一交付是调用 `set_test_result` 工具写入结构化测试总结(总体结论 + 用例结果 + 缺陷/偏差/评估)。请如实记录通过与失败,不要粉饰。\n\n**测试是门禁**:只要有用例失败(status=failed),平台会判定本节点未通过并把流程按失败/回滚边打回上游修复。因此务必据实填写每个用例的 status,不要为了通过而谎报。若节点配置了 `block_on_skipped=true`,则 skipped 用例同样会阻塞门禁(默认 false,仅 failed 阻塞,与单仓现网一致)。\n\n**计划贴合度门禁(plan_coverage)**:当本次 run 存在 plan.json 且叶子非空时,必须在 `set_test_result` 提交 `plan_coverage[]`,逐叶子填写 `plan_id`、`passed`(须为 true)、非空 `evidence`(可选 `title`)。须覆盖全部叶子;未知/重复 plan_id、passed≠true、evidence 为空/空白均导致门禁失败并经 exits.fail 回修,不得进入 submit_mr。无 plan 叶子时可省略该字段。与 cases 门禁同时生效。先 `get_plan` 再逐项填写。\n\n**仓库测试布局**:工作区根 `/root/workspace` 不是仓库,每个仓库位于 `/root/workspace/<name>/`。请在各仓子目录分别探测并执行测试(如 `go test ./...`、`npm test`),将结果汇总到**单一** `set_test_result.cases[]`;用例 `name` 建议加仓名前缀便于阅读,如「[frontend] 单元测试」「[backend] API 测试」。跨仓 E2E 时自行拉起各仓服务(绑定 `127.0.0.1:<port>`)后执行,无需 testMatrix 配置。\n\n**浏览器/端到端 E2E**:沙箱已预装无头 Chromium 与 Playwright 系统依赖(`PLAYWRIGHT_BROWSERS_PATH=/ms-playwright`)及中文字体。需要验证前端/全栈行为时,请在容器内自行启动被测应用(后端 + 前端绑定 `127.0.0.1:<port>`),再以无头方式跑项目 E2E 或临时 Playwright 脚本打 `http://127.0.0.1:<port>`。**不得仅以「没有完整 Web 应用/后端/浏览器,无法做 Playwright 验收」为由把浏览器 E2E 标为 skipped**——环境已具备,应自起应用后据实执行;确有具体技术原因无法执行时,才可 skipped 且须在该用例 detail 写明真实原因。做了浏览器/UI 测试时,把关键页面截图(最多 10 张)提交到测试结果:先把截图存成 PNG 文件,再用沙箱内置命令 `artifact-upload <文件> --caption \"说明\"` 上传(它会打印一个产物名),然后在 `set_test_result` 的 `screenshots` 里用 `{artifact: \"<打印出的产物名>\", caption: \"说明\"}` 引用;平台只保留 artifact 引用(及 caption/mimeType),**不再写时回填**内联图片数据,展示侧按引用懒加载。`screenshots` **只接受 artifact 引用,不支持内联 base64**。\n"
	DefaultReviewContract               = "\n\n## 评审契约(强制)\n你是评审节点,唯一交付是调用 `set_review` 工具写入结构化评审结论(结论 verdict + 概述 + 按严重度排列的意见与建议)。verdict 取 approve|approve_with_comments|request_changes|reject。\n\n**评审是门禁**:verdict 为 request_changes 或 reject 时,平台会判定本节点未通过并把流程按失败/回滚边打回上游整改;approve 或 approve_with_comments 才放行。请根据代码/设计实际质量如实给出 verdict。\n"
	DefaultProposalContract             = "\n\n## 方案契约(强制)\n你是方案节点,唯一交付是调用 `set_proposals` 工具写入结构化候选方案集(背景 + 至少 1 个方案,含优缺点/权衡/工作量/风险);如有推荐方案将其 recommended 置为 true。可给出多个方案供后续确认。\n"
	DefaultMRContract                   = "\n\n## 合并请求契约(强制)\n你是提交 MR 节点,目标是让源分支 `{source}` 能干净地合入目标分支 `{target}` 并存在一个对应的合并请求（MR/PR）。当已有 open 单可复用、工作已合入、或源相对目标已无差异且可解释时,目标亦视为已满足（幂等成功）。工作区根 `/root/workspace` 不是仓库,**先 `cd` 进目标仓目录(`/root/workspace/<name>/`)再执行以下所有 `git` 与对应 CLI（`glab`/`gh`）命令**。请依次完成:\n1. **对齐目标分支并解冲突**:`git fetch origin {target}`,把 `origin/{target}` 合入当前源分支(merge 或 rebase 均可),**逐个解决所有冲突**后 `git add` 已解决文件并提交。\n2. **推送**:`git push origin {source}`(源分支)。**无论后续能否自动建单,都必须先完成本步。**\n3. **按远端主机选型创建/复用合并请求**(按主机与环境变量匹配,不按 Token 有无或 CLI 轮询)。**强制操作顺序**:list open → create（仅当无 open）→ 若 create 非零则解析幂等错误 → list merged/view → `node_complete`。**create 非零退出不得直接判 failed。**\n   - **GitLab**（远端主机为 gitlab.com,或与 `GITLAB_URL` 主机一致）:\n     1) 先 `glab mr list --source-branch {source} --target-branch {target} --state opened`（或等价）查 open;命中则复用其 Web URL,**跳过新建**。\n     2) 无 open 时再 `glab mr create --source-branch {source} --target-branch {target} --fill --yes`。\n     3) create 若因 already exists / No commits between / 已无差异等同类原因失败:**不得直接 failed**;转入查询 open（若有）或 merged 单,再按步骤 4 幂等成功规则结束。\n   - **GitHub**（远端主机为 github.com,或与 `GITHUB_URL` 主机一致,含 GHE）:\n     1) 先 `gh pr list --base {target} --head {source} --state open` 查 open;命中则复用其 Web URL,**跳过新建**。\n     2) 无 open 时再 `gh pr create --base {target} --head {source} --fill`。\n     3) create 若因 already exists / No commits between / 已无差异等同类原因失败:**不得直接 failed**;转入查询 open 或 merged（`gh pr list --state merged` / `gh pr view`）,再按步骤 4 幂等成功规则结束;成功时 PR Web URL 写入同一字段 `outputs.mr_url`。\n   - **匹配不上**（如 Gitea 等）或不支持自动建单:不要假装已建单。\n   凭据由沙箱提供（`GITLAB_*` / `GITHUB_*`）;需预装对应 CLI（`glab` / `gh`）。\n4. **标记完成**（幂等成功优先于「建单 CLI 非零即失败」）:\n   - **幂等 success**（调用 `node_complete`(status=success)）:\n     - **open 复用**:同源→目标已有 open PR/MR → 复用该 Web URL 写入 `outputs.mr_url`,summary 说明复用已有 open 单。\n     - **已合并无新提交**:PR/MR 已合并且当前源相对目标无新提交可建单（含 create 报 No commits between / already exists 后查得 merged）→ success;优先将已合并单 Web URL 写入 `outputs.mr_url`;查不到 URL 时允许空 `mr_url`,summary 须说明已合入/无新提交而跳过新建。\n     - **无历史单已同步**:源相对目标已无差异,且 open/merged 均无同源→目标单 → 允许 success 且 `mr_url` 可空,summary 须说明无差异且无历史单可复用。\n   - **失败**（调用 `node_complete`(status=failed)）:\n     - **closed 未合并**:仅有 closed（未合并）单且当前无新提交可再建单 → failed;不得仅因存在 closed URL 而 success。\n     - **真失败**:无法 push、鉴权/权限失败、冲突未解决、缺少 `glab`/`gh`、托管商不支持自动建单、其它非幂等建单错误 → failed。\n     - 真失败时:`summary`/`error` 必须显式包含「冲突已解决」「源分支已推送」(步骤 1–2 已完成时),并说明建单/CLI/托管商/权限等原因。**不采用**「推送成功即可 success、mr_url 可空」(上列幂等 success 路径除外)。\n   - **`outputs.mr_url` 与 summary**:有 open 必填该 URL;已合并优先填合并单 URL;查不到或无历史单可空;summary 区分复用已有 open / 已合入跳过 / 无差异无历史单跳过 / 失败原因。\n平台不再代验推送/MR/冲突——以你的 node_complete 为准。\n"
	DefaultStructuredRetry              = "【必须完成】本节点尚未写入结构化产物 `{name}`,这是本节点尚未写入的强制交付,缺它即判失败。现在立即调用 `{tool}` 工具写入它(内容为本节点应产出的结论),不要再提问、不要输出其它内容——只需完成这次调用。"
	DefaultClarifiedOpenQuestionsRetry  = "【必须澄清】你写入的需求里仍有以下待确认问题没有和用户敲定:\n{items}\n澄清节点是门禁,不能带着未确认的问题结束。请现在用 `ask_question` 工具把这些问题逐一抛给用户做选择(每个问题给出候选选项),等用户确认后再重新调用 `set_clarified_requirement` 更新结论并清空 open_questions。不要直接结束澄清,也不要替用户擅自拍板。"
	DefaultPreflightContract            = "\n\n## 环境确认契约(强制)\n你是环境确认(preflight)节点:对照计划/仓库/已有 vars 推断运行所需环境项(地址、账号、密码、密钥、端口等)。**唯一结构化交付**是调用 `set_preflight` 写入 `preflight.json`。\n\n**必填**:`summary`(非空)、`confirmed=true`;`fields[]` 每项 `name`+`value` 明文(密码也是明文,无 secret/password 类型);`unresolved` 必须为空或不传。`fields` 可为空数组(无缺口直通)。\n\n**可选字段**(有则写):`label`、`verified`、`verification`(sandbox_probe|user_attested|mixed)、`source`(form|choice|chat)、`notes`。\n\n**流程**:\n1. 无缺口:`set_preflight(confirmed=true, fields=[])` → `node_complete`。不要为问而问。\n2. 有缺口:有限选项用 `ask_question`;需用户键入用 `ask_form`(type 仅 text|url;密码用 text 明文)。调用后立即结束本轮等待用户。\n3. 尽量在沙箱核验(探测连通/登录等);不能核验时用 user_attested 并写 notes。\n4. 确认后 `set_preflight` → `node_complete`。**表单提交不能代替 set_preflight**;禁止用 `write_artifact` 写 `preflight.json`。\n5. 本节点隐藏「确认并流转」:由你调用 `node_complete` 结束。完成后平台把 fields 明文写入运行 vars,不改 SandboxEnv。\n"
	DefaultPreflightRetry               = "【必须完成】环境确认尚未就绪:{reason}。请继续用 `ask_question`/`ask_form` 采集缺口,在沙箱核验后调用 `set_preflight`(confirmed=true, unresolved 为空),再 `node_complete`。不要用 write_artifact 伪造 preflight.json;表单提交不能代替 set_preflight。\n"
	DefaultVisualContract               = "\n\n## 视觉网页契约(强制)\n你是视觉网页节点,唯一交付是一个**单文件、自包含**的网页 `page.html`。当需求涉及既有前端修改时,必须先只读检查现有业务 UI,再在真实页面骨架中呈现**改后目标态**,供人在「人工门禁」里确认后再开工。不得从零编造与业务无关的通用 demo。\n\n### 怎么交付(只有一种方式)\n**只调用 `write_artifact` 工具写入产物**:name 传 `page.html`,content 传完整 HTML,kind 传 `html`。\n**严禁在项目/工作区里写任何文件**——不要 `echo >`、不要新建/修改/格式化仓库文件、不要 `git add`/暂存/提交,以免污染仓库改动。运行时注入的仓库平级布局(如 `/root/workspace/<name>/`)仅用于**只读定位**源码,不构成任何写入授权。平台会把该产物登记为本次运行产物并用 iframe 预览。最终必须存在名为 `page.html` 的产物,否则判定为失败。\n\n### 有既有前端基线时(强制)\n需求指向仓库中已有页面或组件时,生成前必须先只读定位目标路由、页面组件、共享组件、全局样式与设计令牌、布局、业务文案与关键状态,再开始生成。然后在真实页面骨架中应用本次修改,默认只展示改后目标态。须复用现有信息架构、导航、组件外观、颜色、字号、间距、圆角、阴影、信息密度与业务文案;需求涉及的表单、筛选、弹层、切换等关键交互应可在 sandbox 中演示。桌面与移动端要求存在时须具备相应响应式表现。不要套用通用仪表盘或无关设计,不要强制附加与业务页面无关的演示外壳,也不要默认做固定的「修改前/修改后」分屏;仅当需求本身要求比较时才加入切换或并排视图。\n\n### 无既有前端基线时(降级)\n无法定位目标页面或缺少足够视觉依据时:优先沿用仓库中的全局设计系统与可用令牌,克制且一致地补全无法验证的部分;不得臆造不可验证的业务数据或页面结构,也不得声称与现网完全一致。仍须产出完整、可直接审批的页面,而不是说明文档或线框占位。\n\n### 硬性要求\n1. 一个完整的 HTML 文档(以 `<!doctype html>` 开头,含 `<html><head><body>`)。\n2. **所有** CSS 写进 `<style>`、所有 JS 写进 `<script>`,**全部内联**在这一个文件内。\n3. **不引用任何外部资源**(不要外链 CSS/JS/字体/图片 CDN);如需图形用内联 SVG 或 CSS 绘制。\n4. 只产出这一个 `page.html`;不得把密钥、令牌或可用凭据写入页面。\n\n### page.html 运行环境(强制)\npage.html 运行于 Gates HtmlPreview 的 sandbox iframe(sandbox=\"allow-scripts allow-forms\",无 allow-same-origin,文档为 opaque origin)。\n禁止:读取/写入 localStorage、sessionStorage;禁止:依赖 cookie 或同源 Web Storage 的持久化/登录态。\n需要完整 SPA、持久化或真实浏览器能力时,改走 app_preview(noVNC),不要在 srcdoc 中硬做,也不得引导恢复 allow-same-origin。\n"
	DefaultPreviewContract              = "\n\n## 应用预览契约(强制)\n你是应用预览节点。本节点唯一交付是成功调用 `set_preview(port?, url?, label?)`:**port 与 url 必须恰好提供其一**(可多次登记多项);**不要调用 set_test_result**,本节点无结构化 JSON 产物。沙箱内没有 Docker,**不要用 `docker`/`docker compose`**。\n\n**两条合法路径,选一条;不要混用,也不要为了走 port 而在沙箱里反代外部站点。**\n\n### A. 外部 URL(已部署环境)\n若应用已在远程环境运行(dsh-station / staging / 其它已部署地址),直接:\n`set_preview(url=\"http(s)://host[:port]/path\", label=\"…\")`\n- url 必须是绝对 http/https 地址(可含端口与路径)\n- 平台**不做服务端探测**;审批页 iframe 直连该 URL,取点可能降级\n- **禁止**在沙箱内再起反向代理、本地 `dsh web` 或其它本地服务来「满足 port」\n\n### B. 沙箱内原生端口\n若要在本沙箱启动应用:用 `setsid`/`nohup` **真后台**原生启动(如 `npm run dev`、`go run`、`python -m ...`),禁止前台占住 Agent 会话。必须监听 `0.0.0.0:<port>`(不要只绑 `127.0.0.1`;服务在根路径 `/`),再 `set_preview(port, label?)`。示例:\n```\nsetsid npm run dev -- --host 0.0.0.0 --port 8080 > /tmp/app-8080.log 2>&1 < /dev/null &\necho $! > /tmp/app-8080.pid\n```\nVite 加 `--host 0.0.0.0`;Node/Express `app.listen(port, '0.0.0.0')`;Python `--host 0.0.0.0`。调用 `set_preview(port)` 时平台会校验端口可达并对监听进程做 setsid 脱钩保活;不可达则工具失败,可修复后重试。应用**照常服务在根路径 `/` 即可**——平台代理会透明地把资源和链接改写到预览子路径下。\n\n**预览是门禁**:`set_preview` 成功后(url 路径登记即成功;port 路径须探测可达)平台立即结束生产相并进入 parked 复审 ReAct——**不要死等会话自然结束,也不要依赖 node_complete 才进门禁**。结束/Cancel Agent 会话不会拆掉预览服务。未成功注册时平台按 max_rounds(默认 3)同会话催促,超限仍无则节点失败。\n"
	DefaultPreviewDirectContract        = "\n\n## 节点配置:direct_preview(IP 直连)\n本节点已开启 IP 直连预览,覆盖上文「平台子路径反代 / noVNC 取点」约定:\n1. 环境变量 `PREVIEW_PORT` 是平台预映射的端口(Docker 1:1 / K8s Service 同号)。必须监听 `0.0.0.0:$PREVIEW_PORT`(Vite `--port $PREVIEW_PORT --host 0.0.0.0`),再 `set_preview(port=数字($PREVIEW_PORT))`。\n2. 应用服务在根路径 `/`。审批人浏览器将直连该地址,不要改 base href,不要依赖平台 `/preview/...` 改写。\n3. 平台在沙箱入站口自动向 HTML 注入 `<script src=\"$PREVIEW_PICK_SCRIPT_URL\"></script>`，不要改业务 HTML / origin / base href。仅当预览页仍提示未加载取点脚本时，再在 HTML 入口补上该 script（旧沙箱镜像兜底）。\n"
	DefaultPreviewDirectManualContract  = "\n\n## 节点配置:direct_preview(IP 直连)\n本节点已开启 IP 直连预览,覆盖上文「平台子路径反代 / noVNC 取点」约定:\n1. 环境变量 `PREVIEW_PORT` 是平台预映射的端口(Docker 1:1 / K8s Service 同号)。必须监听 `0.0.0.0:$PREVIEW_PORT`(Vite `--port $PREVIEW_PORT --host 0.0.0.0`),再 `set_preview(port=数字($PREVIEW_PORT))`。\n2. 应用服务在根路径 `/`。审批人浏览器将直连该地址,不要改 base href,不要依赖平台 `/preview/...` 改写。\n3. 本节点已关闭自动注入。每个 HTML 入口(Vite 即 `index.html`)必须包含 `<script src=\"$PREVIEW_PICK_SCRIPT_URL\"></script>`，以便审批页取点标注并显示地址栏。不要改应用 origin / base href。\n"
	DefaultPreviewRetry                 = "【必须完成】你尚未成功调用 `set_preview`。请立即用 `set_preview(port?, url?, label?)` 登记预览(**port 与 url 恰好其一**):\n- 已有远程/已部署地址:直接 `set_preview(url=\"http(s)://…\")`,不要在沙箱起反代或本地服务,也不要改用 port。\n- 若在本沙箱启动应用:用 `setsid`/`nohup` **真后台**原生启动(不要用 docker、不要前台占会话),监听 `0.0.0.0:<port>`(不能只绑 127.0.0.1;服务在根路径 `/`),再 `set_preview(port)`。端口不可达时工具会失败,修复后可重试。"
	DefaultApproveContract              = "\n\n## Approve 契约(强制)\n你是 Approve 节点:用多轮 ReAct 对话完成开发前工作。**两份强制交付**(都要写入,不是「唯一交付」;完成标记见下方结束时序):\n1. `set_clarified_requirement` 写入完整需求规格(`open_questions` 必须为空);\n2. `set_plan` 写入最多两级(大目标→小目标)的结构化计划。\n\n若 Agent profile 角色包声明「唯一交付」或禁止 `set_plan`/`set_clarified_requirement`,以本平台契约为准:本节点允许并要求同时写澄清与计划。\n\n用户先说明目标后再行动。建议顺序:用工具对齐需求(可穿插提问与可选调研/视觉/方案),再 `set_plan`,然后等待用户确认并流转。不要在用户发言前编造空泛选择题。不要写实现代码、不要改仓库。\n\n### 澄清(强制)\n**必填字段**:`title`、`summary`、`background`、`goals[]`(≥1)、`in_scope[]`(≥1)、`out_of_scope[]`(≥1)、`functional_requirements[]`(≥1;每条含 `title`+`detail`+≥1 `acceptance_criteria`;`priority` 取 must|should|could,缺省 must)、`assumptions[]`/`dependencies[]`/`constraints[]`(各≥1;无实质内容时写明确「无额外…(已与用户确认)」,禁止省略键)。\n**可选字段**(有则写):`success_metrics`、`personas`、`user_scenarios`、`non_functional_requirements`、`external_interfaces`、`data_entities`、`business_rules`、`edge_cases`、`limitations`、`risks`、`glossary`。\n**禁止**:排期/里程碑/交付日期。需求规格只能用 `set_clarified_requirement`。\n**澄清是门禁**:任何还不确定、需要用户拍板的点,都必须通过 `ask_question` 让用户做选择,不能把疑问塞进 `open_questions` 就结束。选项可标 `recommended`(单选每题最多 1 个;多选应标记 1 个或多个)。调用 `set_clarified_requirement` 时 `open_questions` 必须为空。\n\n### 计划(强制)\n调用 `set_plan`:`goals[]` 大目标,每个可含 `subgoals[]` 小目标(叶子,不可再嵌套);每项 `title`(可选 `detail`);状态由平台初始化为 pending。\n\n**设计区完整性**:若写入设计区,须带齐六节 `architecture`/`data_design`/`interfaces`/`components`/`interaction`/`test_design`;无内容显式「不涉及」。图按需、非强制:architecture/data_design/interaction 可挂 `diagrams[]`(及兼容单数 `diagram`,source 必填);interfaces/components 项亦可选同结构。一等 kind:activity/flowchart/sequence/er——涉及则尽量都提供便于审批,缺可选图种不失败;禁止「必须四种图」。前端同节多图用节内小 Tab(不是左目录)。纯 goals 亦可,不必强行加六节键。进度与 plan_coverage 只计 goals 叶子,设计节不计。\n\n**实质 data_design 硬门禁**:当 `data_design.summary`(去空白)不是「不涉及」/「N/A」时,必须提供至少一张 ER(`diagrams[]` 中 kind=er 或兼容单数 diagram)、至少 1 个 `entities[]`,且每个实体至少 1 个结构化 `fields[]`(每项 `name`+`type` 必填;可选 `pk`/`nullable`/`fk`/`description`);仅 legacy `attributes` 不足以通过。流程:调用 set_plan → 解析与硬门禁 → 入库 → PlanView 展示。\n\n### 可选工具(有助于拍板,不是完成条件)\n- `set_research` 写入调研结论;\n- `set_proposals`:**仅当存在至少两个方向不同、取舍有意义的候选且需要用户择一时才调用**;写入 ≥2 个候选。方向已唯一、用户已拍板、或澄清/计划足以推进时**禁止调用**(尤其禁止写入仅 1 条且标推荐/已选定的「伪选择」)。独立 proposal 节点仍须强制交付;本约束仅针对 Approve 可选路径。与 `ask_question`「禁止为问而问」同理。\n- `write_artifact` 写入 `page.html`(kind=`html`,单文件自包含,禁止外链与 Web Storage),立刻 `set_artifact_preview` 钉到预览 Tab。\n- 可运行应用(沙箱端口或已部署 URL):`set_preview(port?, url?, label?)`(port 与 url 恰好其一)。**不是完成条件**,成功后**不会**结束本节点(与 app_preview 不同)。沙箱内须 `setsid`/`nohup` 真后台、监听 `0.0.0.0:<port>`(不要 docker、不要只绑 127.0.0.1),再 `set_preview(port)`;已部署地址用 `set_preview(url=...)`(iframe 直连,无服务端探测)。\n给人看的预览材料也可用 `write_artifact` + `set_artifact_preview`;选项级并排对比用 `ask_question.demoHtml`(sandbox iframe,无 allow-same-origin)。\n\n### 结束时序(强制)\n- **确认前**(用户未点「确认并流转」):可写澄清与计划,但**禁止**调用 `node_complete`;即使误调,引擎也会清除标记并继续等待确认。\n- **确认后**(用户已点「确认并流转」):基于已知信息立刻补齐两份强制产物(`open_questions` 为空),再调用 `node_complete`;缺产物或未标记则无法完成流转。\n下文「完成标记契约」仅在确认后阶段生效;确认前不要用 `node_complete` 自行结束。\n"
	DefaultOutcomeContract              = "\n\n## 完成标记契约(强制)\n结束本节点前**必须**调用 `node_complete` 标记结果:`status` 取 `success` 或 `failed`;可选 `summary` / `error` / `outputs` / `checks`。写完产物(`set_*` / `write_artifact`)后再调用。未标记将被判定为节点失败。平台先做默认校验(产物/门禁等),通过后才可能做业务 RPC 校验。若需启动长期服务(web / dsh / Harness / 被测应用等),必须用 `setsid`/`nohup` 放入独立会话并重定向日志,禁止前台或未脱钩的命令占住 Agent 回合;不要为收尾杀掉这些进程。\n"
	DefaultOutcomeRetry                 = "【必须完成】你尚未调用 `node_complete` 标记本节点完成结果,这是强制要求。现在立即调用 `node_complete(status=\"success\"|\"failed\", summary?, error?, outputs?)`,不要再提问或输出其它内容——只需完成这次调用。\n"
	// DefaultReviewConfirmReconcile is the review-side counterpart of
	// DefaultApproveConfirmSuffix: a review producer's node_complete already
	// happened in its production phase, so the confirm turn only reconciles
	// products against the transcript.
	DefaultReviewConfirmReconcile = "【确认流转】用户已点击「确认并流转」,复审到此结束。请通读本节点的完整聊天记录,据此补充或修正你已写入的结构化产物:用对应的 `set_*` / `write_artifact` 工具重新写入完整内容,把历次人工反馈已确认的结论落进产物,清掉与对话相矛盾的旧内容。\n- 不要提问、不要调用 ask_question。\n- 不要调用 `node_complete`(本节点的完成由平台在流转时处理)。\n- 若核对后确认无需修改,回一句说明即可,不要空写产物。"
	// DefaultConfirmSummaryContract is the hidden turn that follows the
	// reconcile turn. Its output never reaches the transcript, so it asks for
	// the JSON payload alone.
	DefaultConfirmSummaryContract = "【流转摘要】产物已核对完毕,现在只做最后一件事:通读本节点的完整聊天记录(每一轮人工反馈以及你的处理),归纳出一段面向反馈账本的「Agent 总结」。\n\n**只输出一个 fenced JSON 代码块**,格式严格为:\n" + "```json\n" + `{"agentSummary":"对整段对话中人工反馈意图与要点的归纳"}` + "\n```" + "\n规则:\n- 不要输出 JSON 之外的任何叙述、解释、前缀或后缀。\n- agentSummary 归纳用户在本节点提出的意图、要点及其落点,不要复述你的叙述回复,也不要照抄某一轮反馈原文。\n- 不要调用任何工具,不要提问,不要调用 `node_complete`。\n- 确实无法归纳时输出 `{\"agentSummary\":\"\"}`;禁止模板占位或空泛套话。"
	DefaultReviewCommitWrapUp     = "【流转收尾】用户已确认本节点,即将进入下一步。工作区仍有未提交改动:\n{files}\n\n以上列表可能含已相对基线提交的文件,请以各仓 `git status` 为准,只处理未暂存/未提交的内容。\n\n请你自行决定要不要提交:\n- 有意义的源码/配置改动:按仓 `cd` 进 `/root/workspace/<name>/`,用 `git add` **点名文件**(禁止 `git add -A` / `git add .`),再 `git commit`(写清 why)并 `git push` 当前工作分支。下游节点在全新克隆里工作,不推送就拿不到这些改动。\n- 临时文件、日志、缓存、构建产物、本地密钥、调试垃圾:**不要提交**,保持未跟踪即可。\n- 若全部都是临时文件:什么都不要做,不要空提交。\n- 禁止在 main/master/develop/release-* 上提交或推送。\n- 不要提问、不要改产物、不要调用 node_complete。做完后用一两句话说明提交了什么、跳过了什么即可。\n"

	// DefaultFeedbackHeader is injected only when this node actually has human
	// feedback in scope. Storing feedback that no agent ever reads is the same
	// as not storing it, and a node's first execution has nothing to read, so
	// the clause must not appear there as noise.
	DefaultFeedbackHeader = "\n\n## 历史人工反馈(强制先读)\n本节点此前已收到 {n} 轮人工反馈。**开工前必须先调用 `list_run_history` 通读**,再用 `read_artifact` 逐个读取下列反馈产物的完整内容(含原文、标注与附件)。历次已确认的意见务必遵守,不得在新一轮里回退:\n"
)

// FeedbackHeaderFor renders the history-feedback clause for n rounds.
func FeedbackHeaderFor(n int) string {
	return strings.ReplaceAll(DefaultFeedbackHeader, "{n}", strconv.Itoa(n))
}

// UpstreamHeader returns the configured upstream-artifacts header or the
// built-in default. Nil-safe.
func (p *AgentPrompts) UpstreamHeader() string {
	if p != nil && strings.TrimSpace(p.UpstreamArtifactsHeader) != "" {
		return p.UpstreamArtifactsHeader
	}
	return DefaultUpstreamArtifactsHeader
}

// ProducesContractFor returns the produces-contract clause with {name}
// substituted. Nil-safe.
func (p *AgentPrompts) ProducesContractFor(name string) string {
	tmpl := DefaultProducesContract
	if p != nil && strings.TrimSpace(p.ProducesContract) != "" {
		tmpl = p.ProducesContract
	}
	return strings.ReplaceAll(tmpl, "{name}", name)
}

// ReactOpenSuffixText returns the react opening-turn suffix or the default.
// Nil-safe.
func (p *AgentPrompts) ReactOpenSuffixText() string {
	if p != nil && strings.TrimSpace(p.ReactOpenSuffix) != "" {
		return p.ReactOpenSuffix
	}
	return DefaultReactOpenSuffix
}

// ProducesRetryFor returns the produces re-prompt with {name} substituted.
// Nil-safe.
func (p *AgentPrompts) ProducesRetryFor(name string) string {
	tmpl := DefaultProducesRetry
	if p != nil && strings.TrimSpace(p.ProducesRetry) != "" {
		tmpl = p.ProducesRetry
	}
	return strings.ReplaceAll(tmpl, "{name}", name)
}

// PlanContractText returns the plan-node contract clause or the default.
// Nil-safe.
func (p *AgentPrompts) PlanContractText() string {
	if p != nil && strings.TrimSpace(p.PlanContract) != "" {
		return p.PlanContract
	}
	return DefaultPlanContract
}

// ImplementContractText returns the implement-node contract clause or the
// default. Nil-safe.
func (p *AgentPrompts) ImplementContractText() string {
	if p != nil && strings.TrimSpace(p.ImplementContract) != "" {
		return p.ImplementContract
	}
	return DefaultImplementContract
}

// PlanIncompleteRetryFor returns the implement-node completion re-prompt with
// the incomplete-item list substituted for {items}. Nil-safe.
func (p *AgentPrompts) PlanIncompleteRetryFor(items []string) string {
	tmpl := DefaultPlanIncompleteRetry
	if p != nil && strings.TrimSpace(p.PlanIncompleteRetry) != "" {
		tmpl = p.PlanIncompleteRetry
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString("- ")
		b.WriteString(it)
		b.WriteString("\n")
	}
	return strings.ReplaceAll(tmpl, "{items}", strings.TrimRight(b.String(), "\n"))
}

// contractText is the shared nil-safe accessor for a fixed contract clause.
func contractText(override, def string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return def
}

// ClarifiedRequirementContractText returns the react-node requirement contract.
func (p *AgentPrompts) ClarifiedRequirementContractText() string {
	if p == nil {
		return DefaultClarifiedRequirementContract
	}
	return contractText(p.ClarifiedRequirementContract, DefaultClarifiedRequirementContract)
}

// ClarifiedOpenQuestionsRetryFor returns the react-node gate re-prompt with the
// unresolved-question list substituted for {items}. Nil-safe.
func (p *AgentPrompts) ClarifiedOpenQuestionsRetryFor(items []string) string {
	tmpl := DefaultClarifiedOpenQuestionsRetry
	if p != nil && strings.TrimSpace(p.ClarifiedOpenQuestionsRetry) != "" {
		tmpl = p.ClarifiedOpenQuestionsRetry
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString("- ")
		b.WriteString(it)
		b.WriteString("\n")
	}
	return strings.ReplaceAll(tmpl, "{items}", strings.TrimRight(b.String(), "\n"))
}

// PreflightContractText returns the preflight-node contract. Nil-safe.
func (p *AgentPrompts) PreflightContractText() string {
	if p == nil {
		return DefaultPreflightContract
	}
	return contractText(p.PreflightContract, DefaultPreflightContract)
}

// PreflightRetryText returns the preflight gate re-prompt with {reason}.
func (p *AgentPrompts) PreflightRetryText(reason string) string {
	tmpl := DefaultPreflightRetry
	if p != nil && strings.TrimSpace(p.PreflightRetry) != "" {
		tmpl = p.PreflightRetry
	}
	if strings.TrimSpace(reason) == "" {
		reason = "preflight.json 未就绪"
	}
	return strings.ReplaceAll(tmpl, "{reason}", reason)
}

// ImplementResultContractText returns the implement-node result contract.
func (p *AgentPrompts) ImplementResultContractText() string {
	if p == nil {
		return DefaultImplementResultContract
	}
	return contractText(p.ImplementResultContract, DefaultImplementResultContract)
}

// ResearchContractText returns the research-node contract.
func (p *AgentPrompts) ResearchContractText() string {
	if p == nil {
		return DefaultResearchContract
	}
	return contractText(p.ResearchContract, DefaultResearchContract)
}

// TestContractText returns the test-node contract.
func (p *AgentPrompts) TestContractText() string {
	if p == nil {
		return DefaultTestContract
	}
	return contractText(p.TestContract, DefaultTestContract)
}

// ReviewContractText returns the review-node contract.
func (p *AgentPrompts) ReviewContractText() string {
	if p == nil {
		return DefaultReviewContract
	}
	return contractText(p.ReviewContract, DefaultReviewContract)
}

// ProposalContractText returns the proposal-node contract.
func (p *AgentPrompts) ProposalContractText() string {
	if p == nil {
		return DefaultProposalContract
	}
	return contractText(p.ProposalContract, DefaultProposalContract)
}

// MRContractFor returns the submit_mr-node contract with the {source} and
// {target} branch placeholders substituted. Nil-safe.
func (p *AgentPrompts) MRContractFor(source, target string) string {
	tmpl := DefaultMRContract
	if p != nil && strings.TrimSpace(p.MRContract) != "" {
		tmpl = p.MRContract
	}
	tmpl = strings.ReplaceAll(tmpl, "{source}", source)
	return strings.ReplaceAll(tmpl, "{target}", target)
}

// VisualContractText returns the visual-node contract clause or the default.
// Nil-safe.
func (p *AgentPrompts) VisualContractText() string {
	if p != nil && strings.TrimSpace(p.VisualContract) != "" {
		return p.VisualContract
	}
	return DefaultVisualContract
}

// ApproveContractText returns the approve-node contract (two required
// deliveries: clarified requirement + plan). Nil-safe.
func (p *AgentPrompts) ApproveContractText() string {
	if p != nil && strings.TrimSpace(p.ApproveContract) != "" {
		return p.ApproveContract
	}
	return DefaultApproveContract
}

// PreviewContractText returns the app_preview-node contract clause or the default.
func (p *AgentPrompts) PreviewContractText() string {
	if p != nil && strings.TrimSpace(p.PreviewContract) != "" {
		return p.PreviewContract
	}
	return DefaultPreviewContract
}

// PreviewRetryText returns the app_preview re-prompt when set_preview was not called.
func (p *AgentPrompts) PreviewRetryText() string {
	if p != nil && strings.TrimSpace(p.PreviewRetry) != "" {
		return p.PreviewRetry
	}
	return DefaultPreviewRetry
}

// StructuredRetryFor returns the generic structured-product re-prompt with the
// artifact {name} and its {tool} substituted. Nil-safe.
func (p *AgentPrompts) StructuredRetryFor(name, tool string) string {
	tmpl := DefaultStructuredRetry
	if p != nil && strings.TrimSpace(p.StructuredRetry) != "" {
		tmpl = p.StructuredRetry
	}
	tmpl = strings.ReplaceAll(tmpl, "{name}", name)
	return strings.ReplaceAll(tmpl, "{tool}", tool)
}

// OutcomeContractText returns the node_complete contract clause. Nil-safe.
func (p *AgentPrompts) OutcomeContractText() string {
	if p != nil && strings.TrimSpace(p.OutcomeContract) != "" {
		return p.OutcomeContract
	}
	return DefaultOutcomeContract
}

// OutcomeRetryText returns the re-prompt when node_complete was not called.
func (p *AgentPrompts) OutcomeRetryText() string {
	if p != nil && strings.TrimSpace(p.OutcomeRetry) != "" {
		return p.OutcomeRetry
	}
	return DefaultOutcomeRetry
}

// ReviewCommitWrapUpFor returns the confirm-time git wrap-up prompt with the
// dirty-file list substituted for {files}. Nil-safe.
func (p *AgentPrompts) ReviewCommitWrapUpFor(files string) string {
	tmpl := DefaultReviewCommitWrapUp
	if p != nil && strings.TrimSpace(p.ReviewCommitWrapUp) != "" {
		tmpl = p.ReviewCommitWrapUp
	}
	if strings.TrimSpace(files) == "" {
		files = "(工作区 git status 为 dirty,未能列出文件)"
	}
	return strings.ReplaceAll(tmpl, "{files}", files)
}

// ReviewConfirmReconcileText returns the review-side confirm-time reconcile
// prompt or the default. Nil-safe.
func (p *AgentPrompts) ReviewConfirmReconcileText() string {
	if p != nil && strings.TrimSpace(p.ReviewConfirmReconcile) != "" {
		return p.ReviewConfirmReconcile
	}
	return DefaultReviewConfirmReconcile
}

// ConfirmSummaryContractText returns the hidden confirm-time summary contract
// or the default. Nil-safe.
func (p *AgentPrompts) ConfirmSummaryContractText() string {
	if p != nil && strings.TrimSpace(p.ConfirmSummaryContract) != "" {
		return p.ConfirmSummaryContract
	}
	return DefaultConfirmSummaryContract
}
