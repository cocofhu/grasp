import { computed, getCurrentScope, onScopeDispose, ref, watch } from 'vue'
import type { WFEdge, WFNode } from '@/lib/shared/types'

export const HISTORY_LIMIT = 100
export const COALESCE_MS = 500

export interface GraphSnapshotTarget {
  nodes: WFNode[]
  edges: WFEdge[]
}

export interface CanvasHistoryOptions {
  limit?: number
  coalesceMs?: number
}

function serialize(g: GraphSnapshotTarget): string {
  return JSON.stringify({ nodes: g.nodes, edges: g.edges })
}

/**
 * Snapshot undo stack over a reactive graph.
 *
 * - `commit()` records a discrete step right away (add, delete, connect, layout, drag end).
 * - Any other change (typing in the inspector) is coalesced: it is recorded after
 *   `coalesceMs` without further changes, as one step.
 * - Between `begin()` and `end()` (a drag) changes are not recorded; `end()` records one step.
 */
export function useCanvasHistory(graph: GraphSnapshotTarget, opts: CanvasHistoryOptions = {}) {
  const limit = opts.limit ?? HISTORY_LIMIT
  const delay = opts.coalesceMs ?? COALESCE_MS
  const stack = ref<string[]>([serialize(graph)])
  const index = ref(0)
  let batching = 0
  let applying = false
  let timer: ReturnType<typeof setTimeout> | null = null

  function clearTimer() {
    if (timer) clearTimeout(timer)
    timer = null
  }

  function push(snap: string) {
    if (snap === stack.value[index.value]) return
    const next = stack.value.slice(0, index.value + 1)
    next.push(snap)
    while (next.length > limit + 1) next.shift()
    stack.value = next
    index.value = next.length - 1
  }

  /** Records any pending coalesced change now. */
  function flush() {
    clearTimer()
    if (!batching) push(serialize(graph))
  }

  function commit() {
    flush()
  }

  function begin() {
    flush()
    batching++
  }

  function end() {
    if (batching > 0) batching--
    if (!batching) flush()
  }

  function apply(snap: string) {
    const parsed = JSON.parse(snap) as GraphSnapshotTarget
    applying = true
    graph.nodes.splice(0, graph.nodes.length, ...parsed.nodes)
    graph.edges.splice(0, graph.edges.length, ...parsed.edges)
    queueMicrotask(() => {
      applying = false
    })
  }

  function undo(): boolean {
    flush()
    if (index.value <= 0) return false
    index.value--
    apply(stack.value[index.value]!)
    return true
  }

  function redo(): boolean {
    clearTimer()
    if (index.value >= stack.value.length - 1) return false
    index.value++
    apply(stack.value[index.value]!)
    return true
  }

  /** Drops all history; the current graph becomes the only step (after load / import). */
  function reset() {
    clearTimer()
    batching = 0
    stack.value = [serialize(graph)]
    index.value = 0
  }

  const stop = watch(
    () => serialize(graph),
    (snap) => {
      if (applying || batching) return
      if (snap === stack.value[index.value]) return
      clearTimer()
      timer = setTimeout(() => {
        timer = null
        if (!batching) push(serialize(graph))
      }, delay)
    },
  )

  if (getCurrentScope()) {
    onScopeDispose(() => {
      stop()
      clearTimer()
    })
  }

  return {
    canUndo: computed(() => index.value > 0),
    canRedo: computed(() => index.value < stack.value.length - 1),
    size: computed(() => stack.value.length - 1),
    commit,
    flush,
    begin,
    end,
    undo,
    redo,
    reset,
    stop: () => {
      stop()
      clearTimer()
    },
  }
}

export type CanvasHistory = ReturnType<typeof useCanvasHistory>
