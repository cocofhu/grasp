import type { McpCall } from '../shared/types'
import type { MergedAcpEvent } from './mergeAcpEvents'

/** Session-scoped (SPA visit) displayable snapshot for a run node. */
type LiveLogEventPageSnapshot = {
  events: MergedAcpEvent[]
  nextCursor: string
  hasMore: boolean
  live: boolean
}

type LiveLogNodeSnapshot = {
  eventPage?: LiveLogEventPageSnapshot
  mcpCalls?: McpCall[]
}

function key(runId: string, nodeId: string): string {
  return `${runId}\0${nodeId}`
}

/** Module-level cache — survives RunDetailView hard remount within one SPA visit. */
const store = new Map<string, LiveLogNodeSnapshot>()

export function getLiveLogSnapshot(runId: string, nodeId: string): LiveLogNodeSnapshot | undefined {
  return store.get(key(runId, nodeId))
}

export function putLiveLogEventPage(
  runId: string,
  nodeId: string,
  page: LiveLogEventPageSnapshot,
): void {
  if (!runId || !nodeId) return
  const prev = store.get(key(runId, nodeId)) || {}
  // Non-empty timelines must not be overwritten by an empty page (busy-only WS).
  if (!page.events.length && (prev.eventPage?.events.length ?? 0) > 0) return
  store.set(key(runId, nodeId), {
    ...prev,
    eventPage: {
      events: page.events.slice(),
      nextCursor: page.nextCursor,
      hasMore: page.hasMore,
      live: page.live,
    },
  })
}

export function putLiveLogMcpCalls(runId: string, nodeId: string, mcpCalls: McpCall[]): void {
  if (!runId || !nodeId || !mcpCalls.length) return
  const prev = store.get(key(runId, nodeId)) || {}
  store.set(key(runId, nodeId), {
    ...prev,
    mcpCalls: mcpCalls.slice(),
  })
}

/** True when the snapshot has displayable timeline content. */
export function snapshotHasContent(snap: LiveLogNodeSnapshot | undefined): boolean {
  if (!snap) return false
  return !!(snap.eventPage?.events.length || snap.mcpCalls?.length)
}

/** List all cached node snapshots for a run (for restore after hard remount). */
export function listLiveLogSnapshotsForRun(
  runId: string,
): Array<{ nodeId: string; snapshot: LiveLogNodeSnapshot }> {
  const out: Array<{ nodeId: string; snapshot: LiveLogNodeSnapshot }> = []
  const prefix = `${runId}\0`
  for (const [k, snapshot] of store) {
    if (!k.startsWith(prefix)) continue
    const nodeId = k.slice(prefix.length)
    if (nodeId) out.push({ nodeId, snapshot })
  }
  return out
}

/** Drop every run except the one being viewed — prevents cross-run bleed. */
export function clearLiveLogSnapshotsExceptRun(keepRunId: string): void {
  const prefix = `${keepRunId}\0`
  for (const k of [...store.keys()]) {
    if (!k.startsWith(prefix)) store.delete(k)
  }
}

/** Test helper — wipe the whole session cache. */
export function clearAllLiveLogSnapshots(): void {
  store.clear()
}

/** Clone a page into reactive state shape (events copied). */
export function cloneEventPageSnapshot(
  page: LiveLogEventPageSnapshot,
): LiveLogEventPageSnapshot {
  return {
    events: page.events.slice() as MergedAcpEvent[],
    nextCursor: page.nextCursor,
    hasMore: page.hasMore,
    live: page.live,
  }
}
