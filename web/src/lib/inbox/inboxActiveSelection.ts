import type { InboxItem } from '@/lib/shared/types'

/** Stable key for inbox list membership (matches usePendingGates.itemKey). */
export function inboxItemKey(it: Pick<InboxItem, 'runId' | 'nodeId'>): string {
  return `${it.runId}:${it.nodeId}`
}

/** `?run=&node=` → membership key. Missing either side → empty (no selection). */
export function inboxQueryKey(run: unknown, node: unknown): string {
  const r = String(run ?? '').trim()
  const n = String(node ?? '').trim()
  if (!r || !n) return ''
  return `${r}:${n}`
}

export function findInboxItemByKey<T extends Pick<InboxItem, 'runId' | 'nodeId'>>(
  list: T[],
  key: string,
): T | undefined {
  if (!key) return undefined
  return list.find((it) => inboxItemKey(it) === key)
}

/** Prefer run+node; if the parked node id is still empty, take the first card on that run. */
export function findInboxItemForHandoff<T extends Pick<InboxItem, 'runId' | 'nodeId'>>(
  list: T[],
  handoff: Pick<InboxItem, 'runId' | 'nodeId'>,
): T | undefined {
  if (handoff.nodeId) {
    const hit = findInboxItemByKey(list, inboxItemKey(handoff))
    if (hit) return hit
  }
  return list.find((it) => it.runId === handoff.runId)
}

/** `?run=&node=` exact card, else the first card on that run (guessed node id may differ). */
export function findInboxItemForQuery<T extends Pick<InboxItem, 'runId' | 'nodeId'>>(
  list: T[],
  run: unknown,
  node: unknown,
): T | undefined {
  const exact = findInboxItemByKey(list, inboxQueryKey(run, node))
  if (exact) return exact
  const runId = String(run ?? '').trim()
  if (!runId) return undefined
  return list.find((it) => it.runId === runId)
}

/** Triple used for inbox-context fetch / processed short-circuit. */
export function inboxTripleKey(
  it: Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'>,
): string {
  return `${it.runId}:${it.nodeId}:${it.iteration ?? 1}`
}

/**
 * After removing `removedKey` from `prevList`, pick the neighbor active item:
 * prefer the next item at the same index; else the previous; empty → null.
 */
export function pickNextActiveAfterRemove<T extends Pick<InboxItem, 'runId' | 'nodeId'>>(
  prevList: T[],
  removedKey: string,
  keyOf: (it: T) => string = inboxItemKey,
): T | null {
  const idx = prevList.findIndex((it) => keyOf(it) === removedKey)
  if (idx < 0) {
    return prevList[0] || null
  }
  const without = prevList.filter((_, i) => i !== idx)
  if (!without.length) return null
  // After removal, the old "next" slides into idx; else take the new last (old prev).
  return without[idx] || without[idx - 1] || null
}

/** True when inbox-context returned the leave marker (HTTP 200, pending:false). */
export function isInboxLeftPending(ctx: unknown): ctx is { pending: false } {
  if (!ctx || typeof ctx !== 'object') return false
  const rec = ctx as { pending?: unknown; type?: unknown }
  return rec.pending === false && rec.type == null
}
