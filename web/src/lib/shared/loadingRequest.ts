import { DEFAULT_LOADING_TIMEOUT_MS, type LoadingTerminalState } from './loadingTypes'

export class LoadingTimeoutError extends Error {
  readonly timeoutMs: number
  constructor(timeoutMs = DEFAULT_LOADING_TIMEOUT_MS) {
    super(`Request timed out after ${timeoutMs}ms`)
    this.name = 'LoadingTimeoutError'
    this.timeoutMs = timeoutMs
  }
}

export function isAbortError(err: unknown): boolean {
  if (typeof DOMException !== 'undefined' && err instanceof DOMException && err.name === 'AbortError') {
    return true
  }
  return err instanceof Error && err.name === 'AbortError'
}

function isTimeoutError(err: unknown): boolean {
  return err instanceof LoadingTimeoutError || (err instanceof Error && err.name === 'LoadingTimeoutError')
}

type TimeoutController = {
  signal: AbortSignal
  readonly timedOut: boolean
  abort: () => void
  clear: () => void
}

/**
 * AbortController + timeout with a setTimeout fallback (same idea as RehydrateOrchestrator).
 * Timeout abort is distinguishable via `timedOut` so callers can treat it as error+retry,
 * while user/navigation AbortError stays `cancelled`.
 */
export function createTimeoutController(timeoutMs: number, parent?: AbortSignal): TimeoutController {
  const ctrl = new AbortController()
  let timedOut = false
  let timer: ReturnType<typeof setTimeout> | null = null

  const onParentAbort = () => {
    if (!ctrl.signal.aborted) ctrl.abort()
  }
  if (parent) {
    if (parent.aborted) ctrl.abort()
    else parent.addEventListener('abort', onParentAbort, { once: true })
  }

  if (timeoutMs > 0 && Number.isFinite(timeoutMs)) {
    timer = setTimeout(() => {
      timedOut = true
      if (!ctrl.signal.aborted) ctrl.abort()
    }, timeoutMs)
  }

  return {
    signal: ctrl.signal,
    get timedOut() {
      return timedOut
    },
    abort() {
      if (!ctrl.signal.aborted) ctrl.abort()
    },
    clear() {
      if (timer != null) {
        clearTimeout(timer)
        timer = null
      }
      parent?.removeEventListener('abort', onParentAbort)
    },
  }
}

export function terminalFromCatch(err: unknown, timedOut: boolean): LoadingTerminalState {
  if (timedOut || isTimeoutError(err)) return 'error'
  if (isAbortError(err)) return 'cancelled'
  return 'error'
}

type GenerationGate = {
  next: () => number
  isCurrent: (mine: number) => boolean
  readonly current: number
}

export function createGenerationGate(): GenerationGate {
  let gen = 0
  return {
    next() {
      return ++gen
    },
    isCurrent(mine: number) {
      return mine === gen
    },
    get current() {
      return gen
    },
  }
}

export async function runWithTimeout<T>(
  fn: (signal: AbortSignal) => Promise<T>,
  opts?: {
    timeoutMs?: number
    signal?: AbortSignal
    generation?: { mine: number; isCurrent: (mine: number) => boolean }
  },
): Promise<{ status: LoadingTerminalState; value?: T; error?: unknown; timedOut?: boolean }> {
  const timeoutMs = opts?.timeoutMs ?? DEFAULT_LOADING_TIMEOUT_MS
  const tc = createTimeoutController(timeoutMs, opts?.signal)
  try {
    const aborted = new Promise<never>((_, reject) => {
      const onAbort = () => {
        if (tc.timedOut) reject(new LoadingTimeoutError(timeoutMs))
        else reject(new DOMException('Aborted', 'AbortError'))
      }
      if (tc.signal.aborted) onAbort()
      else tc.signal.addEventListener('abort', onAbort, { once: true })
    })
    const value = await Promise.race([fn(tc.signal), aborted])
    if (opts?.generation && !opts.generation.isCurrent(opts.generation.mine)) {
      return { status: 'cancelled' }
    }
    return { status: 'success', value }
  } catch (err) {
    if (opts?.generation && !opts.generation.isCurrent(opts.generation.mine)) {
      return { status: 'cancelled' }
    }
    if (tc.timedOut || isTimeoutError(err)) {
      return {
        status: 'error',
        error: isTimeoutError(err) ? err : new LoadingTimeoutError(timeoutMs),
        timedOut: true,
      }
    }
    if (isAbortError(err)) {
      return { status: 'cancelled', error: err }
    }
    return { status: 'error', error: err }
  } finally {
    tc.clear()
  }
}
