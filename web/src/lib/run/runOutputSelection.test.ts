import { describe, expect, it } from 'vitest'
import { firstGraphOutputNodeId, lastOutputNodeId, resolveOutputFocusNodeId } from './runOutputSelection'
import type { Run } from '../shared/types'

describe('lastOutputNodeId', () => {
  it('picks the latest started output node', () => {
    const run: Run = {
      id: 'r1',
      workflowId: 'w',
      workflowName: 'W',
      status: 'completed',
      trigger: 'manual',
      startedAt: '2026-01-01T00:00:00Z',
      durationSec: 1,
      progress: 1,
      nodeRuns: {
        out1: { nodeId: 'out1', status: 'completed', startedAt: '2026-01-01T00:00:10Z' },
        out2: { nodeId: 'out2', status: 'completed', startedAt: '2026-01-01T00:00:20Z' },
        plan: { nodeId: 'plan', status: 'completed', startedAt: '2026-01-01T00:00:05Z' },
      },
      artifacts: [],
    }
    const nodes = [
      { id: 'plan', type: 'agent' },
      { id: 'out1', type: 'output' },
      { id: 'out2', type: 'output' },
    ]
    expect(lastOutputNodeId(run, nodes)).toBe('out2')
  })

  it('falls back to first graph output when nothing executed', () => {
    const run = {
      id: 'r2',
      workflowId: 'w',
      workflowName: 'W',
      status: 'completed',
      trigger: 'manual',
      startedAt: '2026-01-01T00:00:00Z',
      durationSec: 1,
      progress: 1,
      nodeRuns: {},
      artifacts: [],
    } as Run
    const nodes = [
      { id: 'plan', type: 'agent' },
      { id: 'out1', type: 'output' },
      { id: 'out2', type: 'output' },
    ]
    expect(lastOutputNodeId(run, nodes)).toBeNull()
    expect(firstGraphOutputNodeId(nodes)).toBe('out1')
    expect(resolveOutputFocusNodeId(run, nodes)).toBe('out1')
  })

  it('returns null when graph has no output node', () => {
    const run = {
      id: 'r3',
      workflowId: 'w',
      workflowName: 'W',
      status: 'completed',
      trigger: 'manual',
      startedAt: '2026-01-01T00:00:00Z',
      durationSec: 1,
      progress: 1,
      nodeRuns: { plan: { nodeId: 'plan', status: 'completed', startedAt: '2026-01-01T00:00:05Z' } },
      artifacts: [],
    } as Run
    expect(resolveOutputFocusNodeId(run, [{ id: 'plan', type: 'agent' }])).toBeNull()
  })
})
