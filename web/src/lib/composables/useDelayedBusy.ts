import { getCurrentInstance, onUnmounted, ref, type Ref } from 'vue'
import {
  DEFAULT_MIN_VISIBLE_MS,
  DEFAULT_SHOW_AFTER_MS,
  type LoadingMode,
} from '../shared/loadingTypes'

type DelayedBusyOptions = {
  mode?: LoadingMode | (() => LoadingMode) | Ref<LoadingMode>
  showAfterMs?: number
  minVisibleMs?: number
}

type DelayedBusyController = {
  busy: Ref<boolean>
  showUi: Ref<boolean>
  setBusy: (next: boolean) => void
  start: () => void
  stop: () => void
  reset: () => void
  dispose: () => void
}

function resolveMode(options: DelayedBusyOptions): LoadingMode {
  const m = options.mode
  if (!m) return 'initial'
  if (typeof m === 'function') return m()
  if (typeof m === 'object' && m && 'value' in m) return m.value
  return m
}

/**
 * Delay showing loading UI (`showAfter`) and keep it visible at least `minVisible`.
 * Thresholds default by mode; reduced-motion still honors this delay strategy.
 */
export function createDelayedBusy(options: DelayedBusyOptions = {}): DelayedBusyController {
  const busy = ref(false)
  const showUi = ref(false)
  let gen = 0
  let showTimer: ReturnType<typeof setTimeout> | null = null
  let minVisibleTimer: ReturnType<typeof setTimeout> | null = null
  let hideQueued = false
  let minVisibleReady = true

  function showAfter(): number {
    if (options.showAfterMs != null) return options.showAfterMs
    return DEFAULT_SHOW_AFTER_MS[resolveMode(options)]
  }

  function minVisible(): number {
    if (options.minVisibleMs != null) return options.minVisibleMs
    return DEFAULT_MIN_VISIBLE_MS[resolveMode(options)]
  }

  function clearTimers() {
    if (showTimer) {
      clearTimeout(showTimer)
      showTimer = null
    }
    if (minVisibleTimer) {
      clearTimeout(minVisibleTimer)
      minVisibleTimer = null
    }
  }

  function tryHide() {
    if (hideQueued && minVisibleReady && !busy.value) {
      showUi.value = false
      hideQueued = false
      clearTimers()
    }
  }

  function reveal(myGen: number) {
    if (myGen !== gen || !busy.value) return
    showUi.value = true
    const min = minVisible()
    minVisibleReady = min <= 0
    if (!minVisibleReady) {
      minVisibleTimer = setTimeout(() => {
        minVisibleReady = true
        tryHide()
      }, min)
    }
  }

  function setBusy(next: boolean) {
    if (next) {
      gen += 1
      const myGen = gen
      busy.value = true
      hideQueued = false
      if (showUi.value) {
        // Already visible (e.g. overlapping refresh): keep UI, restart minVisible clock.
        const min = minVisible()
        if (minVisibleTimer) {
          clearTimeout(minVisibleTimer)
          minVisibleTimer = null
        }
        minVisibleReady = min <= 0
        if (!minVisibleReady) {
          minVisibleTimer = setTimeout(() => {
            minVisibleReady = true
            tryHide()
          }, min)
        }
        return
      }
      clearTimers()
      const delay = showAfter()
      if (delay <= 0) {
        reveal(myGen)
      } else {
        showTimer = setTimeout(() => reveal(myGen), delay)
      }
      return
    }

    busy.value = false
    if (!showUi.value) {
      clearTimers()
      hideQueued = false
      minVisibleReady = true
      return
    }
    hideQueued = true
    tryHide()
  }

  function reset() {
    gen += 1
    clearTimers()
    busy.value = false
    showUi.value = false
    hideQueued = false
    minVisibleReady = true
  }

  return {
    busy,
    showUi,
    setBusy,
    start: () => setBusy(true),
    stop: () => setBusy(false),
    reset,
    dispose: reset,
  }
}

export function useDelayedBusy(options: DelayedBusyOptions = {}): DelayedBusyController {
  const ctrl = createDelayedBusy(options)
  if (getCurrentInstance()) {
    onUnmounted(() => ctrl.dispose())
  }
  return ctrl
}
