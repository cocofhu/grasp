import { describe, expect, it } from 'vitest'
import { visitModelSelectVisible } from './visitModel'

describe('visitModelSelectVisible', () => {
  const open = { modelSwitch: true, active: true }

  it('shows on a sendable member composer', () => {
    expect(visitModelSelectVisible(open)).toBe(true)
  })

  it('hides while watching, after the visit, on a cold session, and for visitors', () => {
    expect(visitModelSelectVisible({ ...open, observe: true })).toBe(false)
    expect(visitModelSelectVisible({ ...open, followAuto: true })).toBe(false)
    expect(visitModelSelectVisible({ ...open, done: true })).toBe(false)
    expect(visitModelSelectVisible({ ...open, active: false })).toBe(false)
    expect(visitModelSelectVisible({ ...open, coldSession: true })).toBe(false)
    expect(visitModelSelectVisible({ ...open, composerLocked: true })).toBe(false)
    expect(visitModelSelectVisible({ active: true })).toBe(false)
  })
})
