import { describe, expect, it } from 'vitest'
import type { Run } from '@/lib/shared/types'
import {
  applyPersistedClarify,
  artifactsListFingerprint,
  clarifyStreamAheadOfServer,
  clarifyTurnsFingerprint,
  mergeRunChromeFields,
  reactSessionsBusyFingerprint,
  runChromeFingerprint,
  runSnapshotUnchanged,
} from './mergeRunChrome'

function run(over: Partial<Run> = {}): Run {
  return {
    id: 'run-1',
    workflowId: 'wf',
    workflowName: 'wf',
    status: 'running',
    trigger: 'manual',
    startedAt: '2026-01-01T00:00:00Z',
    durationSec: 0,
    progress: 0.2,
    nodeRuns: { n1: { nodeId: 'n1', status: 'running', outputs: {} } },
    artifacts: [{ id: 'a1', name: 'plan.json', kind: 'json', nodeId: 'n1', sizeBytes: 10, createdAt: '', workflowName: '' }],
    reactSessions: {
      n1: { busy: true, waiting: 0, items: [{ id: 'q1', text: 'hello' }] },
    },
    clarify: { nodeId: 'n1', turns: [{ role: 'agent', text: 'streaming', at: 't' }], done: false },
    ...over,
  } as unknown as Run
}

describe('mergeRunChromeFields (g1.1)', () => {
  it('patches status, progress, nodeRuns, artifacts and keeps dialogue buffers', () => {
    const current = run()
    const sessions = current.reactSessions
    const clarify = current.clarify
    const snapshot = run({
      status: 'waiting_human',
      progress: 0.6,
      durationSec: 12,
      nodeRuns: { n1: { nodeId: 'n1', status: 'waiting_human', outputs: {} } },
      artifacts: [
        {
          id: 'a1',
          name: 'plan.json',
          kind: 'json',
          nodeId: 'n1',
          sizeBytes: 99,
          revision: 2,
          updatedAt: 't2',
          createdAt: '',
          workflowName: '',
        },
      ],
      reactSessions: { n1: { busy: false, waiting: 0, items: [] } },
      clarify: { nodeId: 'n1', turns: [], done: true },
    })

    const merged = mergeRunChromeFields(current, snapshot)
    expect(merged.status).toBe('waiting_human')
    expect(merged.progress).toBe(0.6)
    expect(merged.nodeRuns.n1.status).toBe('waiting_human')
    expect(merged.artifacts[0].sizeBytes).toBe(99)
    expect(merged.reactSessions).toBe(sessions)
    expect(merged.clarify).toBe(clarify)
    expect(merged.clarify?.turns[0].text).toBe('streaming')
  })

  it('reuses the artifacts array when the list fingerprint is unchanged (g2.2)', () => {
    const current = run()
    const snapshot = run({
      artifacts: current.artifacts.map((a) => ({ ...a })),
    })
    const merged = mergeRunChromeFields(current, snapshot)
    expect(merged.artifacts).toBe(current.artifacts)
    expect(artifactsListFingerprint(merged.artifacts)).toBe(artifactsListFingerprint(current.artifacts))
  })

  it('runSnapshotUnchanged is true for identical chrome + busy flags', () => {
    const a = run()
    const b = run()
    expect(runChromeFingerprint(a)).toBe(runChromeFingerprint(b))
    expect(reactSessionsBusyFingerprint(a)).toBe(reactSessionsBusyFingerprint(b))
    expect(clarifyTurnsFingerprint(a)).toBe(clarifyTurnsFingerprint(b))
    expect(runSnapshotUnchanged(a, b)).toBe(true)
    expect(runSnapshotUnchanged(a, run({ progress: 0.9 }))).toBe(false)
  })
})

describe('clarify turn fingerprint (g1.1 / g2.1)', () => {
  const chrome = {
    status: 'waiting_human' as const,
    progress: 0.4,
    reactSessions: { n1: { busy: false, waiting: 0, items: [] } },
  }

  function slot(turns: { role: 'agent' | 'human'; text: string; thought?: string; streaming?: boolean }[], done = false) {
    return {
      nodeId: 'n1',
      done,
      turns: turns.map((t) => ({ at: 't', thought: '', ...t })),
    }
  }

  function parked(
    turns: { role: 'agent' | 'human'; text: string; thought?: string; streaming?: boolean }[],
    extra: Partial<Run> = {},
  ) {
    const clarify = slot(turns)
    return run({
      ...chrome,
      clarify,
      clarifyByNode: { n1: { ...clarify, turns: clarify.turns.map((t) => ({ ...t })) } },
      ...extra,
    })
  }

  it('empty turns and turns with text are not the same snapshot when chrome matches', () => {
    const empty = parked([])
    const filled = parked([
      { role: 'human', text: '开始' },
      { role: 'agent', text: '已落盘回复', thought: '先想清楚' },
    ])
    expect(runChromeFingerprint(empty)).toBe(runChromeFingerprint(filled))
    expect(reactSessionsBusyFingerprint(empty)).toBe(reactSessionsBusyFingerprint(filled))
    expect(runSnapshotUnchanged(empty, filled)).toBe(false)
    expect(clarifyTurnsFingerprint(empty)).not.toBe(clarifyTurnsFingerprint(filled))
  })

  it('two identical transcripts stay unchanged, including clarifyByNode', () => {
    const turns = [
      { role: 'human' as const, text: '开始' },
      { role: 'agent' as const, text: '已落盘回复', thought: '先想清楚' },
    ]
    const a = parked(turns)
    const b = parked(turns)
    expect(clarifyTurnsFingerprint(a)).toBe(clarifyTurnsFingerprint(b))
    expect(runSnapshotUnchanged(a, b)).toBe(true)
  })

  it('last-turn thought or done flips the fingerprint without a chrome change', () => {
    const base = parked([{ role: 'agent', text: '正文', thought: '甲' }])
    const thought = parked([{ role: 'agent', text: '正文', thought: '乙' }])
    const done = run({
      ...chrome,
      clarify: slot([{ role: 'agent', text: '正文', thought: '甲' }], true),
      clarifyByNode: {
        n1: slot([{ role: 'agent', text: '正文', thought: '甲' }], true),
      },
    })
    expect(runSnapshotUnchanged(base, thought)).toBe(false)
    expect(runSnapshotUnchanged(base, done)).toBe(false)
  })

  it('a clarifyByNode-only growth is a snapshot change (UI prefers per-node turns)', () => {
    const empty = slot([])
    const current = run({
      ...chrome,
      clarify: empty,
      clarifyByNode: { n1: { ...empty, turns: [] } },
    })
    const snapshot = run({
      ...chrome,
      clarify: empty,
      clarifyByNode: {
        n1: slot([{ role: 'agent', text: '仅节点转录', thought: '思考' }]),
      },
    })
    expect(runSnapshotUnchanged(current, snapshot)).toBe(false)
  })
})

describe('applyPersistedClarify (g1.2 / g1.3 / g2.2)', () => {
  const chrome = {
    status: 'waiting_human' as const,
    progress: 0.4,
  }

  function slot(
    turns: { role: 'agent' | 'human'; text: string; thought?: string; streaming?: boolean }[],
    nodeId = 'n1',
  ) {
    return {
      nodeId,
      done: false,
      turns: turns.map((t) => ({ at: 't', thought: '', ...t })),
    }
  }

  it('writes a longer idle transcript onto an empty panel and clears stale busy', () => {
    const current = run({
      ...chrome,
      reactSessions: { n1: { busy: true, waiting: 0, items: [] } },
      clarify: slot([]),
      clarifyByNode: { n1: slot([]) },
    })
    const saved = slot([
      { role: 'human', text: '开始' },
      { role: 'agent', text: '已落盘回复', thought: '先想清楚' },
    ])
    const snapshot = run({
      ...chrome,
      reactSessions: { n1: { busy: false, waiting: 0, items: [] } },
      clarify: saved,
      clarifyByNode: { n1: { ...saved, turns: saved.turns.map((t) => ({ ...t })) } },
    })
    expect(runSnapshotUnchanged(current, snapshot)).toBe(false)
    expect(clarifyStreamAheadOfServer(current, snapshot)).toBe(false)

    const merged = applyPersistedClarify(current, snapshot)
    expect(merged.clarify?.turns.map((t) => t.text)).toEqual(['开始', '已落盘回复'])
    expect(merged.clarify?.turns[1]?.thought).toBe('先想清楚')
    expect(merged.clarifyByNode?.n1?.turns.map((t) => t.text)).toEqual(['开始', '已落盘回复'])
    expect(merged.reactSessions?.n1?.busy).toBe(false)
    expect(merged.status).toBe('waiting_human')
  })

  it('keeps a streaming bubble when the server snapshot is shorter', () => {
    const live = slot([{ role: 'agent', text: 'live bubble', thought: '思考中', streaming: true }])
    const current = run({
      ...chrome,
      reactSessions: { n1: { busy: true, waiting: 0, items: [] } },
      clarify: live,
      clarifyByNode: { n1: { ...live, turns: live.turns.map((t) => ({ ...t })) } },
    })
    const snapshot = run({
      ...chrome,
      reactSessions: { n1: { busy: true, waiting: 0, items: [] } },
      clarify: slot([]),
      clarifyByNode: { n1: slot([]) },
    })
    expect(clarifyStreamAheadOfServer(current, snapshot)).toBe(true)
    const merged = applyPersistedClarify(current, snapshot)
    expect(merged.clarify?.turns[0]?.text).toBe('live bubble')
    expect(merged.clarify?.turns[0]?.thought).toBe('思考中')
    expect(merged.clarify?.turns[0]?.streaming).toBe(true)
    expect(merged.clarifyByNode?.n1?.turns[0]?.text).toBe('live bubble')
  })

  it('does not copy another node while the streaming node is held', () => {
    const live = slot([{ role: 'agent', text: 'live bubble', streaming: true }])
    const current = run({
      ...chrome,
      reactSessions: {
        n1: { busy: true, waiting: 0, items: [] },
        n2: { busy: false, waiting: 0, items: [] },
      },
      clarify: live,
      clarifyByNode: {
        n1: { ...live, turns: live.turns.map((t) => ({ ...t })) },
        n2: slot([], 'n2'),
      },
    })
    const snapshot = run({
      ...chrome,
      reactSessions: {
        n1: { busy: true, waiting: 0, items: [] },
        n2: { busy: false, waiting: 0, items: [] },
      },
      clarify: slot([]),
      clarifyByNode: {
        n1: slot([]),
        n2: slot([{ role: 'agent', text: 'n2 已落盘' }], 'n2'),
      },
    })
    const merged = applyPersistedClarify(current, snapshot)
    expect(merged.clarifyByNode?.n1?.turns[0]?.text).toBe('live bubble')
    expect(merged.clarify?.turns[0]?.text).toBe('live bubble')
    expect(merged.clarifyByNode?.n2?.turns[0]?.text).toBe('n2 已落盘')
  })
})
