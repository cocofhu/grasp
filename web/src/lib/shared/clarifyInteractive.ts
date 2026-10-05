// Run-graph node interactivity, decided by the agent node's capability
// snapshot (`caps`, set at run start) — never by type strings.
import type { WFNode } from './types'
import { canPreview, isClarify, isInteractive, reviewEnabled } from '../workflow/agentCapabilities'

export type CapsNode = Pick<WFNode, 'type' | 'caps'> | null | undefined

function agentCaps(node: CapsNode) {
  return node?.type === 'agent' ? node.caps : undefined
}

/** Multi-turn clarify dialogue (ask_question + waiting_human inbox). */
export function isClarifyNode(node: CapsNode): boolean {
  return isClarify(agentCaps(node))
}

/** Auto Agent that parks for human review after its run. */
export function isReviewNode(node: CapsNode): boolean {
  return reviewEnabled(agentCaps(node))
}

/** Holds a human conversation at all (clarify or post-run review). */
export function isInteractiveNode(node: CapsNode): boolean {
  return isInteractive(agentCaps(node))
}

/** May register a running app preview (set_preview). */
export function isPreviewNode(node: CapsNode): boolean {
  return canPreview(agentCaps(node))
}

/** Looks a node up in a run graph. */
export function findGraphNode(
  nodes: WFNode[] | null | undefined,
  nodeId: string | null | undefined,
): WFNode | undefined {
  if (!nodeId) return undefined
  return (nodes || []).find((n) => n.id === nodeId)
}
