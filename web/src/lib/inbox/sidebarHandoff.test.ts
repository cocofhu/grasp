import { afterEach, describe, expect, it } from 'vitest'
import {
  SIDEBAR_FLIGHT_MS,
  SIDEBAR_HANDOFF_TTL_MS,
  __resetSidebarHandoffForTests,
  armSidebarHandoff,
  clearSidebarHandoff,
  decideSidebarFlight,
  noteSidebarHandoffOutcome,
  readSidebarHandoff,
} from './sidebarHandoff'

afterEach(() => {
  __resetSidebarHandoffForTests()
})

describe('decideSidebarFlight', () => {
  const base = {
    handoffArmed: false,
    confirmedAuto: false,
    reducedMotion: false,
    targetCollapsed: false,
  }

  it('flies into 自动过程 only for this click after auto is confirmed', () => {
    expect(decideSidebarFlight({ ...base, previous: 'running', next: 'auto', handoffArmed: true, confirmedAuto: true })).toEqual({ type: 'fly-in', landing: 'row' })
    expect(decideSidebarFlight({ ...base, previous: 'running', next: 'auto', handoffArmed: true, confirmedAuto: false }).type).toBe('place')
    expect(decideSidebarFlight({ ...base, previous: 'running', next: 'auto', handoffArmed: false, confirmedAuto: true }).type).toBe('place')
  })

  it('does not fly in when the handoff lands on a person or the run leaves', () => {
    expect(decideSidebarFlight({ ...base, previous: 'running', next: 'running', handoffArmed: true, confirmedAuto: false }).type).toBe('stay')
    expect(decideSidebarFlight({ ...base, previous: 'running', next: null, handoffArmed: true, confirmedAuto: true }).type).toBe('leave')
  })

  it('does not fly in when the row was not already on screen', () => {
    expect(decideSidebarFlight({ ...base, previous: null, next: 'auto', handoffArmed: true, confirmedAuto: true }).type).toBe('place')
  })

  it('flies back when an automatic row becomes a person row, and only renames between automatic nodes', () => {
    expect(decideSidebarFlight({ ...base, previous: 'auto', next: 'running' })).toEqual({ type: 'fly-back', landing: 'row' })
    expect(decideSidebarFlight({ ...base, previous: 'auto', next: 'auto' }).type).toBe('stay')
  })

  it('places directly when motion is reduced, and lands on the heading when the target is folded', () => {
    expect(decideSidebarFlight({ ...base, previous: 'running', next: 'auto', handoffArmed: true, confirmedAuto: true, reducedMotion: true }).type).toBe('place')
    expect(decideSidebarFlight({ ...base, previous: 'auto', next: 'running', reducedMotion: true }).type).toBe('place')
    expect(decideSidebarFlight({
      ...base,
      previous: 'running',
      next: 'auto',
      handoffArmed: true,
      confirmedAuto: true,
      targetCollapsed: true,
    })).toEqual({ type: 'fly-in', landing: 'heading' })
    expect(decideSidebarFlight({ ...base, previous: 'auto', next: 'running', targetCollapsed: true })).toEqual({ type: 'fly-back', landing: 'heading' })
  })

  it('keeps one move under 600ms', () => {
    expect(SIDEBAR_FLIGHT_MS).toBeLessThanOrEqual(600)
    expect(SIDEBAR_FLIGHT_MS).toBeGreaterThan(0)
  })
})

describe('sidebar handoff mark', () => {
  it('confirms an early auto enter, and drops the mark for a person, the end, or a timeout', () => {
    noteSidebarHandoffOutcome('run-1', 'auto')
    armSidebarHandoff('run-1', 'clarify', 1_000)
    expect(readSidebarHandoff('run-1', 1_000)).toMatchObject({ fromNodeId: 'clarify', confirmedAuto: true })

    noteSidebarHandoffOutcome('run-1', 'human')
    expect(readSidebarHandoff('run-1', 1_000)).toBeNull()

    armSidebarHandoff('run-2', 'gate', 1_000)
    noteSidebarHandoffOutcome('run-2', 'ended')
    expect(readSidebarHandoff('run-2', 1_000)).toBeNull()

    armSidebarHandoff('run-3', 'clarify', 1_000)
    expect(readSidebarHandoff('run-3', 1_000 + SIDEBAR_HANDOFF_TTL_MS + 1)).toBeNull()

    armSidebarHandoff('run-4', 'clarify', 1_000)
    clearSidebarHandoff('run-4')
    expect(readSidebarHandoff('run-4', 1_000)).toBeNull()
  })
})
