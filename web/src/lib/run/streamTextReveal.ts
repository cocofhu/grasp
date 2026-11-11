/**
 * Smooth “catch-up” stream text reveal (Demo gold standard).
 * Maintains authoritative target vs revealed prefix; lags accelerate;
 * flush aligns immediately on turn end / interrupt / failure.
 *
 * Sits *before* streamMarkdownPreview: feed revealed prefixes into
 * markdown coalesce — never bind absolute snapshots straight to the DOM.
 */

type StreamTextRevealOptions = {
  /** Called whenever revealed text changes (may lag target). */
  onReveal?: (revealed: string) => void
  /** Inject for tests; defaults to requestAnimationFrame. */
  schedule?: (cb: () => void) => number
  cancel?: (id: number) => void
  /** Inter-tick delay after rAF (Demo default ~22ms). */
  delayMs?: number
  /** Inject setTimeout; defaults to global. */
  delay?: (cb: () => void, ms: number) => number
  clearDelay?: (id: number) => void
  /** prefers-reduced-motion; defaults to matchMedia. */
  prefersReducedMotion?: () => boolean
  /**
   * When true, setTarget reveals the full target immediately.
   * Used by Vitest component suites so mid-stream assertions stay stable;
   * production leaves this false for typewriter catch-up.
   */
  sync?: boolean
}

type StreamTextReveal = {
  /** Absolute authoritative snapshot (ACP resume / chunk). */
  setTarget: (text: string) => void
  getTarget: () => string
  getRevealed: () => string
  /** Start (or resume) the reveal loop if behind. */
  start: () => void
  /** Cancel pending timers without changing texts. */
  stop: () => void
  /** Force revealed === target and notify (turn_done / interrupt / error). */
  flush: () => void
  /** Clear target + revealed and stop. */
  reset: () => void
}

function defaultPrefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return false
  }
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

/** Demo lag → charsPerTick tiers (reduce ≈40). */
export function charsPerTickForLag(lag: number, reduce: boolean): number {
  if (reduce) return 40
  if (lag > 80) return 8
  if (lag > 40) return 5
  if (lag > 16) return 3
  return 2
}

export function createStreamTextReveal(opts: StreamTextRevealOptions = {}): StreamTextReveal {
  const schedule = opts.schedule ?? ((cb) => requestAnimationFrame(cb))
  const cancel = opts.cancel ?? ((id) => cancelAnimationFrame(id))
  const delayMs = opts.delayMs ?? 22
  const delay = opts.delay ?? ((cb, ms) => window.setTimeout(cb, ms) as unknown as number)
  const clearDelay = opts.clearDelay ?? ((id) => clearTimeout(id))
  const prefersReducedMotion = opts.prefersReducedMotion ?? defaultPrefersReducedMotion
  const sync = opts.sync === true

  let target = ''
  let revealed = ''
  let rafId = 0
  let timeoutId = 0
  let running = false

  function notify() {
    opts.onReveal?.(revealed)
  }

  function clearSchedulers() {
    if (rafId) {
      cancel(rafId)
      rafId = 0
    }
    if (timeoutId) {
      clearDelay(timeoutId)
      timeoutId = 0
    }
    running = false
  }

  function alignFull() {
    clearSchedulers()
    if (revealed !== target) {
      revealed = target
      notify()
    }
  }

  function tick() {
    rafId = 0
    if (revealed.length >= target.length) {
      running = false
      return
    }
    const reduce = prefersReducedMotion()
    const lag = target.length - revealed.length
    const n = charsPerTickForLag(lag, reduce)
    revealed = target.slice(0, revealed.length + n)
    notify()
    if (revealed.length < target.length) {
      const wait = reduce ? 0 : delayMs
      timeoutId = delay(() => {
        timeoutId = 0
        rafId = schedule(() => tick())
      }, wait)
    } else {
      running = false
    }
  }

  function start() {
    if (sync) {
      alignFull()
      return
    }
    if (revealed.length >= target.length) return
    if (running) return
    running = true
    rafId = schedule(() => tick())
  }

  return {
    setTarget(text: string) {
      target = text ?? ''
      // Keep revealed as a monotone prefix of target when possible.
      if (revealed && !target.startsWith(revealed)) {
        if (revealed.startsWith(target)) {
          // Target shrank (rare): clamp down.
          revealed = target
          notify()
          clearSchedulers()
          return
        }
        // Divergent absolute replace — restart from empty.
        revealed = ''
        notify()
        clearSchedulers()
      }
      if (sync) {
        alignFull()
        return
      }
      // reduced-motion: large strides via tick (charsPerTick≈40, delay 0).
      start()
    },
    getTarget: () => target,
    getRevealed: () => revealed,
    start,
    stop() {
      clearSchedulers()
    },
    flush() {
      clearSchedulers()
      revealed = target
      notify()
    },
    reset() {
      clearSchedulers()
      target = ''
      revealed = ''
      notify()
    },
  }
}
