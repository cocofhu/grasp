/**
 * Pending-send queue reconciliation shared by every agent chat surface. The
 * server FIFO (chatsession) is authoritative; the client keeps optimistic rows
 * only until the next queue_state / turn_begin names them.
 */
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

export type ReviewStateVersion = {
  nodeId?: string
  sessionGeneration?: number
  revision?: number
}

/** Compare server state, never network arrival order or message text. */
export function createReviewStateCursor() {
  const versions = new Map<string, { generation: number; revision: number }>()
  let generationFloor = -1
  let snapshotGenerations = new Map<string, number>()
  return {
    get hasVersion() {
      return versions.size > 0 || generationFloor >= 0
    },
    accept(frame: ReviewStateVersion | Record<string, unknown>): boolean {
      const generation = frame.sessionGeneration
      const revision = frame.revision
      // Older servers and local optimistic frames have no server version.
      if (typeof generation !== 'number' || typeof revision !== 'number' ||
        !Number.isSafeInteger(generation) || !Number.isSafeInteger(revision) ||
        generation < 0 || revision < 0) return true
      const key = typeof frame.nodeId === 'string' ? frame.nodeId : ''
      const previous = versions.get(key)
      if (generation <= generationFloor && previous?.generation !== generation && snapshotGenerations.get(key) !== generation) return false
      if (previous && (generation < previous.generation ||
        (generation === previous.generation && revision < previous.revision))) return false
      versions.set(key, { generation, revision })
      return true
    },
    acceptServer(frame: (ReviewStateVersion & { event?: string }) | Record<string, unknown>): boolean {
      const key = typeof frame.nodeId === 'string' ? frame.nodeId : ''
      const event = typeof frame.event === 'string' ? frame.event : ''
      if (versions.has(key) && ['queue_state', 'turn_begin', 'turn_done', 'error'].includes(event) &&
        (typeof frame.sessionGeneration !== 'number' || typeof frame.revision !== 'number')) return false
      return this.accept(frame)
    },
    /** Check before installSnapshot: an omitted FIFO is idle only when the
     * capture watermark covers the session this viewer has already seen. */
    acceptSnapshot(nodeId: string, session?: ReviewStateVersion, watermark?: number): boolean {
      if (session) return this.acceptServer({ ...session, nodeId, event: 'queue_state' })
      const previous = versions.get(nodeId)
      return !previous || (typeof watermark === 'number' && Number.isSafeInteger(watermark) &&
        watermark >= previous.generation)
    },
    installSnapshot(sessions: Record<string, ReviewStateVersion | undefined>, watermark?: number) {
      if (typeof watermark !== 'number' || !Number.isSafeInteger(watermark) || watermark < 0) return
      generationFloor = Math.max(generationFloor, watermark)
      snapshotGenerations = new Map(Object.entries(sessions).flatMap(([nodeId, state]) =>
        typeof state?.sessionGeneration === 'number' ? [[nodeId, state.sessionGeneration] as const] : []))
      for (const [nodeId, state] of versions) {
        if (!(nodeId in sessions) && state.generation <= watermark) versions.delete(nodeId)
      }
      for (const [nodeId, state] of Object.entries(sessions)) {
        if (state) this.accept({ ...state, nodeId })
      }
    },
    reset() {
      versions.clear()
      generationFloor = -1
      snapshotGenerations.clear()
    },
  }
}

export type SessionQueueItem = {
  /** Server item id; absent on an optimistic row not yet acknowledged. */
  id?: string
  /** Stable sender identity; distinct tabs may send identical payloads. */
  clientMessageId?: string
  text: string
  images: ClarifyImage[]
  annotations: ReactAnnotation[]
}

/** Item shape in queue_state.items, queue_state.activeItem and turn_begin.item. */
export type SessionFrameItem = {
  id?: string
  clientMessageId?: string
  text?: string
  images?: ClarifyImage[]
  annotations?: ReactAnnotation[]
}

/** Element-level clone so queue rows never share annotation refs with the composer. */
export function cloneAnnotations(anns?: ReactAnnotation[] | null): ReactAnnotation[] {
  if (!anns?.length) return []
  return anns.map((a) => ({ ...a }))
}

/** Element-level clone for attachment lists (same contract as annotations). */
export function cloneImages(imgs?: ClarifyImage[] | null): ClarifyImage[] {
  if (!imgs?.length) return []
  return imgs.map((im) => ({ ...im }))
}

function frameId(it: SessionFrameItem | null | undefined): string | undefined {
  return typeof it?.id === 'string' && it.id ? it.id : undefined
}

/** waiting=0 ∧ !busy ∧ no activeItem: nothing runs or waits on the server. */
export function isAuthoritativeIdle(
  waiting: number,
  busy: boolean | undefined,
  activeItem: SessionFrameItem | null | undefined,
): boolean {
  return waiting === 0 && !busy && !activeItem
}

/**
 * Rebuild the local queue from queue_state.items. Rows match by server id,
 * then by text for optimistic rows; frame attachments win over local ones.
 * With no turn in flight one optimistic row may stay ahead of the server
 * (HTTP ack / turn_begin still on the way).
 */
export function reconcileQueue(
  local: SessionQueueItem[],
  items: SessionFrameItem[],
  inFlight: boolean,
): SessionQueueItem[] {
  const rebuilt: SessionQueueItem[] = items.map((it) => {
    const text = it.text ?? ''
    const id = frameId(it)
    const pendingMatch = local.find((q) => !q.id && (it.clientMessageId
      ? q.clientMessageId === it.clientMessageId
      : q.text === text))
    const match = id ? local.find((q) => q.id === id) ?? pendingMatch : pendingMatch
    return {
      id: id ?? match?.id,
      ...(it.clientMessageId || match?.clientMessageId
        ? { clientMessageId: it.clientMessageId ?? match?.clientMessageId } : {}),
      text,
      images: Array.isArray(it.images) ? cloneImages(it.images) : cloneImages(match?.images),
      annotations: Array.isArray(it.annotations)
        ? cloneAnnotations(it.annotations)
        : cloneAnnotations(match?.annotations),
    }
  })
  const maxLocal = inFlight ? rebuilt.length : rebuilt.length + 1
  if (local.length > maxLocal) {
    const optimistic = local.slice(rebuilt.length).slice(0, Math.max(0, maxLocal - rebuilt.length))
    return [...rebuilt, ...optimistic]
  }
  if (local.length < rebuilt.length) return rebuilt
  return [...rebuilt, ...local.slice(rebuilt.length)]
}

/**
 * turn_begin names the item that just started: remove it from the local
 * queue by id, or by text when the server sent no id. An id that is already
 * gone never falls back to text, which would steal a same-text waiter.
 */
export function takeTurnBeginItem(
  local: SessionQueueItem[],
  item: SessionFrameItem | null | undefined,
): { queue: SessionQueueItem[]; taken?: SessionQueueItem } {
  const id = frameId(item)
  const text = item?.text ?? ''
  let idx = -1
  if (id) idx = local.findIndex((q) => q.id === id)
  else if (text) idx = local.findIndex((q) => q.text === text)
  if (idx < 0) return { queue: local }
  return { queue: [...local.slice(0, idx), ...local.slice(idx + 1)], taken: local[idx] }
}

/**
 * After turn_done / error drop only optimistic rows; server-acknowledged
 * waiters stay until the next authoritative queue_state. Returns local itself
 * when nothing changed.
 */
export function dropGhostItems(local: SessionQueueItem[]): SessionQueueItem[] {
  return local.some((q) => !q.id) ? local.filter((q) => !!q.id) : local
}
