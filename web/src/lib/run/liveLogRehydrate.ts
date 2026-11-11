/** Independent of Boot's 120s stage timeout — covers REST timeline re-read hang. */
export const REHYDRATE_TIMEOUT_MS = 10_000

export type RehydrateStatus = 'idle' | 'loading' | 'ready' | 'error'

type NodeEventsFetchResult = 'ok' | 'error' | 'stale'

/** Boot "waiting for first event" is only valid after a successful rehydrate. */
export function allowBootEmptyState(rehydrate: RehydrateStatus | undefined | null): boolean {
  return rehydrate === 'ready'
}

export function isAbortError(err: unknown): boolean {
  if (typeof DOMException !== 'undefined' && err instanceof DOMException && err.name === 'AbortError') {
    return true
  }
  return err instanceof Error && err.name === 'AbortError'
}

/**
 * After a rehydrate fetch settles, decide whether this attempt may write a
 * terminal status. Stale generations / already-timed-out attempts return null.
 */
export function resolveRehydrateAfterFetch(
  status: RehydrateStatus,
  attemptGen: number,
  activeGen: number,
  result: NodeEventsFetchResult,
): RehydrateStatus | null {
  if (attemptGen !== activeGen) return null
  if (status !== 'loading') return null
  if (result === 'stale') return null
  return result === 'ok' ? 'ready' : 'error'
}

type RehydrateOrchestratorOptions = {
  timeoutMs?: number
  /** Resolve ok/error; AbortError (or aborted signal) is treated as stale. */
  fetch: (signal: AbortSignal) => Promise<'ok' | 'error'>
  onStatus?: (status: RehydrateStatus) => void
  schedule?: (fn: () => void, ms: number) => { clear: () => void }
}

/**
 * Per-node REST rehydrate state machine with generation + AbortController.
 * Guarantees an older in-flight attempt cannot flip a newer loading→error,
 * and timeout/retry cancel the previous request.
 */
export class RehydrateOrchestrator {
  private _status: RehydrateStatus = 'idle'
  private gen = 0
  private abort: AbortController | null = null
  private clearTimer: (() => void) | null = null
  private readonly timeoutMs: number
  private readonly fetchFn: (signal: AbortSignal) => Promise<'ok' | 'error'>
  private readonly onStatus?: (status: RehydrateStatus) => void
  private readonly schedule: (fn: () => void, ms: number) => { clear: () => void }

  constructor(opts: RehydrateOrchestratorOptions) {
    this.timeoutMs = opts.timeoutMs ?? REHYDRATE_TIMEOUT_MS
    this.fetchFn = opts.fetch
    this.onStatus = opts.onStatus
    this.schedule =
      opts.schedule ??
      ((fn, ms) => {
        const id = setTimeout(fn, ms)
        return { clear: () => clearTimeout(id) }
      })
  }

  get status(): RehydrateStatus {
    return this._status
  }

  private setStatus(next: RehydrateStatus) {
    this._status = next
    this.onStatus?.(next)
  }

  private beginFetch(): AbortSignal {
    this.abort?.abort()
    this.abort = new AbortController()
    return this.abort.signal
  }

  private armTimeout(gen: number) {
    this.clearTimer?.()
    const handle = this.schedule(() => {
      if (this.gen !== gen) return
      if (this._status === 'loading') {
        this.setStatus('error')
        this.abort?.abort()
      }
    }, this.timeoutMs)
    this.clearTimer = () => handle.clear()
  }

  private clearArmedTimeout() {
    this.clearTimer?.()
    this.clearTimer = null
  }

  /** Soft refresh after ready (does not change rehydrate status). */
  async refresh(): Promise<void> {
    const signal = this.beginFetch()
    try {
      await this.fetchFn(signal)
    } catch (err) {
      if (isAbortError(err) || signal.aborted) return
      // Keep ready; poll failures are non-fatal for rehydrate UI.
    }
  }

  async run(opts: { running: boolean; force?: boolean }): Promise<void> {
    if (!opts.running) {
      this.clearArmedTimeout()
      this.abort?.abort()
      this.setStatus('idle')
      return
    }

    const force = !!opts.force
    const cur = this._status
    if (!force && cur === 'error') return
    if (!force && cur === 'loading') return
    if (!force && cur === 'ready') {
      await this.refresh()
      return
    }

    const gen = ++this.gen
    const signal = this.beginFetch()
    this.setStatus('loading')
    this.armTimeout(gen)

    let result: NodeEventsFetchResult
    try {
      const r = await this.fetchFn(signal)
      result = signal.aborted ? 'stale' : r
    } catch (err) {
      result = isAbortError(err) || signal.aborted ? 'stale' : 'error'
    }

    const next = resolveRehydrateAfterFetch(this._status, gen, this.gen, result)
    if (next == null) return
    this.clearArmedTimeout()
    this.setStatus(next)
  }

  dispose() {
    this.clearArmedTimeout()
    this.abort?.abort()
    this.abort = null
  }
}
