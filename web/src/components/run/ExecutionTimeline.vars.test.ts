// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { NodeRun, Run, WFNode } from '@/lib/shared/types'
import ExecutionTimeline from './ExecutionTimeline.vue'

const nodes: WFNode[] = [
  { id: 'input', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'research', type: 'agent', label: '调研', position: { x: 0, y: 0 }, config: {} },
]

function exec(over: Partial<NodeRun> = {}): NodeRun {
  return {
    nodeId: 'research',
    iteration: 1,
    status: 'completed',
    durationSec: 12,
    startedAt: '2026-07-18T00:00:00Z',
    outputs: {},
    ...over,
  } as NodeRun
}

function baseRun(over: Partial<Run> = {}): Run {
  return {
    id: 'run-1',
    workflowId: 'wf-1',
    workflowName: 'wf',
    status: 'completed',
    createdAt: '2026-07-18T00:00:00Z',
    startedAt: '2026-07-18T00:00:00Z',
    durationSec: 100,
    nodes: [],
    edges: [],
    nodeStates: {},
    artifacts: [],
    nodeExecutions: { research: [exec()] },
    ...over,
  } as Run
}

function mountTimeline(run: Run, props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ExecutionTimeline, {
    props: { run, nodes, selectedNodeId: null, selectedExecIdx: -1, ...props },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        StatusPill: true,
        VarValueDisplay: { props: ['value'], template: '<pre data-testid="var-full">{{ value }}</pre>' },
        TruncatedTextTooltip: { props: ['text'], template: '<span>{{ text }}</span>' },
      },
    },
    attachTo: document.body,
  })
}

beforeEach(() => {
  // scrollIntoView is not implemented in happy-dom.
  Element.prototype.scrollIntoView = vi.fn()
})

afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('ExecutionTimeline', () => {
  it('renders the empty state when nothing has executed', async () => {
    const w = mountTimeline(baseRun({ nodeExecutions: {}, nodeRuns: {} }))
    await flushPromises()
    expect((w.vm as any).items).toEqual([])
    expect(w.findAll('[data-testid="timeline-item"]')).toHaveLength(0)
    expect(w.text()).toBeTruthy()
    w.unmount()
  })

  it('falls back to nodeRuns when a node has no execution history', async () => {
    const w = mountTimeline(
      baseRun({
        nodeExecutions: { research: [] },
        nodeRuns: { research: exec({ status: 'running', startedAt: '2026-07-18T00:00:00Z' }) },
      } as never),
    )
    await flushPromises()
    const vm = w.vm as any
    expect(vm.items).toHaveLength(1)
    expect(vm.items[0].status).toBe('running')
    w.unmount()
  })

  it('labels an execution by its node id when the node is unknown', async () => {
    const w = mountTimeline(baseRun({ nodeExecutions: { ghost: [exec({ nodeId: 'ghost' })] } } as never))
    await flushPromises()
    const vm = w.vm as any
    expect(vm.items[0].label).toBe('ghost')
    expect(vm.items[0].type).toBe('agent')
    expect(vm.iconOf('nope')).toBe('agent')
    expect(vm.iconOf('research')).toBeTruthy()
    w.unmount()
  })

  it('numbers iterations from the array index when the payload omits them', async () => {
    const w = mountTimeline(
      baseRun({
        nodeExecutions: {
          research: [
            exec({ iteration: 0, startedAt: '2026-07-18T00:00:00Z' }),
            exec({ iteration: 0, startedAt: '2026-07-18T00:00:01Z' }),
          ],
        },
      } as never),
    )
    await flushPromises()
    expect((w.vm as any).items.map((i: any) => i.iteration)).toEqual([1, 2])
    // The repeat badge only shows from the second pass onwards.
    expect(w.text()).toContain('2')
    w.unmount()
  })

  it('sorts variable snapshot keys and formats chip text', async () => {
    const w = mountTimeline(
      baseRun({
        nodeExecutions: {
          research: [exec({ varsSnapshot: { zebra: 1, apple: 'a', nested: { x: 1 } } })],
        },
      } as never),
    )
    await flushPromises()
    const vm = w.vm as any

    expect(vm.items[0].vars.map((v: any) => v.k)).toEqual(['apple', 'nested', 'zebra'])
    expect(vm.varEntries(undefined)).toEqual([])
    expect(vm.fmtVar('a')).toBeTruthy()
    w.unmount()
  })

  it('expands one variable chip at a time', async () => {
    const w = mountTimeline(
      baseRun({
        nodeExecutions: { research: [exec({ varsSnapshot: { a: 1, b: { deep: true } } })] },
      } as never),
    )
    await flushPromises()
    const vm = w.vm as any
    const it = vm.items[0]

    expect(vm.expandedVar(it)).toBeNull()

    const ev = { stopPropagation: vi.fn() }
    vm.toggleVarExpand(it, 'a', ev)
    await flushPromises()
    expect(ev.stopPropagation).toHaveBeenCalled()
    expect(vm.expandedKey).toBe('research:0:a')
    expect(vm.expandedVar(it)).toEqual({ k: 'a', v: 1 })

    // Selecting another key replaces the expansion.
    vm.toggleVarExpand(it, 'b')
    await flushPromises()
    expect(vm.expandedVar(it)).toEqual({ k: 'b', v: { deep: true } })

    // Clicking the same key collapses it.
    vm.toggleVarExpand(it, 'b')
    await flushPromises()
    expect(vm.expandedKey).toBeNull()
    w.unmount()
  })

  it('scopes the expanded chip to its own execution row', async () => {
    const w = mountTimeline(
      baseRun({
        nodeExecutions: {
          research: [
            exec({ varsSnapshot: { a: 1 }, startedAt: '2026-07-18T00:00:00Z' }),
            exec({ varsSnapshot: { a: 2 }, startedAt: '2026-07-18T00:00:01Z' }),
          ],
        },
      } as never),
    )
    await flushPromises()
    const vm = w.vm as any

    vm.toggleVarExpand(vm.items[0], 'a')
    await flushPromises()
    expect(vm.expandedVar(vm.items[0])).toEqual({ k: 'a', v: 1 })
    expect(vm.expandedVar(vm.items[1])).toBeNull()

    // A key that vanished from the snapshot resolves to null rather than throwing.
    vm.expandedKey = 'research:0:gone'
    expect(vm.expandedVar(vm.items[0])).toBeNull()
    w.unmount()
  })

  it('pretty-prints object variables and falls back for cyclic values', async () => {
    const w = mountTimeline(baseRun())
    await flushPromises()
    const vm = w.vm as any

    expect(vm.fmtVarFull({ a: 1 })).toBe('{\n  "a": 1\n}')
    expect(vm.fmtVarFull('plain')).toBe('plain')
    expect(vm.fmtVarFull(null)).toBeTruthy()

    const cyclic: Record<string, unknown> = {}
    cyclic.self = cyclic
    expect(vm.fmtVarFull(cyclic)).toContain('object')
    w.unmount()
  })

  it('builds MCP chips with an args/result tooltip', async () => {
    const w = mountTimeline(
      baseRun({
        nodeExecutions: {
          research: [
            exec({
              mcpCalls: [
                { tool: 'search', args: '{"q":1}', result: 'ok', isError: false },
                { tool: 'write', args: '', result: '', isError: true },
              ],
            } as never),
          ],
        },
      } as never),
    )
    await flushPromises()
    const vm = w.vm as any

    expect(vm.items[0].mcp).toHaveLength(2)
    expect(vm.items[0].mcp[0].tip).toContain('search')
    expect(vm.items[0].mcp[1].isError).toBe(true)
    // Empty args/result fall back to the placeholder token.
    expect(vm.items[0].mcp[1].tip.split('\n')).toHaveLength(3)
    expect(vm.mcpEntries(undefined)).toEqual([])
    w.unmount()
  })

  it('shows a live duration for running rows and a fixed one when terminal', async () => {
    const w = mountTimeline(
      baseRun({
        status: 'running',
        nodeExecutions: {
          research: [
            exec({ status: 'running', durationSec: undefined, startedAt: '2026-07-18T00:00:00Z' }),
            exec({ status: 'waiting_human', durationSec: undefined, startedAt: undefined }),
            exec({ status: 'completed', durationSec: 42, startedAt: '2026-07-18T00:00:02Z' }),
            exec({ status: 'pending', durationSec: undefined, startedAt: undefined }),
            exec({ status: 'cancelled', durationSec: undefined, startedAt: '2026-07-18T00:00:03Z' }),
          ],
        },
      } as never),
      { nowMs: Date.parse('2026-07-18T00:01:00Z') },
    )
    await flushPromises()
    const vm = w.vm as any
    const byStatus = (s: string) => vm.items.find((i: any) => i.status === s)

    expect(vm.displayDuration(byStatus('running'))).toBeGreaterThan(0)
    // waiting_human without a start time has nothing to count from.
    expect(vm.displayDuration(byStatus('waiting_human'))).toBeNull()
    expect(vm.displayDuration(byStatus('completed'))).toBe(42)
    expect(vm.displayDuration(byStatus('pending'))).toBeNull()
    expect(vm.displayDuration(byStatus('cancelled'))).toBeNull()
    expect(vm.hasActiveItems).toBe(true)
    expect(vm.runLive).toBe(true)
    w.unmount()
  })

  it('ticks its own clock only when the parent does not supply one', async () => {
    vi.useFakeTimers()
    try {
      const w = mountTimeline(baseRun({ status: 'running' }))
      const vm = w.vm as any
      const before = vm.localNowMs
      await vi.advanceTimersByTimeAsync(1200)
      expect(vm.localNowMs).toBeGreaterThanOrEqual(before)

      const shared = mountTimeline(baseRun({ status: 'running' }), { nowMs: 1234 })
      const sharedVm = shared.vm as any
      const sharedBefore = sharedVm.localNowMs
      await vi.advanceTimersByTimeAsync(1200)
      expect(sharedVm.localNowMs).toBe(sharedBefore)
      expect(sharedVm.tickMs).toBe(1234)

      w.unmount()
      shared.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('emits a selection and highlights the active row', async () => {
    const w = mountTimeline(baseRun(), { selectedNodeId: 'research', selectedExecIdx: 0 })
    await flushPromises()
    const vm = w.vm as any

    expect(vm.isSelected(vm.items[0])).toBe(true)
    expect(w.get('[data-testid="timeline-item"]').attributes('data-selected')).toBe('true')

    vm.onSelect(vm.items[0])
    expect(w.emitted('select')![0]).toEqual(['research', 0])
    w.unmount()
  })

  it('is inert in non-interactive stats mode', async () => {
    const w = mountTimeline(baseRun(), {
      interactive: false,
      selectedNodeId: 'research',
      selectedExecIdx: 0,
    })
    await flushPromises()
    const vm = w.vm as any

    expect(vm.isSelected(vm.items[0])).toBe(false)
    vm.onSelect(vm.items[0])
    expect(w.emitted('select')).toBeFalsy()

    vm.scrollSelectedIntoView()
    expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled()
    w.unmount()
  })

  it('scrolls the selected row into view when the token is bumped', async () => {
    const w = mountTimeline(baseRun(), { selectedNodeId: 'research', selectedExecIdx: 0 })
    await flushPromises()

    await w.setProps({ ensureVisibleToken: 1 })
    await flushPromises()
    expect(Element.prototype.scrollIntoView).toHaveBeenCalled()
    w.unmount()
  })

  it('skips scrolling when nothing is selected', async () => {
    const w = mountTimeline(baseRun())
    await flushPromises()
    ;(w.vm as any).scrollSelectedIntoView()
    expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled()

    // A selection that has no matching row is also a no-op.
    await w.setProps({ selectedNodeId: 'ghost', selectedExecIdx: 0, ensureVisibleToken: 2 })
    await flushPromises()
    expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled()
    w.unmount()
  })

  it('summarises token usage across the run', async () => {
    const w = mountTimeline(
      baseRun({
        nodeExecutions: {
          research: [
            exec({ usage: { inputTokens: 100, outputTokens: 50, cacheReadTokens: 10, cacheWriteTokens: 0 } } as never),
            exec({ usage: null, startedAt: '2026-07-18T00:00:01Z' } as never),
          ],
        },
      } as never),
    )
    await flushPromises()
    const vm = w.vm as any

    expect(vm.wallSec).toBeGreaterThan(0)
    expect(vm.usageSummary).toBeTruthy()
    expect(w.find('[data-testid="timeline-tokens"]').exists()).toBe(true)
    w.unmount()
  })
})
