import { describe, expect, it } from 'vitest'
import type { Run } from '@/lib/shared/types'
import { bootKey, bootSteps, clearBootRecords, getBootRecord } from '@/lib/run/bootRecord'
import { activeSegment, chainBootPending, clipTask, isPlumbingNode, isRunTerminal, normalizeRunChain, priorSegments, runChainFromRun, seedChainBoots } from './runConversationChain'

function run(over: Partial<Run> = {}): Run {
  return {
    id: 'r1',
    status: 'running',
    nodes: [
      { id: 'clarify', type: 'agent', label: '需求澄清', position: { x: 0, y: 0 }, config: {} },
      { id: 'code', type: 'agent', label: '编码', position: { x: 0, y: 0 }, config: {} },
      { id: 'accept', type: 'agent', label: '验收测试', position: { x: 0, y: 0 }, config: { prompt: '  编写并执行验收用例  ' } },
      { id: 'deploy', type: 'agent', label: '部署', position: { x: 0, y: 0 }, config: {} },
    ],
    nodeRuns: {
      accept: { nodeId: 'accept', iteration: 1, status: 'waiting_human', startedAt: '2026-10-21T05:03:00+08:00' },
      clarify: { nodeId: 'clarify', iteration: 1, status: 'completed', startedAt: '2026-10-21T05:00:00+08:00' },
      code: { nodeId: 'code', iteration: 1, status: 'completed', startedAt: '2026-10-21T05:01:00+08:00' },
      deploy: { nodeId: 'deploy', status: 'pending' },
    },
    clarifyByNode: {
      clarify: { nodeId: 'clarify', iteration: 1, done: true, turns: [{ role: 'human', text: 'a', at: '' }] },
      accept: { nodeId: 'accept', iteration: 1, done: false, turns: [{ role: 'agent', text: 'b', at: '' }] },
    },
    artifacts: [],
    ...over,
  } as unknown as Run
}

describe('runConversationChain', () => {
  it('derives an ordered chain from GET /runs/:id (fallback)', () => {
    const c = runChainFromRun(run())
    expect(c.segments.map((s) => `${s.nodeId}:${s.kind}`)).toEqual(['clarify:chat', 'code:auto', 'accept:chat'])
    expect(c.current).toMatchObject({ id: 'accept', name: '验收测试', mode: 'manual', state: 'waiting_human' })
    expect(priorSegments(c, 'accept', 1).map((s) => s.label)).toEqual(['需求澄清', '编码'])
  })

  it('carries the node task: chain field first, config.prompt fallback, clipped', () => {
    expect(activeSegment(runChainFromRun(run()), 'accept')?.task).toBe('编写并执行验收用例')
    const c = normalizeRunChain({ runId: 'r1', status: 'running', segments: [{ nodeId: 'a', task: '做 A' }] })
    expect(activeSegment(c, 'a')?.task).toBe('做 A')
    expect(clipTask('x'.repeat(300))).toHaveLength(281)
    expect(clipTask('  ')).toBe('')
  })

  it('carries the note each visit handed to the next node', () => {
    const c = normalizeRunChain({
      runId: 'r1',
      status: 'running',
      segments: [
        { nodeId: 'a', handoff: '  交给 B  ', handoffSource: 'platform' },
        { nodeId: 'b', handoff: '   ' },
      ],
    })
    expect(c.segments[0]).toMatchObject({ handoff: '交给 B', handoffSource: 'platform' })
    expect(c.segments[1]!.handoff).toBeUndefined()
    const r = run()
    const first = Object.keys(r.nodeRuns)[0]!
    r.nodeRuns[first] = { ...r.nodeRuns[first]!, outputs: { handoff: '上游说明' } }
    expect(runChainFromRun(r).segments.find((s) => s.nodeId === first)).toMatchObject({ handoff: '上游说明', handoffSource: 'agent' })
  })

  it('keeps each visit\'s boot and seeds the boot records from it', () => {
    clearBootRecords()
    const c = normalizeRunChain({
      runId: 'r1',
      status: 'running',
      segments: [
        { nodeId: 'a', startedAt: '2026-10-21T05:00:00Z', status: 'completed', boot: { status: 'ready', readyAt: '2026-10-21T05:00:28Z' } },
        { nodeId: 'b', startedAt: '2026-10-21T05:01:00Z', status: 'running', boot: { status: 'creating' } },
        { nodeId: 'c', status: 'completed', boot: null },
      ],
    })
    expect(c.segments.map((s) => s.boot)).toEqual([{ status: 'ready', readyAt: '2026-10-21T05:00:28Z' }, { status: 'creating', readyAt: undefined }, undefined])
    seedChainBoots(c)
    const a = getBootRecord(bootKey('r1', 'a', 1))
    expect(a?.live).toBe(true)
    expect(bootSteps(a).map((s) => [s.state, s.sec])).toEqual([['done', 28], ['done', null], ['done', null]])
    expect(bootSteps(getBootRecord(bootKey('r1', 'b', 1))).map((s) => s.state)).toEqual(['running', 'pending', 'pending'])
    expect(getBootRecord(bootKey('r1', 'c', 1))).toBeNull()
    expect(chainBootPending(c, 'a', 1)).toBe(false)
    expect(chainBootPending(c, 'b', 1)).toBe(true)
    expect(chainBootPending(c, 'c', 1)).toBe(false)
    expect(chainBootPending(c, 'd', 1)).toBe(true)
    clearBootRecords()
  })

  it('marks a running node without a conversation as auto', () => {
    const r = run()
    r.nodeRuns.accept = { nodeId: 'accept', iteration: 1, status: 'completed', startedAt: '2026-10-21T05:03:00+08:00' }
    r.nodeRuns.deploy = { nodeId: 'deploy', iteration: 1, status: 'running', startedAt: '2026-10-21T05:04:00+08:00' }
    expect(runChainFromRun(r).current).toMatchObject({ id: 'deploy', name: '部署', mode: 'auto' })
  })

  it('normalizes the proposed conversation-chain response', () => {
    const c = normalizeRunChain({
      runId: 'r1',
      status: 'completed',
      currentNode: null,
      segments: [
        { nodeId: 'clarify', iteration: 1, name: '需求澄清', kind: 'clarify', status: 'completed', turns: [] },
        { nodeId: 'gate', iteration: 1, name: '发布审批', kind: 'gate', status: 'completed' },
        { nodeId: 'code', name: '编码', kind: 'agent', status: 'completed' },
      ],
    })
    expect(c.segments.map((s) => s.kind)).toEqual(['chat', 'gate', 'auto'])
    expect(isRunTerminal(c.status)).toBe(true)
    expect(priorSegments(c, 'missing').length).toBe(3)
  })

  it('#850 contract: name fallback, input/output hidden, blank turns dropped, endedAt / eventCount kept', () => {
    const t0 = '2026-10-20T23:06:11Z'
    const res = {
      runId: 'r1',
      status: 'waiting_human',
      currentNode: { id: 'impl', name: '', iteration: 1, kind: 'review', mode: 'manual' as const, state: 'waiting_human' },
      segments: [
        { nodeId: 'input', iteration: 1, name: '输入', kind: 'agent', mode: 'auto', status: 'completed', startedAt: t0, endedAt: t0, eventCount: 0 },
        { nodeId: 'clarify', iteration: 1, name: '', kind: 'clarify', mode: 'manual', status: 'completed', startedAt: t0, endedAt: '2026-10-20T23:06:12Z',
          turns: [{ role: 'agent' as const, text: '问', at: t0 }, { role: 'human' as const, text: '  ', at: t0 }], task: '澄清' },
        { nodeId: 'design', iteration: 1, name: '方案设计', kind: 'agent', mode: 'auto', status: 'completed', startedAt: t0, endedAt: '2026-10-20T23:06:16Z', eventCount: 8 },
        { nodeId: 'impl', iteration: 1, name: '', kind: 'review', mode: 'manual', status: 'waiting_human', startedAt: t0, turns: [] },
      ],
    }
    const nodes = [
      { id: 'input', type: 'input', label: '输入' },
      { id: 'clarify', type: 'agent', label: '需求澄清' },
      { id: 'impl', type: 'agent', label: '实现' },
    ]
    const c = normalizeRunChain(res, nodes)
    expect(c.segments.map((s) => s.nodeId)).toEqual(['clarify', 'design', 'impl'])
    expect(c.segments[0]).toMatchObject({ label: '需求澄清', kind: 'chat', endedAt: '2026-10-20T23:06:12Z', task: '澄清' })
    expect(c.segments[0].turns.map((t) => t.text)).toEqual(['问'])
    expect(c.segments[1]).toMatchObject({ kind: 'auto', eventCount: 8 })
    expect(c.current?.name).toBe('实现')
    // No graph meta: input/output recognised by the wire signature; unknown names fall back to the id.
    const bare = normalizeRunChain(res)
    expect(bare.segments.map((s) => s.label)).toEqual(['clarify', '方案设计', 'impl'])
    expect(isPlumbingNode(nodes, 'input')).toBe(true)
    expect(isPlumbingNode(nodes, 'impl')).toBe(false)
  })

  it('keeps questions, tools and parts, and still drops a blank turn', () => {
    const c = normalizeRunChain({
      runId: 'r1',
      status: 'completed',
      segments: [{
        nodeId: 'clarify', iteration: 1, name: '需求澄清', kind: 'clarify', status: 'completed',
        turns: [
          {
            role: 'agent', text: '', at: '',
            questions: [{ id: 'q', prompt: '类型', options: [{ id: 'a', label: '其他' }] }],
            tools: [{ title: 'Read', status: 'completed' }],
            parts: [{ kind: 'tool', title: 'Read' }],
          },
          { role: 'human', text: '  ', at: '' },
        ],
      }],
    })
    expect(c.segments[0].turns).toHaveLength(1)
    expect(c.segments[0].turns[0]).toMatchObject({
      questions: [{ prompt: '类型' }],
      tools: [{ title: 'Read' }],
      parts: [{ title: 'Read' }],
    })
  })

  it('gate segments: own segment with turns + decision, or folded into upstream turns (today)', () => {
    const own = normalizeRunChain({
      runId: 'r1',
      status: 'running',
      segments: [
        { nodeId: 'impl', iteration: 1, name: '实现', kind: 'review', status: 'completed', turns: [{ role: 'agent', text: 'a', at: '' }] },
        { nodeId: 'gate', iteration: 1, name: '发布审批', kind: 'gate', mode: 'manual', status: 'completed', turns: [{ role: 'human', text: '打回', at: '' }], decision: 'reject' },
        { nodeId: 'gate', iteration: 2, name: '发布审批', kind: 'gate', mode: 'manual', status: 'completed' },
      ],
    })
    expect(own.segments.map((s) => [s.kind, s.turns.length, s.decision])).toEqual([['chat', 1, undefined], ['gate', 1, 'reject'], ['gate', 0, undefined]])
    const folded = normalizeRunChain({
      runId: 'r1',
      status: 'running',
      segments: [{ nodeId: 'impl', iteration: 1, name: '实现', kind: 'review', status: 'completed', turns: [{ role: 'agent', text: 'a', at: '' }, { role: 'human', text: '打回', at: '' }] }],
    })
    expect(folded.segments.map((s) => [s.kind, s.turns.length])).toEqual([['chat', 2]])
  })
})
