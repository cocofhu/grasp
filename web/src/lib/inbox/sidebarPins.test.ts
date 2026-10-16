// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest'
import {
  loadSidebarPins,
  orderWithPins,
  saveSidebarPins,
  useSidebarPins,
} from './sidebarPins'
import { useAuth } from '@/lib/composables/useAuth'

describe('orderWithPins', () => {
  it('puts the newest pin first and keeps the unpinned relative order', () => {
    const rows = ['extra', 'b', 'c']
    expect(
      orderWithPins(rows, (id) => id, [
        { itemKey: 'c', pinnedAt: 1 },
        { itemKey: 'b', pinnedAt: 2 },
      ]),
    ).toEqual(['b', 'c', 'extra'])
    expect(orderWithPins(rows, (id) => id, [{ itemKey: 'c', pinnedAt: 1 }])).toEqual(['c', 'extra', 'b'])
  })

  it('keeps a pin for a row that is currently absent', () => {
    const pins = [{ itemKey: 'run-1:n', pinnedAt: 5 }]
    expect(orderWithPins([], (id: string) => id, pins)).toEqual([])
    expect(orderWithPins(['other', 'run-1:n'], (id) => id, pins)).toEqual(['run-1:n', 'other'])
  })
})

describe('sidebar pin storage', () => {
  beforeEach(() => {
    localStorage.clear()
    useAuth().clearUser()
  })

  it('stores pins per user and does not share them', () => {
    saveSidebarPins('alice', [{ itemKey: 'run-1:n1', pinnedAt: 5 }])
    saveSidebarPins('bob', [])
    expect(loadSidebarPins('alice')).toEqual([{ itemKey: 'run-1:n1', pinnedAt: 5 }])
    expect(loadSidebarPins('bob')).toEqual([])
  })

  it('sorts a later pin ahead even when the clock does not move', () => {
    useAuth().setUser({ username: 'alice', expiresAt: '2099-01-01T00:00:00Z' })
    const pins = useSidebarPins()
    pins.pin('run-1:a')
    pins.pin('run-2:a')
    const stored = pins.pins.value
    const newer = stored.find((row) => row.itemKey === 'run-2:a')
    const older = stored.find((row) => row.itemKey === 'run-1:a')
    expect(newer!.pinnedAt).toBeGreaterThan(older!.pinnedAt)
    expect(orderWithPins(['run-1:a', 'run-2:a', 'run-3:a'], (id) => id, stored)).toEqual([
      'run-2:a',
      'run-1:a',
      'run-3:a',
    ])
    pins.unpin('run-2:a')
    expect(pins.isPinned('run-2:a')).toBe(false)
    expect(pins.isPinned('run-1:a')).toBe(true)
    expect(loadSidebarPins('bob')).toEqual([])
    useAuth().setUser({ username: 'bob', expiresAt: '2099-01-01T00:00:00Z' })
    expect(pins.isPinned('run-1:a')).toBe(false)
    useAuth().setUser({ username: 'alice', expiresAt: '2099-01-01T00:00:00Z' })
    expect(pins.isPinned('run-1:a')).toBe(true)
  })
})
