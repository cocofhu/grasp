import { onBeforeUnmount, onMounted } from 'vue'

const hidden = () => typeof document !== 'undefined' && document.visibilityState === 'hidden'

/**
 * Calls fn every intervalMs while the tab is visible, and once as soon as it
 * becomes visible again. Returns a stop function.
 */
export function startVisiblePolling(fn: () => unknown, intervalMs: number): () => void {
  const timer = setInterval(() => {
    if (!hidden()) void fn()
  }, intervalMs)
  const onVisible = () => {
    if (!hidden()) void fn()
  }
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisible)
  return () => {
    clearInterval(timer)
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisible)
  }
}

/** Loads on mount, then polls while visible until the component unmounts. */
export function useVisiblePolling(fn: () => unknown, intervalMs: number) {
  let stop: (() => void) | undefined
  onMounted(() => {
    void fn()
    stop = startVisiblePolling(fn, intervalMs)
  })
  onBeforeUnmount(() => stop?.())
}
