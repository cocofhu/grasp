// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount, RouterLinkStub } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import nodes from '@/locales/zh-CN/nodes.json'
import pages from '@/locales/zh-CN/pages.json'
import { TEST_REVIEW_CAPS } from '@/test/capsFixtures'
import { buildPaletteItems, decodePaletteDrag, PALETTE_MIME, type PaletteItem } from './composables/paletteItems'
import NodePalette from './NodePalette.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...nodes, ...pages, ...canvas } },
})
const t = i18n.global.t as (k: string, n?: Record<string, unknown>) => string

function items(agents = [{ name: '测试评审', capabilities: TEST_REVIEW_CAPS }]): PaletteItem[] {
  return buildPaletteItems(agents, (type) => ({ label: `L-${type}`, desc: `D-${type}` }), t)
}

function mountPalette(props: { items?: PaletteItem[]; agentsLoading?: boolean; placingKey?: string | null } = {}) {
  return mount(NodePalette, {
    props: { items: items(), ...props },
    global: { plugins: [i18n], stubs: { Icon: true, RouterLink: RouterLinkStub } },
  })
}

describe('NodePalette', () => {
  beforeEach(() => localStorage.clear())

  it('groups project agents, control and collaboration nodes', () => {
    const w = mountPalette()
    const agent = w.find('[data-testid="palette-group-agent"]')
    expect(agent.find('[data-testid="palette-item-agent:测试评审"]').exists()).toBe(true)
    expect(agent.findAll('[data-testid^="palette-item-"]')).toHaveLength(1)
    expect(agent.find('[data-testid="palette-from-template"]').exists()).toBe(true)
    const control = w.find('[data-testid="palette-group-control"]')
    for (const type of ['input', 'output', 'set_var', 'branch']) {
      expect(control.find(`[data-testid="palette-item-type:${type}"]`).exists()).toBe(true)
    }
    const collab = w.find('[data-testid="palette-group-collab"]')
    expect(collab.findAll('[data-testid^="palette-item-"]').map((i) => i.attributes('data-testid'))).toEqual(['palette-item-type:human_gate'])
  })

  it('shows a one-line capability summary for agents', () => {
    const w = mountPalette()
    const text = w.find('[data-testid="palette-item-agent:测试评审"]').text()
    expect(text).toContain('测试评审')
    expect(text).toContain(t('canvas.caps.review'))
  })

  it('filters by search and shows the empty state', async () => {
    const w = mountPalette()
    await w.find('[data-testid="palette-search"]').setValue('branch')
    expect(w.find('[data-testid="palette-item-type:branch"]').exists()).toBe(true)
    expect(w.find('[data-testid="palette-item-type:input"]').exists()).toBe(false)
    expect(w.find('[data-testid="palette-group-agent"]').exists()).toBe(false)
    await w.find('[data-testid="palette-search"]').setValue('xyz-not-found')
    expect(w.text()).toContain(t('canvas.palette.noMatch'))
  })

  it('starts placement on click and adds right away from the keyboard', async () => {
    const w = mountPalette()
    await w.find('[data-testid="palette-item-agent:测试评审"]').trigger('click')
    expect(w.emitted('place')).toEqual([[{ type: 'agent', agentProfile: '测试评审' }]])
    expect(w.emitted('add')).toBeUndefined()
    await w.find('[data-testid="palette-item-type:output"]').trigger('keydown', { key: 'Enter' })
    expect(w.emitted('add')).toEqual([[{ type: 'output' }]])
  })

  it('highlights the item being placed', async () => {
    const w = mountPalette({ placingKey: 'type:branch' })
    const item = w.find('[data-testid="palette-item-type:branch"]')
    expect(item.attributes('data-placing')).toBe('true')
    expect(item.attributes('aria-pressed')).toBe('true')
    expect(w.find('[data-testid="palette-item-type:input"]').attributes('data-placing')).toBeUndefined()
  })

  it('puts the node spec on the drag payload', async () => {
    const w = mountPalette()
    const data = new Map<string, string>()
    const dataTransfer = { setData: (k: string, v: string) => data.set(k, v), effectAllowed: '' }
    await w.find('[data-testid="palette-item-type:human_gate"]').trigger('dragstart', { dataTransfer })
    expect(decodePaletteDrag(data.get(PALETTE_MIME))).toEqual({ type: 'human_gate', agentProfile: undefined })
    expect(dataTransfer.effectAllowed).toBe('copy')
  })

  it('hints when the project has no agents and skeletons while loading', () => {
    expect(mountPalette({ items: items([]) }).text()).toContain(t('canvas.palette.noAgents'))
    const loading = mountPalette({ items: items([]), agentsLoading: true })
    expect(loading.text()).not.toContain(t('canvas.palette.noAgents'))
    expect(loading.find('.animate-pulse').exists()).toBe(true)
  })

  it('collapses to a rail and remembers it', async () => {
    const w = mountPalette()
    await w.find('[data-testid="palette-toggle"]').trigger('click')
    expect(w.find('[data-testid="palette-search"]').exists()).toBe(false)
    expect(localStorage.getItem('grasp.canvas.paletteCollapsed')).toBe('1')
    const again = mountPalette()
    expect(again.find('[data-testid="palette-search"]').exists()).toBe(false)
    await again.find('[data-testid="palette-toggle"]').trigger('click')
    expect(again.find('[data-testid="palette-search"]').exists()).toBe(true)
  })
})
