import { describe, expect, it } from 'vitest'
import type { InboxItem, Run } from '@/lib/shared/types'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { autoProcessNodeName, handoffProcessAuto, isAutoProcessNode, isAutoProcessRun, keepRowOrder, mergeRunRows, pinRunId, resolveHandoffProcessAuto, runRowFromRun, sidebarAutoNodeName, sidebarAutoTarget, sidebarRunGroup, uniqueAutoDestination } from './sidebarRunRows'

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
  it('shows one row per unfinished run, the open item winning over a sibling of the same run', () => {
    const rows = mergeRunRows(
      [item('r1', 'clarify'), item('r2', 'a'), item('r1', 'review'), item('r9', 'extra')],
      [run('r1'), run('r2')],
      'r1:review',
    )
    expect(rows.map((r) => `${r.runId}:${r.nodeId}`)).toEqual(['r1:review', 'r2:a'])
    expect(mergeRunRows([item('r1', 'clarify'), item('r1', 'review')], [run('r1')], null).map((r) => r.nodeId)).toEqual(['clarify'])
  })

  it('follows the unfinished list order and still decorates a run that has a pending item', () => {
    const rows = mergeRunRows([item('r2', 'a'), item('r1', 'clarify')], [run('r1'), run('r2')], null)
    expect(rows.map((r) => `${r.runId}:${r.nodeId}`)).toEqual(['r1:clarify', 'r2:a'])
  })

  it('does not append inbox runs that are outside the unfinished list', () => {
    const running = Array.from({ length: 7 }, (_, i) => run(`run-${i}`))
    const pending = [
      ...running.map((r) => item(r.id, 'node', r.title)),
      ...Array.from({ length: 4 }, (_, i) => item(`extra-${i}`, 'node', `多余 ${i}`)),
    ]
    const rows = mergeRunRows(pending, running, null)
    expect(rows).toHaveLength(7)
    expect(rows.map((r) => r.runId)).toEqual(running.map((r) => r.id))
    expect(rows.every((r) => r.nodeId === 'node')).toBe(true)
  })

  it('adds running runs without a pending item after the ones that have one, never twice', () => {
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

describe('isAutoProcessNode', () => {
  const node = (over: Record<string, unknown>) =>
    ({ id: 'n', name: '实现', iteration: 1, kind: 'agent', mode: 'auto', state: 'running', ...over }) as never

  it('is an automatic agent that is running or stopping', () => {
    expect(isAutoProcessNode(node({}))).toBe(true)
    expect(isAutoProcessNode(node({ state: 'stopping' }))).toBe(true)
    expect(isAutoProcessNode(node({ state: 'waiting_human' }))).toBe(false)
    expect(isAutoProcessNode(node({ mode: 'manual' }))).toBe(false)
    expect(isAutoProcessNode(node({ kind: 'takeover', mode: 'manual', state: 'stopping' }))).toBe(false)
    expect(isAutoProcessNode(node({ kind: 'clarify', mode: 'manual', state: 'running' }))).toBe(false)
    expect(isAutoProcessNode(node({ kind: 'gate', mode: 'manual', state: 'waiting_human' }))).toBe(false)
    expect(isAutoProcessNode(null)).toBe(false)
  })

  it('reads the node name only for an automatic row, and skips a blank name', () => {
    const asRun = (currentNode?: Record<string, unknown>) => ({ id: 'r', currentNode }) as never
    expect(isAutoProcessRun(asRun(node({})))).toBe(true)
    expect(autoProcessNodeName(asRun(node({ name: '实现' })))).toBe('实现')
    expect(autoProcessNodeName(asRun(node({ name: '  ' })))).toBe('')
    expect(autoProcessNodeName(asRun(node({ kind: 'takeover', mode: 'manual', state: 'stopping', name: '接管' })))).toBe('')
    expect(autoProcessNodeName(asRun())).toBe('')
  })
})

describe('sidebar handoff group', () => {
  const human = { id: 'r', currentNode: { id: 'clarify', name: '澄清', iteration: 1, kind: 'clarify', mode: 'manual', state: 'waiting_human' } } as never
  const claim = { fromNodeId: 'clarify', confirmedAuto: true, autoNodeId: 'impl', autoNodeName: '实现' }

  it('counts a settled automatic handoff before the snapshot is running', () => {
    expect(sidebarRunGroup(human, claim)).toBe('auto')
    expect(sidebarAutoNodeName(human, claim)).toBe('实现')
    expect(sidebarRunGroup(human, { ...claim, confirmedAuto: false })).toBe('running')
    expect(sidebarAutoNodeName(human, null)).toBe('')
  })

  it('keeps the snapshot name once the automatic node is running, and leaves when the snapshot moved on', () => {
    const running = { id: 'r', currentNode: { id: 'impl', name: '实现', iteration: 1, kind: 'agent', mode: 'auto', state: 'running' } } as never
    expect(sidebarRunGroup(running, null)).toBe('auto')
    expect(sidebarAutoNodeName(running, claim)).toBe('实现')
    const review = { id: 'r', currentNode: { id: 'impl', name: '实现', iteration: 1, kind: 'review', mode: 'manual', state: 'waiting_human' } } as never
    expect(sidebarRunGroup(review, claim)).toBe('running')
    const laterAuto = { id: 'r', currentNode: { id: 'impl', name: '实现', iteration: 1, kind: 'agent', mode: 'auto', state: 'busy' } } as never
    expect(sidebarRunGroup(laterAuto, claim)).toBe('auto')
  })
})

describe('uniqueAutoDestination', () => {
  const node = (id: string, type: WFNode['type'], over: Partial<WFNode> = {}): WFNode =>
    ({ id, type, label: over.label ?? id, position: { x: 0, y: 0 }, config: {}, ...over })
  const edge = (source: string, target: string, over: Partial<WFEdge> = {}): WFEdge =>
    ({ id: `${source}-${target}-${over.sourceHandle ?? ''}`, source, target, ...over })
  const auto = (id: string, label = id) => node(id, 'agent', { label, caps: { interaction: 'auto' } })
  const clarify = (id: string) => node(id, 'agent', { label: id, caps: { interaction: 'clarify', tools: ['ask_question'] } })

  it('settles a single automatic agent and skips plumbing', () => {
    const nodes = [clarify('ask'), node('out', 'output', { label: '输出' }), auto('impl', '实现')]
    expect(sidebarAutoTarget(nodes, [edge('ask', 'impl')], 'ask')).toEqual({ nodeId: 'impl', name: '实现' })
    expect(sidebarAutoTarget(nodes, [edge('ask', 'out'), edge('out', 'impl')], 'ask')).toEqual({ nodeId: 'impl', name: '实现' })
  })

  it('does not guess when the graph, the guard, or the destination is not a single automatic agent', () => {
    const nodes = [clarify('ask'), auto('impl', '实现'), node('gate', 'human_gate', { label: '审批' }), node('branch', 'branch')]
    expect(sidebarAutoTarget(nodes, undefined, 'ask')).toBeNull()
    expect(sidebarAutoTarget(nodes, [], 'ask')).toBeNull()
    expect(uniqueAutoDestination(nodes, [edge('ask', 'impl', { when: 'vars.ready' }), edge('ask', 'gate')], 'ask', '')).toBeNull()
    expect(sidebarAutoTarget(nodes, [edge('ask', 'impl'), edge('ask', 'gate', { sourceHandle: 'pass' })], 'ask')).toEqual({ nodeId: 'impl', name: '实现' })
    expect(sidebarAutoTarget(nodes, [edge('ask', 'gate')], 'ask')).toBeNull()
    expect(sidebarAutoTarget(nodes, [edge('ask', 'branch')], 'ask')).toBeNull()
    expect(sidebarAutoTarget(nodes, [edge('ask', 'out')], 'ask')).toBeNull()
    expect(sidebarAutoTarget(
      [node('gate', 'human_gate'), auto('impl', '实现'), auto('revise', '退回')],
      [edge('gate', 'impl', { sourceHandle: 'approve' }), edge('gate', 'revise', { sourceHandle: 'revise' })],
      'gate',
      'approve',
    )).toEqual({ nodeId: 'impl', name: '实现' })
    expect(sidebarAutoTarget(
      [node('gate', 'human_gate'), auto('a', '甲'), auto('b', '乙')],
      [edge('gate', 'a', { sourceHandle: 'approve' }), edge('gate', 'b', { sourceHandle: 'approve' })],
      'gate',
      'approve',
    )).toEqual({ nodeId: 'a', name: '甲' })
    expect(sidebarAutoTarget(
      [clarify('ask'), auto('impl', '实现')],
      [edge('ask', 'impl', { sourceHandle: 'pass' }), edge('ask', 'impl', { sourceHandle: 'fail' })],
      'ask',
    )).toBeNull()
    const gated = node('ask', 'agent', { caps: { interaction: 'clarify', tools: ['ask_question'], writes: [{ schema: 'test_result', required: true }] } })
    expect(sidebarAutoTarget([gated, auto('impl', '实现')], [edge('ask', 'impl', { sourceHandle: 'pass' })], 'ask')).toBeNull()
  })

  it('skips a guard this confirmation has already ruled out', () => {
    const nodes = [node('gate', 'human_gate'), auto('impl', '实现')]
    expect(sidebarAutoTarget(
      nodes,
      [edge('gate', 'gone', { sourceHandle: 'approve', when: 'false' }), edge('gate', 'impl', { sourceHandle: 'approve' })],
      'gate',
      'approve',
    )).toEqual({ nodeId: 'impl', name: '实现' })
    expect(sidebarAutoTarget(
      nodes,
      [edge('gate', 'impl', { sourceHandle: 'approve', kind: 'rollback' }), edge('gate', 'impl', { sourceHandle: 'approve' })],
      'gate',
      'approve',
    )).toBeNull()
  })
})

describe('handoffProcessAuto', () => {
  const node = (id: string, type: WFNode['type'], over: Partial<WFNode> = {}): WFNode =>
    ({ id, type, label: over.label ?? id, position: { x: 0, y: 0 }, config: {}, ...over })
  const edge = (source: string, target: string, over: Partial<WFEdge> = {}): WFEdge =>
    ({ id: `${source}-${target}-${over.sourceHandle ?? ''}`, source, target, ...over })
  const auto = (id: string, label = id) => node(id, 'agent', { label, caps: { interaction: 'auto' } })
  const clarify = (id: string) => node(id, 'agent', { label: id, caps: { interaction: 'clarify', tools: ['ask_question'] } })

  it('returns true, false, or null for automatic, not automatic, and still unknown', () => {
    const nodes = [clarify('ask'), auto('impl', '实现'), node('gate', 'human_gate', { label: '审批' }), node('branch', 'branch'), node('out', 'output', { label: '输出' })]
    expect(handoffProcessAuto(nodes, [edge('ask', 'impl')], 'ask')).toBe(true)
    expect(handoffProcessAuto(nodes, [edge('ask', 'out'), edge('out', 'impl')], 'ask')).toBe(true)
    expect(handoffProcessAuto(nodes, [edge('ask', 'gate')], 'ask')).toBe(false)
    expect(handoffProcessAuto(nodes, [edge('ask', 'branch')], 'ask')).toBe(false)
    expect(handoffProcessAuto(nodes, [edge('ask', 'out')], 'ask')).toBe(false)
    expect(handoffProcessAuto(nodes, [edge('ask', 'impl', { when: 'vars.ready' }), edge('ask', 'gate')], 'ask')).toBeNull()
    expect(handoffProcessAuto(nodes, [], 'ask')).toBeNull()
    expect(handoffProcessAuto(nodes, undefined, 'ask')).toBeNull()
    expect(resolveHandoffProcessAuto({
      nodes,
      edges: [edge('ask', 'gate')],
      fromNodeId: 'ask',
      current: { id: 'impl', mode: 'auto' },
    })).toBe(true)
    expect(resolveHandoffProcessAuto({
      nodes,
      edges: [edge('ask', 'impl')],
      fromNodeId: 'ask',
      current: { id: 'gate', mode: 'manual' },
    })).toBe(false)
  })
})

describe('pinRunId', () => {
  it('reads old run:node pins as the run', () => {
    expect(pinRunId('run-1:clarify')).toBe('run-1')
    expect(pinRunId('run-1')).toBe('run-1')
  })
})
