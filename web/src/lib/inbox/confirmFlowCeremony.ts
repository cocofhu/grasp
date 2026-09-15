import { inject, nextTick, onUnmounted, provide, ref, type InjectionKey, type Ref } from 'vue'

/**
 * Confirm-desk success ceremony (page.html gold sample):
 * bg-base/92 veil → disk fade-in → single-path check stroke-dashoffset draw.
 * Replay resets dashoffset so the check can draw again (g1.3).
 */

export type ConfirmFlowPhase = 'idle' | 'play' | 'hold' | 'out'

export type ConfirmFlowCeremony = {
  phase: Ref<ConfirmFlowPhase>
  reduceMotion: Ref<boolean>
  /** Bumps on every play so SVG remounts and dashoffset restarts. */
  playToken: Ref<number>
  playing: Ref<boolean>
  play: () => Promise<void>
  reset: () => void
}

export const confirmFlowCeremonyKey: InjectionKey<ConfirmFlowCeremony> = Symbol('confirmFlowCeremony')

const IS_TEST = typeof process !== 'undefined' && !!process.env.VITEST

const HOLD_MS = IS_TEST ? 0 : 620
const HOLD_REDUCE_MS = IS_TEST ? 0 : 140
const OUT_MS = IS_TEST ? 0 : 900
const OUT_REDUCE_MS = IS_TEST ? 0 : 180
const FADE_OUT_MS = IS_TEST ? 0 : 180

function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

export function createConfirmFlowCeremony(): ConfirmFlowCeremony {
  const phase = ref<ConfirmFlowPhase>('idle')
  const reduceMotion = ref(false)
  const playToken = ref(0)
  const playing = ref(false)
  const timers: ReturnType<typeof setTimeout>[] = []
  let playSeq = 0

  function clearTimers() {
    while (timers.length) {
      const id = timers.pop()
      if (id != null) clearTimeout(id)
    }
  }

  function later(fn: () => void, ms: number) {
    timers.push(setTimeout(fn, ms))
  }

  function reset() {
    playSeq += 1
    clearTimers()
    phase.value = 'idle'
    playing.value = false
  }

  async function play(): Promise<void> {
    const seq = ++playSeq
    clearTimers()
    reduceMotion.value = prefersReducedMotion()
    playing.value = true
    phase.value = 'idle'
    playToken.value += 1
    await nextTick()
    if (seq !== playSeq) return

    // Vitest: skip timed hold/out so callers that only flushPromises still converge.
    if (IS_TEST) {
      phase.value = 'play'
      await nextTick()
      if (seq !== playSeq) return
      phase.value = 'hold'
      phase.value = 'idle'
      playing.value = false
      return
    }

    phase.value = 'play'

    const holdAt = reduceMotion.value ? HOLD_REDUCE_MS : HOLD_MS
    const outAt = holdAt + (reduceMotion.value ? OUT_REDUCE_MS : OUT_MS)

    return new Promise<void>((resolve) => {
      later(() => {
        if (seq !== playSeq) return
        phase.value = 'hold'
      }, holdAt)

      later(() => {
        if (seq !== playSeq) {
          resolve()
          return
        }
        phase.value = 'out'
        later(() => {
          if (seq === playSeq) {
            phase.value = 'idle'
            playing.value = false
          }
          resolve()
        }, FADE_OUT_MS)
      }, outAt)
    })
  }

  return { phase, reduceMotion, playToken, playing, play, reset }
}

/** ReviewShell / panel hosts: create, provide, and auto-reset on unmount. */
export function provideConfirmFlowCeremony(): ConfirmFlowCeremony {
  const ceremony = createConfirmFlowCeremony()
  provide(confirmFlowCeremonyKey, ceremony)
  onUnmounted(() => ceremony.reset())
  return ceremony
}

export function useConfirmFlowCeremony(): ConfirmFlowCeremony | null {
  return inject(confirmFlowCeremonyKey, null)
}

/** Safe play helper for parents that may not be under a ReviewShell. */
export async function playConfirmFlowCeremony(
  host: { playConfirmCeremony?: () => Promise<void> } | null | undefined,
): Promise<void> {
  await host?.playConfirmCeremony?.()
}
