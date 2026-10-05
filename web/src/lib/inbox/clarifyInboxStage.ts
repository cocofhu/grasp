import type { NodeRun, Run, WFNode } from '../shared/types'
import { isProductNode } from '../run/productNodeArtifacts'

/** Product-owning nodes present in the slim inbox-context range (have an execution). */
export function listClarifyProductNodes(run: Run | null | undefined): WFNode[] {
  if (!run?.nodes) return []
  const execs = run.nodeExecutions || {}
  return run.nodes.filter(
    (n) =>
      isProductNode(n) &&
      !!(execs[n.id]?.length || run.nodeRuns?.[n.id]),
  )
}

/** Prefer the inbox node when it is a product; otherwise the first in range. */
export function defaultClarifyProductId(currentNodeId: string, products: WFNode[]): string | null {
  if (!products.length) return null
  if (products.some((n) => n.id === currentNodeId)) return currentNodeId
  return products[0].id
}

export function pickClarifyNodeRun(
  run: Run | null | undefined,
  nodeId: string,
  iteration: number,
): NodeRun | null {
  if (!run) return null
  const execs = run.nodeExecutions?.[nodeId]
  if (execs?.length) {
    return execs.find((e) => e.iteration === iteration) || execs[execs.length - 1] || null
  }
  return run.nodeRuns?.[nodeId] || null
}

export type ClarifyProductStageKind = 'panel' | 'pending' | 'executedEmpty' | 'loadFailed'

/**
 * Outer clarify stage state for GatesInbox product pane.
 * Loading is handled separately (ArtifactLoadingPane) before this is consulted.
 */
export function resolveClarifyProductStage(opts: {
  loadError: boolean
  run: Run | null
  inboxNodeId: string
  inboxIteration: number
  selectedNode: WFNode | null
  selectedNodeRun: NodeRun | null
}): ClarifyProductStageKind {
  if (opts.loadError || !opts.run) return 'loadFailed'
  // Clarify inbox-context must carry Gate-aligned product fields.
  if (!Array.isArray(opts.run.nodes) || opts.run.nodeExecutions == null) return 'loadFailed'
  if (opts.selectedNode && opts.selectedNodeRun) return 'panel'

  const nodeRun = pickClarifyNodeRun(opts.run, opts.inboxNodeId, opts.inboxIteration)
  if (!nodeRun || nodeRun.status === 'pending') return 'pending'
  return 'executedEmpty'
}
