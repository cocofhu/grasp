import { ref, type Ref } from 'vue'

const WINDOW_MS = 1000

/** Minimal RFB shape for flip hooking (noVNC 1.5.0 private API). */
interface RfbWithDisplay {
  _display?: {
    flip?: (...args: unknown[]) => unknown
  }
}

interface PreviewFpsCounter {
  fps: Ref<number>
  hasRecentFrames: Ref<boolean>
  attach(rfb: RfbWithDisplay): void
  detach(): void
  reset(): void
}

export function createPreviewFpsCounter(): PreviewFpsCounter {
  const fps = ref(0)
  const hasRecentFrames = ref(false)

  let flipTimestamps: number[] = []
  let intervalId: ReturnType<typeof setInterval> | null = null
  let attachedDisplay: { flip?: (...args: unknown[]) => unknown } | null = null
  let origFlip: ((...args: unknown[]) => unknown) | null = null
  let wrapper: ((...args: unknown[]) => unknown) | null = null

  function now() {
    return Date.now()
  }

  function pruneAndUpdate() {
    const t = now()
    flipTimestamps = flipTimestamps.filter((ts) => t - ts <= WINDOW_MS)
    fps.value = flipTimestamps.length
    hasRecentFrames.value = flipTimestamps.length > 0
  }

  function startInterval() {
    stopInterval()
    intervalId = setInterval(pruneAndUpdate, WINDOW_MS)
  }

  function stopInterval() {
    if (intervalId !== null) {
      clearInterval(intervalId)
      intervalId = null
    }
  }

  function attach(rfb: RfbWithDisplay) {
    detach()
    const display = rfb._display
    if (!display?.flip) return

    attachedDisplay = display
    origFlip = display.flip
    flipTimestamps = []

    wrapper = (...args: unknown[]) => {
      flipTimestamps.push(now())
      return origFlip!.apply(display, args)
    }
    display.flip = wrapper

    startInterval()
  }

  function detach() {
    stopInterval()
    if (attachedDisplay && origFlip && wrapper && attachedDisplay.flip === wrapper) {
      attachedDisplay.flip = origFlip
    }
    attachedDisplay = null
    origFlip = null
    wrapper = null
  }

  function reset() {
    flipTimestamps = []
    fps.value = 0
    hasRecentFrames.value = false
  }

  return { fps, hasRecentFrames, attach, detach, reset }
}
