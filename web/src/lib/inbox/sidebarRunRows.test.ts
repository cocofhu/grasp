import { describe, expect, it } from 'vitest'
import type { InboxItem, Run } from '@/lib/shared/types'
import { keepRowOrder, mergeRunRows, pinRunId, runRowFromRun } from './sidebarRunRows'

const item = (runId: string, nodeId: string, runTitle = runId): InboxItem => ({
  type: 'clarify',
  runId,
  nodeId,
  workflowName: 'wf',
  runTitle,
  label: nodeId,
  done: false,
  requestedAt: '',
  updatedAt: '',
})

const run = (id: string, over: Partial<Run> = {}): Run =>
  ({
    id,
    workflowId: 'wf-1',
    workflowName: 'wf',
    title: `运行 ${id}`,
    status: 'running',
    trigger: 'manual',
    startedAt: 'T0',
    durationSec: 0,
    progress: 0,
    ...over,
  }) as Run

describe('mergeRunRows', () => {
  it('shows one row per run, the open item winning over a sibling of the same run', () => {
    const rows = mergeRunRows([item('r1', 'clarify'), item('r2', 'a'), item('r1', 'review')], [], 'r1:review')
    expect(rows.map((r) => `${r.runId}:${r.nodeId}`)).toEqual(['r1:review', 'r2:a'])
    expect(mergeRunRows([item('r1', 'clarify'), item('r1', 'review')], [], null).map((r) => r.nodeId)).toEqual(['clarify'])
  })

  it('adds running runs without a pending item after the pending ones, never twice', () => {
    const rows = mergeRunRows(
      [item('r1', 'clarify')],
      [run('r1'), run('r3', { currentNode: { id: 'impl', name: '实现', iteration: 2, kind: 'agent', mode: 'auto', state: 'running' } })],
      null,
    )
    expect(rows.map((r) => r.runId)).toEqual(['r1', 'r3'])
    expect(rows[1]).toMatchObject({ runTitle: '运行 r3', nodeId: 'impl', iteration: 2, label: '实现' })
  })
})

describe('runRowFromRun', () => {
  it('keeps the run title and falls back to the current node label', () => {
    expect(runRowFromRun(run('r', { currentNodeLabel: '排队' }))).toMatchObject({ runId: 'r', runTitle: '运行 r', label: '排队', nodeId: '' })
  })
})

describe('keepRowOrder', () => {
  it('keeps shown runs in place and slots new runs where they arrive', () => {
    const rows = (ids: string[]) => ids.map((runId) => ({ runId }))
    expect(keepRowOrder(rows(['y', 'x']), ['x', 'y']).map((r) => r.runId)).toEqual(['x', 'y'])
    expect(keepRowOrder(rows(['n', 'y', 'x']), ['x', 'y']).map((r) => r.runId)).toEqual(['n', 'x', 'y'])
    expect(keepRowOrder(rows(['a', 'b']), []).map((r) => r.runId)).toEqual(['a', 'b'])
  })
})

describe('pinRunId', () => {
  it('reads old run:node pins as the run', () => {
    expect(pinRunId('run-1:clarify')).toBe('run-1')
    expect(pinRunId('run-1')).toBe('run-1')
  })
})
