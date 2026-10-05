// @vitest-environment happy-dom
import { defineComponent, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import nodes from '@/locales/zh-CN/nodes.json'
import pages from '@/locales/zh-CN/pages.json'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS } from '@/test/capsFixtures'

const flow = vi.hoisted(() => ({
  fitView: vi.fn(async () => true),
  zoomIn: vi.fn(),
  zoomOut: vi.fn(),
  setCenter: vi.fn(async () => true),
}))

vi.mock('@vue-flow/core', async () => {
  const { ref: vref, defineComponent: dc } = await import('vue')
  return {
    VueFlow: dc({
      name: 'VueFlow',
      props: ['nodes', 'edges', 'nodeTypes', 'edgeTypes', 'isValidConnection', 'nodesDraggable', 'nodesConnectable'],
      emits: [
        'nodes-change',
        'edges-change',
        'node-click',
        'node-double-click',
        'node-drag-stop',
        'pane-click',
        'connect-start',
        'connect',
        'connect-end',
        'move-start',
        'nodes-initialized',
      ],
      template: '<div class="vue-flow" data-testid="vue-flow"><slot /></div>',
    }),
    useVueFlow: () => ({
      ...flow,
      screenToFlowCoordinate: (p: { x: number; y: number }) => ({ x: p.x, y: p.y }),
      viewport: vref({ x: 0, y: 0, zoom: 1 }),
      findNode: (id: string) => ({ id, dimensions: { width: 240, height: 100 }, computedPosition: { x: 10, y: 20 } }),
      dimensions: vref({ width: 800, height: 600 }),
    }),
    MarkerType: { ArrowClosed: 'arrowclosed' },
    Handle: dc({ template: '<div />' }),
    Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
    getSmoothStepPath: () => ['M0 0', 0, 0],
  }
})
vi.mock('@vue-flow/background', () => ({ Background: defineComponent({ template: '<div data-testid="flow-bg" />' }) }))
vi.mock('@vue-flow/minimap', () => ({ MiniMap: defineComponent({ template: '<div data-testid="canvas-minimap" />' }) }))

import { useCanvasEditor } from './composables/useCanvasEditor'
import { PALETTE_MIME } from './composables/paletteItems'
import type { CanvasAgent } from './composables/outlets'
import WorkflowCanvas from './WorkflowCanvas.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...nodes, ...pages, ...canvas } },
})
const t = i18n.global.t as (k: string, n?: Record<string, unknown>) => string

const AGENTS: CanvasAgent[] = [
  { name: '需求澄清', capabilities: CLARIFY_CAPS },
  { name: '实现', capabilities: IMPLEMENT_CAPS },
  { name: '测试评审', capabilities: TEST_REVIEW_CAPS },
]

function sampleGraph() {
  return reactive({
    nodes: [
      { id: 'in', type: 'input', label: '输入', position: { x: 0, y: 0 }, config: { variables: [] } },
      { id: 'test', type: 'agent', label: '测试评审', position: { x: 320, y: 0 }, config: { agent_profile: '测试评审', prompt: '测' } },
      { id: 'out', type: 'output', label: '输出', position: { x: 640, y: 0 }, config: { results: [] } },
    ] as WFNode[],
    edges: [{ id: 'e1', source: 'in', target: 'test' }] as WFEdge[],
  })
}

function makeEditor(graph = sampleGraph()) {
  return useCanvasEditor({ graph, agents: ref(AGENTS), t, typeLabel: (type) => type })
}

const mounted: { unmount: () => void }[] = []
afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  vi.clearAllMocks()
})

function mountCanvas(props: Record<string, unknown>) {
  const w = mount(WorkflowCanvas, {
    props: props as any,
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
  mounted.push(w)
  return w
}

const vueFlow = (w: ReturnType<typeof mountCanvas>) => w.findComponent({ name: 'VueFlow' })

describe('WorkflowCanvas · edit mode', () => {
  it('maps model nodes to agent / control components and edges to flow edges', () => {
    const editor = makeEditor()
    const w = mountCanvas({ nodes: editor.graph.nodes, edges: editor.graph.edges, editor })
    const fnodes = vueFlow(w).props('nodes') as any[]
    expect(fnodes.map((n) => [n.id, n.type])).toEqual([
      ['in', 'control'],
      ['test', 'agent'],
      ['out', 'control'],
    ])
    const test = fnodes[1]
    expect(test.data.outlets.map((o: any) => o.id)).toEqual(['pass', 'fail'])
    expect(test.data.flags.gate).toBe(true)
    expect((vueFlow(w).props('edges') as any[])[0]).toMatchObject({ id: 'e1', type: 'flow' })
    expect(vueFlow(w).props('nodesDraggable')).toBe(true)
  })

  it('validates and records connections', async () => {
    const editor = makeEditor()
    const w = mountCanvas({ nodes: editor.graph.nodes, edges: editor.graph.edges, editor })
    const valid = vueFlow(w).props('isValidConnection') as (c: any) => boolean
    expect(valid({ source: 'test', target: 'in' })).toBe(false)
    expect(valid({ source: 'test', sourceHandle: 'pass', target: 'out' })).toBe(true)
    // Vue Flow re-validates stored edges on every edges update; existing ones must survive.
    const existing = editor.graph.edges[0]
    expect(valid({ ...existing })).toBe(true)
    expect(valid({ source: existing.source, sourceHandle: existing.sourceHandle, target: existing.target })).toBe(false)

    vueFlow(w).vm.$emit('connect-start', { nodeId: 'test', handleId: 'pass', handleType: 'source' })
    await flushPromises()
    const during = (vueFlow(w).props('nodes') as any[]).find((n) => n.id === 'in')
    expect(during.data.connect).toMatchObject({ valid: false })
    vueFlow(w).vm.$emit('connect', { source: 'test', sourceHandle: 'pass', target: 'out' })
    vueFlow(w).vm.$emit('connect-end', new MouseEvent('mouseup'))
    expect(editor.graph.edges.at(-1)).toMatchObject({ source: 'test', sourceHandle: 'pass', target: 'out' })
    expect(editor.history.canUndo.value).toBe(true)
    expect(w.find('[data-testid="quick-add"]').exists()).toBe(false)
  })

  it('opens quick add when a connection is dropped on empty space and links the new node', async () => {
    const editor = makeEditor()
    const w = mountCanvas({ nodes: editor.graph.nodes, edges: editor.graph.edges, editor })
    vueFlow(w).vm.$emit('connect-start', { nodeId: 'test', handleId: 'fail', handleType: 'source' })
    vueFlow(w).vm.$emit('connect-end', new MouseEvent('mouseup', { clientX: 400, clientY: 300 }))
    await flushPromises()
    const qa = w.find('[data-testid="quick-add"]')
    expect(qa.exists()).toBe(true)
    await qa.find('[data-testid="quick-add-item-agent:实现"]').trigger('click')
    const added = editor.graph.nodes.at(-1)!
    expect(added).toMatchObject({ type: 'agent', config: { agent_profile: '实现' } })
    expect(editor.graph.edges.at(-1)).toMatchObject({ source: 'test', sourceHandle: 'fail', target: added.id })
    expect(w.find('[data-testid="quick-add"]').exists()).toBe(false)
  })

  it('adds a node dropped from the palette', async () => {
    const editor = makeEditor()
    const w = mountCanvas({ nodes: editor.graph.nodes, edges: editor.graph.edges, editor })
    const ev = new Event('drop', { bubbles: true, cancelable: true }) as DragEvent
    Object.assign(ev, {
      clientX: 500,
      clientY: 300,
      dataTransfer: { types: [PALETTE_MIME], getData: () => JSON.stringify({ type: 'branch' }) },
    })
    w.find('[data-testid="workflow-canvas"]').element.dispatchEvent(ev)
    await flushPromises()
    const added = editor.graph.nodes.at(-1)!
    expect(added.type).toBe('branch')
    expect(added.position!.x % 8).toBe(0)
    expect(editor.selectedNodeIds.value).toEqual([added.id])
  })

  it('records a node drag as a single snapped step', async () => {
    const editor = makeEditor()
    const w = mountCanvas({ nodes: editor.graph.nodes, edges: editor.graph.edges, editor })
    vueFlow(w).vm.$emit('node-drag-stop', { nodes: [{ id: 'test', position: { x: 333, y: 45 } }] })
    expect(editor.graph.nodes[1]!.position).toEqual({ x: 336, y: 48 })
    expect(editor.history.size.value).toBe(1)
    editor.undo()
    expect(editor.graph.nodes[1]!.position).toEqual({ x: 320, y: 0 })
  })

  it('syncs selection from Vue Flow and opens the inspector target on double click', async () => {
    const editor = makeEditor()
    const w = mountCanvas({ nodes: editor.graph.nodes, edges: editor.graph.edges, editor })
    vueFlow(w).vm.$emit('nodes-change', [{ type: 'select', id: 'test', selected: true }])
    vueFlow(w).vm.$emit('edges-change', [{ type: 'select', id: 'e1', selected: true }])
    expect(editor.selectedNodeIds.value).toEqual(['test'])
    expect(editor.selectedEdgeIds.value).toEqual(['e1'])
    expect(editor.inspectorNodeId.value).toBeNull()
    vueFlow(w).vm.$emit('node-double-click', { node: { id: 'out' } })
    expect(editor.inspectorNodeId.value).toBe('out')
    expect(editor.focusGoalTick.value).toBe(1)
    vueFlow(w).vm.$emit('pane-click')
    expect(editor.selectedNodeIds.value).toEqual([])
    expect(w.emitted('pane-click')).toHaveLength(1)
  })

  it('handles undo, delete, save and help shortcuts', async () => {
    const editor = makeEditor()
    const w = mountCanvas({ nodes: editor.graph.nodes, edges: editor.graph.edges, editor })
    Object.defineProperty(w.element, 'offsetParent', { configurable: true, get: () => document.body })
    editor.setSelection(['out'])
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Delete', cancelable: true }))
    expect(editor.graph.nodes.map((n) => n.id)).toEqual(['in', 'test'])
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'z', ctrlKey: true, cancelable: true }))
    expect(editor.graph.nodes.map((n) => n.id)).toEqual(['in', 'test', 'out'])
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 's', ctrlKey: true, cancelable: true }))
    expect(w.emitted('save')).toHaveLength(1)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '?', cancelable: true }))
    await flushPromises()
    expect(w.find('[data-testid="shortcut-help"]').exists()).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }))
    await flushPromises()
    expect(w.find('[data-testid="shortcut-help"]').exists()).toBe(false)
  })

  it('ignores shortcuts while a modal dialog is open elsewhere', () => {
    const editor = makeEditor()
    mountCanvas({ nodes: editor.graph.nodes, edges: editor.graph.edges, editor })
    const modal = document.createElement('div')
    modal.setAttribute('aria-modal', 'true')
    document.body.appendChild(modal)
    editor.setSelection(['out'])
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Delete', cancelable: true }))
    modal.remove()
    expect(editor.graph.nodes).toHaveLength(3)
  })

  it('starts from the default template on an empty canvas and lays it out', async () => {
    const graph = reactive({ nodes: [] as WFNode[], edges: [] as WFEdge[] })
    const editor = makeEditor(graph)
    const w = mountCanvas({ nodes: graph.nodes, edges: graph.edges, editor })
    expect(w.find('[data-testid="empty-canvas"]').exists()).toBe(true)
    await w.find('[data-testid="empty-canvas-template"]').trigger('click')
    await vi.waitFor(() => expect(flow.fitView).toHaveBeenCalled())
    expect(graph.nodes.map((n) => n.id)).toEqual(['input', 'clarify', 'implement', 'test_review', 'output'])
    expect(graph.nodes.find((n) => n.id === 'test_review')!.config.agent_profile).toBe('测试评审')
    const xs = ['input', 'clarify', 'implement', 'test_review', 'output'].map((id) => graph.nodes.find((n) => n.id === id)!.position!.x)
    expect([...xs].sort((a, b) => a - b)).toEqual(xs)
    expect(editor.history.size.value).toBe(1)
    await w.setProps({ nodes: graph.nodes })
    expect(w.find('[data-testid="empty-canvas"]').exists()).toBe(false)
  })

  it('starts blank with an input and an output node', async () => {
    const graph = reactive({ nodes: [] as WFNode[], edges: [] as WFEdge[] })
    const editor = makeEditor(graph)
    const w = mountCanvas({ nodes: graph.nodes, edges: graph.edges, editor })
    await w.find('[data-testid="empty-canvas-blank"]').trigger('click')
    expect(graph.nodes.map((n) => n.type)).toEqual(['input', 'output'])
    expect(editor.history.size.value).toBe(1)
    expect(editor.selectedNodeIds.value).toEqual([])
  })

  it('auto-lays out an unplaced graph once on init without an undo step', async () => {
    const graph = sampleGraph()
    for (const n of graph.nodes) n.position = { x: 0, y: 0 }
    graph.edges.push({ id: 'e2', source: 'test', sourceHandle: 'pass', target: 'out' })
    const editor = makeEditor(graph)
    const w = mountCanvas({ nodes: graph.nodes, edges: graph.edges, editor, autoLayoutOnInit: true })
    vueFlow(w).vm.$emit('nodes-initialized')
    await vi.waitFor(() => expect(flow.fitView).toHaveBeenCalled())
    expect(new Set(graph.nodes.map((n) => n.position!.x)).size).toBe(3)
    expect(editor.history.canUndo.value).toBe(false)
  })
})

describe('WorkflowCanvas · run mode', () => {
  const runProps = () => {
    const g = sampleGraph()
    return {
      nodes: g.nodes,
      edges: g.edges,
      mode: 'run',
      agents: AGENTS,
      statusMap: { in: 'completed', test: 'running' },
      iterations: { test: 2 },
    }
  }

  it('is read-only and passes run state to nodes and edges', () => {
    const w = mountCanvas(runProps())
    expect(vueFlow(w).props('nodesDraggable')).toBe(false)
    expect(vueFlow(w).props('nodesConnectable')).toBe(false)
    const fnodes = vueFlow(w).props('nodes') as any[]
    expect(fnodes.map((n) => n.data.status)).toEqual(['completed', 'running', 'pending'])
    expect(fnodes[1].data.iteration).toBe(2)
    expect((vueFlow(w).props('edges') as any[])[0].data.run).toBe('traversed')
    expect(w.find('[data-testid="empty-canvas"]').exists()).toBe(false)
  })

  it('lays out an unplaced snapshot for display without touching the model', async () => {
    const p = runProps()
    for (const n of p.nodes) n.position = { x: 0, y: 0 }
    p.edges.push({ id: 'e2', source: 'test', sourceHandle: 'pass', target: 'out' })
    const w = mountCanvas({ ...p, autoLayoutOnInit: true })
    vueFlow(w).vm.$emit('nodes-initialized')
    await vi.waitFor(() => {
      const xs = (vueFlow(w).props('nodes') as any[]).map((n) => n.position.x)
      expect(new Set(xs).size).toBe(3)
    })
    expect(p.nodes.every((n) => n.position!.x === 0 && n.position!.y === 0)).toBe(true)
  })

  it('emits select-node on click', () => {
    const w = mountCanvas(runProps())
    vueFlow(w).vm.$emit('node-click', { node: { id: 'test' } })
    expect(w.emitted('select-node')).toEqual([['test']])
  })

  it('follows the running node and turns follow off when the user pans', async () => {
    const w = mountCanvas({ ...runProps(), follow: true, followNodeId: null })
    expect(w.find('[data-testid="canvas-follow"]').attributes('aria-pressed')).toBe('true')
    await w.setProps({ followNodeId: 'test' })
    await flushPromises()
    expect(flow.setCenter).toHaveBeenCalled()
    vueFlow(w).vm.$emit('move-start')
    expect(w.emitted('update:follow')).toEqual([[false]])
    await w.find('[data-testid="canvas-follow"]').trigger('click')
    expect(w.emitted('update:follow')!.at(-1)).toEqual([false])
  })

  it('hides the follow toggle when follow is not bound', () => {
    expect(mountCanvas(runProps()).find('[data-testid="canvas-follow"]').exists()).toBe(false)
  })
})
