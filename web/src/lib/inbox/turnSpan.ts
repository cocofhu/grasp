import type { ClarifyTurn } from '../shared/types'

type Stamped = Pick<ClarifyTurn, 'role' | 'at'> & { streaming?: boolean }

/**
 * A stored reply's span when it has no start/end of its own: from what it
 * answers (the human turn before it, else the segment's start) to when the
 * reply was stored, which happens once it is done.
 */
export function replySpan(
  prev: Stamped | undefined,
  reply: Stamped,
  fallbackStart?: string,
): { startedAt: string; endedAt: string } | null {
  if (reply.streaming || !reply.at) return null
  const startedAt = prev ? (prev.role === 'human' ? prev.at : '') : fallbackStart || ''
  if (!startedAt) return null
  const from = Date.parse(startedAt)
  const to = Date.parse(reply.at)
  return Number.isFinite(from) && Number.isFinite(to) && to > from ? { startedAt, endedAt: reply.at } : null
}
