// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import OutputSourcesEditor from './OutputSourcesEditor.vue'

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => toast }))

const research: WFNode = { id: 'research', type: 'agent', label: '调研', position: { x: 0, y: 0 }, config: { agent_profile: 'r', prompt: '' } }
const plan: WFNode = { id: 'plan', type: 'agent', label: '计划', position: { x: 0, y: 0 }, config: { agent_profile: 'p', prompt: '' } }

function outputNode(results?: unknown): WFNode {
  return {
    id: 'output',
    type: 'output',
    label: '输出',
    position: { x: 0, y: 0 },
    config: results === undefined ? {} : { results },
  } as WFNode
}

const edges: WFEdge[] = [
  { id: 'e1', source: 'research', target: 'output' } as WFEdge,
  { id: 'e2', source: 'plan', target: 'output' } as WFEdge,
]

function mountEditor(node: WFNode, props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(OutputSourcesEditor, {
    props: { node, allNodes: [research, plan, node], edges, ...props },
    global: {
      plugins: [i18n],
      stubs: {
        AppSwitch: {
          props: ['modelValue', 'ariaLabel'],
          emits: ['update:modelValue'],
          template: '<button type="button" class="sw" @click="$emit(\'update:modelValue\', !modelValue)" />',
        },
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('OutputSourcesEditor', () => {
  it('normalises a missing or malformed results config into an array', async () => {
    const missing = outputNode()
    const w1 = mountEditor(missing)
    await flushPromises()
    expect(missing.config.results).toEqual([])
    expect(w1.text()).toBeTruthy()
    w1.unmount()

    const bogus = outputNode('not-an-array')
    const w2 = mountEditor(bogus)
    await flushPromises()
    expect(bogus.config.results).toEqual([])
    w2.unmount()
  })

  it('lists upstream outputs as available sources', async () => {
    const node = outputNode([])
    const w = mountEditor(node)
    await flushPromises()
    const vm = w.vm as any

    expect(vm.availableOptions.length).toBeGreaterThan(0)
    expect(w.find('[data-testid="output-sources-empty-available"]').exists()).toBe(false)
    expect(w.findAll('button').length).toBeGreaterThan(0)
    w.unmount()
  })

  it('shows the empty-available hint with no upstream nodes', async () => {
    const node = outputNode([])
    const w = mountEditor(node, { allNodes: [node], edges: [] })
    await flushPromises()
    expect((w.vm as any).availableOptions).toEqual([])
    expect(w.find('[data-testid="output-sources-empty-available"]').exists()).toBe(true)
    w.unmount()
  })

  it('adds a source once and warns on a duplicate click', async () => {
    const node = outputNode([])
    const w = mountEditor(node)
    await flushPromises()
    const vm = w.vm as any
    const first = vm.availableOptions[0].value

    vm.add(first)
    await flushPromises()
    expect(node.config.results).toEqual([first])
    expect(toast.success).toHaveBeenCalledTimes(1)

    vm.add(first)
    await flushPromises()
    expect(node.config.results).toEqual([first])
    expect(toast.warn).toHaveBeenCalledTimes(1)

    // The rendered button for an already-selected source is disabled.
    const added = w.findAll('button').find((b) => b.text().includes('✓'))
    expect(added?.attributes('disabled')).toBeDefined()
    w.unmount()
  })

  it('removes a source when its switch is turned off', async () => {
    const node = outputNode([])
    const w = mountEditor(node)
    await flushPromises()
    const vm = w.vm as any
    vm.add(vm.availableOptions[0].value)
    await flushPromises()
    expect(w.findAll('.sw')).toHaveLength(1)

    await w.get('.sw').trigger('click')
    await flushPromises()
    expect(node.config.results).toEqual([])
    expect(w.text()).toBeTruthy()
    w.unmount()
  })

  it('marks a template that is not an upstream output as custom', async () => {
    const node = outputNode(['handwritten.md'])
    const w = mountEditor(node)
    await flushPromises()
    const vm = w.vm as any

    expect(vm.labelFor('handwritten.md')).toContain('(')
    expect(vm.labelFor(vm.availableOptions[0].value)).not.toContain('(自定义')
    w.unmount()
  })

  it('reorders selected sources by drag and drop', async () => {
    const node = outputNode([])
    const w = mountEditor(node)
    await flushPromises()
    const vm = w.vm as any
    const [a, b] = vm.availableOptions.map((o: any) => o.value)
    vm.add(a)
    vm.add(b)
    await flushPromises()
    expect(node.config.results).toEqual([a, b])

    const dt = { effectAllowed: '' }
    vm.onDragStart(b, { dataTransfer: dt })
    expect(vm.dragId).toBe(b)
    expect(dt.effectAllowed).toBe('move')

    vm.onDrop(a)
    await flushPromises()
    expect(node.config.results).toEqual([b, a])
    expect(vm.dragId).toBeNull()
    expect(vm.dragOverId).toBeNull()
    w.unmount()
  })

  it('tolerates a drag event without a dataTransfer payload', async () => {
    const node = outputNode(['x.md'])
    const w = mountEditor(node)
    await flushPromises()
    const vm = w.vm as any

    vm.onDragStart('x.md', {})
    expect(vm.dragId).toBe('x.md')
    w.unmount()
  })

  it('ignores drops that cannot reorder anything', async () => {
    const node = outputNode(['a.md', 'b.md'])
    const w = mountEditor(node)
    await flushPromises()
    const vm = w.vm as any

    // No drag in progress.
    vm.onDrop('a.md')
    expect(node.config.results).toEqual(['a.md', 'b.md'])

    // Dropping onto itself.
    vm.onDragStart('a.md', {})
    vm.onDrop('a.md')
    expect(node.config.results).toEqual(['a.md', 'b.md'])

    // Dropping onto a template that is not in the list.
    vm.onDragStart('a.md', {})
    vm.onDrop('ghost.md')
    expect(node.config.results).toEqual(['a.md', 'b.md'])
    expect(toast.success).not.toHaveBeenCalled()
    w.unmount()
  })

  it('tracks the hovered drop target through the row handlers', async () => {
    const node = outputNode(['a.md', 'b.md'])
    const w = mountEditor(node)
    await flushPromises()
    const rows = w.findAll('[draggable="true"]')
    expect(rows).toHaveLength(2)

    await rows[0].trigger('dragstart')
    await rows[1].trigger('dragover')
    expect((w.vm as any).dragOverId).toBe('b.md')
    expect(rows[1].classes().join(' ')).toContain('ring-accent-2')

    await rows[1].trigger('dragleave')
    expect((w.vm as any).dragOverId).toBeNull()

    await rows[1].trigger('drop')
    await flushPromises()
    expect(node.config.results).toEqual(['b.md', 'a.md'])

    await rows[0].trigger('dragend')
    expect((w.vm as any).dragId).toBeNull()
    w.unmount()
  })
})
