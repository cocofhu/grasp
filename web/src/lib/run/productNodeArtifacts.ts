// Structured products a run node owns: an agent node's declared writes (from
// its caps snapshot), or proposal_select's confirmed pick.

import type { WFNode } from '@/lib/shared/types'
import { declaredProducts } from '@/lib/workflow/agentCapabilities'

type ProductNode = Pick<WFNode, 'type' | 'caps'>

/** Confirmed single proposal — same card as proposals.json, highlighted as selected. */
export const PROPOSAL_SELECT_ARTIFACT = 'proposal.json'

export type ProductArtifactSpec = { name: string; required: boolean; outputKey?: string }

/** Every structured deliverable of a node, in declaration order. */
export function productArtifactsForNode(node: ProductNode | null | undefined): ProductArtifactSpec[] {
  if (!node) return []
  if (node.type === 'proposal_select') return [{ name: PROPOSAL_SELECT_ARTIFACT, required: true }]
  if (node.type !== 'agent') return []
  return declaredProducts(node.caps).map((p) => ({ name: p.artifactName, required: p.required, outputKey: p.outputKey }))
}

/** Primary product: first required, else first declared. */
export function productArtifactName(node: ProductNode | null | undefined): string | undefined {
  const listed = productArtifactsForNode(node)
  return (listed.find((a) => a.required) || listed[0])?.name
}

export function isProductNode(node: ProductNode | null | undefined): boolean {
  return productArtifactsForNode(node).length > 0
}

/** Run-scoped reserved names: one copy per run; later status writes must not hide it. */
export const RUN_SCOPED_PRODUCT_NAMES = new Set(['plan.json'])

const FINISHED_NODE_STATUSES = new Set(['completed', 'failed', 'cancelled'])

/** Pick the store row a structured product panel should bind. */
export function resolveStructuredProductArtifact<T extends { name: string; nodeId?: string }>(opts: {
  name: string
  nodeId: string
  node: ProductNode
  nodeStatus?: string
  hasSnapshot?: boolean
  artifacts: T[]
}): T | null {
  const { name, nodeId, node, artifacts } = opts
  if (!name) return null
  const owned = artifacts.find((a) => a.name === name && a.nodeId === nodeId)
  if (owned) return owned
  const listed = productArtifactsForNode(node)
  if (listed.length <= 1) {
    return artifacts.find((a) => a.name === name) || null
  }
  // plan.json is run-global: update_plan_status may rewrite its nodeId to a
  // later node. Once this node finished (or snapshotted the plan) still bind
  // the run copy; an in-flight node without a snapshot must not pick up an
  // upstream leftover.
  const finished = FINISHED_NODE_STATUSES.has(String(opts.nodeStatus || ''))
  if (RUN_SCOPED_PRODUCT_NAMES.has(name) && (opts.hasSnapshot || finished)) {
    return artifacts.find((a) => a.name === name) || null
  }
  return null
}
