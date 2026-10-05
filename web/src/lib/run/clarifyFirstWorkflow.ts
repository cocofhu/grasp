import type { WFEdge, WFNode, Workflow } from '@/lib/shared/types'
import { isClarify } from '@/lib/workflow/agentCapabilities'
import { nodeCapabilities, type AgentCapsLookup } from '@/lib/workflow/nodeOutlets'

type GraphLike = {
  nodes?: WFNode[] | null
  edges?: WFEdge[] | null
}

function isSuccessEdge(e: WFEdge): boolean {
  return !e.kind || e.kind === 'success'
}

/** First success successor of the input node: unguarded edge, or the only success edge. */
function inputSuccessTargetId(graph: GraphLike): string | null {
  const nodes = graph.nodes || []
  const edges = graph.edges || []
  const start = nodes.find((n) => n.type === 'input')
  if (!start) return null
  const success = edges.filter((e) => e.source === start.id && isSuccessEdge(e))
  if (success.length === 0) return null
  const unguarded = success.filter((e) => !String(e.when || '').trim())
  const picked = unguarded[0] ?? (success.length === 1 ? success[0] : undefined)
  return picked?.target || null
}

/**
 * Node id of the clarify Agent on the input success path, if any. Workflow
 * graphs carry no caps snapshot, so caps come from `agents` by agent_profile.
 */
export function clarifyFirstNodeId(graph: GraphLike, agents?: AgentCapsLookup): string | null {
  const targetId = inputSuccessTargetId(graph)
  if (!targetId) return null
  const target = (graph.nodes || []).find((n) => n.id === targetId)
  return target && isClarify(nodeCapabilities(target, agents)) ? target.id : null
}

/** True when the node after input (success path) is a clarify Agent. */
export function isClarifyFirstWorkflow(graph: GraphLike, agents?: AgentCapsLookup): boolean {
  return !!clarifyFirstNodeId(graph, agents)
}

export function isPublishedClarifyFirst(
  wf: Pick<Workflow, 'status'> & GraphLike,
  agents?: AgentCapsLookup,
): boolean {
  return wf.status === 'published' && isClarifyFirstWorkflow(wf, agents)
}
