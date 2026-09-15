---
description: 通过 artifact-store MCP 读写运行产物(强制产物契约)
alwaysApply: true
---

# 产物 (artifact-store MCP)

本次运行已为你注入名为 `artifact-store` 的 MCP(按本次运行隔离)。所有"产物"必须通过它读写,
而不是只在工作区留文件:

- `write_artifact(name, content, kind?)`:把内容写入平台产物存储,返回 artifact_id。
- `read_artifact(name)`:读取本次运行内上游节点产出的产物。
- `list_artifacts()`:列出本次运行已有产物。

若原生 MCP 不可用、需经 HTTP 调用时:优先用环境变量 `GRASP_ARTIFACT_URL` /
`GRASP_ARTIFACT_TOKEN`。该 URL 必须指向实际提供 `/mcp/runs/:id` 的 API 入口
(本地 Docker 常见为 `host.docker.internal`)。不要改写环境变量中的主机名。

## 强制产物契约 (produces)

- 当本节点的提示词声明了「产物契约 produces: <文件名>」时,你**必须**在结束当前轮次前,
  调用一次 `write_artifact("<文件名>", <完整内容>)` 把该产物写入。
- 未写入声明的 produces,会被编排器判定为节点失败,触发重试 / 回滚 / 人工门禁。
- 如需消费上游产物,优先用 `read_artifact` / `list_artifacts`,而不是猜测文件路径。

## 完成标记 (node_complete,强制)

所有 Agent 类节点在结束前**必须**调用一次 `node_complete`:

- `status`: `success` 或 `failed`(必填)
- `summary` / `error`: 可选说明
- `outputs`: 可选,并入节点输出(如 submit_mr 的 `mr_url`)
- `checks`: 可选自证清单

写完产物(`set_*` / `write_artifact`)后再调用。未标记即判失败。
平台校验顺序固定:**默认检查(产物/门禁等)必须先通过 → 通过后才可能做业务 RPC**。
`submit_mr` 节点平台不再代验 git 推送/MR/冲突,以你的 `node_complete` 为准。

## 结构化产物(框架卡片)

某些内置节点有**专用的结构化产物工具**,应使用对应工具而非 `write_artifact`:

| 节点 | 写入 | 读取 |
| --- | --- | --- |
| 澄清 react | `set_clarified_requirement`(完整需求规格:背景/目标/范围/FR+验收/假设依赖约束等)。给人看的页面/文案可另 `write_artifact`;平台自动钉 Tab,`set_artifact_preview` 用于主动聚焦 | `get_clarified_requirement` |
| Approve | `set_clarified_requirement` + `set_plan`(强制);可选 `set_preview` 登记可运行应用 | `get_clarified_requirement` / `get_plan` |
| 计划 plan | `set_plan` | `get_plan` |
| 调研 research | `set_research` | `get_research` |
| 方案 proposal | `set_proposals` | `get_proposals` |
| 测试 test | `set_test_result` | `get_test_result` |
| 评审 review | `set_review` | `get_review` |
| 实现 implement | `set_implementation_result` | `get_implementation_result` |
| 环境确认 preflight | `set_preflight` | `get_preflight` |
| 应用预览 app_preview | `set_preview`(强制) | — |

- 这些工具只在其对应节点类型可用(Approve 还可选 `set_preview`);编号/状态由平台生成,无需自填 id。
- 需要消费上游的结构化产物时,用对应的 `get_*` 或 `read_artifact` 读取。
