/**
 * Busy-guarded periodic seed retry for hard-refresh / WS reconnect resume.
 * Keeps trying nodeEvents (or equivalent) while the session is still busy and
 * dialogue rails remain empty; stops on content, live incremental, or idle.
 */

type BusySeedRetryStopReason = 'content' | 'idle' | 'live' | 'aborted' | 'max_attempts'

type BusySeedRetryOptions = {
  /** Authoritative busy (queue_state / reactSessions). */
  isBusy: () => boolean
  /** True once thought/message rails have visible content. */
  hasContent: () => boolean
  /** True after a live WS ACP frame applied content (optional). */
  liveIncrementalReceived?: () => boolean
  /**
   * Attempt one seed. Resolve true when content was obtained and applied.
   * Empty / soft-fail / network errors should resolve false (not throw).
   */
  seed: () => Promise<boolean>
  /** Delay between attempts (default 1500ms). */
  intervalMs?: number
  /** Safety cap (default 40 ≈ 60s at 1.5s). */
  maxAttempts?: number
  signal?: AbortSignal
  /** Optional scheduler hooks for tests. */
  schedule?: (fn: () => void, ms: number) => ReturnType<typeof setTimeout>
  clearSchedule?: (id: ReturnType<typeof setTimeout>) => void
}

/**
 * Run seed immediately, then retry while busy && !hasContent && !live.
 * Does not change placeholder copy — callers keep showing「思考中…」.
 */
export async function runBusySeedRetry(
  opts: BusySeedRetryOptions,
): Promise<BusySeedRetryStopReason> {
  const intervalMs = opts.intervalMs ?? 1500
  const maxAttempts = opts.maxAttempts ?? 40
  const schedule = opts.schedule ?? ((fn, ms) => setTimeout(fn, ms))
  const clearSchedule = opts.clearSchedule ?? ((id) => clearTimeout(id))

  if (opts.signal?.aborted) return 'aborted'
  if (!opts.isBusy()) return 'idle'
  if (opts.hasContent()) return 'content'
  if (opts.liveIncrementalReceived?.()) return 'live'

  let attempts = 0
  while (attempts < maxAttempts) {
    if (opts.signal?.aborted) return 'aborted'
    if (!opts.isBusy()) return 'idle'
    if (opts.hasContent()) return 'content'
    if (opts.liveIncrementalReceived?.()) return 'live'

    attempts += 1
    let got = false
    try {
      got = await opts.seed()
    } catch {
      got = false
    }
    if (opts.signal?.aborted) return 'aborted'
    if (got || opts.hasContent()) return 'content'
    if (opts.liveIncrementalReceived?.()) return 'live'
    if (!opts.isBusy()) return 'idle'
    if (attempts >= maxAttempts) return 'max_attempts'

    await new Promise<void>((resolve) => {
      const id = schedule(() => resolve(), intervalMs)
      const onAbort = () => {
        clearSchedule(id)
        resolve()
      }
      if (opts.signal) {
        if (opts.signal.aborted) {
          clearSchedule(id)
          resolve()
          return
        }
        opts.signal.addEventListener('abort', onAbort, { once: true })
      }
    })
  }
  return 'max_attempts'
}

/** Cancel handle for view-owned busy seed loops. */
export function createBusySeedRetryController() {
  let ctrl: AbortController | null = null

  return {
    /** Abort any in-flight loop and start a new one. */
    start(run: (signal: AbortSignal) => Promise<unknown>) {
      ctrl?.abort()
      ctrl = new AbortController()
      const signal = ctrl.signal
      void run(signal)
      return signal
    },
    stop() {
      ctrl?.abort()
      ctrl = null
    },
    get aborted() {
      return !ctrl || ctrl.signal.aborted
    },
  }
}
