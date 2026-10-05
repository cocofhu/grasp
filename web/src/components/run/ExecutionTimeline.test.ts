// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run, WFNode } from '@/lib/shared/types'
import ExecutionTimeline from './ExecutionTimeline.vue'

const nodes: WFNode[] = [
  { id: 'start', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'research', type: 'agent', label: '调研', position: { x: 0, y: 0 }, config: {} },
  { id: 'react', type: 'agent', label: '澄清', position: { x: 0, y: 0 }, config: {} },
]

function baseRun(overrides: Partial<Run> = {}): Run {
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
    nodeExecutions: {
      start: [
        {
          nodeId: 'start',
          iteration: 1,
          status: 'completed',
          durationSec: 0,
          startedAt: '2026-07-18T00:00:00Z',
          outputs: {},
        },
      ],
      research: [
        {
          nodeId: 'research',
          iteration: 1,
          status: 'completed',
          durationSec: 30,
          startedAt: '2026-07-18T00:00:00Z',
          outputs: { summary: 'ok' },
          usage: {
            inputTokens: 100,
            outputTokens: 50,
            cacheReadTokens: 10,
            cacheWriteTokens: 0,
          },
        },
      ],
      react: [
        {
          nodeId: 'react',
          iteration: 1,
          status: 'completed',
          durationSec: 20,
          startedAt: '2026-07-18T00:01:00Z',
          outputs: {},
          usage: {
            inputTokens: 0,
            outputTokens: 0,
            cacheReadTokens: 0,
            cacheWriteTokens: 0,
          },
        },
      ],
    },
    ...overrides,
  } as unknown as Run
}

function mountTimeline(
  run: Run = baseRun(),
  opts: {
    interactive?: boolean
    selectedNodeId?: string | null
    selectedExecIdx?: number
    nowMs?: number
  } = {},
) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ExecutionTimeline, {
    props: {
      run,
      nodes,
      selectedNodeId: opts.selectedNodeId ?? null,
      selectedExecIdx: opts.selectedExecIdx ?? 0,
      interactive: opts.interactive ?? true,
      nowMs: opts.nowMs ?? Date.parse('2026-07-18T00:02:00Z'),
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, StatusPill: true, VarValueDisplay: true },
    },
  })
}

describe('ExecutionTimeline', () => {
  it('renders timeline items from node executions', () => {
    const wrapper = mountTimeline()
    expect(wrapper.text()).toContain('调研')
    expect(wrapper.findAll('[data-testid="timeline-node-label"]').length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('emits select when row clicked in interactive mode', async () => {
    const wrapper = mountTimeline(baseRun(), { interactive: true })
    const row = wrapper.find('li .cursor-pointer')
    await row.trigger('click')
    expect(wrapper.emitted('select')).toBeTruthy()
    wrapper.unmount()
  })

  it('expands variable chip on click', async () => {
    const wrapper = mountTimeline()
    const chip = wrapper.find('[data-testid="timeline-variable-chip"]')
    if (chip.exists()) {
      await chip.trigger('click')
      expect(wrapper.find('pre').exists()).toBe(true)
    }
    wrapper.unmount()
  })

  it('shows empty state when no executions', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(ExecutionTimeline, {
      props: {
        run: { ...baseRun(), nodeExecutions: {} },
        nodes,
        selectedNodeId: null,
        selectedExecIdx: 0,
      },
      global: { plugins: [i18n], stubs: { Icon: true } },
    })
    expect(wrapper.text()).toMatch(/暂无|没有/)
    wrapper.unmount()
  })

  it('shows tokens — without parts when usage is missing', () => {
    const wrapper = mountTimeline()
    const tokens = wrapper.findAll('[data-testid="timeline-tokens"]')
    expect(tokens.some((el) => el.text().includes('—'))).toBe(true)
    // start has no usage → no parts row for that card; research has parts
    const parts = wrapper.findAll('[data-testid="timeline-token-parts"]')
    expect(parts.length).toBe(2)
    wrapper.unmount()
  })

  it('shows tokens 0 and four parts when usage is reported as zero', () => {
    const wrapper = mountTimeline()
    const tokens = wrapper.findAll('[data-testid="timeline-tokens"]')
    expect(tokens.some((el) => /\b0\b/.test(el.text()) && !el.text().includes('—'))).toBe(true)
    const zeroParts = wrapper
      .findAll('[data-testid="timeline-token-parts"]')
      .find((el) => el.text().includes('输入') && el.text().match(/0/))
    expect(zeroParts).toBeTruthy()
    wrapper.unmount()
  })

  it('shows summed tokens and parts for reported usage', () => {
    const wrapper = mountTimeline()
    expect(wrapper.text()).toContain('160') // 100+50+10+0
    const parts = wrapper.findAll('[data-testid="timeline-token-parts"]')
    expect(parts[0]!.text()).toMatch(/输入/)
    expect(parts[0]!.text()).toMatch(/输出/)
    expect(parts[0]!.text()).toMatch(/缓存读/)
    expect(parts[0]!.text()).toMatch(/缓存写/)
    wrapper.unmount()
  })

  it('footer sums usage items and shows token/s + wall clock', () => {
    const wrapper = mountTimeline()
    const footer = wrapper.find('[data-testid="timeline-footer"]')
    expect(footer.exists()).toBe(true)
    // research 160 + react 0 = 160; wall 100 → 1.60 token/s
    expect(wrapper.find('[data-testid="timeline-total-tokens"]').text()).toContain('160')
    expect(wrapper.find('[data-testid="timeline-token-rate"]').text()).toContain('1.60')
    expect(wrapper.find('[data-testid="timeline-wall-clock"]').text()).toMatch(/\d+:\d+/)
    wrapper.unmount()
  })

  it('footer shows — for total and rate when no usage at all', () => {
    const run = baseRun({
      nodeExecutions: {
        start: [
          {
            nodeId: 'start',
            iteration: 1,
            status: 'completed',
            durationSec: 0,
            startedAt: '2026-07-18T00:00:00Z',
            outputs: {},
          },
        ],
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            durationSec: 30,
            startedAt: '2026-07-18T00:00:10Z',
            outputs: {},
          },
        ],
      },
    } as Partial<Run>)
    const wrapper = mountTimeline(run)
    expect(wrapper.find('[data-testid="timeline-total-tokens"]').text()).toContain('—')
    expect(wrapper.find('[data-testid="timeline-token-rate"]').text()).toContain('—')
    expect(wrapper.find('[data-testid="timeline-wall-clock"]').text()).not.toContain('—')
    wrapper.unmount()
  })

  it('footer token/s is — when wall clock is 0 but usage exists', () => {
    const run = baseRun({ durationSec: 0, status: 'completed' })
    // Force wall to 0: completed with durationSec 0 and startedAt same instant
    const wrapper = mountTimeline(run as Run, { nowMs: Date.parse('2026-07-18T00:00:00Z') })
    expect(wrapper.find('[data-testid="timeline-total-tokens"]').text()).toContain('160')
    expect(wrapper.find('[data-testid="timeline-token-rate"]').text()).toContain('—')
    wrapper.unmount()
  })

  it('footer total is 0 (not —) when only reported-zero usage', () => {
    const run = baseRun({
      nodeExecutions: {
        react: [
          {
            nodeId: 'react',
            iteration: 1,
            status: 'completed',
            durationSec: 20,
            startedAt: '2026-07-18T00:01:00Z',
            outputs: {},
            usage: {
              inputTokens: 0,
              outputTokens: 0,
              cacheReadTokens: 0,
              cacheWriteTokens: 0,
            },
          },
        ],
      },
    } as Partial<Run>)
    const wrapper = mountTimeline(run)
    const total = wrapper.find('[data-testid="timeline-total-tokens"]').text()
    expect(total).toContain('0')
    expect(total).not.toContain('—')
    wrapper.unmount()
  })

  it('keeps Run 汇总 inside the scroll document flow (not shrink-0 sticky)', () => {
    const wrapper = mountTimeline()
    const footer = wrapper.find('[data-testid="timeline-footer"]')
    expect(footer.exists()).toBe(true)
    expect(footer.classes()).not.toContain('shrink-0')
    // Opaque elevated bg — not translucent /60 which looked like a floating cut.
    expect(footer.classes()).toContain('bg-elevated')
    expect(footer.classes().some((c) => c.includes('/60'))).toBe(false)
    const scroll = wrapper.find('.scroll-area')
    expect(scroll.exists()).toBe(true)
    expect(scroll.element.contains(footer.element)).toBe(true)
    wrapper.unmount()
  })

  it('marks selected timeline-item for scrollIntoView targeting', () => {
    const wrapper = mountTimeline(undefined, {
      selectedNodeId: 'research',
      selectedExecIdx: 0,
    })
    const selected = wrapper.find('[data-testid="timeline-item"][data-selected="true"]')
    expect(selected.exists()).toBe(true)
    expect(selected.attributes('data-item-key')).toBe('research:0')
    wrapper.unmount()
  })
})
