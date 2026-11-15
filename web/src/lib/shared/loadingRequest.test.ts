import { describe, expect, it, vi } from 'vitest'
import {
  createTimeoutController,
  isAbortError,
  LoadingTimeoutError,
  runWithTimeout,
} from './loadingRequest'

describe('loadingRequest timeout / abort / race', () => {
  it('times out and reports error (not cancelled)', async () => {
    vi.useFakeTimers()
    const p = runWithTimeout(() => new Promise(() => {}), { timeoutMs: 100 })
    await vi.advanceTimersByTimeAsync(100)
    const result = await p
    expect(result.status).toBe('error')
    expect(result.timedOut).toBe(true)
    expect(result.error).toBeInstanceOf(LoadingTimeoutError)
    vi.useRealTimers()
  })

  it('AbortError is cancelled, not error or empty', async () => {
    const ctrl = new AbortController()
    const p = runWithTimeout(
      (signal) =>
        new Promise((_, reject) => {
          signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
        }),
      { timeoutMs: 5000, signal: ctrl.signal },
    )
    ctrl.abort()
    const result = await p
    expect(result.status).toBe('cancelled')
    expect(isAbortError(result.error)).toBe(true)
  })

  it('createTimeoutController timedOut is true only on timeout', async () => {
    vi.useFakeTimers()
    const parent = new AbortController()
    const tc = createTimeoutController(50, parent.signal)
    expect(tc.timedOut).toBe(false)
    await vi.advanceTimersByTimeAsync(50)
    expect(tc.timedOut).toBe(true)
    expect(tc.signal.aborted).toBe(true)
    tc.clear()
    vi.useRealTimers()
  })
})
