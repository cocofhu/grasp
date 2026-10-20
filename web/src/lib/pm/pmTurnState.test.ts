// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { PM_ACTIVE_THREAD_STORAGE_KEY, isPmFailKind, pmWsReconnectDelayMs } from './pmTurnState'

describe('pmTurnState', () => {
  it('isPmFailKind accepts the product kinds', () => {
    for (const k of ['connection', 'sandbox', 'empty', 'unknown', 'stopped', 'interrupted']) {
      expect(isPmFailKind(k)).toBe(true)
    }
    expect(isPmFailKind('bogus')).toBe(false)
  })

  it('backs off thread reconnects up to 15s', () => {
    expect([0, 1, 2, 3, 4, 9].map(pmWsReconnectDelayMs)).toEqual([1000, 2000, 4000, 8000, 15000, 15000])
    expect(pmWsReconnectDelayMs(-1)).toBe(1000)
  })

  it('uses one active thread selection for the workspace', () => {
    expect(PM_ACTIVE_THREAD_STORAGE_KEY).toBe('pm-leader:active-thread')
  })
})

