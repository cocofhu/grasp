// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createWsHeartbeat } from './wsHeartbeat'

describe('workflow socket heartbeat', () => {
  let heartbeat: ReturnType<typeof createWsHeartbeat>
  const onTimeout = vi.fn()
  beforeEach(() => {
    vi.useFakeTimers()
    onTimeout.mockClear()
    heartbeat = createWsHeartbeat({ onTimeout })
  })
  afterEach(() => {
    heartbeat.stop()
    vi.useRealTimers()
  })

  it('recovers a silent socket without waiting for a close event', () => {
    heartbeat.start()
    vi.advanceTimersByTime(60_000)
    expect(onTimeout).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(60_000)
    expect(onTimeout).toHaveBeenCalledTimes(1)
  })

  it('keeps an idle connection alive on server heartbeats', () => {
    heartbeat.start()
    for (let i = 0; i < 10; i++) {
      vi.advanceTimersByTime(25_000)
      heartbeat.touch()
    }
    expect(onTimeout).not.toHaveBeenCalled()
    vi.advanceTimersByTime(60_000)
    expect(onTimeout).toHaveBeenCalledTimes(1)
  })

  it('checks elapsed wall time when a suspended page resumes', () => {
    heartbeat.start()
    vi.setSystemTime(Date.now() + 120_000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(onTimeout).toHaveBeenCalledTimes(1)
  })

  it('reconnects immediately when the network becomes available again', () => {
    heartbeat.start()
    window.dispatchEvent(new Event('online'))
    expect(onTimeout).toHaveBeenCalledTimes(1)
  })

  it('cleans up timers and listeners when navigating away', () => {
    heartbeat.start()
    heartbeat.stop()
    vi.advanceTimersByTime(120_000)
    window.dispatchEvent(new Event('online'))
    document.dispatchEvent(new Event('visibilitychange'))
    heartbeat.touch()
    expect(onTimeout).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
})
