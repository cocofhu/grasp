import { describe, expect, it } from 'vitest'
import { isPmFailKind, pmActiveThreadStorageKey, pmWsReconnectDelayMs } from './pmTurnState'

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

  it('keys the active thread per project', () => {
    expect(pmActiveThreadStorageKey('p1')).toBe('pm-leader:active-thread:p1')
  })
})
