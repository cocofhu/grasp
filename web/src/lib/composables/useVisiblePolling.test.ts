// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { startVisiblePolling } from './useVisiblePolling'

describe('startVisiblePolling', () => {
  let visibility: 'visible' | 'hidden'
  beforeEach(() => {
    vi.useFakeTimers()
    visibility = 'visible'
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility)
  })
  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('polls while visible, pauses while hidden and refreshes on return', () => {
    const fn = vi.fn()
    const stop = startVisiblePolling(fn, 1000)
    vi.advanceTimersByTime(2000)
    expect(fn).toHaveBeenCalledTimes(2)

    visibility = 'hidden'
    document.dispatchEvent(new Event('visibilitychange'))
    vi.advanceTimersByTime(3000)
    expect(fn).toHaveBeenCalledTimes(2)

    visibility = 'visible'
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).toHaveBeenCalledTimes(3)

    stop()
    vi.advanceTimersByTime(3000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).toHaveBeenCalledTimes(3)
  })
})
