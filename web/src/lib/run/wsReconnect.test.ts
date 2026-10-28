import { describe, expect, it, vi } from 'vitest'
import { createWsReconnectController } from './wsReconnect'

describe('createWsReconnectController', () => {
  it('schedules reconnect on close when shouldReconnect', () => {
    const connect = vi.fn()
    const timers: Array<{ fn: () => void; ms: number }> = []
    const ctrl = createWsReconnectController({
      connect,
      shouldReconnect: () => true,
      baseDelayMs: 10,
      maxDelayMs: 10,
      schedule: (fn, ms) => {
        timers.push({ fn, ms })
        return timers.length as unknown as ReturnType<typeof setTimeout>
      },
      clearSchedule: vi.fn(),
    })
    expect(ctrl.onClose()).toBe(true)
    expect(timers).toHaveLength(1)
    timers[0].fn()
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('skips reconnect after intentional close', () => {
    const connect = vi.fn()
    const ctrl = createWsReconnectController({
      connect,
      shouldReconnect: () => true,
      schedule: (fn) => {
        fn()
        return 1 as unknown as ReturnType<typeof setTimeout>
      },
      clearSchedule: vi.fn(),
    })
    ctrl.markIntentionalClose()
    expect(ctrl.onClose()).toBe(false)
    expect(connect).not.toHaveBeenCalled()
  })

  it('resets backoff on markOpened', () => {
    const ctrl = createWsReconnectController({
      connect: vi.fn(),
      shouldReconnect: () => true,
      baseDelayMs: 100,
      maxDelayMs: 10_000,
      schedule: () => 1 as unknown as ReturnType<typeof setTimeout>,
      clearSchedule: vi.fn(),
    })
    ctrl.onClose()
    expect(ctrl.attempt).toBe(1)
    ctrl.markOpened()
    expect(ctrl.attempt).toBe(0)
  })

  it('retries failed replacement handshakes without resetting their backoff', () => {
    const schedule = vi.fn(() => 1 as unknown as ReturnType<typeof setTimeout>)
    const ctrl = createWsReconnectController({
      connect: vi.fn(), shouldReconnect: () => true, schedule, clearSchedule: vi.fn(),
    })
    ctrl.markIntentionalClose()
    ctrl.markConnecting()
    expect(ctrl.onClose()).toBe(true)
    ctrl.markConnecting()
    expect(ctrl.onClose()).toBe(true)
    expect(ctrl.attempt).toBe(2)
    expect(schedule).toHaveBeenCalledTimes(2)
  })
})
