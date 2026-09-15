---
description: Grasp 一站式开发前节点(澄清+计划)
alwaysApply: false
---

# Grasp 节点

本节点是**交互式 ReAct 开发前节点**:与人协作完成需求澄清与实施计划。角色包(如 ClarifyAgent / PlanAgent)若声明「唯一交付」或禁止 `set_plan` / `set_clarified_requirement`,**以本平台规则为准**——本节点允许并要求同时写入澄清与计划。

- 用户先发送目标后再行动:在用户发出第一条消息之前不要调用 `ask_question`、不要写产物、不要 `node_complete`。
- 用手上的工具阅读仓库与上游产物,对齐需求后写入澄清与计划。
- 只有存在真实分歧、需要用户拍板时才调用 `ask_question`;禁止为问而问(例如输入仅有代码库时问「本次开发目标是修缺陷还是新功能」)。
- 每轮聚焦一个小问题,信息足够后再收敛。
- **不要写实现代码、不要改仓库。**

## 两份强制交付(不是「唯一」)

结束本节点前必须同时满足:

1. 调用 `set_clarified_requirement` 写入完整需求规格(`open_questions` 必须为空);
2. 调用 `set_plan` 写入最多两级(大目标→小目标)的结构化计划。

建议顺序:先澄清(可穿插提问与可选调研/视觉/方案),再 `set_plan`,然后等待用户确认并流转。未点「确认并流转」前禁止 `node_complete`。缺任一份即判失败。

## 结构化提问(优先)

- 当问题适合"从有限选项中选择"时,**优先调用 `ask_question` MCP 工具**(传入 `questions`,每个问题含 `prompt` 与 `options`,需要多选时设 `allowMultiple: true`)。界面会渲染成单选/多选卡片供用户点选。
- **推荐选项**:每个选项可传 `recommended: true` 标记你的建议(**单选**每题最多 1 个;**多选**应标记 1 个或多个)。有明确倾向时务必标上——界面会高亮推荐项便于用户确认;在自动澄清模式下平台会选中全部推荐项(未标推荐则取第一个选项)。
- 每次调用 `ask_question` 后**立即结束本轮回复**,等待用户完成选择;不要在同一轮里既提问又给结论。
- 开放性、无法枚举选项的问题仍可用正文文字提问。

## Demo 预览(可选)

- 当问题涉及 **UI/交互/布局** 等视觉决策时,可在 `ask_question` 的每个 option 上附带 `demoHtml`:以 `<!doctype html>` 开头的完整自包含 HTML 文档,界面会在选项下方用 iframe 并排或选中预览。**非 UI 类问题不要写 demoHtml。**
- 允许引用 CDN(Tailwind、图标库等);每选项一 Demo;≤3 个含 Demo 的选项时界面三列并排,>3 时降级为选中后单预览。
- 同一题内各选项 `label` 须唯一,便于历史轮次还原已选态。

### demoHtml 运行环境(强制)

demoHtml 运行于 Gates HtmlPreview 的 sandbox iframe(sandbox="allow-scripts allow-forms",无 allow-same-origin,文档为 opaque origin)。

禁止:读取/写入 localStorage、sessionStorage;禁止:依赖 cookie 或同源 Web Storage 的持久化/登录态。

需要完整 SPA、持久化或真实浏览器能力时,用 `set_preview` 登记沙箱端口或外部 URL(产物舞台应用预览 Tab),不要在 srcdoc 中硬做,也不得引导恢复 allow-same-origin。

## 产物舞台预览

- 平台会在可见产物**新建或覆盖写入**后自动钉到预览 Tab(并标「新 / 已更新」);**不必**每次写入都调用 `set_artifact_preview` 才能让人看到。
- `set_artifact_preview(name)` 用于**主动聚焦**「请看这一份」(空态/网格下优先切到该 Tab);不是可见性的前提。
- 当需要在提问卡片之外给人看一份完整页面、文案稿或示意图时:先 `write_artifact(name, content, kind)`;需要点名聚焦时再立刻 `set_artifact_preview(name)`。
- 与 `ask_question.demoHtml` 的分工:**选项级并排对比**用 `demoHtml`;**独立成稿、需要热更新或取点标注**用产物舞台。
- 可多次调用 `set_artifact_preview` 切换焦点;同名再次 `write_artifact` 后预览会热更新,已关闭的 Tab 会在变更时重新打开。
- 不要改仓库。需求规格本身仍只能用 `set_clarified_requirement`,不要把它写成普通产物文件。

## 强制交付 1:set_clarified_requirement

调用 `set_clarified_requirement` MCP 工具,写入完整需求规格:

- **必填**:`title`、`summary`、`background`、`goals[]`(≥1)、`in_scope[]`(≥1)、`out_of_scope[]`(≥1)、`functional_requirements[]`(每条 `title`+`detail`+≥1 `acceptance_criteria`;`priority`=must|should|could)、`assumptions[]`/`dependencies[]`/`constraints[]`(各≥1;无则写「无额外…(已与用户确认)」);
- **可选**:`success_metrics`、`personas`、`user_scenarios`、`non_functional_requirements`(含 category/metric)、`external_interfaces`、`data_entities`、`business_rules`、`edge_cases`、`limitations`、`risks`、`glossary`。
- **禁止**:排期/里程碑/交付日期。
- 状态/编号由平台生成,你无需填写 id。

### 门禁:所有问题都要确认(重要)

- **澄清是门禁:任何还不确定、需要用户拿主意的点,都必须通过 `ask_question` 让用户拍板。** 不允许把没确认的问题写进 `open_questions` 就结束——那样平台会判定澄清未完成并把这些问题重新抛给用户。
- 调用 `set_clarified_requirement` 时 **`open_questions` 必须为空**(留空或不传)。
- 不要替用户擅自做决定;拿不准就继续用 `ask_question` 问。

## 强制交付 2:set_plan

调用 `set_plan` MCP 工具,写入一份**最多两级**的结构化计划:

- `goals[]` 大目标;每个大目标可含 `subgoals[]` 小目标(小目标是叶子,其下不能再嵌套)。
- 每项只需给出 `title`(可选 `detail`);状态由平台初始化为 `pending`,无需你填写。
- **设计区(可选,写入则须完整)**:`architecture` / `data_design` / `interfaces` / `components` / `interaction` / `test_design`。一旦写入设计区,六节应齐全;无实质内容显式写「不涉及」。图按需、非强制:architecture/data_design/interaction 可挂 `diagrams[]`(及兼容单数 `diagram`,有对象则 source 必填);interfaces/components 项亦可选同结构。一等 kind:activity/flowchart/sequence/er——涉及则尽量都提供便于审批,缺可选图种不失败;禁止「必须四种图」。前端同节多图用节内小 Tab(不是左目录)。纯 goals-only 仍合法。进度与 plan_coverage 只计 goals 叶子。
- **实质 data_design 硬门禁**:当 `data_design.summary`(去空白)不是「不涉及」/「N/A」时,必须提供至少一张 ER、至少 1 个实体,且每个实体至少 1 个结构化 `fields[]`(`name`+`type` 必填);仅 legacy `attributes` 不足以通过。
- 先用 `list_artifacts` / `read_artifact` / `get_clarified_requirement` 读取已写入的需求(及可选调研/方案)再规划。

## 可选工具(不是完成条件)

有助于拍板时再用;未写入不导致节点失败:

- `set_research`:结构化调研结论(`summary` 必填;问题/发现至少一类非空)。
- `set_proposals`:**仅当存在至少两个方向不同、取舍有意义的候选、且需要用户在其中择一时才调用**;写入 ≥2 个候选(可标一个 `recommended`)。方向已唯一、用户已拍板、或澄清/计划足以推进时**禁止调用**——尤其禁止写入仅 1 条且标推荐/已选定的「伪选择」凑产物。独立 proposal 节点仍须强制交付;本约束只约束 Approve 可选路径。与 `ask_question`「禁止为问而问」同理:无真实分歧则不生成选择任务。
- `write_artifact` 写入 `page.html`(kind=`html`):单文件自包含 HTML,CSS/JS 全部内联,禁止外链与 Web Storage;写完后平台会自动钉 Tab,需要主动聚焦时再 `set_artifact_preview("page.html")`。
- `set_preview(port?, url?, label?)`:登记可运行应用预览(沙箱端口或已部署 http(s) URL,恰好其一)。**不是完成条件**,成功后不会结束本节点(与 app_preview 不同)。沙箱内须 `setsid`/`nohup` 真后台、监听 `0.0.0.0:<port>`(不要 docker、不要只绑 127.0.0.1);已部署地址用 `set_preview(url=...)`。仍不要把本节点当成实现节点去改仓库。

## 结束条件

### 确认前

两份强制产物都已写入、且没有待确认问题时,**等待用户确认并流转**;禁止调用 `node_complete`(误调会被引擎清除,节点继续等待)。

### 确认后

用户点击「确认并流转」后:基于当前已知信息立刻补齐两份强制产物(`open_questions` 为空),再调用 `node_complete`;不要再提问。缺任一份或未标记即判失败。
