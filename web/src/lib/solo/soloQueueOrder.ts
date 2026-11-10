import { computed, ref, type Ref } from 'vue'
import type { SoloChatTransport, SoloQueueItem } from './useSoloChat'

type SoloQueueOrderContext = {
  sessionId: Ref<string>
  queued: Ref<SoloQueueItem[]>
  transport: Pick<SoloChatTransport, 'reorderSoloQueue'>
  /** True while the session and chat epoch captured at request time are still shown. */
  isCurrent: (id: string, epoch: number) => boolean
  epoch: () => number
}

/**
 * A drag only reorders the display. `queued` stays the server order, which
 * output arriving before turn_begin is attributed to, until queue_state.
 */
export function createSoloQueueOrder(ctx: SoloQueueOrderContext) {
  const overlay = ref<string[] | null>(null)
  let seq = 0

  const displayQueued = computed(() => {
    const order = overlay.value
    if (!order) return ctx.queued.value
    const rank = new Map(order.map((id, i) => [id, i]))
    const server = ctx.queued.value.filter((item) => !item.local)
    const ranked = server.filter((item) => rank.has(item.id)).sort((a, b) => rank.get(a.id)! - rank.get(b.id)!)
    return [...ranked, ...server.filter((item) => !rank.has(item.id)), ...ctx.queued.value.filter((item) => item.local)]
  })

  function clear() {
    overlay.value = null
  }

  /** Indices are positions in displayQueued; unacknowledged local items never move. */
  async function reorderQueued(fromIndex: number, toIndex: number) {
    const id = ctx.sessionId.value
    const epoch = ctx.epoch()
    const shown = displayQueued.value
    const moved = shown[fromIndex]
    const target = shown[toIndex]
    if (!id || fromIndex === toIndex || !moved || !target || moved.local || target.local) return
    const ids = shown.filter((item) => !item.local).map((item) => item.id)
    const to = ids.indexOf(target.id)
    ids.splice(ids.indexOf(moved.id), 1)
    ids.splice(to, 0, moved.id)
    const request = ++seq
    overlay.value = ids
    try {
      await ctx.transport.reorderSoloQueue(id, ids)
    } catch {
      // The server order stays authoritative; a newer queue_state may already be shown.
      if (!ctx.isCurrent(id, epoch)) return
      if (request === seq) overlay.value = null
    }
  }

  return { displayQueued, clear, reorderQueued }
}
