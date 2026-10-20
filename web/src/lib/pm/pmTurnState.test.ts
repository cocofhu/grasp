// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import { isPmFailKind, migratePmActiveThreadStorage, pmActiveThreadStorageKey, pmWsReconnectDelayMs } from './pmTurnState'

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
    expect(pmActiveThreadStorageKey('p1')).toBe('pm-leader:active-thread')
  })
})

describe('PM local preference conversion', () => {
  afterEach(() => localStorage.clear())
  it('uses the previously selected workspace only when its remembered thread is visible', () => {
    localStorage.setItem('grasp-project-context', 'old-p')
    localStorage.setItem('pm-leader:active-thread:old-p', 'old-thread')
    localStorage.setItem('pm-leader:active-thread:canonical', 'canonical-thread')
    migratePmActiveThreadStorage(['old-thread', 'canonical-thread'], 'canonical')
    expect(localStorage.getItem(pmActiveThreadStorageKey())).toBe('old-thread')
    expect(localStorage.getItem('grasp-project-context')).toBeNull()
    expect(localStorage.getItem('pm-leader:active-thread:old-p')).toBe('old-thread')
  })
  it('waits for visible data and replaces an inaccessible local selection', () => {
    localStorage.setItem('grasp-project-context', 'old-p')
    localStorage.setItem('pm-leader:active-thread:old-p', 'visible')
    localStorage.setItem(pmActiveThreadStorageKey(), 'another-user-thread')
    migratePmActiveThreadStorage([], 'canonical')
    expect(localStorage.getItem('pm-leader:workspace-selection-v1')).toBeNull()
    migratePmActiveThreadStorage(['visible'], 'canonical')
    expect(localStorage.getItem(pmActiveThreadStorageKey())).toBe('visible')
  })
})
