// 平台内置 MCP 发现目录（集成页）。条目在每次运行开始时由编排器注入，此处仅作发现与说明。

export interface McpTool {
  name: string
  signatureKey: string
  descKey: string
  io: 'write' | 'read'
}

export interface McpServer {
  id: string
  name: string
  builtin: boolean
  descKey: string
  overviewKey?: string
  tools: McpTool[]
  conventionKey?: string
}

export const BUILTIN_MCPS: McpServer[] = [
  {
    id: 'grasp-tool',
    name: 'grasp-tool',
    builtin: true,
    descKey: 'mcp.graspTool.desc',
    overviewKey: 'mcp.graspTool.overview',
    tools: [
      { name: 'write_artifact', signatureKey: 'mcp.graspTool.tools.write_artifact.signature', descKey: 'mcp.graspTool.tools.write_artifact.desc', io: 'write' },
      { name: 'read_artifact', signatureKey: 'mcp.graspTool.tools.read_artifact.signature', descKey: 'mcp.graspTool.tools.read_artifact.desc', io: 'read' },
      { name: 'list_artifacts', signatureKey: 'mcp.graspTool.tools.list_artifacts.signature', descKey: 'mcp.graspTool.tools.list_artifacts.desc', io: 'read' },
      { name: 'ask_question', signatureKey: 'mcp.graspTool.tools.ask_question.signature', descKey: 'mcp.graspTool.tools.ask_question.desc', io: 'write' },
      { name: 'ask_form', signatureKey: 'mcp.graspTool.tools.ask_form.signature', descKey: 'mcp.graspTool.tools.ask_form.desc', io: 'write' },
      { name: 'set_plan', signatureKey: 'mcp.graspTool.tools.set_plan.signature', descKey: 'mcp.graspTool.tools.set_plan.desc', io: 'write' },
      { name: 'get_plan', signatureKey: 'mcp.graspTool.tools.get_plan.signature', descKey: 'mcp.graspTool.tools.get_plan.desc', io: 'read' },
      { name: 'update_plan_status', signatureKey: 'mcp.graspTool.tools.update_plan_status.signature', descKey: 'mcp.graspTool.tools.update_plan_status.desc', io: 'write' },
      { name: 'set_clarified_requirement', signatureKey: 'mcp.graspTool.tools.set_clarified_requirement.signature', descKey: 'mcp.graspTool.tools.set_clarified_requirement.desc', io: 'write' },
      { name: 'get_clarified_requirement', signatureKey: 'mcp.graspTool.tools.get_clarified_requirement.signature', descKey: 'mcp.graspTool.tools.get_clarified_requirement.desc', io: 'read' },
      { name: 'set_preflight', signatureKey: 'mcp.graspTool.tools.set_preflight.signature', descKey: 'mcp.graspTool.tools.set_preflight.desc', io: 'write' },
      { name: 'get_preflight', signatureKey: 'mcp.graspTool.tools.get_preflight.signature', descKey: 'mcp.graspTool.tools.get_preflight.desc', io: 'read' },
      { name: 'set_research', signatureKey: 'mcp.graspTool.tools.set_research.signature', descKey: 'mcp.graspTool.tools.set_research.desc', io: 'write' },
      { name: 'get_research', signatureKey: 'mcp.graspTool.tools.get_research.signature', descKey: 'mcp.graspTool.tools.get_research.desc', io: 'read' },
      { name: 'set_root_cause', signatureKey: 'mcp.graspTool.tools.set_root_cause.signature', descKey: 'mcp.graspTool.tools.set_root_cause.desc', io: 'write' },
      { name: 'get_root_cause', signatureKey: 'mcp.graspTool.tools.get_root_cause.signature', descKey: 'mcp.graspTool.tools.get_root_cause.desc', io: 'read' },
      { name: 'set_test_result', signatureKey: 'mcp.graspTool.tools.set_test_result.signature', descKey: 'mcp.graspTool.tools.set_test_result.desc', io: 'write' },
      { name: 'get_test_result', signatureKey: 'mcp.graspTool.tools.get_test_result.signature', descKey: 'mcp.graspTool.tools.get_test_result.desc', io: 'read' },
      { name: 'set_review', signatureKey: 'mcp.graspTool.tools.set_review.signature', descKey: 'mcp.graspTool.tools.set_review.desc', io: 'write' },
      { name: 'get_review', signatureKey: 'mcp.graspTool.tools.get_review.signature', descKey: 'mcp.graspTool.tools.get_review.desc', io: 'read' },
      { name: 'set_implementation_result', signatureKey: 'mcp.graspTool.tools.set_implementation_result.signature', descKey: 'mcp.graspTool.tools.set_implementation_result.desc', io: 'write' },
      { name: 'get_implementation_result', signatureKey: 'mcp.graspTool.tools.get_implementation_result.signature', descKey: 'mcp.graspTool.tools.get_implementation_result.desc', io: 'read' },
      { name: 'set_merge_request', signatureKey: 'mcp.graspTool.tools.set_merge_request.signature', descKey: 'mcp.graspTool.tools.set_merge_request.desc', io: 'write' },
      { name: 'get_merge_request', signatureKey: 'mcp.graspTool.tools.get_merge_request.signature', descKey: 'mcp.graspTool.tools.get_merge_request.desc', io: 'read' },
      { name: 'set_preview', signatureKey: 'mcp.graspTool.tools.set_preview.signature', descKey: 'mcp.graspTool.tools.set_preview.desc', io: 'write' },
      { name: 'page_state', signatureKey: 'mcp.graspTool.tools.page_state.signature', descKey: 'mcp.graspTool.tools.page_state.desc', io: 'read' },
      { name: 'page_click', signatureKey: 'mcp.graspTool.tools.page_click.signature', descKey: 'mcp.graspTool.tools.page_click.desc', io: 'write' },
      { name: 'page_input', signatureKey: 'mcp.graspTool.tools.page_input.signature', descKey: 'mcp.graspTool.tools.page_input.desc', io: 'write' },
      { name: 'page_select', signatureKey: 'mcp.graspTool.tools.page_select.signature', descKey: 'mcp.graspTool.tools.page_select.desc', io: 'write' },
      { name: 'page_scroll', signatureKey: 'mcp.graspTool.tools.page_scroll.signature', descKey: 'mcp.graspTool.tools.page_scroll.desc', io: 'write' },
      { name: 'set_artifact_preview', signatureKey: 'mcp.graspTool.tools.set_artifact_preview.signature', descKey: 'mcp.graspTool.tools.set_artifact_preview.desc', io: 'write' },
      { name: 'node_complete', signatureKey: 'mcp.graspTool.tools.node_complete.signature', descKey: 'mcp.graspTool.tools.node_complete.desc', io: 'write' },
      { name: 'list_run_history', signatureKey: 'mcp.graspTool.tools.list_run_history.signature', descKey: 'mcp.graspTool.tools.list_run_history.desc', io: 'read' },
      { name: 'get_history_detail', signatureKey: 'mcp.graspTool.tools.get_history_detail.signature', descKey: 'mcp.graspTool.tools.get_history_detail.desc', io: 'read' },
    ],
    conventionKey: 'mcp.graspTool.convention',
  },
]
