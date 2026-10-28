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

## Solo 会话历史

Solo 的工具列表提供只读 `get_history(view?, cursor?, limit?)`，用于模型组或编码工具切换后接续当前任务：

- `view="summary"`（默认）：覆盖会话的用户要求、更正记录及近期对话；后续更正以较新的用户消息为准。超出输入预算的范围会明确提示回查。
- `view="messages"`：分页读取完整消息正文、附件引用和可见工具结果。页内按时间正序，使用 `next_cursor` 继续读取更早记录；`history_version` 表示固定的历史快照。
- 会话范围由当前 MCP 授权确定，不需要也不接受其他会话 ID。历史来自平台原始记录，不通过 `write_artifact` 修改。

新工具已收到基础交接上下文；需要核对较早要求、决定或执行结果时，主动回查历史。工作流执行历史继续使用 `list_run_history` / `get_history_detail`。

若原生 MCP 不可用、需经 HTTP 调用时:优先用环境变量 `GRASP_ARTIFACT_URL` /
`GRASP_ARTIFACT_TOKEN`。直接使用环境变量中的地址，不要改主机名，也不要为了连通产物接口去检查容器引擎。

## 产物契约

- 本 Agent 能写哪些产物由它的能力声明决定;每种结构化产物都有专用的 `set_*` 工具,
  字段契约在节点提示词里给出。结构化产物必须用对应的 `set_*` 工具写,而不是 `write_artifact`。
- 声明为必写的产物缺失时,编排器会催促补写,仍未写入则判定节点失败。
- 读取上游产物用 `read_artifact` / `list_artifacts` 或对应的 `get_*`,不要猜测文件路径;
  只能读取能力声明里允许的产物。

## 完成标记 (node_complete,强制)

Agent 节点在结束前**必须**调用一次 `node_complete`:

- `status`: `success` 或 `failed`(必填)
- `summary` / `error`: 可选说明
- `outputs`: 可选,并入节点输出(如 `mr_url`、`branches`)
- `checks`: 可选自证清单

写完产物(`set_*` / `write_artifact`)后再调用。未标记即判失败。
平台校验顺序固定:**默认检查(产物/门禁等)必须先通过 → 通过后才可能做业务 RPC**。
