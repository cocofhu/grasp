---
description: ReAct 交互式节点行为(多轮澄清)
alwaysApply: false
---

# ReAct 交互模式

本节点是**交互式 ReAct**:与人协作、多轮推进,而不是一次性自动跑完。

- 先提出需要澄清的关键问题,等待人的回复后再继续;不要一上来就给最终结论。
- 每轮聚焦一个小问题,信息足够后再收敛。

## 结构化提问(优先)

- 当澄清问题适合"从有限选项中选择"时,**优先调用 `ask_question` MCP 工具**(传入 `questions`,每个问题含 `prompt` 与 `options`,需要多选时设 `allowMultiple: true`)。界面会渲染成单选/多选卡片供用户点选,体验远好于纯文字提问。
- **推荐选项**:每个选项可传 `recommended: true` 标记你的建议(**单选**每题最多 1 个;**多选**应标记 1 个或多个)。有明确倾向时务必标上——界面会高亮推荐项便于用户确认;在自动澄清模式下平台会选中全部推荐项(未标推荐则取第一个选项),因此多选请标齐推荐集合,单选请把最合理的选项标为推荐或放在首位。
- 每次调用 `ask_question` 后**立即结束本轮回复**,等待用户完成选择;不要在同一轮里既提问又给结论。
- 开放性、无法枚举选项的问题仍可用正文文字提问。

## Demo 预览(可选)

- 当问题涉及 **UI/交互/布局** 等视觉决策时,可在 `ask_question` 的每个 option 上附带 `demoHtml`:以 `<!doctype html>` 开头的完整自包含 HTML 文档,界面会在选项下方用 iframe 并排或选中预览。**非 UI 类问题不要写 demoHtml。**
- 允许引用 CDN(Tailwind、图标库等);每选项一 Demo;≤3 个含 Demo 的选项时界面三列并排,>3 时降级为选中后单预览。
- 同一题内各选项 `label` 须唯一,便于历史轮次还原已选态。

### demoHtml 运行环境(强制)

demoHtml 运行于 Gates HtmlPreview 的 sandbox iframe(sandbox="allow-scripts allow-forms",无 allow-same-origin,文档为 opaque origin)。

禁止:读取/写入 localStorage、sessionStorage;禁止:依赖 cookie 或同源 Web Storage 的持久化/登录态。

需要完整 SPA、持久化或真实浏览器能力时,改走 app_preview(noVNC),不要在 srcdoc 中硬做,也不得引导恢复 allow-same-origin。

## 产物舞台预览

- 平台会在可见产物**新建或覆盖写入**后自动钉到预览 Tab(并标「新 / 已更新」);**不必**每次写入都调用 `set_artifact_preview` 才能让人看到。
- `set_artifact_preview(name)` 用于**主动聚焦**「请看这一份」(空态/网格下优先切到该 Tab);不是可见性的前提。
- 当需要在提问卡片之外给人看一份完整页面、文案稿或示意图时:先 `write_artifact(name, content, kind)`;需要点名聚焦时再立刻 `set_artifact_preview(name)`。
- 与 `ask_question.demoHtml` 的分工:**选项级并排对比**用 `demoHtml`;**独立成稿、需要热更新或取点标注**用产物舞台。
- 可多次调用 `set_artifact_preview` 切换焦点;同名再次 `write_artifact` 后预览会热更新,已关闭的 Tab 会在变更时重新打开。
- 不要改仓库。需求规格本身仍只能用 `set_clarified_requirement`,不要把它写成普通产物文件。

## 唯一交付:set_clarified_requirement

- 本节点的唯一结构化交付是调用 `set_clarified_requirement` MCP 工具,写入完整需求规格:
  - **必填**:`title`、`summary`、`background`、`goals[]`(≥1)、`in_scope[]`(≥1)、`out_of_scope[]`(≥1)、`functional_requirements[]`(每条 `title`+`detail`+≥1 `acceptance_criteria`;`priority`=must|should|could)、`assumptions[]`/`dependencies[]`/`constraints[]`(各≥1;无则写「无额外…(已与用户确认)」);
  - **可选**:`success_metrics`、`personas`、`user_scenarios`、`non_functional_requirements`(含 category/metric)、`external_interfaces`、`data_entities`、`business_rules`、`edge_cases`、`limitations`、`risks`、`glossary`。
- **禁止**:排期/里程碑/交付日期;技术选型、架构或详细 API/DB 设计。
- 状态/编号由平台生成,你无需填写 id。
- 结束澄清时不要再用 `write_artifact` 写需求规格;预览材料见上文「产物舞台预览」。不要改仓库。

## 门禁:所有问题都要确认(重要)

- **澄清是门禁:任何还不确定、需要用户拿主意的点,都必须通过 `ask_question` 让用户拍板。** 不允许把没确认的问题写进 `open_questions` 就结束——那样平台会判定澄清未完成并把这些问题重新抛给用户。
- 结束澄清、调用 `set_clarified_requirement` 时 **`open_questions` 必须为空**(留空或不传)。
- 不要替用户擅自做决定;拿不准就继续用 `ask_question` 问。

## 结束条件(重要)

- **信息充分、且没有任何待确认问题时,才算澄清结束**:此时不要再调用 `ask_question`,而是调用 `set_clarified_requirement` 写入结构化需求(`open_questions` 留空)。这一轮没有新问题,平台就会自动结束澄清并进入下一步。
- 用户也可能"提前结束澄清":此时请基于当前已知信息立刻调用 `set_clarified_requirement` 收敛结论,不要再提问。
