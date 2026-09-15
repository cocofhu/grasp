import { describe, expect, it } from 'vitest'
import {
  isApproveStillStarting,
  isStartFailedRun,
  makeIncomingGhost,
  resolveIncomingApproval,
  vanishedStartingRows,
} from './inboxStartingCards'
import type { InboxItem, Run } from '@/lib/shared/types'

function clarify(over: Partial<InboxItem> = {}): InboxItem {
  return {
    type: 'clarify',
    kind: 'clarify',
    runId: 'run-1',
    nodeId: 'ap',
    iteration: 1,
    workflowName: 'wf',
    runTitle: 't',
    label: 'l',
    done: false,
    requestedAt: '',
    updatedAt: '',
    ...over,
  } as InboxItem
}

const keyOf = (it: InboxItem) => `${it.runId}:${it.nodeId}:${it.iteration ?? 1}`

describe('resolveIncomingApproval', () => {
  it('prefers the query and falls back to the handoff for each part', () => {
    expect(resolveIncomingApproval('run-q', 'node-q', { runId: 'run-h', nodeId: 'node-h' })).toEqual({
      runId: 'run-q',
      nodeId: 'node-q',
    })
    expect(resolveIncomingApproval('', '', { runId: 'run-h', nodeId: 'node-h' })).toEqual({
      runId: 'run-h',
      nodeId: 'node-h',
    })
    expect(resolveIncomingApproval('run-q', '', { runId: 'run-h', nodeId: 'node-h' })).toEqual({
      runId: 'run-q',
      nodeId: 'node-h',
    })
  })

  it('is null without a run id, and tolerates non-string query values', () => {
    expect(resolveIncomingApproval('', '', null)).toBeNull()
    expect(resolveIncomingApproval(undefined, 'node-q', null)).toBeNull()
    expect(resolveIncomingApproval(['run-a'], undefined, null)).toEqual({
      runId: 'run-a',
      nodeId: '',
    })
  })
})

describe('makeIncomingGhost', () => {
  it('labels the ghost with the clipped seed text and marks it starting', () => {
    const ghost = makeIncomingGhost({ runId: 'run-1', nodeId: 'ap' }, '  把登录做清楚  ', 'T0')
    expect(ghost.state).toBe('starting')
    expect(ghost.label).toBe('把登录做清楚')
    expect(ghost.runTitle).toBe('把登录做清楚')
    expect(ghost.nodeId).toBe('ap')
    expect(ghost.requestedAt).toBe('T0')
    expect(ghost.updatedAt).toBe('T0')
  })

  it('falls back to a placeholder label and node id', () => {
    const ghost = makeIncomingGhost({ runId: 'run-1', nodeId: '' }, '', 'T0')
    expect(ghost.label).toBe('…')
    expect(ghost.nodeId).toBe('grasp')
  })
})

describe('isStartFailedRun', () => {
  it('detects a sandbox-setup failure that leaves the run non-terminal', () => {
    // The engine records an approve sandbox-setup failure on the node execution
    // and stops without failing the run, so run status alone says "running".
    const run = {
      status: 'running',
      nodeRuns: { ap: { nodeId: 'ap', status: 'failed', error: 'sandbox setup failed' } },
    } as unknown as Run
    expect(isStartFailedRun(run, 'ap')).toBe(true)
  })

  it('detects a whole-run failure or cancel even without node detail', () => {
    expect(isStartFailedRun({ status: 'failed' } as Run, 'ap')).toBe(true)
    expect(isStartFailedRun({ status: 'cancelled' } as Run, 'ap')).toBe(true)
  })

  it('is false while the node is still booting', () => {
    const run = {
      status: 'running',
      nodeRuns: { ap: { nodeId: 'ap', status: 'running' } },
    } as unknown as Run
    expect(isStartFailedRun(run, 'ap')).toBe(false)
  })

  it('does not invent a failure for a node id the run does not know', () => {
    const run = {
      status: 'running',
      nodeRuns: { approve_7gl8: { nodeId: 'approve_7gl8', status: 'failed' } },
    } as unknown as Run
    expect(isStartFailedRun(run, 'ap')).toBe(false)
    expect(isStartFailedRun(run, '')).toBe(false)
  })

  it('detects approve setup failure via graph when the hinted id misses', () => {
    const run = {
      status: 'running',
      nodes: [
        { id: 'in', type: 'input' },
        { id: 'approve_7gl6', type: 'approve' },
      ],
      nodeRuns: {
        approve_7gl6: { nodeId: 'approve_7gl6', status: 'failed', error: 'sandbox setup failed' },
      },
    } as unknown as Run
    expect(isStartFailedRun(run, 'ap')).toBe(true)
    expect(isStartFailedRun(run, '')).toBe(true)
  })
})

describe('isApproveStillStarting', () => {
  it('is true only while the approve node (or whole run) looks like a fresh boot', () => {
    expect(
      isApproveStillStarting(
        {
          status: 'running',
          nodeRuns: { ap: { nodeId: 'ap', status: 'running' } },
        } as unknown as Run,
        'ap',
      ),
    ).toBe(true)
    expect(isApproveStillStarting({ status: 'queued' } as Run, 'ap')).toBe(true)
    expect(isApproveStillStarting({ status: 'running' } as Run, '')).toBe(true)
  })

  it('is false once the approval parked, completed, or the run left the boot window', () => {
    expect(
      isApproveStillStarting(
        {
          status: 'waiting_human',
          nodeRuns: { ap: { nodeId: 'ap', status: 'waiting_human' } },
        } as unknown as Run,
        'ap',
      ),
    ).toBe(false)
    expect(
      isApproveStillStarting(
        {
          status: 'running',
          nodeRuns: {
            ap: { nodeId: 'ap', status: 'completed' },
            implement: { nodeId: 'implement', status: 'running' },
          },
        } as unknown as Run,
        'ap',
      ),
    ).toBe(false)
    expect(isApproveStillStarting({ status: 'completed' } as Run, 'ap')).toBe(false)
    expect(isApproveStillStarting({ status: 'failed' } as Run, 'ap')).toBe(false)
  })

  it('uses graph approve nodes when the hinted id misses (ap vs approve_7gl6)', () => {
    const run = {
      status: 'running',
      nodes: [
        { id: 'in', type: 'input' },
        { id: 'approve_7gl6', type: 'approve' },
        { id: 'implement_qnlc', type: 'implement' },
      ],
      nodeRuns: {
        in: { nodeId: 'in', status: 'completed' },
        approve_7gl6: { nodeId: 'approve_7gl6', status: 'completed' },
        implement_qnlc: { nodeId: 'implement_qnlc', status: 'running' },
      },
    } as unknown as Run
    expect(isApproveStillStarting(run, 'ap')).toBe(false)
    expect(isApproveStillStarting(run, '')).toBe(false)
  })

  it('keeps the ghost while approve is still booting after input completed', () => {
    const run = {
      status: 'running',
      nodes: [
        { id: 'in', type: 'input' },
        { id: 'approve_7gl6', type: 'approve' },
      ],
      nodeRuns: {
        in: { nodeId: 'in', status: 'completed' },
        approve_7gl6: { nodeId: 'approve_7gl6', status: 'running' },
      },
    } as unknown as Run
    expect(isApproveStillStarting(run, 'ap')).toBe(true)
    expect(isApproveStillStarting(run, '')).toBe(true)
  })
})

describe('vanishedStartingRows', () => {
  it('reports only starting rows that left the list', () => {
    const starting = clarify({ runId: 'run-s', state: 'starting' })
    const parked = clarify({ runId: 'run-p' })
    const gone = vanishedStartingRows([starting, parked], [], keyOf)
    expect(gone.map((it) => it.runId)).toEqual(['run-s'])
  })

  it('keeps rows that are still present, and skips the client ghost', () => {
    const starting = clarify({ runId: 'run-s', state: 'starting' })
    const ghost = clarify({ runId: 'run-g', state: 'starting' })
    expect(vanishedStartingRows([starting], [starting], keyOf)).toEqual([])
    expect(vanishedStartingRows([starting, ghost], [], keyOf, keyOf(ghost))).toEqual([starting])
  })

  it('reports nothing on the first load, when there is no previous list', () => {
    expect(vanishedStartingRows([], [clarify()], keyOf)).toEqual([])
  })
})
