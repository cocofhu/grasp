import { inject, onScopeDispose, provide, shallowRef, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { api } from '@/lib/api/api'
import { LIVE_CARD_HOST, createLiveStore, type LiveCardHost, type LiveStoreApi } from '@/lib/inbox/liveVariants'

/**
 * Read-only Live cards for the main app chat. Each run node's sessions load
 * once when a chat shows it and follow the run events socket (`live` frames)
 * afterwards. Cards here never drive the preview page.
 */

type Entry = {
  key: string
  runId: string
  nodeId: string
  live: LiveStoreApi
  users: number
  inflight: Promise<void> | null
  again: boolean
  /** Sessions a card already asked for, so a missing one costs one fetch. */
  asked: Set<string>
}

/** Share pages have no login session; their drawer brings its own host. */
const PUBLIC_SHARE_RUN_ID = 'public-share'

const entries = new Map<string, Entry>()

const keyOf = (runId: string, nodeId: string) => `${runId}:${nodeId}`

function load(entry: Entry): Promise<void> {
  if (entry.inflight) {
    entry.again = true
    return entry.inflight
  }
  entry.inflight = (async () => {
    do {
      entry.again = false
      try {
        const res = await api.liveSessions(entry.runId, entry.nodeId)
        if (entries.get(entry.key) !== entry) return
        entry.live.setEnabled(res?.enabled === true)
        // Merge rather than replace: a frame newer than this response may already be in.
        for (const raw of Array.isArray(res?.sessions) ? res.sessions : []) entry.live.apply(raw)
      } catch {
        // Cards fall back to what the message itself carries.
      }
    } while (entry.again && entries.get(entry.key) === entry)
  })().finally(() => {
    entry.inflight = null
  })
  return entry.inflight
}

function acquire(runId: string, nodeId: string): Entry {
  const key = keyOf(runId, nodeId)
  const found = entries.get(key)
  if (found) {
    found.users++
    return found
  }
  const entry: Entry = { key, runId, nodeId, live: createLiveStore(), users: 1, inflight: null, again: false, asked: new Set() }
  entries.set(key, entry)
  void load(entry)
  return entry
}

function release(entry: Entry) {
  entry.users--
  if (entry.users <= 0 && entries.get(entry.key) === entry) entries.delete(entry.key)
}

/** Feed one run events frame; true when it was a `live` frame for a node on screen. */
export function applyRunLiveFrame(frame: unknown): boolean {
  if (!frame || typeof frame !== 'object') return false
  const m = frame as { type?: unknown; runId?: unknown; nodeId?: unknown; session?: unknown }
  if (m.type !== 'live' || typeof m.runId !== 'string' || typeof m.nodeId !== 'string') return false
  const entry = entries.get(keyOf(m.runId, m.nodeId))
  return !!entry && !!entry.live.apply(m.session)
}

/** Reload every node of the run on screen (after a socket reconnect). */
export function refreshRunLive(runId: string): Promise<void> {
  const loads: Promise<void>[] = []
  for (const entry of entries.values()) if (entry.runId === runId) loads.push(load(entry))
  return Promise.all(loads).then(() => undefined)
}

/**
 * Provide a read-only LIVE_CARD_HOST for the ClarifyChat below. Must run in a
 * parent of ClarifyChat. A host already provided above (the preview drawer)
 * wins and is returned as is.
 */
export function useRunLiveCardHost(runId: MaybeRefOrGetter<string | undefined>, nodeId: MaybeRefOrGetter<string | undefined>): LiveCardHost | null {
  const existing = inject(LIVE_CARD_HOST, null)
  if (existing) return existing
  if (toValue(runId) === PUBLIC_SHARE_RUN_ID) return null

  const empty = createLiveStore()
  const current = shallowRef<Entry | null>(null)
  const host: LiveCardHost = {
    get store() {
      return (current.value?.live ?? empty).store
    },
    interactive: false,
    command: () => {},
    ensure(sid: string) {
      const entry = current.value
      if (!entry || entry.live.store.sessions[sid] || entry.asked.has(sid)) return
      entry.asked.add(sid)
      void load(entry)
    },
  }
  provide(LIVE_CARD_HOST, host)

  watch(
    () => [toValue(runId) || '', toValue(nodeId) || ''] as const,
    ([r, n]) => {
      const prev = current.value
      current.value = r && n ? acquire(r, n) : null
      if (prev) release(prev)
    },
    { immediate: true },
  )
  onScopeDispose(() => {
    if (current.value) release(current.value)
    current.value = null
  })
  return host
}
