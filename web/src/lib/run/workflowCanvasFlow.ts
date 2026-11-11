/**
 * Stable Vue Flow node/edge object identity helpers.
 * Only selected (or other field) changes replace that element's object;
 * untouched elements keep the previous reference so Vue Flow can skip reconcile.
 */

export type FlowNodeCacheEntry<T extends { id: string; selected?: boolean }> = {
  /** Structural fingerprint excluding `selected`. */
  fingerprint: string
  selected: boolean
  obj: T
}

/** Build a fingerprint string from structural fields (must exclude selected). */
export function flowFingerprint(parts: unknown): string {
  return JSON.stringify(parts)
}

/**
 * Reuse the previous flow element object when only `selected` changed;
 * rebuild when the structural fingerprint differs.
 */
export function reuseFlowElement<T extends { id: string; selected?: boolean }>(
  cache: Map<string, FlowNodeCacheEntry<T>>,
  id: string,
  fingerprint: string,
  selected: boolean,
  build: () => T,
): T {
  const prev = cache.get(id)
  if (prev && prev.fingerprint === fingerprint) {
    if (prev.selected === selected) return prev.obj
    const obj = { ...prev.obj, selected }
    cache.set(id, { fingerprint, selected, obj })
    return obj
  }
  const obj = build()
  cache.set(id, { fingerprint, selected, obj })
  return obj
}

/** Drop cache entries for ids no longer present in the graph. */
export function pruneFlowCache<T extends { id: string; selected?: boolean }>(
  cache: Map<string, FlowNodeCacheEntry<T>>,
  liveIds: Iterable<string>,
): void {
  const keep = new Set(liveIds)
  for (const id of cache.keys()) {
    if (!keep.has(id)) cache.delete(id)
  }
}

/**
 * Assert helper for tests: given two consecutive reconcile results after a
 * selection-only change, unchanged node ids must keep object identity.
 */
export function unchangedIdsKeepIdentity<T extends { id: string }>(
  before: T[],
  after: T[],
  changedIds: Set<string>,
): boolean {
  const beforeById = new Map(before.map((n) => [n.id, n]))
  for (const n of after) {
    if (changedIds.has(n.id)) continue
    const prev = beforeById.get(n.id)
    if (!prev || prev !== n) return false
  }
  return true
}
