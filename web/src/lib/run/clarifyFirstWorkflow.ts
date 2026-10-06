import type { PublishedWorkflowSnapshot, WFEdge, WFNode, Workflow } from '@/lib/shared/types'
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

type HomeWorkflowPick = Pick<
  Workflow,
  'status' | 'version' | 'publishedVersion' | 'publishedSnapshot'
> &
  GraphLike

/**
 * Graph the home page should treat as the published snapshot.
 * A newer draft head is ignored once publishedSnapshot (or an equal version
 * pair) is present. Status published still means the head itself is that snapshot.
 */
export function publishedHomeGraph(wf: HomeWorkflowPick): (GraphLike & Partial<Pick<PublishedWorkflowSnapshot, 'name' | 'description'>>) | null {
  const ver = wf.publishedVersion ?? 0
  const snap = wf.publishedSnapshot
  if (snap && (ver > 0 || (snap.version ?? 0) > 0)) return snap
  if (wf.status === 'published') return wf
  if (ver > 0 && ver === wf.version) return wf
  return null
}

/** True when the published snapshot (not merely the head status) is clarify-first. */
export function isPublishedClarifyFirst(
  wf: HomeWorkflowPick,
  agents?: AgentCapsLookup,
): boolean {
  const graph = publishedHomeGraph(wf)
  return !!graph && isClarifyFirstWorkflow(graph, agents)
}

/** Card fields for home: name, description, and graph come from the published snapshot. */
export function withPublishedSnapshot<T extends Workflow>(wf: T): T {
  const snap = wf.publishedSnapshot
  if (!snap || publishedHomeGraph(wf) !== snap) return wf
  const name = snap.name?.trim() ? snap.name : wf.name
  return {
    ...wf,
    name,
    description: snap.description ?? wf.description,
    nodes: snap.nodes ?? wf.nodes,
    edges: snap.edges ?? wf.edges,
  }
}
