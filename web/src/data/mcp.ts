// 平台内置 MCP 发现目录（集成页）。运行级条目在每次运行开始时由编排器注入；
// 通用 / PM 会话级条目见于 Agent mcp[] 与 PM Leader 咨询沙箱，此处仅作发现与说明。

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
  /** run = 运行级；project = 项目/PM 会话级；agent = 任意 Agent 可声明的通用平台 MCP */
  scope: 'run' | 'project' | 'agent'
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
    scope: 'run',
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
      { name: 'set_proposals', signatureKey: 'mcp.artifactStore.tools.set_proposals.signature', descKey: 'mcp.artifactStore.tools.set_proposals.desc', io: 'write' },
      { name: 'get_proposals', signatureKey: 'mcp.artifactStore.tools.get_proposals.signature', descKey: 'mcp.artifactStore.tools.get_proposals.desc', io: 'read' },
      { name: 'set_test_result', signatureKey: 'mcp.artifactStore.tools.set_test_result.signature', descKey: 'mcp.artifactStore.tools.set_test_result.desc', io: 'write' },
      { name: 'get_test_result', signatureKey: 'mcp.artifactStore.tools.get_test_result.signature', descKey: 'mcp.artifactStore.tools.get_test_result.desc', io: 'read' },
      { name: 'set_review', signatureKey: 'mcp.artifactStore.tools.set_review.signature', descKey: 'mcp.artifactStore.tools.set_review.desc', io: 'write' },
      { name: 'get_review', signatureKey: 'mcp.artifactStore.tools.get_review.signature', descKey: 'mcp.artifactStore.tools.get_review.desc', io: 'read' },
      { name: 'set_implementation_result', signatureKey: 'mcp.artifactStore.tools.set_implementation_result.signature', descKey: 'mcp.artifactStore.tools.set_implementation_result.desc', io: 'write' },
      { name: 'get_implementation_result', signatureKey: 'mcp.artifactStore.tools.get_implementation_result.signature', descKey: 'mcp.artifactStore.tools.get_implementation_result.desc', io: 'read' },
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
  {
    id: 'memory-store',
    name: 'memory-store',
    builtin: true,
    scope: 'agent',
    descKey: 'mcp.memoryStore.desc',
    overviewKey: 'mcp.memoryStore.overview',
    tools: [
      { name: 'list_memories', signatureKey: 'mcp.memoryStore.tools.list_memories.signature', descKey: 'mcp.memoryStore.tools.list_memories.desc', io: 'read' },
      { name: 'get_memory', signatureKey: 'mcp.memoryStore.tools.get_memory.signature', descKey: 'mcp.memoryStore.tools.get_memory.desc', io: 'read' },
      { name: 'search_memories', signatureKey: 'mcp.memoryStore.tools.search_memories.signature', descKey: 'mcp.memoryStore.tools.search_memories.desc', io: 'read' },
      { name: 'upsert_memory', signatureKey: 'mcp.memoryStore.tools.upsert_memory.signature', descKey: 'mcp.memoryStore.tools.upsert_memory.desc', io: 'write' },
      { name: 'delete_memory', signatureKey: 'mcp.memoryStore.tools.delete_memory.signature', descKey: 'mcp.memoryStore.tools.delete_memory.desc', io: 'write' },
    ],
    conventionKey: 'mcp.memoryStore.convention',
  },
  {
    id: 'context-store',
    name: 'context-store',
    builtin: true,
    scope: 'agent',
    descKey: 'mcp.contextStore.desc',
    overviewKey: 'mcp.contextStore.overview',
    tools: [
      { name: 'list_conversations', signatureKey: 'mcp.contextStore.tools.list_conversations.signature', descKey: 'mcp.contextStore.tools.list_conversations.desc', io: 'read' },
      { name: 'get_messages', signatureKey: 'mcp.contextStore.tools.get_messages.signature', descKey: 'mcp.contextStore.tools.get_messages.desc', io: 'read' },
      { name: 'search_messages', signatureKey: 'mcp.contextStore.tools.search_messages.signature', descKey: 'mcp.contextStore.tools.search_messages.desc', io: 'read' },
      { name: 'get_current_conversation', signatureKey: 'mcp.contextStore.tools.get_current_conversation.signature', descKey: 'mcp.contextStore.tools.get_current_conversation.desc', io: 'read' },
      { name: 'get_attached_context', signatureKey: 'mcp.contextStore.tools.get_attached_context.signature', descKey: 'mcp.contextStore.tools.get_attached_context.desc', io: 'read' },
    ],
    conventionKey: 'mcp.contextStore.convention',
  },
  {
    id: 'task-scheduler',
    name: 'task-scheduler',
    builtin: true,
    scope: 'agent',
    descKey: 'mcp.taskScheduler.desc',
    overviewKey: 'mcp.taskScheduler.overview',
    tools: [
      { name: 'list_jobs', signatureKey: 'mcp.taskScheduler.tools.list_jobs.signature', descKey: 'mcp.taskScheduler.tools.list_jobs.desc', io: 'read' },
      { name: 'list_job_runs', signatureKey: 'mcp.taskScheduler.tools.list_job_runs.signature', descKey: 'mcp.taskScheduler.tools.list_job_runs.desc', io: 'read' },
      { name: 'create_job', signatureKey: 'mcp.taskScheduler.tools.create_job.signature', descKey: 'mcp.taskScheduler.tools.create_job.desc', io: 'write' },
      { name: 'update_job', signatureKey: 'mcp.taskScheduler.tools.update_job.signature', descKey: 'mcp.taskScheduler.tools.update_job.desc', io: 'write' },
      { name: 'delete_job', signatureKey: 'mcp.taskScheduler.tools.delete_job.signature', descKey: 'mcp.taskScheduler.tools.delete_job.desc', io: 'write' },
      { name: 'run_job_now', signatureKey: 'mcp.taskScheduler.tools.run_job_now.signature', descKey: 'mcp.taskScheduler.tools.run_job_now.desc', io: 'write' },
    ],
    conventionKey: 'mcp.taskScheduler.convention',
  },
  {
    id: 'pm-progress',
    name: 'pm-progress',
    builtin: true,
    scope: 'project',
    descKey: 'mcp.pmProgress.desc',
    overviewKey: 'mcp.pmProgress.overview',
    tools: [
      { name: 'pm_get_progress', signatureKey: 'mcp.pmProgress.tools.pm_get_progress.signature', descKey: 'mcp.pmProgress.tools.pm_get_progress.desc', io: 'read' },
      { name: 'pm_list_blockers', signatureKey: 'mcp.pmProgress.tools.pm_list_blockers.signature', descKey: 'mcp.pmProgress.tools.pm_list_blockers.desc', io: 'read' },
      { name: 'pm_get_plan_summary', signatureKey: 'mcp.pmProgress.tools.pm_get_plan_summary.signature', descKey: 'mcp.pmProgress.tools.pm_get_plan_summary.desc', io: 'read' },
      { name: 'pm_get_artifact_summary', signatureKey: 'mcp.pmProgress.tools.pm_get_artifact_summary.signature', descKey: 'mcp.pmProgress.tools.pm_get_artifact_summary.desc', io: 'read' },
      { name: 'pm_get_risk_trends', signatureKey: 'mcp.pmProgress.tools.pm_get_risk_trends.signature', descKey: 'mcp.pmProgress.tools.pm_get_risk_trends.desc', io: 'read' },
      { name: 'pm_compare_runs', signatureKey: 'mcp.pmProgress.tools.pm_compare_runs.signature', descKey: 'mcp.pmProgress.tools.pm_compare_runs.desc', io: 'read' },
    ],
    conventionKey: 'mcp.pmProgress.convention',
  },
  {
    id: 'pm-workflow-read',
    name: 'pm-workflow-read',
    builtin: true,
    scope: 'project',
    descKey: 'mcp.pmWorkflowRead.desc',
    overviewKey: 'mcp.pmWorkflowRead.overview',
    tools: [
      { name: 'pm_list_workflows', signatureKey: 'mcp.pmWorkflowRead.tools.pm_list_workflows.signature', descKey: 'mcp.pmWorkflowRead.tools.pm_list_workflows.desc', io: 'read' },
      { name: 'pm_get_workflow', signatureKey: 'mcp.pmWorkflowRead.tools.pm_get_workflow.signature', descKey: 'mcp.pmWorkflowRead.tools.pm_get_workflow.desc', io: 'read' },
      { name: 'pm_get_workflow_graph', signatureKey: 'mcp.pmWorkflowRead.tools.pm_get_workflow_graph.signature', descKey: 'mcp.pmWorkflowRead.tools.pm_get_workflow_graph.desc', io: 'read' },
      { name: 'pm_list_versions', signatureKey: 'mcp.pmWorkflowRead.tools.pm_list_versions.signature', descKey: 'mcp.pmWorkflowRead.tools.pm_list_versions.desc', io: 'read' },
      { name: 'pm_list_runs', signatureKey: 'mcp.pmWorkflowRead.tools.pm_list_runs.signature', descKey: 'mcp.pmWorkflowRead.tools.pm_list_runs.desc', io: 'read' },
      { name: 'pm_list_pending_gates', signatureKey: 'mcp.pmWorkflowRead.tools.pm_list_pending_gates.signature', descKey: 'mcp.pmWorkflowRead.tools.pm_list_pending_gates.desc', io: 'read' },
      { name: 'pm_get_artifact', signatureKey: 'mcp.pmWorkflowRead.tools.pm_get_artifact.signature', descKey: 'mcp.pmWorkflowRead.tools.pm_get_artifact.desc', io: 'read' },
    ],
    conventionKey: 'mcp.pmWorkflowRead.convention',
  },
  {
    id: 'pm-workflow-write',
    name: 'pm-workflow-write',
    builtin: true,
    scope: 'project',
    descKey: 'mcp.pmWorkflowWrite.desc',
    overviewKey: 'mcp.pmWorkflowWrite.overview',
    tools: [
      { name: 'pm_create_workflow', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_create_workflow.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_create_workflow.desc', io: 'write' },
      { name: 'pm_update_workflow', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_update_workflow.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_update_workflow.desc', io: 'write' },
      { name: 'pm_copy_workflow', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_copy_workflow.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_copy_workflow.desc', io: 'write' },
      { name: 'pm_delete_workflow', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_delete_workflow.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_delete_workflow.desc', io: 'write' },
      { name: 'pm_publish_workflow', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_publish_workflow.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_publish_workflow.desc', io: 'write' },
      { name: 'pm_start_run', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_start_run.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_start_run.desc', io: 'write' },
      { name: 'pm_resume_gate', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_resume_gate.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_resume_gate.desc', io: 'write' },
      { name: 'pm_react_reply', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_react_reply.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_react_reply.desc', io: 'write' },
      { name: 'pm_cancel_run', signatureKey: 'mcp.pmWorkflowWrite.tools.pm_cancel_run.signature', descKey: 'mcp.pmWorkflowWrite.tools.pm_cancel_run.desc', io: 'write' },
    ],
    conventionKey: 'mcp.pmWorkflowWrite.convention',
  },
  {
    id: 'pm-agent-fs',
    name: 'pm-agent-fs',
    builtin: true,
    scope: 'project',
    descKey: 'mcp.pmAgentFs.desc',
    overviewKey: 'mcp.pmAgentFs.overview',
    tools: [
      { name: 'pm_get_org', signatureKey: 'mcp.pmAgentFs.tools.pm_get_org.signature', descKey: 'mcp.pmAgentFs.tools.pm_get_org.desc', io: 'read' },
      { name: 'pm_list_agent_templates', signatureKey: 'mcp.pmAgentFs.tools.pm_list_agent_templates.signature', descKey: 'mcp.pmAgentFs.tools.pm_list_agent_templates.desc', io: 'read' },
      { name: 'pm_create_agent_from_template', signatureKey: 'mcp.pmAgentFs.tools.pm_create_agent_from_template.signature', descKey: 'mcp.pmAgentFs.tools.pm_create_agent_from_template.desc', io: 'write' },
      { name: 'pm_set_org_membership', signatureKey: 'mcp.pmAgentFs.tools.pm_set_org_membership.signature', descKey: 'mcp.pmAgentFs.tools.pm_set_org_membership.desc', io: 'write' },
      { name: 'pm_ensure_child_group', signatureKey: 'mcp.pmAgentFs.tools.pm_ensure_child_group.signature', descKey: 'mcp.pmAgentFs.tools.pm_ensure_child_group.desc', io: 'write' },
      { name: 'pm_fs_list', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_list.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_list.desc', io: 'read' },
      { name: 'pm_fs_read', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_read.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_read.desc', io: 'read' },
      { name: 'pm_fs_write', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_write.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_write.desc', io: 'write' },
      { name: 'pm_fs_delete', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_delete.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_delete.desc', io: 'write' },
      { name: 'pm_fs_mkdir', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_mkdir.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_mkdir.desc', io: 'write' },
      { name: 'pm_fs_rename', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_rename.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_rename.desc', io: 'write' },
      { name: 'pm_fs_history', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_history.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_history.desc', io: 'read' },
      { name: 'pm_fs_diff', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_diff.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_diff.desc', io: 'read' },
      { name: 'pm_fs_restore', signatureKey: 'mcp.pmAgentFs.tools.pm_fs_restore.signature', descKey: 'mcp.pmAgentFs.tools.pm_fs_restore.desc', io: 'write' },
    ],
    conventionKey: 'mcp.pmAgentFs.convention',
  },
  {
    id: 'pm-prd-manager',
    name: 'pm-prd-manager',
    builtin: true,
    scope: 'project',
    descKey: 'mcp.pmPrdManager.desc',
    overviewKey: 'mcp.pmPrdManager.overview',
    tools: [
      { name: 'pm_list_requirement_drafts', signatureKey: 'mcp.pmPrdManager.tools.pm_list_requirement_drafts.signature', descKey: 'mcp.pmPrdManager.tools.pm_list_requirement_drafts.desc', io: 'read' },
      { name: 'pm_get_requirement_draft', signatureKey: 'mcp.pmPrdManager.tools.pm_get_requirement_draft.signature', descKey: 'mcp.pmPrdManager.tools.pm_get_requirement_draft.desc', io: 'read' },
      { name: 'pm_create_requirement_draft', signatureKey: 'mcp.pmPrdManager.tools.pm_create_requirement_draft.signature', descKey: 'mcp.pmPrdManager.tools.pm_create_requirement_draft.desc', io: 'write' },
    ],
    conventionKey: 'mcp.pmPrdManager.convention',
  },
]
