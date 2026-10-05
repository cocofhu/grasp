// @vitest-environment happy-dom
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import nodes from '@/locales/zh-CN/nodes.json'
import pages from '@/locales/zh-CN/pages.json'
import type { WFEdge } from '@/lib/shared/types'
import { CANVAS_CTX, type CanvasContext, type CanvasEdgeData, type CanvasNodeData } from './composables/canvasContext'
import { buildPaletteItems } from './composables/paletteItems'

vi.mock('@vue-flow/core', async () => {
  const { defineComponent: dc } = await import('vue')
  return {
    Handle: dc({ props: ['id', 'type', 'position'], template: '<div class="handle" :data-handle-type="type" :data-handle-id="id"><slot /></div>' }),
    EdgeLabelRenderer: dc({ template: '<div class="label-renderer"><slot /></div>' }),
    Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
    getSmoothStepPath: () => ['M0 0 L10 10', 5, 5],
  }
})

import AgentNode from './nodes/AgentNode.vue'
import ControlNode from './nodes/ControlNode.vue'
import CollabNode from './nodes/CollabNode.vue'
import FlowEdge from './edges/FlowEdge.vue'
import EdgeLabelEditor from './edges/EdgeLabelEditor.vue'
import QuickAdd from './chrome/QuickAdd.vue'
import CanvasToolbar from './chrome/CanvasToolbar.vue'
import ShortcutHelp from './chrome/ShortcutHelp.vue'
import EmptyCanvas from './chrome/EmptyCanvas.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...nodes, ...pages, ...canvas } },
})
const t = i18n.global.t as (k: string, n?: Record<string, unknown>) => string

function makeCtx(mode: 'edit' | 'run' = 'edit'): CanvasContext {
  return {
    mode: ref(mode),
    hoveredEdge: ref<string | null>(null),
    setEdgeHover: vi.fn(function (this: void, id: string | null) {
      ctxRef.hoveredEdge.value = id
    }),
    onNodeMenu: vi.fn(),
    onRename: vi.fn(),
    onReply: vi.fn(),
    onEdgeInsert: vi.fn(),
    onEdgeDelete: vi.fn(),
    onEdgeEdit: vi.fn(),
  }
}
let ctxRef: CanvasContext

const mounted: { unmount: () => void }[] = []
afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
})

function mountWith(comp: any, props: Record<string, unknown>, ctx: CanvasContext | null = makeCtx()) {
  if (ctx) ctxRef = ctx
  const w = mount(comp, {
    props,
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Icon: true }, provide: ctx ? { [CANVAS_CTX as symbol]: ctx } : {} },
  })
  mounted.push(w)
  return w
}

function nodeData(over: Partial<CanvasNodeData> = {}): CanvasNodeData {
  return {
    nodeType: 'agent',
    title: '测试评审',
    typeLabel: 'Agent',
    icon: 'robot',
    agentName: '测试评审',
    flags: { ask: false, preview: true, review: true, gate: true },
    goal: '跑测试并评审',
    outlets: [
      { id: 'pass', label: '通过', tone: 'ok' },
      { id: 'fail', label: '不通过', tone: 'err' },
    ],
    hasTarget: true,
    mode: 'edit',
    ...over,
  }
}

describe('AgentNode', () => {
  it('renders avatar, goal, capability icons and labelled pass / fail outlets', () => {
    const w = mountWith(AgentNode, { id: 'n1', data: nodeData() })
    expect(w.find('.cnode-avatar').text()).toBe('测')
    expect(w.find('[data-testid="canvas-node-goal"]').text()).toBe('跑测试并评审')
    expect(w.findAll('[data-testid^="canvas-cap-"]').map((c) => c.attributes('data-testid'))).toEqual([
      'canvas-cap-preview',
      'canvas-cap-review',
      'canvas-cap-gate',
    ])
    expect(w.find('[data-testid="canvas-outlet-pass"]').classes()).toContain('tone-ok')
    expect(w.find('[data-testid="canvas-outlet-fail"]').text()).toContain('不通过')
    expect(w.find('[data-testid="canvas-port-in"]').exists()).toBe(true)
  })

  it('prompts for an agent and a goal when missing', () => {
    const w = mountWith(AgentNode, { id: 'n1', data: nodeData({ agentName: undefined, goal: '', flags: undefined, outlets: [{ id: '', label: '', tone: 'default' }] }) })
    expect(w.text()).toContain(t('canvas.node.noAgent'))
    const goal = w.find('[data-testid="canvas-node-goal"]')
    expect(goal.classes()).toContain('is-empty')
    expect(goal.text()).toBe(t('canvas.node.goalEmpty'))
    expect(w.find('.cnode-outlets').exists()).toBe(false)
    expect(w.find('[data-testid="canvas-outlet-default"]').exists()).toBe(true)
  })

  it('shows the issue badge with messages in edit mode', () => {
    const w = mountWith(AgentNode, { id: 'n1', data: nodeData({ issues: ['缺少目标', '未连接'] }), selected: true })
    expect(w.find('.cnode').classes()).toContain('is-selected')
    expect(w.find('[data-testid="canvas-node-issues"]').text()).toBe('2')
    expect(w.find('.cnode-issues').text()).toContain('缺少目标')
  })

  it('runs the more menu actions', async () => {
    const ctx = makeCtx()
    const w = mountWith(AgentNode, { id: 'n1', data: nodeData() }, ctx)
    await w.find('[data-testid="canvas-node-menu"]').trigger('click')
    await w.find('[data-testid="canvas-node-menu-delete"]').trigger('click')
    expect(ctx.onNodeMenu).toHaveBeenCalledWith('n1', 'delete')
    expect(w.find('.cnode-menu').exists()).toBe(false)
  })

  it('renames inline: Enter commits, Esc cancels', async () => {
    const ctx = makeCtx()
    const w = mountWith(AgentNode, { id: 'n1', data: nodeData({ renaming: true }) }, ctx)
    const input = w.find('[data-testid="canvas-node-rename"]')
    await input.setValue('新名字')
    await input.trigger('keydown', { key: 'Enter' })
    expect(ctx.onRename).toHaveBeenCalledWith('n1', '新名字')
    await new Promise((r) => queueMicrotask(() => r(null)))
    await input.trigger('keydown', { key: 'Escape' })
    expect(ctx.onRename).toHaveBeenLastCalledWith('n1', null)
  })

  it('shows run states: iteration, failure reason, reply button', async () => {
    const ctx = makeCtx('run')
    const failed = mountWith(AgentNode, { id: 'n1', data: nodeData({ mode: 'run', status: 'failed', failReason: '测试失败', iteration: 3 }) }, ctx)
    expect(failed.find('.cnode').classes()).toEqual(expect.arrayContaining(['is-run', 'st-failed']))
    expect(failed.find('[data-testid="canvas-node-iteration"]').text()).toContain('3')
    expect(failed.find('[data-testid="canvas-node-fail"]').text()).toBe('测试失败')
    expect(failed.find('[data-testid="canvas-node-menu"]').exists()).toBe(false)

    const waiting = mountWith(AgentNode, { id: 'n2', data: nodeData({ mode: 'run', status: 'waiting_human', issues: ['x'] }) }, ctx)
    await waiting.find('[data-testid="canvas-node-reply"]').trigger('click')
    expect(ctx.onReply).toHaveBeenCalledWith('n2')
    expect(waiting.find('[data-testid="canvas-node-issues"]').exists()).toBe(false)
  })

  it('marks ports valid / invalid while connecting with the reason', () => {
    const w = mountWith(AgentNode, { id: 'n1', data: nodeData({ connect: { valid: false, reason: '输入节点只能有一个' } }) })
    const port = w.find('[data-testid="canvas-port-in"]')
    expect(port.classes()).toContain('is-invalid')
    expect(port.text()).toBe('输入节点只能有一个')
    const ok = mountWith(AgentNode, { id: 'n2', data: nodeData({ connect: { valid: true } }) })
    expect(ok.find('[data-testid="canvas-port-in"]').classes()).toContain('is-valid')
  })
})

describe('Control / collab nodes', () => {
  it('renders the type icon, summary and one outlet per branch case', () => {
    const w = mountWith(ControlNode, {
      id: 'b',
      data: nodeData({
        nodeType: 'branch',
        title: '分支',
        icon: 'branch',
        agentName: undefined,
        subtitle: '2 个分支',
        outlets: [
          { id: 'c1', label: 'x > 1', tone: 'default' },
          { id: 'else', label: '否则', tone: 'default' },
        ],
      }),
    })
    expect(w.find('.cnode').attributes('style')).toContain('200px')
    expect(w.text()).toContain('2 个分支')
    expect(w.findAll('.cnode-outlet')).toHaveLength(2)
  })

  it('input nodes have no target port', () => {
    const w = mountWith(ControlNode, { id: 'in', data: nodeData({ nodeType: 'input', hasTarget: false, outlets: [{ id: '', label: '', tone: 'default' }] }) })
    expect(w.find('[data-testid="canvas-port-in"]').exists()).toBe(false)
  })

  it('collab nodes list action outlets', () => {
    const w = mountWith(CollabNode, {
      id: 'g',
      data: nodeData({
        nodeType: 'human_gate',
        title: '人工确认',
        icon: 'gate',
        agentName: undefined,
        outlets: [
          { id: 'approve', label: '通过', tone: 'default' },
          { id: 'reject', label: '驳回', tone: 'default' },
        ],
      }),
    })
    expect(w.find('[data-testid="canvas-outlet-approve"]').exists()).toBe(true)
    expect(w.find('[data-testid="canvas-outlet-reject"]').exists()).toBe(true)
  })
})

describe('FlowEdge', () => {
  const edgeData = (over: Partial<CanvasEdgeData> = {}): CanvasEdgeData => ({
    tone: 'default',
    dashed: false,
    label: '',
    hasCondition: false,
    editable: true,
    sourceLabel: 'A',
    targetLabel: 'B',
    ...over,
  })
  const base = { id: 'e1', sourceX: 0, sourceY: 0, targetX: 300, targetY: 0, sourcePosition: 'right', targetPosition: 'left' }
  const mountEdge = (props: Record<string, unknown>, ctx = makeCtx()) =>
    mountWith(defineComponent({ render: () => h('svg', [h(FlowEdge, props as any)]) }), {}, ctx)

  it('shows the condition label and edits it on click', async () => {
    const ctx = makeCtx()
    const w = mountEdge({ ...base, data: edgeData({ label: 'x > 1', hasCondition: true, tone: 'ok' }) }, ctx)
    expect(w.find('[data-testid="canvas-edge-e1"]').classes()).toContain('tone-ok')
    await w.find('[data-testid="canvas-edge-label"]').trigger('click')
    expect(ctx.onEdgeEdit).toHaveBeenCalledWith('e1', expect.anything())
    expect(w.find('[data-testid="canvas-edge-insert"]').exists()).toBe(false)
  })

  it('reveals insert / delete on hover', async () => {
    const ctx = makeCtx()
    const w = mountEdge({ ...base, data: edgeData() }, ctx)
    expect(w.find('[data-testid="canvas-edge-mid-e1"]').exists()).toBe(false)
    await w.find('.vue-flow__edge-interaction').trigger('mouseenter')
    expect(ctx.setEdgeHover).toHaveBeenCalledWith('e1')
    await w.vm.$nextTick()
    await w.find('[data-testid="canvas-edge-insert"]').trigger('click')
    expect(ctx.onEdgeInsert).toHaveBeenCalledWith('e1', expect.anything())
    await w.find('[data-testid="canvas-edge-delete"]').trigger('click')
    expect(ctx.onEdgeDelete).toHaveBeenCalledWith('e1')
    await w.find('[data-testid="canvas-edge-edit"]').trigger('click')
    expect(ctx.onEdgeEdit).toHaveBeenCalled()
  })

  it('draws back edges dashed and run states as classes', () => {
    const back = mountEdge({ ...base, sourceX: 600, targetX: 100, data: edgeData({ run: 'active' }) })
    const path = back.find('[data-testid="canvas-edge-e1"]')
    expect(path.attributes('data-back')).toBe('true')
    expect(path.classes()).toEqual(expect.arrayContaining(['is-dashed', 'run-active']))
  })

  it('never shows controls when not editable', async () => {
    const ctx = makeCtx('run')
    const w = mountEdge({ ...base, selected: true, data: edgeData({ editable: false, label: 'when' }) }, ctx)
    expect(w.find('[data-testid="canvas-edge-delete"]').exists()).toBe(false)
    expect(w.find('[data-testid="canvas-edge-label"]').attributes('disabled')).toBeDefined()
  })
})

describe('EdgeLabelEditor', () => {
  const edge: WFEdge = { id: 'e1', source: 'a', target: 'b', when: 'x', kind: 'rollback', label: 'n' }
  it('edits condition, kind and note and closes on Enter / Esc / outside click', async () => {
    const w = mountWith(EdgeLabelEditor, { edge, x: 100, y: 100, bounds: { width: 800, height: 600 } }, null)
    await new Promise((r) => setTimeout(r))
    expect(document.activeElement).toBe(w.find('[data-testid="edge-when-input"]').element)
    expect((w.find('[data-testid="edge-kind-select"]').element as HTMLSelectElement).value).toBe('rollback')
    await w.find('[data-testid="edge-when-input"]').setValue('y > 2')
    await w.find('[data-testid="edge-kind-select"]').setValue('failure')
    await w.find('[data-testid="edge-note-input"]').setValue('备注')
    expect(w.emitted('update')).toEqual([[{ when: 'y > 2' }], [{ kind: 'failure' }], [{ label: '备注' }]])
    await w.find('[data-testid="edge-when-input"]').trigger('keydown', { key: 'Enter' })
    await w.find('[data-testid="edge-label-editor"]').trigger('keydown', { key: 'Escape' })
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    expect(w.emitted('close')).toHaveLength(3)
    await w.find('[data-testid="edge-delete"]').trigger('click')
    expect(w.emitted('delete')).toHaveLength(1)
  })
})

describe('QuickAdd', () => {
  const items = buildPaletteItems([{ name: '实现' }], (type) => ({ label: `L-${type}`, desc: '' }), t)
  it('searches, moves with arrows and picks with Enter', async () => {
    const w = mountWith(QuickAdd, { items, title: '添加节点', x: 10, y: 10, bounds: { width: 800, height: 600 } }, null)
    await new Promise((r) => setTimeout(r))
    const input = w.find('[data-testid="quick-add-input"]')
    expect(document.activeElement).toBe(input.element)
    await input.setValue('L-')
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'Enter' })
    expect((w.emitted('pick')![0]![0] as any).key).toBe('type:output')
    await input.setValue('')
    await w.find('[data-testid="quick-add-item-agent:实现"]').trigger('click')
    expect((w.emitted('pick')![1]![0] as any).spec).toEqual({ type: 'agent', agentProfile: '实现' })
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    expect(w.emitted('close')).toHaveLength(1)
  })
})

describe('chrome', () => {
  it('toolbar emits its actions and disables undo / redo', async () => {
    const w = mountWith(CanvasToolbar, { zoom: 0.75, editable: true, minimap: false, canUndo: false, canRedo: true }, null)
    expect(w.find('[data-testid="canvas-zoom"]').text()).toBe('75%')
    const buttons = w.findAll('button')
    for (const b of buttons) await b.trigger('click')
    const emitted = Object.keys(w.emitted())
    for (const e of ['zoom-in', 'zoom-out', 'fit', 'layout', 'toggle-minimap', 'redo', 'help']) expect(emitted).toContain(e)
    expect(emitted).not.toContain('undo')
    const ro = mountWith(CanvasToolbar, { zoom: 1, editable: false, minimap: true }, null)
    ro.findAll('button').forEach((b) => void b.trigger('click'))
    expect(Object.keys(ro.emitted())).not.toContain('layout')
  })

  it('shortcut help lists the shortcuts and closes', async () => {
    const w = mountWith(ShortcutHelp, {}, null)
    expect(w.text()).toContain(t('canvas.shortcuts.actions.undo'))
    await w.find('[data-testid="shortcut-help"]').trigger('mousedown')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('empty canvas offers template and blank starts', async () => {
    const w = mountWith(EmptyCanvas, {}, null)
    expect(w.text()).toContain('/')
    await w.find('[data-testid="empty-canvas-template"]').trigger('click')
    await w.find('[data-testid="empty-canvas-blank"]').trigger('click')
    expect(w.emitted('template')).toHaveLength(1)
    expect(w.emitted('blank')).toHaveLength(1)
  })
})
