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
    id: 'artifact-store',
    name: 'artifact-store',
    builtin: true,
    descKey: 'mcp.artifactStore.desc',
    overviewKey: 'mcp.artifactStore.overview',
    tools: [
      { name: 'write_artifact', signatureKey: 'mcp.artifactStore.tools.write_artifact.signature', descKey: 'mcp.artifactStore.tools.write_artifact.desc', io: 'write' },
      { name: 'read_artifact', signatureKey: 'mcp.artifactStore.tools.read_artifact.signature', descKey: 'mcp.artifactStore.tools.read_artifact.desc', io: 'read' },
      { name: 'list_artifacts', signatureKey: 'mcp.artifactStore.tools.list_artifacts.signature', descKey: 'mcp.artifactStore.tools.list_artifacts.desc', io: 'read' },
      { name: 'ask_question', signatureKey: 'mcp.artifactStore.tools.ask_question.signature', descKey: 'mcp.artifactStore.tools.ask_question.desc', io: 'write' },
      { name: 'ask_form', signatureKey: 'mcp.artifactStore.tools.ask_form.signature', descKey: 'mcp.artifactStore.tools.ask_form.desc', io: 'write' },
      { name: 'set_plan', signatureKey: 'mcp.artifactStore.tools.set_plan.signature', descKey: 'mcp.artifactStore.tools.set_plan.desc', io: 'write' },
      { name: 'get_plan', signatureKey: 'mcp.artifactStore.tools.get_plan.signature', descKey: 'mcp.artifactStore.tools.get_plan.desc', io: 'read' },
      { name: 'update_plan_status', signatureKey: 'mcp.artifactStore.tools.update_plan_status.signature', descKey: 'mcp.artifactStore.tools.update_plan_status.desc', io: 'write' },
      { name: 'set_clarified_requirement', signatureKey: 'mcp.artifactStore.tools.set_clarified_requirement.signature', descKey: 'mcp.artifactStore.tools.set_clarified_requirement.desc', io: 'write' },
      { name: 'get_clarified_requirement', signatureKey: 'mcp.artifactStore.tools.get_clarified_requirement.signature', descKey: 'mcp.artifactStore.tools.get_clarified_requirement.desc', io: 'read' },
      { name: 'set_preflight', signatureKey: 'mcp.artifactStore.tools.set_preflight.signature', descKey: 'mcp.artifactStore.tools.set_preflight.desc', io: 'write' },
      { name: 'get_preflight', signatureKey: 'mcp.artifactStore.tools.get_preflight.signature', descKey: 'mcp.artifactStore.tools.get_preflight.desc', io: 'read' },
      { name: 'set_research', signatureKey: 'mcp.artifactStore.tools.set_research.signature', descKey: 'mcp.artifactStore.tools.set_research.desc', io: 'write' },
      { name: 'get_research', signatureKey: 'mcp.artifactStore.tools.get_research.signature', descKey: 'mcp.artifactStore.tools.get_research.desc', io: 'read' },
      { name: 'set_root_cause', signatureKey: 'mcp.artifactStore.tools.set_root_cause.signature', descKey: 'mcp.artifactStore.tools.set_root_cause.desc', io: 'write' },
      { name: 'get_root_cause', signatureKey: 'mcp.artifactStore.tools.get_root_cause.signature', descKey: 'mcp.artifactStore.tools.get_root_cause.desc', io: 'read' },
      { name: 'set_test_result', signatureKey: 'mcp.artifactStore.tools.set_test_result.signature', descKey: 'mcp.artifactStore.tools.set_test_result.desc', io: 'write' },
      { name: 'get_test_result', signatureKey: 'mcp.artifactStore.tools.get_test_result.signature', descKey: 'mcp.artifactStore.tools.get_test_result.desc', io: 'read' },
      { name: 'set_review', signatureKey: 'mcp.artifactStore.tools.set_review.signature', descKey: 'mcp.artifactStore.tools.set_review.desc', io: 'write' },
      { name: 'get_review', signatureKey: 'mcp.artifactStore.tools.get_review.signature', descKey: 'mcp.artifactStore.tools.get_review.desc', io: 'read' },
      { name: 'set_implementation_result', signatureKey: 'mcp.artifactStore.tools.set_implementation_result.signature', descKey: 'mcp.artifactStore.tools.set_implementation_result.desc', io: 'write' },
      { name: 'get_implementation_result', signatureKey: 'mcp.artifactStore.tools.get_implementation_result.signature', descKey: 'mcp.artifactStore.tools.get_implementation_result.desc', io: 'read' },
      { name: 'set_merge_request', signatureKey: 'mcp.artifactStore.tools.set_merge_request.signature', descKey: 'mcp.artifactStore.tools.set_merge_request.desc', io: 'write' },
      { name: 'get_merge_request', signatureKey: 'mcp.artifactStore.tools.get_merge_request.signature', descKey: 'mcp.artifactStore.tools.get_merge_request.desc', io: 'read' },
      { name: 'set_preview', signatureKey: 'mcp.artifactStore.tools.set_preview.signature', descKey: 'mcp.artifactStore.tools.set_preview.desc', io: 'write' },
      { name: 'page_state', signatureKey: 'mcp.artifactStore.tools.page_state.signature', descKey: 'mcp.artifactStore.tools.page_state.desc', io: 'read' },
      { name: 'page_click', signatureKey: 'mcp.artifactStore.tools.page_click.signature', descKey: 'mcp.artifactStore.tools.page_click.desc', io: 'write' },
      { name: 'page_input', signatureKey: 'mcp.artifactStore.tools.page_input.signature', descKey: 'mcp.artifactStore.tools.page_input.desc', io: 'write' },
      { name: 'page_select', signatureKey: 'mcp.artifactStore.tools.page_select.signature', descKey: 'mcp.artifactStore.tools.page_select.desc', io: 'write' },
      { name: 'page_scroll', signatureKey: 'mcp.artifactStore.tools.page_scroll.signature', descKey: 'mcp.artifactStore.tools.page_scroll.desc', io: 'write' },
      { name: 'set_artifact_preview', signatureKey: 'mcp.artifactStore.tools.set_artifact_preview.signature', descKey: 'mcp.artifactStore.tools.set_artifact_preview.desc', io: 'write' },
      { name: 'node_complete', signatureKey: 'mcp.artifactStore.tools.node_complete.signature', descKey: 'mcp.artifactStore.tools.node_complete.desc', io: 'write' },
      { name: 'list_run_history', signatureKey: 'mcp.artifactStore.tools.list_run_history.signature', descKey: 'mcp.artifactStore.tools.list_run_history.desc', io: 'read' },
      { name: 'get_history_detail', signatureKey: 'mcp.artifactStore.tools.get_history_detail.signature', descKey: 'mcp.artifactStore.tools.get_history_detail.desc', io: 'read' },
    ],
    conventionKey: 'mcp.artifactStore.convention',
  },
]
