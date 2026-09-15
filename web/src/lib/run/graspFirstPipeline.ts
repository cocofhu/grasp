import type { WFEdge, WFNode, Workflow } from '@/lib/shared/types'
import { isGrasp } from '@/lib/shared/clarifyInteractive'

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

/** Node id of the Grasp that sits on the input success path, if any. */
export function graspFirstNodeId(graph: GraphLike): string | null {
  const targetId = inputSuccessTargetId(graph)
  if (!targetId) return null
  const target = (graph.nodes || []).find((n) => n.id === targetId)
  return target && isGrasp(target.type) ? target.id : null
}

/** True when the node after input (success path) is a Grasp node. */
export function isGraspFirstPipeline(graph: GraphLike): boolean {
  return !!graspFirstNodeId(graph)
}

export function isPublishedGraspFirst(wf: Pick<Workflow, 'status'> & GraphLike): boolean {
  return wf.status === 'published' && isGraspFirstPipeline(wf)
}

/** @deprecated Use graspFirstNodeId */
export const approveFirstNodeId = graspFirstNodeId
/** @deprecated Use isGraspFirstPipeline */
export const isApproveFirstPipeline = isGraspFirstPipeline
/** @deprecated Use isPublishedGraspFirst */
export const isPublishedApproveFirst = isPublishedGraspFirst
