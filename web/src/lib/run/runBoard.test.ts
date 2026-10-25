import { describe, expect, it } from 'vitest'
import { compareRunsByStartedAtDesc, runBoardTitle, runIdShort, runProgressPercent, sortRunsByStartedAtDesc } from './runBoard'
import type { Run } from '../shared/types'

function stubRun(partial: Partial<Run> & Pick<Run, 'id'>): Run {
  return {
    workflowId: 'wf',
    workflowName: 'Workflow',
    status: 'running',
    trigger: 'manual',
    startedAt: '',
    durationSec: 0,
    progress: 0,
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

describe('runIdShort', () => {
  it('strips run- prefix', () => {
    expect(runIdShort('run-abc123')).toBe('abc123')
  })

  it('keeps ids without prefix', () => {
    expect(runIdShort('abc123')).toBe('abc123')
  })
})

describe('runProgressPercent', () => {
  it('rounds a 0–1 ratio to an integer percent', () => {
    expect(runProgressPercent(0.16666666666666666)).toBe(17)
    expect(runProgressPercent(0)).toBe(0)
    expect(runProgressPercent(1)).toBe(100)
  })

  it('clamps out-of-range ratios before rounding', () => {
    expect(runProgressPercent(1.4)).toBe(100)
    expect(runProgressPercent(-0.2)).toBe(0)
  })

  it('returns null for missing or non-finite values', () => {
    expect(runProgressPercent(null)).toBeNull()
    expect(runProgressPercent(undefined)).toBeNull()
    expect(runProgressPercent(Number.NaN)).toBeNull()
    expect(runProgressPercent('0.5')).toBeNull()
  })
})

describe('runBoardTitle', () => {
  it('prefers title', () => {
    expect(runBoardTitle(stubRun({ id: 'run-1', title: 'My req', workflowName: 'WF' }))).toBe('My req')
  })

  it('falls back to workflowName', () => {
    expect(runBoardTitle(stubRun({ id: 'run-1', title: '', workflowName: 'WF' }))).toBe('WF')
  })

  it('falls back to short id', () => {
    expect(runBoardTitle(stubRun({ id: 'run-ab12', title: undefined, workflowName: '' }))).toBe('#ab12')
  })
})

describe('sortRunsByStartedAtDesc', () => {
  it('orders by startedAt descending', () => {
    const a = stubRun({ id: 'run-a', startedAt: '2026-07-18T10:00:00Z' })
    const b = stubRun({ id: 'run-b', startedAt: '2026-07-18T12:00:00Z' })
    const c = stubRun({ id: 'run-c', startedAt: '2026-07-18T11:00:00Z' })
    expect(sortRunsByStartedAtDesc([a, b, c]).map((r) => r.id)).toEqual(['run-b', 'run-c', 'run-a'])
  })

  it('puts missing startedAt after valid ones', () => {
    const a = stubRun({ id: 'run-a', startedAt: '' })
    const b = stubRun({ id: 'run-b', startedAt: '2026-07-18T12:00:00Z' })
    expect(compareRunsByStartedAtDesc(a, b)).toBeGreaterThan(0)
    expect(sortRunsByStartedAtDesc([a, b]).map((r) => r.id)).toEqual(['run-b', 'run-a'])
  })
})
