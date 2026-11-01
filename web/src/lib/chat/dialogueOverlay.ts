/**
 * The only page-local layers allowed on top of the server dialogue (besides
 * optimistic sends in dialogueState). Each one covers the gap between a click
 * here and the server state that answers it, is keyed by a server id or
 * version, and gives way as soon as the server state says the same thing.
 * Anything that is not such a gap belongs in the server state (AGENTS.md #7).
 */
import { isRetryableFailedAgent } from '@/lib/inbox/clarifyEmptyFail'
import type { ClarifyTurn } from '@/lib/shared/types'
import { versionLess, type DialogueOrphan, type DialogueQueueRow, type DialogueState, type DialogueVersion } from './dialogueState'

/**
 * 确认并流转 closed the socket at `settledAt`: nothing up to that version may
 * keep streaming. A newer server version takes over.
 */
export function isSettled(settledAt: DialogueVersion | null, st: DialogueState | null): boolean {
  if (!settledAt) return false
  return !st || !versionLess(settledAt, st)
}

export function settleRows<T extends ClarifyTurn>(rows: T[]): T[] {
  return rows.map((r) => (r.streaming ? { ...r, streaming: false } : r))
}

/** Retry was clicked: the trailing failed reply shows as a new attempt. */
export function retryAttemptRows<T extends ClarifyTurn>(rows: T[]): T[] {
  const last = rows[rows.length - 1]
  if (!isRetryableFailedAgent(last)) return rows
  return [...rows.slice(0, -1), {
    ...last!, text: '', thought: '', outcome: undefined, error: undefined,
    parts: undefined, tools: undefined, questions: undefined, streaming: true, localPending: true,
  }]
}

/** The server has picked the retry up (busy) or no longer ends on a failed reply. */
export function retryTakenOver(st: DialogueState): boolean {
  return st.busy || !isRetryableFailedAgent(st.rows[st.rows.length - 1])
}

/** Queue rows removed (`hidden`) or reordered (`order`) here, until the server queue agrees. */
export type QueueOverlay = { hidden: string[]; order: string[] | null }

export function visibleQueue(rows: DialogueQueueRow[], o: QueueOverlay): DialogueQueueRow[] {
  const shown = rows.filter((q) => !q.id || !o.hidden.includes(q.id))
  const order = o.order
  if (!order) return shown
  const rank = (q: DialogueQueueRow) => {
    const i = q.id ? order.indexOf(q.id) : -1
    return i < 0 ? order.length : i
  }
  return shown.map((q, i) => ({ q, i })).sort((a, b) => rank(a.q) - rank(b.q) || a.i - b.i).map((x) => x.q)
}

/** Drop what the server queue (ids in server order) already reflects. */
export function pruneQueueOverlay(o: QueueOverlay, serverIds: string[]): QueueOverlay {
  const hidden = o.hidden.filter((id) => serverIds.includes(id))
  let order = o.order
  if (order && serverIds.join('|') === order.filter((id) => serverIds.includes(id)).join('|')) order = null
  return { hidden, order }
}

/** The orphan sandbox op Stop was clicked for stays hidden while the server still lists it. */
export function visibleOrphan(orphan: DialogueOrphan | null, cancelledOp: string | null): DialogueOrphan | null {
  if (orphan && cancelledOp === orphan.runningOpId) return null
  return orphan
}

/** Stop was clicked on `turnId` and the server still runs it. */
export function isStopping(turnId: string | null, runningTurnIds: string[]): boolean {
  return !!turnId && runningTurnIds.includes(turnId)
}
