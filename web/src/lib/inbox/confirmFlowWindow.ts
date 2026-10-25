import { onBeforeUnmount, ref, watch, type WatchSource } from 'vue'

/**
 * True while a confirm-flow window is actually on screen, including the
 * 「已流转」hold after the open flag has already been cleared.
 */
export const confirmFlowWindowVisible = ref(false)

const tokens = new Set<symbol>()
let onHidden: (() => void) | null = null

/** Sidebar (and anything else) runs this once the last window has left. */
export function onConfirmFlowWindowHidden(fn: () => void) {
  onHidden = fn
}

function publish() {
  const visible = tokens.size > 0
  const was = confirmFlowWindowVisible.value
  confirmFlowWindowVisible.value = visible
  if (was && !visible) onHidden?.()
}

/** Call from ConfirmFlowDialog setup. `shown` is the flag that tracks the real window. */
export function bindConfirmFlowWindow(shown: WatchSource<boolean>) {
  const token = Symbol('confirm-flow-window')
  watch(
    shown,
    (value) => {
      if (value) tokens.add(token)
      else tokens.delete(token)
      publish()
    },
    { immediate: true },
  )
  onBeforeUnmount(() => {
    if (!tokens.delete(token)) return
    publish()
  })
}

export function __resetConfirmFlowWindowForTests() {
  tokens.clear()
  const was = confirmFlowWindowVisible.value
  confirmFlowWindowVisible.value = false
  if (was) onHidden?.()
}
