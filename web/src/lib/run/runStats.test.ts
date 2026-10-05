import { describe, expect, it } from 'vitest'
import {
  aggregateMultiRuns,
  aggregateSingleRun,
  flattenProcesses,
  hasHumanWait,
  isInvalidStart,
  mergeByNode,
  mergeByType,
  pickDefaultTimelineNodeId,
  resolveProcessDuration,
  resolveRunWallSec,
  sharePct,
} from './runStats'
import type { Run, WFNode } from '../shared/types'
import { ASK_CAPS, AUTO_CAPS, IMPLEMENT_CAPS } from '@/test/capsFixtures'

const nodes: WFNode[] = [
  { id: 'research', type: 'agent', label: '调研', position: { x: 0, y: 0 }, config: {}, caps: AUTO_CAPS },
  { id: 'react', type: 'agent', label: '需求澄清', position: { x: 0, y: 0 }, config: {}, caps: ASK_CAPS },
  { id: 'gate', type: 'human_gate', label: '人工门禁', position: { x: 0, y: 0 }, config: {} },
  { id: 'skip', type: 'agent', label: '视觉', position: { x: 0, y: 0 }, config: {}, caps: AUTO_CAPS },
]

function baseRun(partial: Partial<Run> & Pick<Run, 'nodeExecutions'>): Run {
  return {
    id: 'run-1',
    workflowId: 'wf',
    workflowName: 'W',
    status: 'completed',
    trigger: 'manual',
    startedAt: '2026-01-01T00:00:00Z',
    durationSec: 100,
    progress: 1,
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

describe('sharePct', () => {
  it('rounds percentage against wall clock', () => {
    expect(sharePct(25, 100)).toBe(25)
    expect(sharePct(1, 3)).toBe(33)
  })

  it('degrades when denominator is 0', () => {
    expect(sharePct(10, 0)).toBeNull()
  })

  it('uses max(denom, 1) when denom is positive fraction path', () => {
    expect(sharePct(1, 1)).toBe(100)
  })
})

describe('resolveProcessDuration / hasHumanWait', () => {
  it('uses live elapsed for running nodes', () => {
    const start = '2026-01-01T00:00:00Z'
    const now = Date.parse(start) + 45_000
    expect(
      resolveProcessDuration({ status: 'running', startedAt: start, durationSec: 0 }, now),
    ).toBe(45)
  })

  it('uses durationSec for skipped/cancelled', () => {
    expect(resolveProcessDuration({ status: 'skipped', durationSec: 3 }, Date.now())).toBe(3)
    expect(resolveProcessDuration({ status: 'cancelled', durationSec: 7 }, Date.now())).toBe(7)
  })

  it('flags human wait heuristically', () => {
    expect(hasHumanWait('waiting_human', { type: 'agent' })).toBe(true)
    expect(hasHumanWait('completed', { type: 'human_gate' })).toBe(true)
    expect(hasHumanWait('completed', { type: 'agent', caps: ASK_CAPS })).toBe(true)
    expect(hasHumanWait('completed', { type: 'agent', caps: IMPLEMENT_CAPS })).toBe(true)
    expect(hasHumanWait('completed', { type: 'agent', caps: AUTO_CAPS })).toBe(false)
  })

  it('resolveRunWallSec uses live elapsed for waiting_human', () => {
    const start = '2026-01-01T00:00:00Z'
    const now = Date.parse(start) + 90_000
    expect(
      resolveRunWallSec(
        { status: 'waiting_human', startedAt: start, durationSec: 0 },
        now,
      ),
    ).toBe(90)
    expect(
      resolveRunWallSec(
        { status: 'completed', startedAt: start, durationSec: 42 },
        now,
      ),
    ).toBe(42)
  })
})

describe('isInvalidStart / resolveRunWallSec sentinels (F1/F7)', () => {
  const now = Date.parse('2026-08-11T21:17:00Z')
  const goZero = '0001-01-01T00:00:00Z'

  it('treats empty, unparseable, pre-epoch, and year-1 as invalid', () => {
    expect(isInvalidStart(undefined)).toBe(true)
    expect(isInvalidStart(null)).toBe(true)
    expect(isInvalidStart('')).toBe(true)
    expect(isInvalidStart('   ')).toBe(true)
    expect(isInvalidStart('not-a-date')).toBe(true)
    expect(isInvalidStart('1969-12-31T23:59:59Z')).toBe(true)
    expect(isInvalidStart(goZero)).toBe(true)
    expect(isInvalidStart('0001-01-01T00:00:00+00:00')).toBe(true)
    expect(isInvalidStart('2026-07-18T00:00:00Z')).toBe(false)
    expect(isInvalidStart('1970-01-01T00:00:00Z')).toBe(false)
  })

  it('queued / running / waiting_human with Go-zero start contribute 0 (no now-start)', () => {
    for (const status of ['queued', 'running', 'waiting_human'] as const) {
      expect(resolveRunWallSec({ status, startedAt: goZero, durationSec: 0 }, now)).toBe(0)
      expect(resolveRunWallSec({ status, startedAt: '', durationSec: 99 }, now)).toBe(0)
      expect(resolveRunWallSec({ status, startedAt: 'bogus', durationSec: 0 }, now)).toBe(0)
    }
  })

  it('does not zero a running run with a recent valid startedAt', () => {
    const start = '2026-08-11T21:16:00Z'
    expect(
      resolveRunWallSec({ status: 'running', startedAt: start, durationSec: 0 }, now),
    ).toBe(60)
  })

  it('keeps terminal duration even when startedAt is Go zero', () => {
    expect(
      resolveRunWallSec(
        { status: 'completed', startedAt: goZero, durationSec: 744 },
        now,
      ),
    ).toBe(744)
  })

  it('queued+0001 mean stays minute-scale, not million hours (F7 inflation lock)', () => {
    const queued = resolveRunWallSec(
      { status: 'queued', startedAt: goZero, durationSec: 0 },
      now,
    )
    const a = resolveRunWallSec(
      { status: 'completed', startedAt: '2026-08-12T00:00:00Z', durationSec: 744 },
      now,
    )
    const b = resolveRunWallSec(
      { status: 'completed', startedAt: '2026-08-12T01:00:00Z', durationSec: 1936 },
      now,
    )
    const avg = (queued + queued + a + b) / 4
    expect(queued).toBe(0)
    expect(avg).toBe((744 + 1936) / 4)
    expect(avg).toBeLessThan(3600)
    expect(avg / 3600).toBeLessThan(1_000_000)
  })
})

describe('flattenProcesses', () => {
  it('orders by startedAt then iteration and includes skipped', () => {
    const run = baseRun({
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:05Z',
            durationSec: 10,
          },
        ],
        skip: [
          {
            nodeId: 'skip',
            iteration: 1,
            status: 'skipped',
            startedAt: '2026-01-01T00:00:01Z',
            durationSec: 2,
          },
        ],
        react: [
          {
            nodeId: 'react',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:05Z',
            durationSec: 20,
          },
          {
            nodeId: 'react',
            iteration: 2,
            status: 'completed',
            startedAt: '2026-01-01T00:00:05Z',
            durationSec: 5,
          },
        ],
      },
    })
    const list = flattenProcesses(run, nodes, Date.now())
    expect(list.map((p) => `${p.nodeId}#${p.iteration}`)).toEqual([
      'skip#1',
      'research#1',
      'react#1',
      'react#2',
    ])
    expect(list.find((p) => p.nodeId === 'skip')?.durationSec).toBe(2)
    expect(list.find((p) => p.nodeId === 'react')?.hasHumanWait).toBe(true)
  })

  it('carries NodeRun.usage onto ProcessAtom', () => {
    const usage = {
      inputTokens: 10,
      outputTokens: 5,
      cacheReadTokens: 0,
      cacheWriteTokens: 0,
    }
    const run = baseRun({
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:00Z',
            durationSec: 10,
            usage,
          },
        ],
        gate: [
          {
            nodeId: 'gate',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:10Z',
            durationSec: 5,
          },
        ],
      },
    })
    const list = flattenProcesses(run, nodes, Date.now())
    expect(list.find((p) => p.nodeId === 'research')?.usage).toEqual(usage)
    expect(list.find((p) => p.nodeId === 'gate')?.usage).toBeUndefined()
  })
})

describe('usage aggregation', () => {
  const usageA = {
    inputTokens: 100,
    outputTokens: 50,
    cacheReadTokens: 10,
    cacheWriteTokens: 0,
  }
  const usageB = {
    inputTokens: 20,
    outputTokens: 10,
    cacheReadTokens: 0,
    cacheWriteTokens: 5,
  }

  it('single: totals match timeline semantics; unreported → null not 0', () => {
    const withUsage = baseRun({
      durationSec: 100,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:00Z',
            durationSec: 40,
            usage: usageA,
          },
        ],
        gate: [
          {
            nodeId: 'gate',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:40Z',
            durationSec: 25,
          },
        ],
        react: [
          {
            nodeId: 'react',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:01:05Z',
            durationSec: 30,
            usage: usageB,
          },
        ],
      },
    })
    const s = aggregateSingleRun(withUsage, nodes, 'node', 100, Date.now())
    expect(s.totalTokens).toBe(195)
    expect(s.tokenRate).toBe('1.95')
    expect(s.items.find((i) => i.key === 'research')?.totalTokens).toBe(160)
    expect(s.items.find((i) => i.key === 'gate')?.totalTokens).toBeNull()
    expect(s.items.find((i) => i.key === 'react')?.totalTokens).toBe(35)

    const none = baseRun({
      durationSec: 50,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:00Z',
            durationSec: 40,
          },
        ],
      },
    })
    const empty = aggregateSingleRun(none, nodes, 'process', 50, Date.now())
    expect(empty.totalTokens).toBeNull()
    expect(empty.tokenRate).toBeNull()
    expect(empty.items.every((i) => i.totalTokens == null)).toBe(true)

    const zero = baseRun({
      durationSec: 10,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:00Z',
            durationSec: 10,
            usage: {
              inputTokens: 0,
              outputTokens: 0,
              cacheReadTokens: 0,
              cacheWriteTokens: 0,
            },
          },
        ],
      },
    })
    const z = aggregateSingleRun(zero, nodes, 'process', 10, Date.now())
    expect(z.totalTokens).toBe(0)
    expect(z.tokenRate).toBe('0.00')
    expect(z.items[0]?.totalTokens).toBe(0)

    const badWall = aggregateSingleRun(withUsage, nodes, 'process', 0, Date.now())
    expect(badWall.totalTokens).toBe(195)
    expect(badWall.tokenRate).toBeNull()
  })

  it('multi: Σ/avg ignore runs without usage; rate uses wall sum', () => {
    const runA = baseRun({
      id: 'a',
      durationSec: 100,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:00Z',
            durationSec: 40,
            usage: usageA,
          },
        ],
      },
      nodes,
    })
    const runB = baseRun({
      id: 'b',
      durationSec: 80,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-02T00:00:00Z',
            durationSec: 20,
          },
        ],
      },
      nodes,
    })
    const runC = baseRun({
      id: 'c',
      durationSec: 60,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-03T00:00:00Z',
            durationSec: 30,
            usage: usageB,
          },
        ],
      },
      nodes,
    })
    const multi = aggregateMultiRuns(
      [
        { run: runA, wallSec: 100 },
        { run: runB, wallSec: 80 },
        { run: runC, wallSec: 60 },
      ],
      'node',
      Date.now(),
    )
    expect(multi.wallSumSec).toBe(240)
    expect(multi.totalTokens).toBe(195)
    expect(multi.usageRunCount).toBe(2)
    expect(multi.avgTokens).toBe(98)
    expect(multi.tokenRate).toBe('0.81')
    expect(multi.items.find((i) => i.key === 'research')?.totalTokens).toBe(195)

    const none = aggregateMultiRuns(
      [
        { run: runB, wallSec: 80 },
        {
          run: baseRun({
            id: 'd',
            durationSec: 10,
            nodeExecutions: {
              gate: [
                {
                  nodeId: 'gate',
                  iteration: 1,
                  status: 'completed',
                  startedAt: '2026-01-04T00:00:00Z',
                  durationSec: 10,
                },
              ],
            },
            nodes,
          }),
          wallSec: 10,
        },
      ],
      'node',
      Date.now(),
    )
    expect(none.totalTokens).toBeNull()
    expect(none.avgTokens).toBeNull()
    expect(none.tokenRate).toBeNull()
    expect(none.usageRunCount).toBe(0)
  })
})

describe('aggregateSingleRun', () => {
  const run = baseRun({
    durationSec: 100,
    nodeExecutions: {
      research: [
        {
          nodeId: 'research',
          iteration: 1,
          status: 'completed',
          startedAt: '2026-01-01T00:00:00Z',
          durationSec: 20,
        },
      ],
      react: [
        {
          nodeId: 'react',
          iteration: 1,
          status: 'completed',
          startedAt: '2026-01-01T00:00:20Z',
          durationSec: 30,
        },
        {
          nodeId: 'react',
          iteration: 2,
          status: 'completed',
          startedAt: '2026-01-01T00:01:00Z',
          durationSec: 10,
        },
      ],
      gate: [
        {
          nodeId: 'gate',
          iteration: 1,
          status: 'completed',
          startedAt: '2026-01-01T00:00:50Z',
          durationSec: 25,
        },
      ],
    },
  })

  it('process dimension maps 1:1 with merge count 1', () => {
    const s = aggregateSingleRun(run, nodes, 'process', 100, Date.now())
    expect(s.items).toHaveLength(4)
    expect(s.nodeSumSec).toBe(85)
    expect(s.gapSec).toBe(15)
    expect(s.bottleneck?.key).toBe('react#1')
    expect(s.bottleneck?.sharePct).toBe(30)
  })

  it('merges by node with counts', () => {
    const s = aggregateSingleRun(run, nodes, 'node', 100, Date.now())
    const react = s.items.find((i) => i.key === 'react')
    expect(react?.durationSec).toBe(40)
    expect(react?.count).toBe(2)
    expect(react?.hasHumanWait).toBe(true)
  })

  it('merges by type', () => {
    const merged = mergeByType(flattenProcesses(run, nodes, Date.now()))
    expect(merged.find((m) => m.key === 'agent')?.durationSec).toBe(60)
    expect(merged.find((m) => m.key === 'human_gate')?.durationSec).toBe(25)
    expect(mergeByNode(flattenProcesses(run, nodes, Date.now())).length).toBe(3)
  })

  it('live elapsed refreshes in-progress nodes', () => {
    const live = baseRun({
      status: 'running',
      durationSec: 0,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'running',
            startedAt: '2026-01-01T00:00:00Z',
            durationSec: 0,
          },
        ],
      },
    })
    const t0 = Date.parse('2026-01-01T00:00:00Z')
    const a = aggregateSingleRun(live, nodes, 'process', 10, t0 + 10_000)
    const b = aggregateSingleRun(live, nodes, 'process', 20, t0 + 20_000)
    expect(a.items[0]?.durationSec).toBe(10)
    expect(b.items[0]?.durationSec).toBe(20)
  })
})

describe('aggregateMultiRuns', () => {
  it('averages by runHits and sums wall clocks', () => {
    const runA = baseRun({
      id: 'a',
      durationSec: 100,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:00Z',
            durationSec: 40,
          },
        ],
        react: [
          {
            nodeId: 'react',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:40Z',
            durationSec: 50,
          },
        ],
      },
      nodes,
    })
    const runB = baseRun({
      id: 'b',
      durationSec: 80,
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-02T00:00:00Z',
            durationSec: 20,
          },
        ],
      },
      nodes,
    })
    const multi = aggregateMultiRuns(
      [
        { run: runA, wallSec: 100 },
        { run: runB, wallSec: 80 },
      ],
      'node',
      Date.now(),
    )
    expect(multi.wallSumSec).toBe(180)
    expect(multi.selectedCount).toBe(2)
    const research = multi.items.find((i) => i.key === 'research')
    expect(research?.durationSec).toBe(60)
    expect(research?.count).toBe(2)
    expect(research?.runHits).toBe(2)
    expect(research?.avgSec).toBe(30)
    // subset: only run B
    const onlyB = aggregateMultiRuns([{ run: runB, wallSec: 80 }], 'node', Date.now())
    expect(onlyB.items).toHaveLength(1)
    expect(onlyB.wallSumSec).toBe(80)
  })

  it('buckets by per-run type when same nodeId changes type across versions', () => {
    const runOld = baseRun({
      id: 'old',
      durationSec: 30,
      nodeExecutions: {
        n1: [
          {
            nodeId: 'n1',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-01T00:00:00Z',
            durationSec: 30,
          },
        ],
      },
      nodes: [{ id: 'n1', type: 'agent', label: 'R', position: { x: 0, y: 0 }, config: {} }],
    })
    const runNew = baseRun({
      id: 'new',
      durationSec: 40,
      nodeExecutions: {
        n1: [
          {
            nodeId: 'n1',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-01-02T00:00:00Z',
            durationSec: 40,
          },
        ],
      },
      nodes: [{ id: 'n1', type: 'branch', label: 'P', position: { x: 0, y: 0 }, config: {} }],
    })
    const byNode = aggregateMultiRuns(
      [
        { run: runOld, wallSec: 30 },
        { run: runNew, wallSec: 40 },
      ],
      'node',
      Date.now(),
    )
    expect(byNode.items).toHaveLength(1)
    expect(byNode.items[0]?.durationSec).toBe(70)

    const byType = aggregateMultiRuns(
      [
        { run: runOld, wallSec: 30 },
        { run: runNew, wallSec: 40 },
      ],
      'type',
      Date.now(),
    )
    expect(byType.items.map((i) => i.key).sort()).toEqual(['agent', 'branch'])
  })
})

describe('pickDefaultTimelineNodeId', () => {
  it('picks the last running node in timeline order when several are running', () => {
    const id = pickDefaultTimelineNodeId(
      baseRun({
        status: 'running',
        nodeExecutions: {
          a: [{ nodeId: 'a', status: 'running', startedAt: '2026-01-01T00:00:00Z', iteration: 1 }],
          b: [{ nodeId: 'b', status: 'running', startedAt: '2026-01-01T00:01:00Z', iteration: 1 }],
          c: [{ nodeId: 'c', status: 'completed', startedAt: '2026-01-01T00:02:00Z', iteration: 1 }],
        },
      }),
    )
    expect(id).toBe('b')
  })

  it('prefers waiting_human over completed when both exist', () => {
    const id = pickDefaultTimelineNodeId(
      baseRun({
        status: 'waiting_human',
        nodeExecutions: {
          done: [
            { nodeId: 'done', status: 'completed', startedAt: '2026-01-01T00:00:00Z', iteration: 1 },
          ],
          gate: [
            {
              nodeId: 'gate',
              status: 'waiting_human',
              startedAt: '2026-01-01T00:01:00Z',
              iteration: 1,
            },
          ],
        },
      }),
    )
    expect(id).toBe('gate')
  })

  it('falls back to the latest executed entry when nothing is active', () => {
    const id = pickDefaultTimelineNodeId(
      baseRun({
        nodeExecutions: {
          early: [
            { nodeId: 'early', status: 'completed', startedAt: '2026-01-01T00:00:00Z', iteration: 1 },
          ],
          late: [
            { nodeId: 'late', status: 'completed', startedAt: '2026-01-01T00:05:00Z', iteration: 1 },
          ],
        },
      }),
    )
    expect(id).toBe('late')
  })

  it('does not treat entries without startedAt as the most recent execution', () => {
    const id = pickDefaultTimelineNodeId(
      baseRun({
        nodeExecutions: {
          pending: [{ nodeId: 'pending', status: 'pending', iteration: 1 }],
          done: [
            { nodeId: 'done', status: 'completed', startedAt: '2026-01-01T00:00:00Z', iteration: 1 },
          ],
        },
      }),
    )
    expect(id).toBe('done')
  })

  it('returns undefined when only unstarted shells exist', () => {
    expect(
      pickDefaultTimelineNodeId(
        baseRun({
          nodeExecutions: {
            pending: [{ nodeId: 'pending', status: 'pending', iteration: 1 }],
          },
        }),
      ),
    ).toBeUndefined()
  })

  it('returns undefined for empty executions', () => {
    expect(pickDefaultTimelineNodeId(baseRun({ nodeExecutions: {}, nodeRuns: {} }))).toBeUndefined()
  })
})
