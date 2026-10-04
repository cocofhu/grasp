/**
 * Pending-send queue reconciliation shared by every agent chat surface. The
 * server FIFO (chatsession) is authoritative; the client keeps optimistic rows
 * only until the next queue_state / turn_begin names them.
 */
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

export type SessionQueueItem = {
  /** Server item id; absent on an optimistic row not yet acknowledged. */
  id?: string
  text: string
  images: ClarifyImage[]
  annotations: ReactAnnotation[]
}

/** Item shape in queue_state.items, queue_state.activeItem and turn_begin.item. */
export type SessionFrameItem = {
  id?: string
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
    const match = id
      ? local.find((q) => q.id === id) ?? local.find((q) => !q.id && q.text === text)
      : local.find((q) => q.text === text)
    return {
      id: id ?? match?.id,
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
