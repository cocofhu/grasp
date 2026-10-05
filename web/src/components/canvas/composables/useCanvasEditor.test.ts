import { describe, expect, it, vi } from 'vitest'
import { reactive, ref } from 'vue'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { IMPLEMENT_CAPS, TEST_REVIEW_CAPS } from '@/test/capsFixtures'
import type { CanvasAgent } from './outlets'
import { useCanvasEditor } from './useCanvasEditor'

const t = (k: string) => k
const AGENTS: CanvasAgent[] = [
  { name: 'impl', capabilities: IMPLEMENT_CAPS },
  { name: 'tester', capabilities: TEST_REVIEW_CAPS },
]

function setup() {
  const graph = reactive({
    nodes: [
      { id: 'in', type: 'input', label: 'In', position: { x: 0, y: 0 }, config: {} },
      { id: 'a', type: 'agent', label: 'A', position: { x: 320, y: 0 }, config: { agent_profile: 'tester', prompt: 'x' } },
      { id: 'out', type: 'output', label: 'Out', position: { x: 640, y: 0 }, config: {} },
    ] as WFNode[],
    edges: [{ id: 'e1', source: 'in', target: 'a' }] as WFEdge[],
  })
  const notify = vi.fn()
  const editor = useCanvasEditor({ graph, agents: ref(AGENTS), t, typeLabel: (type) => `L-${type}`, notify })
  return { graph, editor, notify }
}

describe('useCanvasEditor', () => {
  it('adds a node, selects it and records one step', () => {
    const { graph, editor } = setup()
    const n = editor.addNode({ type: 'set_var' }, { x: 13, y: 13 })!
    expect(n).toMatchObject({ type: 'set_var', label: 'L-set_var', position: { x: 16, y: 16 } })
    expect(graph.nodes).toHaveLength(4)
    expect(editor.selectedNodeIds.value).toEqual([n.id])
    expect(editor.inspectorNodeId.value).toBe(n.id)
    expect(editor.history.size.value).toBe(1)
  })

  it('refuses a second input node with a notice', () => {
    const { graph, editor, notify } = setup()
    expect(editor.addNode({ type: 'input' })).toBeNull()
    expect(notify).toHaveBeenCalledWith('canvas.rules.singleInput')
    expect(graph.nodes).toHaveLength(3)
  })

  it('links a node added from an outlet or after the selected node', () => {
    const { graph, editor } = setup()
    const fromFail = editor.addNode({ type: 'agent', agentProfile: 'impl' }, { x: 0, y: 200 }, { kind: 'outlet', source: 'a', sourceHandle: 'fail' })!
    expect(fromFail.label).toBe('impl')
    expect(graph.edges.at(-1)).toMatchObject({ source: 'a', sourceHandle: 'fail', target: fromFail.id })

    const after = editor.addNode({ type: 'output' }, undefined, { kind: 'after', nodeId: 'a' })!
    expect(graph.edges.at(-1)).toMatchObject({ source: 'a', sourceHandle: 'pass', target: after.id })
  })

  it('inserts a node in the middle of an edge', () => {
    const { graph, editor } = setup()
    graph.edges[0]!.when = 'ok'
    const mid = editor.addNode({ type: 'set_var' }, { x: 160, y: 0 }, { kind: 'edge', edgeId: 'e1' })!
    expect(graph.edges.find((e) => e.id === 'e1')).toMatchObject({ source: 'in', target: mid.id, when: 'ok' })
    expect(graph.edges.find((e) => e.source === mid.id)).toMatchObject({ target: 'a' })
    expect(graph.edges.find((e) => e.source === mid.id)!.when).toBeUndefined()
  })

  it('connects with rule checks', () => {
    const { graph, editor, notify } = setup()
    expect(editor.connect({ source: 'a', target: 'in' })).toBe(false)
    expect(notify).toHaveBeenCalledWith('canvas.rules.inputNoIncoming')
    expect(editor.connect({ source: 'a', sourceHandle: 'pass', target: 'out' })).toBe(true)
    expect(graph.edges.at(-1)).toMatchObject({ sourceHandle: 'pass', target: 'out' })
  })

  it('updates edge fields, dropping empty values and the default kind', () => {
    const { graph, editor } = setup()
    editor.updateEdge('e1', { when: 'x > 1', kind: 'rollback', label: 'note' })
    expect(graph.edges[0]).toMatchObject({ when: 'x > 1', kind: 'rollback', label: 'note' })
    editor.updateEdge('e1', { when: '', kind: 'success', label: undefined })
    expect(graph.edges[0]).toEqual({ id: 'e1', source: 'in', target: 'a' })
  })

  it('removes the selection with its edges, and undo brings it back', () => {
    const { graph, editor } = setup()
    editor.setSelection(['a'])
    expect(editor.removeSelection()).toBe(true)
    expect(graph.nodes.map((n) => n.id)).toEqual(['in', 'out'])
    expect(graph.edges).toEqual([])
    expect(editor.removeSelection()).toBe(false)
    editor.undo()
    expect(graph.nodes).toHaveLength(3)
    expect(graph.edges).toHaveLength(1)
    editor.redo()
    expect(graph.nodes).toHaveLength(2)
  })

  it('renames through F2 state and ignores blank names', () => {
    const { graph, editor } = setup()
    editor.setSelection(['a'])
    editor.startRename()
    expect(editor.renamingId.value).toBe('a')
    editor.renameNode('a', '  ')
    expect(graph.nodes[1]!.label).toBe('A')
    editor.renameNode('a', 'Tester')
    expect(graph.nodes[1]!.label).toBe('Tester')
    expect(editor.renamingId.value).toBeNull()
  })

  it('select all hides the inspector; additive select toggles', () => {
    const { editor } = setup()
    editor.selectAll()
    expect(editor.selectedNodeIds.value).toHaveLength(3)
    expect(editor.inspectorNodeId.value).toBeNull()
    editor.selectNode('a')
    editor.selectNode('out', true)
    expect(editor.selectedNodeIds.value).toEqual(['a', 'out'])
    editor.selectNode('a', true)
    expect(editor.selectedNodeIds.value).toEqual(['out'])
  })

  it('copies, pastes and duplicates as single steps', () => {
    const { graph, editor } = setup()
    editor.setSelection(['a'])
    expect(editor.copy()).toBe(true)
    expect(editor.paste({ x: 0, y: 400 })).toBe(true)
    expect(graph.nodes).toHaveLength(4)
    expect(editor.duplicate()).toBe(true)
    expect(graph.nodes).toHaveLength(5)
    expect(editor.history.size.value).toBe(2)
  })

  it('navigates along edges with arrow keys', () => {
    const { graph, editor } = setup()
    graph.edges.push({ id: 'e2', source: 'a', sourceHandle: 'pass', target: 'out' })
    expect(editor.navigate('right')).toBe('in')
    expect(editor.navigate('right')).toBe('a')
    expect(editor.navigate('right')).toBe('out')
    expect(editor.navigate('right')).toBeNull()
    expect(editor.navigate('left')).toBe('a')
  })

  it('lays the graph out as one undoable step', async () => {
    const { graph, editor } = setup()
    for (const n of graph.nodes) n.position = { x: 0, y: 0 }
    graph.edges.push({ id: 'e2', source: 'a', sourceHandle: 'pass', target: 'out' })
    editor.history.reset()
    await editor.autoLayout(false)
    const xs = graph.nodes.map((n) => n.position!.x)
    expect(xs[0]!).toBeLessThan(xs[1]!)
    expect(xs[1]!).toBeLessThan(xs[2]!)
    expect(editor.history.size.value).toBe(1)
    editor.undo()
    expect(graph.nodes.every((n) => n.position!.x === 0)).toBe(true)
  })

  it('reports issues per node', () => {
    const { editor, graph } = setup()
    graph.nodes[1]!.config.prompt = ''
    expect(editor.nodeIssues.value.get('a')).toContain('canvas.issues.noGoal')
    expect(editor.nodeIssues.value.get('out')).toContain('canvas.issues.unreachable')
  })
})
