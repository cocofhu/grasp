import type { ClarifyTurn, InboxItem, Run } from '@/lib/shared/types'
import { isClarifyNode } from '@/lib/shared/clarifyInteractive'

export type InboxClarifySession = {
  nodeId: string
  iteration?: number
  turns: ClarifyTurn[]
  done: boolean
  previewArtifact?: string
}

/** Resolve the open clarify/review session for an inbox item's node. */
export function pickInboxClarifySession(
  run: Run | null | undefined,
  nodeId: string,
): InboxClarifySession | null {
  if (!run) return null
  return (
    run.clarifyByNode?.[nodeId] ||
    (run.clarify?.nodeId === nodeId ? run.clarify : null) ||
    null
  )
}

/**
 * Mirror RunDetailView.reviewActive for inbox: post-run product review on a
 * non-clarify Agent (backend only seeds review sessions for review-enabled caps).
 * Inbox API type stays "clarify"; mode is decided from the loaded graph.
 */
export function resolveInboxReviewState(
  item: Pick<InboxItem, 'type' | 'nodeId'> | null | undefined,
  run: Run | null | undefined,
  conv: InboxClarifySession | null,
): { reviewActive: boolean; nodeMissing: boolean } {
  if (!item || item.type !== 'clarify' || !run) {
    return { reviewActive: false, nodeMissing: false }
  }
  const n = run.nodes?.find((node) => node.id === item.nodeId)
  if (!n) {
    return { reviewActive: false, nodeMissing: true }
  }
  if (isClarifyNode(n)) {
    return { reviewActive: false, nodeMissing: false }
  }
  return { reviewActive: !!conv && !conv.done, nodeMissing: false }
}

export function inboxComposerMode(reviewActive: boolean): 'clarify' | 'review' {
  return reviewActive ? 'review' : 'clarify'
}
