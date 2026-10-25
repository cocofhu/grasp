import { afterEach, describe, expect, it } from 'vitest'
import { __resetConfirmFlowWindowForTests, confirmFlowWindowVisible } from './confirmFlowWindow'
import { sidebarRunGroup } from './sidebarRunRows'
import {
  SIDEBAR_EARLY_AUTO_MS,
  SIDEBAR_FLIGHT_MS,
  SIDEBAR_HANDOFF_TTL_MS,
  __resetSidebarHandoffForTests,
  applySidebarEnter,
  armSidebarHandoff,
  clearSidebarHandoff,
  decideSidebarFlight,
  isSidebarRunDismissed,
  noteSidebarHandoffOutcome,
  noteSidebarRunStatus,
  readSidebarHandoff,
  releaseHeldSidebarHandoffs,
  sidebarChainHandoff,
} from './sidebarHandoff'

afterEach(() => {
  __resetConfirmFlowWindowForTests()
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
  it('drops the mark for a person, the end, or a timeout', () => {
    armSidebarHandoff('run-1', 'clarify', 1_000, { nodeId: 'impl', name: '实现' })
    noteSidebarHandoffOutcome('run-1', 'human', undefined, 1_100)
    expect(readSidebarHandoff('run-1', 1_100)).toBeNull()

    armSidebarHandoff('run-2', 'gate', 1_000)
    noteSidebarHandoffOutcome('run-2', 'ended')
    expect(readSidebarHandoff('run-2', 1_000)).toBeNull()
    expect(isSidebarRunDismissed('run-2')).toBe(true)

    armSidebarHandoff('run-3', 'clarify', 1_000)
    expect(readSidebarHandoff('run-3', 1_000 + SIDEBAR_HANDOFF_TTL_MS + 1)).toBeNull()

    armSidebarHandoff('run-4', 'clarify', 1_000)
    clearSidebarHandoff('run-4')
    expect(readSidebarHandoff('run-4', 1_000)).toBeNull()
  })

  it('keeps this handoff’s next node when it enters just before the click is recorded', () => {
    const now = 5_000
    noteSidebarHandoffOutcome('run-1', 'auto', { nodeId: 'impl', name: '实现' }, now)
    expect(readSidebarHandoff('run-1', now)).toBeNull()
    armSidebarHandoff('run-1', 'clarify', now + 10)
    expect(readSidebarHandoff('run-1', now + 10)).toMatchObject({
      fromNodeId: 'clarify',
      confirmedAuto: true,
      autoNodeId: 'impl',
      autoNodeName: '实现',
    })
  })

  it('does not treat the node being confirmed, or an older automatic enter, as this destination', () => {
    const now = 5_000
    applySidebarEnter('run-1', { nodeId: 'review', name: '评审', mode: 'auto' }, [{ id: 'review', type: 'agent' }], now)
    armSidebarHandoff('run-1', 'review', now + 20, null)
    expect(readSidebarHandoff('run-1', now + 20)).toMatchObject({ fromNodeId: 'review', confirmedAuto: false })

    noteSidebarHandoffOutcome('run-2', 'auto', { nodeId: 'impl', name: '实现' }, now)
    armSidebarHandoff('run-2', 'clarify', now + SIDEBAR_EARLY_AUTO_MS + 1, null)
    expect(readSidebarHandoff('run-2', now + SIDEBAR_EARLY_AUTO_MS + 1)).toMatchObject({ confirmedAuto: false })

    noteSidebarHandoffOutcome('run-3', 'auto', undefined, now)
    armSidebarHandoff('run-3', 'clarify', now + 10, null)
    expect(readSidebarHandoff('run-3', now + 10)).toMatchObject({ confirmedAuto: false })
  })

  it('keeps the graph target when an earlier automatic enter is still remembered', () => {
    const now = 5_000
    applySidebarEnter('run-1', { nodeId: 'impl', name: '实现', mode: 'auto' }, [{ id: 'impl', type: 'agent' }], now)
    armSidebarHandoff('run-1', 'review', now + 30, { nodeId: 'test', name: '测试' })
    expect(readSidebarHandoff('run-1', now + 30)).toMatchObject({
      confirmedAuto: true,
      autoNodeId: 'test',
      autoNodeName: '测试',
    })

    applySidebarEnter('run-1', { nodeId: 'review', name: '评审', mode: 'auto' }, [{ id: 'review', type: 'agent' }], now + 40)
    expect(readSidebarHandoff('run-1', now + 40)).toMatchObject({
      confirmedAuto: true,
      autoNodeId: 'test',
      autoNodeName: '测试',
    })
  })

  it('settles the click immediately when the destination is already an automatic agent', () => {
    armSidebarHandoff('run-1', 'clarify', 1_000, { nodeId: 'impl', name: '实现' })
    expect(readSidebarHandoff('run-1', 1_000)).toMatchObject({ confirmedAuto: true, autoNodeName: '实现' })
    noteSidebarHandoffOutcome('run-1', 'auto', { nodeId: 'test', name: '测试' })
    expect(readSidebarHandoff('run-1', 1_000)).toMatchObject({ confirmedAuto: true, autoNodeId: 'test', autoNodeName: '测试' })
  })

  it('hides a finished run from both groups, and a plumbing enter does not settle anything', () => {
    armSidebarHandoff('run-1', 'clarify', 1_000, { nodeId: 'impl', name: '实现' })
    noteSidebarRunStatus('run-1', 'completed')
    expect(readSidebarHandoff('run-1', 1_000)).toBeNull()
    expect(isSidebarRunDismissed('run-1')).toBe(true)
    noteSidebarRunStatus('run-1', 'running')
    expect(isSidebarRunDismissed('run-1')).toBe(true)

    applySidebarEnter('run-2', { nodeId: 'out', name: '输出', mode: 'auto' }, [{ id: 'out', type: 'output' }])
    armSidebarHandoff('run-2', 'clarify', 1_000)
    expect(readSidebarHandoff('run-2', 1_000)).toMatchObject({ confirmedAuto: false })

    applySidebarEnter('run-2', { nodeId: 'impl', name: '实现', mode: 'auto' }, [{ id: 'out', type: 'output' }, { id: 'impl', type: 'agent' }])
    expect(readSidebarHandoff('run-2', 1_000)).toMatchObject({ confirmedAuto: true, autoNodeName: '实现' })

    applySidebarEnter('run-2', { nodeId: 'gate', name: '审批', mode: 'manual' }, [{ id: 'gate', type: 'human_gate' }])
    expect(readSidebarHandoff('run-2', 1_000)).toBeNull()
  })

  it('does not move the row into 自动过程 until the confirm window has left', () => {
    confirmFlowWindowVisible.value = true
    armSidebarHandoff('run-1', 'clarify', 1_000, { nodeId: 'impl', name: '实现' })
    const waiting = {
      id: 'run-1',
      currentNode: { id: 'clarify', name: '澄清', iteration: 1, kind: 'clarify', mode: 'manual', state: 'waiting_human' },
    } as never
    const jumped = {
      id: 'run-1',
      currentNode: { id: 'impl', name: '实现', iteration: 1, kind: 'agent', mode: 'auto', state: 'running' },
    } as never
    const held = readSidebarHandoff('run-1', 1_000)
    expect(held?.held).toBe(true)
    expect(sidebarRunGroup(waiting, held)).toBe('running')
    expect(sidebarRunGroup(jumped, held)).toBe('running')
    releaseHeldSidebarHandoffs()
    const released = readSidebarHandoff('run-1', 1_000)
    expect(released?.held).toBe(false)
    expect(sidebarRunGroup(waiting, released)).toBe('auto')
    expect(sidebarRunGroup(jumped, released)).toBe('auto')
    expect(decideSidebarFlight({
      previous: 'running',
      next: 'auto',
      handoffArmed: true,
      confirmedAuto: true,
      reducedMotion: false,
      targetCollapsed: false,
    }).type).toBe('fly-in')
  })

  it('ignores a replay of the node being confirmed, then settles on the next automatic node', () => {
    armSidebarHandoff('run-1', 'review', 1_000, null)
    noteSidebarHandoffOutcome('run-1', 'auto', { nodeId: 'review', name: '评审' }, 1_100)
    expect(readSidebarHandoff('run-1', 1_100)).toMatchObject({ confirmedAuto: false })
    noteSidebarHandoffOutcome('run-1', 'auto', { nodeId: 'impl', name: '实现' }, 1_200)
    expect(readSidebarHandoff('run-1', 1_200)).toMatchObject({ confirmedAuto: true, autoNodeId: 'impl', autoNodeName: '实现' })
  })
})

describe('sidebar chain snapshot', () => {
  const nodes = [
    { id: 'clarify', type: 'agent' },
    { id: 'out', type: 'output' },
    { id: 'impl', type: 'agent' },
  ]

  it('does not treat a plumbing node as the automatic destination', () => {
    expect(sidebarChainHandoff(
      { status: 'running', current: { id: 'out', name: '输出', mode: 'auto' } },
      'clarify',
      nodes,
    )).toBeNull()
    expect(sidebarChainHandoff(
      { status: 'running', current: { id: 'impl', name: '实现', mode: 'auto' } },
      'clarify',
      nodes,
    )).toEqual({ outcome: 'auto', target: { nodeId: 'impl', name: '实现' } })
  })

  it('does not negate a click that is still on the same node, and ends the run when the chain does', () => {
    expect(sidebarChainHandoff(
      { status: 'running', current: { id: 'clarify', name: '澄清', mode: 'manual' } },
      'clarify',
      nodes,
    )).toBeNull()
    expect(sidebarChainHandoff(
      { status: 'completed', current: { id: 'out', name: '输出', mode: 'auto' } },
      'clarify',
      nodes,
    )).toEqual({ outcome: 'ended' })
    expect(sidebarChainHandoff(
      { status: 'waiting_human', current: { id: 'gate', name: '审批', mode: 'manual' } },
      'clarify',
      nodes,
    )).toEqual({ outcome: 'human' })
  })
})
