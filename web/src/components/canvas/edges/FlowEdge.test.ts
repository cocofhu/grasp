// @vitest-environment happy-dom
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import { clearMarkdownCache, getMarkdownParseCount, resetMarkdownParseCount } from '@/lib/shared/markdown'
import { CANVAS_CTX, type CanvasContext, type CanvasEdgeData } from '../composables/canvasContext'

vi.mock('@vue-flow/core', async () => {
  const { defineComponent: dc, ref: vref } = await import('vue')
  return {
    useVueFlow: () => ({ getNodes: vref([]) }),
    EdgeLabelRenderer: dc({ template: '<div class="label-renderer"><slot /></div>' }),
    Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
    getSmoothStepPath: () => ['M0 0 L10 10', 5, 5],
  }
})

import FlowEdge from './FlowEdge.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...canvas } },
})
const t = i18n.global.t as (k: string, n?: Record<string, unknown>) => string

const ctx: CanvasContext = {
  mode: ref('run'),
  hoveredEdge: ref<string | null>(null),
  setEdgeHover: vi.fn(),
  onNodeMenu: vi.fn(),
  onRename: vi.fn(),
  onReply: vi.fn(),
  onEdgeInsert: vi.fn(),
  onEdgeDelete: vi.fn(),
  onEdgeEdit: vi.fn(),
}

const base = { id: 'e1', sourceX: 0, sourceY: 0, targetX: 300, targetY: 0, sourcePosition: 'right', targetPosition: 'left' }

function edgeData(over: Partial<CanvasEdgeData> = {}): CanvasEdgeData {
  return {
    tone: 'default',
    dashed: false,
    label: '',
    hasCondition: false,
    editable: false,
    sourceLabel: '需求澄清',
    targetLabel: '实现',
    ...over,
  }
}

const mounted: { unmount: () => void }[] = []
afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  vi.clearAllMocks()
})
beforeEach(() => {
  clearMarkdownCache()
  resetMarkdownParseCount()
})

function mountEdge(data: CanvasEdgeData) {
  const w = mount(defineComponent({ render: () => h('svg', [h(FlowEdge, { ...base, data } as any)]) }), {
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Icon: true }, provide: { [CANVAS_CTX as symbol]: ctx } },
  })
  mounted.push(w)
  return w
}

const MD = '交接如下：\n\n# 关键变化\n\n- 已推送分支 feat/x\n- 更新 `plan.json`'

describe('FlowEdge · handoff', () => {
  it('renders no chip or label layer without a handoff', () => {
    const w = mountEdge(edgeData())
    expect(w.find('[data-testid="canvas-edge-handoff"]').exists()).toBe(false)
    expect(w.find('[data-testid="canvas-edge-mid-e1"]').exists()).toBe(false)
  })

  it('shows an icon chip labelled 交接 instead of the raw text, with aria', () => {
    const w = mountEdge(edgeData({ handoff: { text: MD, source: 'agent' } }))
    const chip = w.get('[data-testid="canvas-edge-handoff"]')
    expect(chip.text()).toBe(t('canvas.handoff.chip'))
    expect(chip.text()).not.toContain('关键变化')
    expect(chip.attributes('aria-expanded')).toBe('false')
    expect(chip.attributes('aria-label')).toBe(t('canvas.handoff.aria', { source: '需求澄清', target: '实现' }))
    expect(chip.classes()).not.toContain('is-platform')
    expect(w.find('[data-testid="canvas-edge-handoff-pop"]').exists()).toBe(false)
  })

  it('opens a card with route, markdown body and source meta, then toggles closed', async () => {
    const w = mountEdge(edgeData({ handoff: { text: MD, source: 'agent' } }))
    expect(getMarkdownParseCount()).toBe(0)

    await w.get('[data-testid="canvas-edge-handoff"]').trigger('click')
    expect(getMarkdownParseCount()).toBe(1)
    expect(w.get('[data-testid="canvas-edge-handoff"]').attributes('aria-expanded')).toBe('true')

    const pop = w.get('[data-testid="canvas-edge-handoff-pop"]')
    const text = pop.text()
    expect(text).toContain(t('canvas.handoff.title'))
    expect(text).toContain('需求澄清')
    expect(text).toContain('实现')

    const body = w.get('[data-testid="canvas-edge-handoff-body"]')
    expect(body.classes()).toContain('md')
    expect(body.find('h1').exists()).toBe(true)
    expect(body.find('ul li').exists()).toBe(true)
    expect(body.find('code').exists()).toBe(true)

    expect(w.get('[data-testid="canvas-edge-handoff-source"]').text()).toBe(t('canvas.handoff.fromNode', { source: '需求澄清' }))

    await w.get('[data-testid="canvas-edge-handoff"]').trigger('click')
    expect(w.find('[data-testid="canvas-edge-handoff-pop"]').exists()).toBe(false)
  })

  it('reuses the markdown LRU cache when reopened', async () => {
    const w = mountEdge(edgeData({ handoff: { text: MD, source: 'agent' } }))
    await w.get('[data-testid="canvas-edge-handoff"]').trigger('click')
    await w.get('[data-testid="canvas-edge-handoff"]').trigger('click')
    await w.get('[data-testid="canvas-edge-handoff"]').trigger('click')
    expect(getMarkdownParseCount()).toBe(1)
  })

  it('closes from the close button and stops wheel from panning the canvas', async () => {
    const w = mountEdge(edgeData({ handoff: { text: MD, source: 'agent' } }))
    await w.get('[data-testid="canvas-edge-handoff"]').trigger('click')
    const wheel = new WheelEvent('wheel', { bubbles: true, cancelable: true })
    w.get('[data-testid="canvas-edge-handoff-pop"]').element.dispatchEvent(wheel)
    expect(w.find('[data-testid="canvas-edge-handoff-pop"]').exists()).toBe(true)
    await w.get('[data-testid="canvas-edge-handoff-close"]').trigger('click')
    expect(w.find('[data-testid="canvas-edge-handoff-pop"]').exists()).toBe(false)
  })

  it('marks platform handoffs and swaps the footer meta', async () => {
    const w = mountEdge(edgeData({ handoff: { text: MD, source: 'platform' } }))
    const chip = w.get('[data-testid="canvas-edge-handoff"]')
    expect(chip.classes()).toContain('is-platform')
    await chip.trigger('click')
    expect(w.find('[data-testid="canvas-edge-handoff-source"]').exists()).toBe(false)
    expect(w.get('[data-testid="canvas-edge-handoff-platform"]').text()).toBe(t('canvas.handoff.platformNote'))
  })

  it('ignores empty or whitespace-only handoff text', () => {
    const blank = mountEdge(edgeData({ handoff: { text: '   \n\t ', source: 'agent' } }))
    expect(blank.find('[data-testid="canvas-edge-handoff"]').exists()).toBe(false)
    const empty = mountEdge(edgeData({ handoff: { text: '', source: 'platform' } }))
    expect(empty.find('[data-testid="canvas-edge-handoff"]').exists()).toBe(false)
  })

  it('keeps a single visit as the existing note, without a switcher', async () => {
    const w = mountEdge(edgeData({ handoff: { text: MD, source: 'agent' } }))
    await w.get('[data-testid="canvas-edge-handoff"]').trigger('click')
    expect(w.find('[data-testid="canvas-edge-handoff-tabs"]').exists()).toBe(false)
    expect(w.get('[data-testid="canvas-edge-handoff-source"]').text()).toBe(t('canvas.handoff.fromNode', { source: '需求澄清' }))
    expect(w.get('[data-testid="canvas-edge-handoff"]').attributes('aria-label')).toBe(
      t('canvas.handoff.aria', { source: '需求澄清', target: '实现' }),
    )
  })

  it('opens on the latest visit and switches body and footnote', async () => {
    const w = mountEdge(
      edgeData({
        sourceLabel: '实现',
        targetLabel: '测试评审',
        handoffs: [
          { text: '第一次：已推送工作分支', source: 'agent' },
          { text: '第二次：已按失败用例修复', source: 'agent' },
        ],
      }),
    )
    const chip = w.get('[data-testid="canvas-edge-handoff"]')
    expect(chip.text()).toBe(t('canvas.handoff.chip'))
    expect(chip.attributes('aria-label')).toBe(t('canvas.handoff.ariaCount', { source: '实现', target: '测试评审', n: 2 }))
    expect(w.find('[data-testid="canvas-edge-handoff-pop"]').exists()).toBe(false)

    await chip.trigger('click')
    const tabs = w.findAll('[data-testid="canvas-edge-handoff-tab"]')
    expect(tabs).toHaveLength(2)
    expect(tabs[0]!.text()).toBe(t('canvas.handoff.visit', { n: 1 }))
    expect(tabs[1]!.text()).toBe(t('canvas.handoff.visit', { n: 2 }))
    expect(tabs[1]!.classes()).toContain('is-on')
    expect(w.get('[data-testid="canvas-edge-handoff-body"]').text()).toContain('第二次')
    expect(w.get('[data-testid="canvas-edge-handoff-source"]').text()).toBe(t('canvas.handoff.fromNodeVisit', { source: '实现', n: 2 }))
    const route = w.get('[data-testid="canvas-edge-handoff-pop"]').text()
    expect(route).toContain('实现')
    expect(route).toContain('测试评审')

    await tabs[0]!.trigger('click')
    expect(w.findAll('[data-testid="canvas-edge-handoff-tab"]')[0]!.classes()).toContain('is-on')
    expect(w.get('[data-testid="canvas-edge-handoff-body"]').text()).toContain('第一次')
    expect(w.get('[data-testid="canvas-edge-handoff-body"]').text()).not.toContain('第二次')
    expect(w.get('[data-testid="canvas-edge-handoff-source"]').text()).toBe(t('canvas.handoff.fromNodeVisit', { source: '实现', n: 1 }))
    expect(w.find('[data-testid="canvas-edge-handoff-pop"]').exists()).toBe(true)
  })

  it('uses the platform footnote only for the visit that was written by the platform', async () => {
    const w = mountEdge(
      edgeData({
        sourceLabel: '实现',
        targetLabel: '测试评审',
        handoffs: [
          { text: '平台补写的第一次', source: 'platform' },
          { text: '节点自己写的第二次', source: 'agent' },
        ],
      }),
    )
    const chip = w.get('[data-testid="canvas-edge-handoff"]')
    expect(chip.classes()).not.toContain('is-platform')
    await chip.trigger('click')
    expect(w.find('[data-testid="canvas-edge-handoff-platform"]').exists()).toBe(false)
    expect(w.get('[data-testid="canvas-edge-handoff-source"]').text()).toBe(t('canvas.handoff.fromNodeVisit', { source: '实现', n: 2 }))

    await w.findAll('[data-testid="canvas-edge-handoff-tab"]')[0]!.trigger('click')
    expect(w.find('[data-testid="canvas-edge-handoff-source"]').exists()).toBe(false)
    expect(w.get('[data-testid="canvas-edge-handoff-platform"]').text()).toBe(t('canvas.handoff.platformNote'))
    expect(w.get('[data-testid="canvas-edge-handoff"]').classes()).toContain('is-platform')
    expect(w.get('[data-testid="canvas-edge-handoff-body"]').text()).toContain('平台补写')
  })

  it('drops a blank visit so one remaining note has no switcher', async () => {
    const w = mountEdge(
      edgeData({
        handoffs: [
          { text: MD, source: 'agent' },
          { text: '  \n', source: 'agent' },
        ],
      }),
    )
    await w.get('[data-testid="canvas-edge-handoff"]').trigger('click')
    expect(w.find('[data-testid="canvas-edge-handoff-tabs"]').exists()).toBe(false)
    expect(w.get('[data-testid="canvas-edge-handoff-body"]').text()).toContain('关键变化')
  })
})
