// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ProjectTokenStats } from '@/lib/shared/types'
import TokenStatsPanel from './TokenStatsPanel.vue'

const getProjectTokenStats = vi.fn()

vi.mock('@/lib/api/api', () => ({
  api: {
    getProjectTokenStats: (...args: unknown[]) => getProjectTokenStats(...args),
  },
}))

function sampleStats(partial: Partial<ProjectTokenStats> = {}): ProjectTokenStats {
  return {
    window: '30d',
    bucketWidth: 'day',
    timezone: 'Asia/Shanghai',
    empty: false,
    trend: [
      {
        bucket: '2026-07-24',
        total: 100,
        inputTokens: 40,
        outputTokens: 30,
        cacheReadTokens: 20,
        cacheWriteTokens: 10,
      },
      {
        bucket: '2026-07-25',
        total: 80,
        inputTokens: 30,
        outputTokens: 25,
        cacheReadTokens: 15,
        cacheWriteTokens: 10,
      },
    ],
    composition: {
      inputTokens: 70,
      outputTokens: 55,
      cacheReadTokens: 35,
      cacheWriteTokens: 20,
      total: 180,
    },
    workflows: [
      { workflowId: 'wf-a', name: 'approve-main', total: 120 },
      { workflowId: 'wf-b', name: 'doc-review', total: 40 },
      { name: 'other', total: 20, other: true },
    ],
    modelComposition: [
      { modelKey: 'claude-sonnet-4', name: 'claude-sonnet-4', total: 100, filled: true },
      { modelKey: '未知/未分桶', name: '未知模型', total: 40, unknown: true },
    ],
    modelRanking: [
      { modelKey: 'claude-sonnet-4', name: 'claude-sonnet-4', total: 100, filled: true },
      { modelKey: '未知/未分桶', name: '未知模型', total: 40, unknown: true },
    ],
    ...partial,
  }
}

function mountPanel(extra: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(TokenStatsPanel, {
    props: { projectId: 'proj-1', ...extra },
    global: {
      plugins: [i18n],
      stubs: {
        TokenTrendChart: { template: '<div data-testid="stub-trend" />' },
        TokenDonutChart: { template: '<div data-testid="stub-donut" />' },
        TokenModelComposition: { template: '<div data-testid="stub-model-comp" />' },
        TokenModelRank: { template: '<div data-testid="stub-model-rank" />' },
        TokenWorkflowRank: { template: '<div data-testid="stub-rank" />' },
      },
    },
  })
}

describe('TokenStatsPanel', () => {
  beforeEach(() => {
    getProjectTokenStats.mockReset()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('loads default 30d window and renders charts (g2.1/g2.2)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats())
    const wrapper = mountPanel()
    expect(wrapper.find('[data-testid="token-stats-loading"]').exists()).toBe(true)
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenCalledWith(
      'proj-1',
      expect.objectContaining({ window: '30d' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(wrapper.find('[data-testid="token-stats-charts"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-stats-window-badge"]').text()).toContain('近 30 天')
    expect(wrapper.find('[data-testid="token-stats-window-30d"]').attributes('aria-selected')).toBe('true')
    const rankHead = wrapper.find('[data-testid="token-stats-model-rank-head"]')
    expect(rankHead.exists()).toBe(true)
    expect(rankHead.classes()).toEqual(expect.arrayContaining(['flex', 'justify-between']))
    expect(rankHead.classes()).not.toContain('flex-col')
    expect(rankHead.text()).toContain('模型消耗排行')
    expect(rankHead.text()).toContain('Top10 · 其余 → other')
    const rankCard = wrapper.find('[data-testid="token-stats-model-rank-card"]')
    expect(rankCard.text()).not.toMatch(/「未知」与 other 不同/)
    expect(rankCard.text()).not.toMatch(/相关用量按其实际消耗参与排行/)
    wrapper.unmount()
  })

  it('narrow layout: head stacks, windows ~44px touch, panel clips overflow (g2.1/g2.2/g3.2)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats())
    const wrapper = mountPanel()
    await flushPromises()

    const panel = wrapper.find('[data-testid="token-stats-panel"]')
    expect(panel.classes()).toEqual(expect.arrayContaining(['min-w-0', 'overflow-x-clip']))

    const head = wrapper.find('[data-testid="token-stats-head"]')
    expect(head.classes()).toEqual(expect.arrayContaining(['flex-col']))
    expect(head.classes()).toEqual(expect.arrayContaining(['md:flex-row']))

    const winBtn = wrapper.find('[data-testid="token-stats-window-7d"]')
    expect(winBtn.classes()).toEqual(expect.arrayContaining(['min-h-11']))

    const trendCard = wrapper.find('[data-testid="token-stats-trend-card"]')
    expect(trendCard.classes()).toEqual(expect.arrayContaining(['min-w-0', 'overflow-x-clip']))

    // Stack order: trend → composition → rank (g4.2)
    const trendEl = trendCard.element as HTMLElement
    const compEl = wrapper.find('[data-testid="token-stats-comp-card"]').element as HTMLElement
    const rankEl = wrapper.find('[data-testid="token-stats-rank-card"]').element as HTMLElement
    expect(trendEl.compareDocumentPosition(compEl) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(compEl.compareDocumentPosition(rankEl) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    wrapper.unmount()
  })

  it('shows empty state when API reports empty (g2.5 null≠0)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats({ empty: true, trend: [], workflows: [] }))
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="token-stats-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-stats-charts"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('未上报不会显示为 0')
    wrapper.unmount()
  })

  it('shows unified failure + retry and clears stale data on window switch (g2.1/g2.5)', async () => {
    getProjectTokenStats.mockResolvedValueOnce(sampleStats())
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="token-stats-charts"]').exists()).toBe(true)

    let resolveNext: (v: ProjectTokenStats) => void = () => {}
    getProjectTokenStats.mockImplementationOnce(
      () =>
        new Promise<ProjectTokenStats>((resolve) => {
          resolveNext = resolve
        }),
    )
    await wrapper.find('[data-testid="token-stats-window-7d"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="token-stats-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-stats-charts"]').exists()).toBe(false)

    resolveNext(sampleStats({ window: '7d' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="token-stats-charts"]').exists()).toBe(true)

    getProjectTokenStats.mockRejectedValueOnce(new Error('network'))
    await wrapper.find('[data-testid="token-stats-window-all"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="token-stats-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-stats-retry"]').exists()).toBe(true)

    getProjectTokenStats.mockResolvedValueOnce(sampleStats({ window: 'all', bucketWidth: 'week' }))
    await wrapper.find('[data-testid="token-stats-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="token-stats-charts"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('chart cards use card radius rounded-lg (g2.2)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats())
    const wrapper = mountPanel()
    await flushPromises()
    const cards = [
      wrapper.find('[data-testid="token-stats-trend-card"]'),
      wrapper.find('[data-testid="token-stats-comp-card"]'),
      wrapper.find('[data-testid="token-stats-rank-card"]'),
      wrapper.find('[data-testid="token-stats-model-comp-card"]'),
      wrapper.find('[data-testid="token-stats-model-rank-card"]'),
    ]
    for (const card of cards) {
      expect(card.classes()).toEqual(
        expect.arrayContaining(['rounded-lg', 'border', 'border-line', 'bg-surface']),
      )
    }
    wrapper.unmount()

    getProjectTokenStats.mockResolvedValue(sampleStats({ empty: true, trend: [], workflows: [] }))
    const empty = mountPanel()
    await flushPromises()
    expect(empty.find('[data-testid="token-stats-empty"]').html()).toMatch(/rounded-lg/)
    empty.unmount()
  })

  it('puts 近 24 小时 first, keeps default 30d, requests window=24h, and shows grainHour (g2.3/g2.5)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats())
    const wrapper = mountPanel()
    await flushPromises()
    const winBtns = wrapper.find('[data-testid="token-stats-windows"]').findAll('button')
    expect(winBtns.map((b) => b.text())).toEqual(['近 24 小时', '近 7 天', '近 30 天', '近 90 天', '全部历史'])
    expect(wrapper.find('[data-testid="token-stats-window-24h"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-stats-window-30d"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="token-stats-window-24h"]').attributes('aria-selected')).toBe('false')
    expect(wrapper.text()).toContain('按日聚合 · 本地时区')

    getProjectTokenStats.mockResolvedValueOnce(
      sampleStats({
        window: '24h',
        bucketWidth: 'hour',
        trend: [
          {
            bucket: '2026-07-24T20',
            total: 12,
            inputTokens: 6,
            outputTokens: 4,
            cacheReadTokens: 1,
            cacheWriteTokens: 1,
          },
        ],
      }),
    )
    await wrapper.find('[data-testid="token-stats-window-24h"]').trigger('click')
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-1',
      expect.objectContaining({ window: '24h' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
    expect(wrapper.find('[data-testid="token-stats-window-24h"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="token-stats-window-badge"]').text()).toContain('近 24 小时')
    expect(wrapper.text()).toContain('按小时聚合 · 本地时区')
    wrapper.unmount()
  })

  it('opens on the carried range and otherwise stays on 30d (g3.4)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats({ window: 'all' }))
    const carried = mountPanel({ initialWindow: 'all' })
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-1',
      expect.objectContaining({ window: 'all' }),
      expect.anything(),
    )
    expect(carried.find('[data-testid="token-stats-window-all"]').attributes('aria-selected')).toBe('true')
    expect(carried.find('[data-testid="token-stats-window-badge"]').text()).toContain('全部历史')
    carried.unmount()

    getProjectTokenStats.mockClear()
    getProjectTokenStats.mockResolvedValue(sampleStats())
    const direct = mountPanel()
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-1',
      expect.objectContaining({ window: '30d' }),
      expect.anything(),
    )
    direct.unmount()
  })

  it('keeps the route window across project changes and reapplies query updates (review v2)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats({ window: 'all' }))
    const carried = mountPanel({ initialWindow: 'all' })
    await flushPromises()
    getProjectTokenStats.mockClear()
    await carried.setProps({ projectId: 'proj-2' })
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-2',
      expect.objectContaining({ window: 'all' }),
      expect.anything(),
    )
    expect(carried.find('[data-testid="token-stats-window-badge"]').text()).toContain('全部历史')

    getProjectTokenStats.mockClear()
    await carried.setProps({ initialWindow: '7d' })
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-2',
      expect.objectContaining({ window: '7d' }),
      expect.anything(),
    )
    expect(carried.find('[data-testid="token-stats-window-badge"]').text()).toContain('近 7 天')

    getProjectTokenStats.mockClear()
    await carried.setProps({
      projectId: 'proj-3',
      initialWindow: '',
      initialFrom: '',
      initialTo: '',
      initialGranularity: '',
    })
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-3',
      expect.objectContaining({ window: '30d' }),
      expect.anything(),
    )
    carried.unmount()
  })

  it('sends a phase filter to the locked project stats (review v4)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats())
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-testid="token-stats-filter-phase"]').setValue('chat')
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-1',
      expect.objectContaining({ phase: 'chat' }),
      expect.anything(),
    )
    wrapper.unmount()
  })

  it('sends source, workflow, model, node, status, and custom range filters (g2.1)', async () => {
    getProjectTokenStats.mockResolvedValue(sampleStats({
      filterOptions: {
        workflows: [{ key: 'wf-a', name: 'approve-main' }],
        models: [{ key: 'sonnet', name: 'Sonnet' }],
        nodeTypes: [{ key: 'agent', name: 'agent' }],
      },
    } as Partial<ProjectTokenStats>))
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.find('[data-testid="token-stats-source-studio"]').trigger('click')
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-1',
      expect.objectContaining({ source: 'studio' }),
      expect.anything(),
    )
    await wrapper.find('[data-testid="token-stats-filter-workflow"]').setValue('wf-a')
    await wrapper.find('[data-testid="token-stats-filter-model"]').setValue('sonnet')
    await wrapper.find('[data-testid="token-stats-filter-node"]').setValue('agent')
    await wrapper.find('[data-testid="token-stats-filter-status"]').setValue('failed')
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-1',
      expect.objectContaining({ source: 'studio', workflowId: 'wf-a', modelKey: 'sonnet', nodeType: 'agent', status: 'failed' }),
      expect.anything(),
    )
    getProjectTokenStats.mockClear()
    await wrapper.find('[data-testid="token-stats-window-custom"]').trigger('click')
    await flushPromises()
    expect(getProjectTokenStats).toHaveBeenLastCalledWith(
      'proj-1',
      expect.objectContaining({ window: 'custom', from: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/) }),
      expect.anything(),
    )
    wrapper.unmount()
  })

  it('shows project-scoped metrics and distributions and skips cross-project charts (g2.2 g2.3 g4.3)', async () => {
    getProjectTokenStats.mockResolvedValue({
      ...sampleStats(),
      kpi: {
        total: 180,
        deltaPct: 10,
        inputTokens: 70,
        outputTokens: 55,
        cacheReadTokens: 35,
        cacheWriteTokens: 20,
        failedTotal: 12,
        runCount: 3,
        modelCount: 2,
        cacheHitRate: 0.25,
        cost: 1.5,
      },
      currency: 'USD',
      unpricedModels: ['opus'],
      sources: [
        { key: 'workflow', name: 'workflow', total: 120 },
        { key: 'studio', name: 'studio', total: 60 },
      ],
      nodeTypes: [{ key: 'agent', name: 'agent', total: 120 }],
      statuses: [{ key: 'ok', name: 'ok', total: 168 }, { key: 'failed', name: 'failed', total: 12 }],
      phases: [{ key: 'production', name: 'production', total: 100 }, { key: 'chat', name: 'chat', total: 80 }],
      topRuns: [{ runId: 'r1', title: 'Run 1', total: 90 }],
      projects: [{ projectId: 'proj-1', name: 'Board', total: 180 }],
      heatmap: { rows: ['Sonnet'], cols: ['Board'], grid: [[180]] },
    })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="token-stats-kpi-total"]').text()).toContain('180')
    expect(wrapper.find('[data-testid="token-stats-kpi-cache"]').text()).toContain('25.0%')
    expect(wrapper.find('[data-testid="token-stats-kpi-cost"]').text()).toContain('$1.50')
    expect(wrapper.find('[data-testid="token-stats-kpi-cost-unpriced"]').text()).toContain('1 个模型未定价')
    expect(wrapper.find('[data-testid="token-stats-kpi-failed"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-stats-kpi-runs"]').text()).toContain('3')
    expect(wrapper.find('[data-testid="token-stats-kpi-models"]').text()).toContain('2')
    expect(wrapper.find('[data-testid="token-stats-source-card"]').text()).toContain('Agent Studio')
    expect(wrapper.find('[data-testid="token-stats-node-card"]').text()).toContain('agent')
    expect(wrapper.find('[data-testid="token-stats-status-card"]').text()).toContain('失败')
    expect(wrapper.find('[data-testid="token-stats-phase-card"]').text()).toContain('对话')
    expect(wrapper.find('[data-testid="token-stats-runs-card"]').text()).toContain('Run 1')
    expect(wrapper.find('[data-testid="token-stats-projects"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="token-stats-heatmap"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('项目占比')
    expect(wrapper.text()).not.toContain('模型与项目对照')
    wrapper.unmount()

    getProjectTokenStats.mockResolvedValue({
      ...sampleStats({ empty: true, trend: [], workflows: [] }),
      kpi: { total: 0, inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0, failedTotal: 0, runCount: 0, modelCount: 0, cost: 0 },
      sources: [],
      trend: [],
    })
    const empty = mountPanel()
    await flushPromises()
    expect(empty.find('[data-testid="token-stats-empty"]').exists()).toBe(true)
    expect(empty.find('[data-testid="token-stats-kpis"]').exists()).toBe(false)
    expect(empty.find('[data-testid="token-stats-charts"]').exists()).toBe(false)
    empty.unmount()
  })
})
