import { computed, ref, type ComputedRef, type Ref } from 'vue'
import { inboxTripleKey } from '@/lib/inbox/inboxActiveSelection'
import type { InboxItem } from '@/lib/shared/types'

type Triple = Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'>

type InboxProcessingState = {
  /** Triples that left pending — never softRefresh/load inbox-context for these. */
  processedTriples: Set<string>
  /** Processed triples observed absent from a subsequent list load (safe to unmark if they reappear). */
  confirmedAbsentTriples: Set<string>
  /**
   * In-flight confirm intents (triple keys). Non-empty ⇒ list reselection / refresh
   * stay gated (race with lagging list). Force confirm is gated per-item only so a
   * neighbor can confirm while an earlier reactReply/resume is still pending.
   */
  processingIntentKeys: Ref<Set<string>>
  processingLock: ComputedRef<boolean>
  addProcessingIntent: (triple: string) => void
  removeProcessingIntent: (triple: string) => void
  isProcessingIntent: (it: Triple | null | undefined) => boolean
  /** Per-item card/select gate — not the global processingLock (plan g1.2). */
  isItemCardDisabled: (it: InboxItem) => boolean
  markProcessed: (it: Triple) => void
  unmarkProcessed: (it: Triple) => void
  isProcessedTriple: (it: Triple | null | undefined) => boolean
  /**
   * Allow re-fetch only after a processed triple was confirmed gone, then reappears as pending.
   * Avoids clearing the mark on the immediate lagging loadList that still contains the item.
   */
  reconcileProcessedWithList: (list: InboxItem[]) => void
}

/** Approval bookkeeping: which triples were acted on and which confirms are still in flight. */
export function createInboxProcessingState(): InboxProcessingState {
  const processedTriples = new Set<string>()
  const confirmedAbsentTriples = new Set<string>()
  const processingIntentKeys = ref(new Set<string>())
  const processingLock = computed(() => processingIntentKeys.value.size > 0)

  function addProcessingIntent(triple: string) {
    const next = new Set(processingIntentKeys.value)
    next.add(triple)
    processingIntentKeys.value = next
  }

  function removeProcessingIntent(triple: string) {
    if (!processingIntentKeys.value.has(triple)) return
    const next = new Set(processingIntentKeys.value)
    next.delete(triple)
    processingIntentKeys.value = next
  }

  function isProcessingIntent(it: Triple | null | undefined) {
    if (!it) return false
    return processingIntentKeys.value.has(inboxTripleKey(it))
  }

  function isItemCardDisabled(it: InboxItem) {
    return isProcessingIntent(it) || isProcessedTriple(it)
  }

  function markProcessed(it: Triple) {
    processedTriples.add(inboxTripleKey(it))
  }

  function unmarkProcessed(it: Triple) {
    const key = inboxTripleKey(it)
    processedTriples.delete(key)
    confirmedAbsentTriples.delete(key)
  }

  function isProcessedTriple(it: Triple | null | undefined) {
    if (!it) return false
    return processedTriples.has(inboxTripleKey(it))
  }

  function reconcileProcessedWithList(list: InboxItem[]) {
    const pending = new Set(list.map((it) => inboxTripleKey(it)))
    for (const key of [...processedTriples]) {
      if (!pending.has(key)) {
        confirmedAbsentTriples.add(key)
        continue
      }
      if (confirmedAbsentTriples.has(key)) {
        processedTriples.delete(key)
        confirmedAbsentTriples.delete(key)
      }
    }
  }

  return {
    processedTriples,
    confirmedAbsentTriples,
    processingIntentKeys,
    processingLock,
    addProcessingIntent,
    removeProcessingIntent,
    isProcessingIntent,
    isItemCardDisabled,
    markProcessed,
    unmarkProcessed,
    isProcessedTriple,
    reconcileProcessedWithList,
  }
}

type InboxContextFlight = {
  /** Per-triple AbortController for in-flight inbox-context fetches. */
  aborts: Map<string, AbortController>
  abort: (triple: string) => void
  /** Abort every in-flight fetch (page teardown). */
  abortAll: () => void
  /**
   * Start a fetch for triple: null while one is already in flight for it; switching
   * triples aborts the previous one.
   */
  acquire: (triple: string) => AbortSignal | null
  /**
   * End the fetch that owns signal. A terminal frame retained during that flight
   * runs onTrailingRefresh once, after the owner's own cleanup.
   */
  release: (triple: string, signal: AbortSignal) => void
  /** A terminal frame arrived while triple's fetch was in flight: refresh once after it. */
  retainTrailingRefresh: (triple: string) => void
}

/** Single-flight inbox-context fetches for the selected triple. */
export function createInboxContextFlight(onTrailingRefresh: (triple: string) => void): InboxContextFlight {
  const aborts = new Map<string, AbortController>()
  /** Single-flight: one softRefresh/loadActiveRun at a time for the selected triple. */
  let inFlight: string | null = null
  /** Terminal frames received during that flight retain one coalesced trailing refresh. */
  const trailingRefreshes = new Set<string>()

  function abort(triple: string) {
    trailingRefreshes.delete(triple)
    const ctrl = aborts.get(triple)
    if (!ctrl) return
    ctrl.abort()
    aborts.delete(triple)
    if (inFlight === triple) inFlight = null
  }

  function abortAll() {
    for (const triple of [...aborts.keys()]) abort(triple)
  }

  function acquire(triple: string): AbortSignal | null {
    // Single-flight: drop duplicate softRefresh/load while one request is in flight.
    if (inFlight === triple) return null
    // Switching selection: abort the previous triple's in-flight request.
    if (inFlight && inFlight !== triple) {
      abort(inFlight)
    }
    abort(triple)
    const ctrl = new AbortController()
    aborts.set(triple, ctrl)
    inFlight = triple
    return ctrl.signal
  }

  function release(triple: string, signal: AbortSignal) {
    const ctrl = aborts.get(triple)
    const ownsFlight = ctrl?.signal === signal && inFlight === triple
    if (ctrl?.signal === signal) aborts.delete(triple)
    if (ownsFlight) inFlight = null
    if (!ownsFlight || signal.aborted || !trailingRefreshes.delete(triple)) return
    // Run after the owner's finally finishes its loading cleanup. The selected
    // triple may have moved or been processed while the request was in flight.
    queueMicrotask(() => onTrailingRefresh(triple))
  }

  function retainTrailingRefresh(triple: string) {
    if (inFlight === triple) trailingRefreshes.add(triple)
  }

  return { aborts, abort, abortAll, acquire, release, retainTrailingRefresh }
}
