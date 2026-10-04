// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import TokenAnalyticsView from './TokenAnalyticsView.vue'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import nav from '@/locales/zh-CN/nav.json'
import route from '@/locales/zh-CN/route.json'
import { TOKEN_PART_COLORS } from '@/components/board/token-stats/tokenStatsShared'

vi.mock('@/lib/api/api', () => ({
  api: {
    getGlobalTokenStats: vi.fn(),
    listTokenUsageEvents: vi.fn(),
    getTokenPricing: vi.fn(),
    updateTokenPricing: vi.fn(),
  },
}))

const pushMock = vi.fn()

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: pushMock }),
}))

vi.mock('vue-echarts', () => ({
  default: {
    name: 'VChart',
    template: '<div data-testid="mock-vchart"><canvas /></div>',
    props: ['option'],
  },
}))

vi.mock('@/components/charts/echartsSetup', () => ({
  registerECharts: () => {},
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...pages, ...nav, ...route } },
})

import { api } from '@/lib/api/api'
import { displayRunTitle } from '@/lib/run/runTitle'

const sampleData = {
  window: 'all',
  bucketWidth: 'day',
  timezone: 'Asia/Shanghai',
  empty: false,
  kpi: {
    total: 5000,
    deltaPct: 12.5,
    inputTokens: 3000,
    outputTokens: 1500,
    cacheReadTokens: 400,
    cacheWriteTokens: 100,
    workflowTotal: 4200,
    pmTotal: 800,
    projectCount: 2,
    runCount: 5,
    modelCount: 2,
  },
  trend: [{ bucket: '2026-07-01', total: 100, workflowTotal: 80, pmTotal: 20, inputTokens: 40, outputTokens: 30, cacheReadTokens: 20, cacheWriteTokens: 10 }],
  prevTrend: [{ bucket: '2026-06-01', total: 80, workflowTotal: 60, pmTotal: 20, inputTokens: 30, outputTokens: 25, cacheReadTokens: 15, cacheWriteTokens: 10 }],
  composition: { inputTokens: 3000, outputTokens: 1500, cacheReadTokens: 400, cacheWriteTokens: 100, total: 5000 },
  projects: [
    { projectId: 'p1', name: 'Grasp', total: 3000, inputTokens: 1800, outputTokens: 900, cacheReadTokens: 200, cacheWriteTokens: 100 },
    { projectId: 'p2', name: 'Other project', total: 2000, inputTokens: 1200, outputTokens: 600, cacheReadTokens: 200, cacheWriteTokens: 0 },
  ],
  modelRanking: [
    { modelKey: 'sonnet', name: 'Sonnet', total: 4000, inputTokens: 2400, outputTokens: 1200, cacheReadTokens: 300, cacheWriteTokens: 100 },
    { modelKey: 'opus', name: 'Opus', total: 1000, inputTokens: 600, outputTokens: 300, cacheReadTokens: 100, cacheWriteTokens: 0 },
  ],
  nodeTypes: [{ name: 'agent', total: 4000 }],
  workflows: [
    { workflowId: 'w1', name: 'main', total: 3000, inputTokens: 1800, outputTokens: 900, cacheReadTokens: 200, cacheWriteTokens: 100, kind: 'workflow' as const },
    { workflowId: 'w2', name: 'review', total: 2000, inputTokens: 1200, outputTokens: 600, cacheReadTokens: 200, cacheWriteTokens: 0, kind: 'workflow' as const },
  ],
  heatmap: { rows: ['Sonnet'], cols: ['Grasp'], grid: [[3000]] },
  topRuns: [{ runId: 'r1', title: 'Run 1', projectId: 'p1', projectName: 'Grasp', workflowName: 'main', modelKey: 'sonnet', modelName: 'Sonnet', total: 500 }],
  projectTrends: [{ key: 'p1', name: 'Grasp', trend: [{ bucket: '2026-07-01', total: 100, workflowTotal: 80, pmTotal: 20, inputTokens: 40, outputTokens: 30, cacheReadTokens: 20, cacheWriteTokens: 10 }] }],
  modelTrends: [{ key: 'sonnet', name: 'Sonnet', trend: [{ bucket: '2026-07-01', total: 100, workflowTotal: 80, pmTotal: 20, inputTokens: 40, outputTokens: 30, cacheReadTokens: 20, cacheWriteTokens: 10 }] }],
  filterOptions: { projects: [{ key: 'p1', name: 'Grasp' }], models: [{ key: 'sonnet', name: 'Sonnet' }] },
}

describe('TokenAnalyticsView', () => {
  beforeEach(() => {
    pushMock.mockClear()
    vi.mocked(api.getGlobalTokenStats).mockResolvedValue(sampleData)
  })

  it('loads global stats with h-full root and three KPI cards', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(wrapper.find('[data-testid="token-analytics-page"]').classes()).toContain('h-full')
    expect(wrapper.find('[data-testid="token-analytics-kpis"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-analytics-kpi-total"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-analytics-kpi-merge"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-analytics-kpi-scope"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="token-analytics-kpi-total"], [data-testid="token-analytics-kpi-merge"], [data-testid="token-analytics-kpi-scope"]')).toHaveLength(3)
    wrapper.unmount()
  })

  it('uses plain-language title and hides section navigation', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(wrapper.text()).toContain('用量统计')
    expect(wrapper.text()).not.toContain('全局 Token 分析台')
    expect(wrapper.text()).not.toContain('较上一窗')
    expect(wrapper.text()).not.toContain('vs previous window')
    expect(wrapper.find('[data-testid="token-analytics-section-nav-mobile"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="token-analytics-section-nav"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('统计导航')
    expect(wrapper.text()).not.toContain('四分量')
    expect(wrapper.text()).not.toContain('多折线')
    wrapper.unmount()
  })

  it('shows input-side total and output whose exact sum matches total (g1.1/g1.2/g2.1)', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const merge = wrapper.find('[data-testid="token-analytics-kpi-merge"]')
    const faceText = Array.from(merge.element.children)
      .filter((el) => el.classList.contains('mt-2.5'))
      .map((el) => el.textContent || '')
      .join('')
    expect(faceText).toContain('输入')
    expect(faceText).toContain('输出')
    expect(faceText).not.toContain('缓存读')
    expect(faceText).not.toContain('缓存写')
    expect(faceText).not.toMatch(/\d[\d,.]*[KMB]?\s*\/\s*\d/)
    const input = wrapper.find('[data-testid="token-analytics-kpi-input"]')
    const output = wrapper.find('[data-testid="token-analytics-kpi-output"]')
    const total = wrapper.find('[data-testid="token-analytics-kpi-total"]')
    expect(input.text()).toBe('3.5K')
    expect(output.text()).toBe('1.5K')
    expect(input.attributes('data-token-count')).toBe('3500')
    expect(output.attributes('data-token-count')).toBe('1500')
    expect(
      Number(input.attributes('data-token-count')) + Number(output.attributes('data-token-count')),
    ).toBe(Number(total.attributes('data-token-count')))
    expect(wrapper.find('[data-testid="token-analytics-kpi-detail"]').classes()).toContain('hidden')
    wrapper.unmount()
  })

  it('keeps four-part exact KPI detail on hover and focus (g1.3/g2.1)', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(wrapper.find('[data-testid="token-analytics-kpi-detail"]').exists()).toBe(true)
    const merge = wrapper.find('[data-testid="token-analytics-kpi-merge"]')
    await merge.trigger('mouseenter')
    const detail = wrapper.find('[data-testid="token-analytics-kpi-detail"]')
    expect(detail.text()).toContain('3,000')
    expect(detail.text()).toContain('1,500')
    expect(detail.text()).toContain('400')
    expect(detail.text()).toContain('100')
    expect(detail.text()).toContain('缓存读')
    expect(detail.text()).toContain('缓存写')
    await merge.trigger('focus')
    expect(detail.text()).toContain('缓存读')
    wrapper.unmount()
  })

  it('uses overflow-visible plot areas so tooltips are not clipped', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(wrapper.find('[data-testid="token-analytics-plot-lines"]').classes()).toContain('overflow-visible')
    expect(wrapper.find('[data-testid="token-analytics-plot-bars"]').classes()).toContain('overflow-visible')
    expect(wrapper.find('[data-testid="token-analytics-plot-area"]').classes()).toContain('overflow-visible')
    expect(wrapper.find('[data-testid="token-analytics-plot-heat"]').classes()).toContain('overflow-visible')
    wrapper.unmount()
  })

  it('pie charts use right-side legend, visible labels, and compact tooltip', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const charts = wrapper.findAllComponents({ name: 'VChart' })
    expect(charts.length).toBeGreaterThan(0)
    const pieChart = charts.find((c) => {
      const opt = c.props('option') as { series?: { type?: string }[] }
      return opt?.series?.[0]?.type === 'pie'
    })
    expect(pieChart).toBeTruthy()
    const option = pieChart!.props('option') as {
      legend?: { orient?: string; right?: number; bottom?: number }
      tooltip?: { formatter?: unknown; appendToBody?: boolean }
      series?: { label?: { show?: boolean }; center?: string[]; radius?: string | string[] }[]
    }
    expect(option.legend?.orient).toBe('vertical')
    expect(option.legend?.right).toBe(0)
    expect(option.legend?.bottom).toBeUndefined()
    expect(option.series?.[0]?.label?.show).toBe(true)
    expect(option.series?.[0]?.center?.[0]).toBe('38%')
    expect(typeof option.tooltip?.formatter).toBe('function')
    expect(option.tooltip?.appendToBody).toBe(true)
    expect((option.tooltip as { borderRadius?: number })?.borderRadius).toBe(12)
    expect(JSON.stringify(option.tooltip)).not.toContain('#1a1d23')
    wrapper.unmount()
  })

  it('axis charts use compact tooltip formatters and append tooltips to body', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const charts = wrapper.findAllComponents({ name: 'VChart' })
    const lineChart = charts.find((c) => {
      const opt = c.props('option') as { series?: { type?: string }[] }
      return opt?.series?.[0]?.type === 'line' && opt?.series?.length === 2
    })
    expect(lineChart).toBeTruthy()
    const option = lineChart!.props('option') as {
      tooltip?: { valueFormatter?: (v: number) => string; appendToBody?: boolean; confine?: boolean }
      yAxis?: { axisLabel?: { formatter?: (v: number) => string } }
    }
    expect(option.tooltip?.appendToBody).toBe(true)
    expect(option.tooltip?.confine).toBe(false)
    expect(option.tooltip?.valueFormatter?.(2_080_982_825)).toBe('2.08B')
    expect(option.yAxis?.axisLabel?.formatter?.(2_500_000_000)).toBe('2.5B')
    wrapper.unmount()
  })

  it('defaults and falls back to the first comparable bar dimension', async () => {
    const cases = [
      { data: sampleData, active: 'project' },
      { data: { ...sampleData, projects: sampleData.projects.slice(0, 1) }, active: 'workflow' },
      {
        data: {
          ...sampleData,
          projects: sampleData.projects.slice(0, 1),
          workflows: sampleData.workflows.slice(0, 1),
        },
        active: 'model',
      },
    ]
    for (const scenario of cases) {
      vi.mocked(api.getGlobalTokenStats).mockResolvedValueOnce(scenario.data)
      const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
      await flushPromises()
      expect(wrapper.find(`[data-testid="token-analytics-bar-dimension-${scenario.active}"]`).classes())
        .toContain('font-semibold')
      wrapper.unmount()
    }
  })

  it('disables non-comparable dimensions and hides bars when none are comparable', async () => {
    vi.mocked(api.getGlobalTokenStats).mockResolvedValueOnce({
      ...sampleData,
      projects: sampleData.projects.slice(0, 1),
      workflows: sampleData.workflows.slice(0, 1),
      modelRanking: sampleData.modelRanking.slice(0, 1),
    })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(wrapper.find('[data-testid="token-analytics-bars"]').exists()).toBe(false)
    wrapper.unmount()

    vi.mocked(api.getGlobalTokenStats).mockResolvedValueOnce({
      ...sampleData,
      projects: sampleData.projects.slice(0, 1),
    })
    const fallbackWrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const projectButton = fallbackWrapper.find('[data-testid="token-analytics-bar-dimension-project"]')
    expect(projectButton.attributes('disabled')).toBeDefined()
    expect(projectButton.classes()).toContain('opacity-40')
    fallbackWrapper.unmount()
  })

  it('switches bar dimensions locally and applies bar styling and colors', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    vi.mocked(api.getGlobalTokenStats).mockClear()
    await wrapper.find('[data-testid="token-analytics-bar-dimension-workflow"]').trigger('click')
    await flushPromises()
    expect(api.getGlobalTokenStats).not.toHaveBeenCalled()

    const barChart = wrapper.findAllComponents({ name: 'VChart' }).find((chart) => {
      const option = chart.props('option') as { series?: { type?: string }[] }
      return option.series?.[0]?.type === 'bar'
    })
    const option = barChart!.props('option') as {
      xAxis: { data: string[] }
      series: Array<{
        barMaxWidth: number
        itemStyle: { color: string }
        data: Array<{ itemStyle: { borderRadius: number | number[] } }>
      }>
    }
    expect(option.xAxis.data).toEqual(['main', 'review'])
    expect(option.series.every((series) => series.barMaxWidth === 28)).toBe(true)
    expect(option.series.map((series) => series.itemStyle.color)).toEqual(
      Object.values(TOKEN_PART_COLORS),
    )
    expect(option.series.some((series) =>
      series.data.some((item) => Array.isArray(item.itemStyle.borderRadius)),
    )).toBe(true)
    wrapper.unmount()
  })

  it('opens the drill-down modal from project/model bars and applies the drill as a page filter', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n], stubs: { teleport: true } } })
    await flushPromises()
    const getBarChart = () => wrapper.findAllComponents({ name: 'VChart' }).find((chart) => {
      const option = chart.props('option') as { series?: { type?: string; stack?: string }[] }
      return option.series?.[0]?.type === 'bar' && option.series?.[0]?.stack === 'total'
    })!

    vi.mocked(api.getGlobalTokenStats).mockClear()
    getBarChart().vm.$emit('click', {
      componentType: 'series',
      name: 'Grasp',
      data: { drill: [{ dim: 'project', key: 'p1', name: 'Grasp' }], other: false },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="token-drill-modal"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-drill-breadcrumb"]').text()).toContain('项目：Grasp')
    expect(api.getGlobalTokenStats).toHaveBeenLastCalledWith(
      expect.objectContaining({ projectId: 'p1' }),
      expect.anything(),
    )

    vi.mocked(api.getGlobalTokenStats).mockClear()
    await wrapper.find('[data-testid="token-drill-apply"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="token-drill-modal"]').exists()).toBe(false)
    expect(api.getGlobalTokenStats).toHaveBeenLastCalledWith(
      expect.objectContaining({ projectId: 'p1' }),
      expect.anything(),
    )
    expect((wrapper.find('[data-testid="token-analytics-filter-project"]').element as HTMLSelectElement).value).toBe('p1')
    wrapper.unmount()
  })

  it('does not open the drill-down for other bars or bars without a drill key', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n], stubs: { teleport: true } } })
    await flushPromises()
    const barChart = wrapper.findAllComponents({ name: 'VChart' }).find((chart) => {
      const option = chart.props('option') as { series?: { type?: string; stack?: string }[] }
      return option.series?.[0]?.type === 'bar' && option.series?.[0]?.stack === 'total'
    })!
    vi.mocked(api.getGlobalTokenStats).mockClear()
    barChart.vm.$emit('click', { componentType: 'series', name: 'main', data: { other: false } })
    barChart.vm.$emit('click', {
      componentType: 'series',
      name: 'other',
      data: { drill: [{ dim: 'model', key: 'x', name: 'x' }], other: true },
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="token-drill-modal"]').exists()).toBe(false)
    expect(api.getGlobalTokenStats).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('attaches drill paths to bar items (workflow bars drill into workflowId)', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.find('[data-testid="token-analytics-bar-dimension-workflow"]').trigger('click')
    const barChart = wrapper.findAllComponents({ name: 'VChart' }).find((chart) => {
      const option = chart.props('option') as { series?: { type?: string; stack?: string }[] }
      return option.series?.[0]?.type === 'bar' && option.series?.[0]?.stack === 'total'
    })!
    const option = barChart.props('option') as { series: Array<{ data: Array<{ drill?: Array<{ dim: string; key: string }> }> }> }
    expect(option.series[0].data[0].drill).toEqual([{ dim: 'workflow', key: 'w1', name: 'main' }])
    wrapper.unmount()
  })

  it('drills nested inside the modal and lists ledger events', async () => {
    vi.mocked(api.listTokenUsageEvents).mockResolvedValue({
      total: 1,
      page: 1,
      pageSize: 20,
      currency: 'USD',
      items: [{
        id: 1, at: '2026-07-01T10:00:00Z', source: 'workflow', phase: 'production', status: 'failed',
        projectId: 'p1', projectName: 'Grasp', runId: 'r1', runTitle: 'Run 1', nodeType: 'agent',
        modelKey: 'sonnet', total: 10, inputTokens: 6, outputTokens: 4, cacheReadTokens: 0, cacheWriteTokens: 0, cost: 0.5, priced: true,
      }],
    })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n], stubs: { teleport: true } } })
    await flushPromises()
    await wrapper.find('[data-testid="token-analytics-project-detail-p1"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="token-drill-modal"]').exists()).toBe(true)

    await wrapper.find('[data-testid="token-drill-tab-events"]').trigger('click')
    await flushPromises()
    expect(api.listTokenUsageEvents).toHaveBeenLastCalledWith(
      expect.objectContaining({ projectId: 'p1', page: 1, sort: 'time' }),
      expect.anything(),
    )
    const table = wrapper.find('[data-testid="token-events-table"]')
    expect(table.text()).toContain('失败')
    expect(table.text()).toContain('$0.50')

    vi.mocked(api.getGlobalTokenStats).mockClear()
    const modelBtn = table.findAll('button').find((b) => b.text() === 'sonnet')!
    await modelBtn.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="token-drill-breadcrumb"]').text()).toContain('模型：sonnet')
    expect(api.getGlobalTokenStats).toHaveBeenLastCalledWith(
      expect.objectContaining({ projectId: 'p1', modelKey: 'sonnet' }),
      expect.anything(),
    )

    await wrapper.find('[data-testid="token-drill-crumb-0"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="token-drill-breadcrumb"]').text()).not.toContain('模型：sonnet')
    wrapper.unmount()
  })

  it('opens run drill from the runs table on the events tab', async () => {
    vi.mocked(api.listTokenUsageEvents).mockResolvedValue({ total: 0, page: 1, pageSize: 20, currency: 'USD', items: [] })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n], stubs: { teleport: true } } })
    await flushPromises()
    await wrapper.find('[data-testid="token-analytics-run-detail-r1"]').trigger('click')
    await flushPromises()
    expect(api.listTokenUsageEvents).toHaveBeenLastCalledWith(expect.objectContaining({ runId: 'r1' }), expect.anything())
    expect(wrapper.find('[data-testid="token-drill-open-run"]').exists()).toBe(true)
    await wrapper.find('[data-testid="token-drill-open-run"]').trigger('click')
    expect(pushMock).toHaveBeenCalledWith('/runs/r1')
    wrapper.unmount()
  })

  it('shows cost, cache hit and failed KPI cards', async () => {
    vi.mocked(api.getGlobalTokenStats).mockResolvedValueOnce({
      ...sampleData,
      currency: 'USD',
      kpi: { ...sampleData.kpi, cost: 12.345, costDeltaPct: -5, cacheHitRate: 0.1, failedTotal: 250 },
      unpricedModels: ['opus'],
    })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const cost = wrapper.find('[data-testid="token-analytics-kpi-cost"]')
    expect(cost.text()).toContain('$12.35')
    expect(cost.text()).toContain('▼ 5.0%')
    expect(cost.text()).toContain('1 个模型未定价')
    expect(wrapper.find('[data-testid="token-analytics-kpi-cache"]').text()).toContain('10.0%')
    const failedCard = wrapper.find('[data-testid="token-analytics-kpi-failed"]')
    expect(failedCard.text()).toContain('250')
    expect(failedCard.text()).toContain('5.0%')
    wrapper.unmount()
  })

  it('prompts to configure prices when nothing is priced', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const cost = wrapper.find('[data-testid="token-analytics-kpi-cost"]')
    expect(cost.text()).toContain('未配置单价')
    expect(wrapper.find('[data-testid="token-analytics-cost"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders weekday-hour heatmap and treemap when data is present', async () => {
    const weekHour = Array.from({ length: 7 }, () => Array.from({ length: 24 }, () => 0))
    weekHour[0][9] = 120
    vi.mocked(api.getGlobalTokenStats).mockResolvedValueOnce({
      ...sampleData,
      weekHour,
      tree: [{ key: 'p1', name: 'Grasp', kind: 'project', value: 3000, children: [
        { key: 'w1', name: 'main', kind: 'workflow', value: 3000, children: [{ key: 'agent', name: 'agent', kind: 'nodeType', value: 3000 }] },
      ] }],
    })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const charts = wrapper.findAllComponents({ name: 'VChart' })
    const tree = charts.find((c) => (c.props('option') as { series?: { type?: string }[] }).series?.[0]?.type === 'treemap')
    expect(tree).toBeTruthy()
    const treeData = (tree!.props('option') as { series: Array<{ data: Array<{ drill: unknown; children: Array<{ drill: unknown; children: Array<{ drill: unknown }> }> }> }> }).series[0].data
    expect(treeData[0].children[0].children[0].drill).toEqual([
      { dim: 'project', key: 'p1', name: 'Grasp' },
      { dim: 'workflow', key: 'w1', name: 'main' },
      { dim: 'nodeType', key: 'agent', name: 'agent' },
    ])
    expect(wrapper.find('[data-testid="token-analytics-plot-weekhour"]').findComponent({ name: 'VChart' }).exists()).toBe(true)
    wrapper.unmount()
  })

  it('requests a custom date range and studio source', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    vi.mocked(api.getGlobalTokenStats).mockClear()
    await wrapper.find('[data-testid="token-analytics-window-custom"]').trigger('click')
    await flushPromises()
    expect(api.getGlobalTokenStats).toHaveBeenLastCalledWith(
      expect.objectContaining({ window: 'custom', from: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/), to: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/) }),
      expect.anything(),
    )
    const from = wrapper.find('[data-testid="token-analytics-range-from"]')
    ;(from.element as HTMLInputElement).value = '2026-07-01'
    await from.trigger('change')
    const to = wrapper.find('[data-testid="token-analytics-range-to"]')
    ;(to.element as HTMLInputElement).value = '2026-07-03'
    await to.trigger('change')
    await wrapper.find('[data-testid="token-analytics-granularity"]').setValue('hour')
    await wrapper.find('[data-testid="token-analytics-source-studio"]').trigger('click')
    await flushPromises()
    expect(api.getGlobalTokenStats).toHaveBeenLastCalledWith(
      expect.objectContaining({ window: 'custom', from: '2026-07-01', to: '2026-07-03', granularity: 'hour', source: 'studio' }),
      expect.anything(),
    )
    wrapper.unmount()
  })

  it('does not render pie caption text under chart titles', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const text = wrapper.text()
    expect(text).not.toMatch(/agent \d+%/)
    expect(text).not.toMatch(/main \d+%/)
    expect(wrapper.findAll('.text-\\[11px\\].text-txt3').filter((el) => el.text().includes('% · '))).toHaveLength(0)
    wrapper.unmount()
  })

  it('shows empty state when API returns empty', async () => {
    vi.mocked(api.getGlobalTokenStats).mockResolvedValue({ ...sampleData, empty: true, kpi: { ...sampleData.kpi, total: 0 } })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(wrapper.find('[data-testid="token-analytics-empty"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('enables source, status, phase, and run bars only when at least two buckets exist (g3.1 g4.2)', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    for (const dim of ['source', 'status', 'phase', 'run']) {
      expect(wrapper.find(`[data-testid="token-analytics-bar-dimension-${dim}"]`).attributes('disabled')).toBeDefined()
    }
    wrapper.unmount()

    vi.mocked(api.getGlobalTokenStats).mockResolvedValueOnce({
      ...sampleData,
      sources: [
        { key: 'workflow', name: 'workflow', total: 100, inputTokens: 60, outputTokens: 40, cost: 1.5 },
        { key: 'studio', name: 'studio', total: 40, inputTokens: 20, outputTokens: 20, cost: 0.5 },
      ],
      statuses: [
        { key: 'ok', name: 'ok', total: 90, inputTokens: 50, outputTokens: 40, cost: 1 },
        { key: 'failed', name: 'failed', total: 50, inputTokens: 30, outputTokens: 20, cost: 0.4 },
      ],
      phases: [
        { key: 'production', name: 'production', total: 80, inputTokens: 40, outputTokens: 40, cost: 1 },
        { key: 'chat', name: 'chat', total: 60, inputTokens: 30, outputTokens: 30, cost: 0.2 },
      ],
      topRuns: [
        { ...sampleData.topRuns[0], runId: 'r1', title: 'Run 1', total: 50, inputTokens: 30, outputTokens: 20 },
        { ...sampleData.topRuns[0], runId: 'r2', title: 'Run 2', total: 40, inputTokens: 20, outputTokens: 20 },
      ],
    })
    const comparable = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    for (const dim of ['source', 'status', 'phase', 'run']) {
      const btn = comparable.find(`[data-testid="token-analytics-bar-dimension-${dim}"]`)
      expect(btn.attributes('disabled')).toBeUndefined()
      await btn.trigger('click')
      expect(btn.classes()).toContain('font-semibold')
    }
    for (const dim of ['source', 'nodeType', 'status', 'phase']) {
      expect(comparable.find(`[data-testid="token-analytics-cost-dimension-${dim}"]`).exists()).toBe(true)
    }
    await comparable.find('[data-testid="token-analytics-bar-dimension-phase"]').trigger('click')
    const barOption = comparable.find('[data-testid="token-analytics-plot-bars"]').findComponent({ name: 'VChart' }).props('option') as {
      series: Array<{ cursor?: string; data: Array<{ drill?: Array<{ dim: string; key: string }> }> }>
    }
    expect(barOption.series[0]?.cursor).toBe('pointer')
    expect(barOption.series[0]?.data.map((d) => d.drill?.[0]?.dim)).toEqual(['phase', 'phase'])
    expect(barOption.series[0]?.data.map((d) => d.drill?.[0]?.key)).toEqual(['production', 'chat'])
    vi.mocked(api.getGlobalTokenStats).mockClear()
    await comparable.find('[data-testid="token-analytics-filter-phase"]').setValue('chat')
    await flushPromises()
    expect(api.getGlobalTokenStats).toHaveBeenLastCalledWith(
      expect.objectContaining({ phase: 'chat' }),
      expect.anything(),
    )
    comparable.unmount()
  })

  it('navigates to project board when clicking project name', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const projectBtn = wrapper.findAll('button.text-accent-2').find((b) => b.text() === 'Grasp')
    expect(projectBtn).toBeTruthy()
    await projectBtn!.trigger('click')
    expect(pushMock).toHaveBeenCalledWith({ path: '/projects/p1', query: { tab: 'board', window: 'all' } })
    wrapper.unmount()
  })

  it('truncates long multiline run titles to a single line in top runs table', async () => {
    // plan coverage: g2.1 — 60-char cap, single line, tooltip for overflow
    const longTitle =
      '5. 迁移编号须严格递增，不得重用历史编号；6. 所有新增表结构变更必须配套 SQLite migration，禁止只改 Go struct；7. 关键路径必须有可重现的 Go tests。\n\n三、接口与安全\n对外 HTTP 接口保持向后兼容；敏感字段不得出现在日志。\n\n四、验收测试\n须覆盖主流程与边界，PR 不得带未测试的 Sendable 变更。'
    vi.mocked(api.getGlobalTokenStats).mockResolvedValue({
      ...sampleData,
      topRuns: [{ ...sampleData.topRuns[0], title: longTitle }],
    })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const runsTable = wrapper.find('[data-testid="token-analytics-runs-table"]')
    const runBtn = runsTable.find('tbody button.text-accent-2')
    const displayed = runBtn.text()
    expect(displayed.endsWith('…')).toBe(true)
    expect(displayed.length).toBeLessThanOrEqual(61)
    expect(displayed).not.toContain('\n')
    expect(displayed).not.toContain('三、接口与安全')
    expect(runBtn.classes()).toContain('truncate')
    const tooltip = displayRunTitle(longTitle).replace(/\s+/g, ' ').trim()
    expect(runBtn.attributes('title')).toBe(tooltip)
    expect(tooltip.length).toBeGreaterThan(60)
    wrapper.unmount()
  })

  it('shows short run titles without ellipsis in top runs table', async () => {
    // plan coverage: g2.1 — short titles keep full text, no tooltip
    const shortTitle = '我要讲解一个技术 或者一个产品 目前模板太少了'
    vi.mocked(api.getGlobalTokenStats).mockResolvedValue({
      ...sampleData,
      topRuns: [{ ...sampleData.topRuns[0], title: shortTitle }],
    })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const runsTable = wrapper.find('[data-testid="token-analytics-runs-table"]')
    const runBtn = runsTable.find('tbody button.text-accent-2')
    expect(runBtn.text()).toBe(shortTitle)
    expect(runBtn.attributes('title')).toBeUndefined()
    wrapper.unmount()
  })

  it('navigates to run detail when clicking truncated run title', async () => {
    // plan coverage: g2.1 — click truncated title still goes to /runs/:id
    const longTitle = 'A'.repeat(80)
    vi.mocked(api.getGlobalTokenStats).mockResolvedValue({
      ...sampleData,
      topRuns: [{ ...sampleData.topRuns[0], runId: 'r-long', title: longTitle }],
    })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const runsTable = wrapper.find('[data-testid="token-analytics-runs-table"]')
    await runsTable.find('tbody button.text-accent-2').trigger('click')
    expect(pushMock).toHaveBeenCalledWith('/runs/r-long')
    wrapper.unmount()
  })

  function firePointer(
    el: Element,
    type: 'pointerdown' | 'pointermove' | 'pointerup' | 'pointercancel',
    clientX: number,
  ) {
    el.dispatchEvent(
      new PointerEvent(type, {
        bubbles: true,
        cancelable: true,
        clientX,
        pointerId: 1,
      }),
    )
  }

  it('renders header sashes with i18n aria-label even when topRuns is empty', async () => {
    // plan coverage: g1.1 / f5 — empty topRuns still shows four draggable headers
    vi.mocked(api.getGlobalTokenStats).mockResolvedValue({ ...sampleData, topRuns: [] })
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const runsTable = wrapper.find('[data-testid="token-analytics-runs-table"]')
    expect(runsTable.text()).toContain('运行')
    expect(runsTable.text()).toContain('项目')
    expect(runsTable.text()).toContain('模型')
    expect(runsTable.text()).toContain('总量')
    for (const key of ['run', 'project', 'model', 'total'] as const) {
      const sash = runsTable.find(`[data-testid="token-analytics-runs-col-sash-${key}"]`)
      expect(sash.exists()).toBe(true)
      expect(sash.classes()).toContain('cursor-col-resize')
      expect(sash.attributes('role')).toBe('separator')
      expect(sash.attributes('aria-label')).toMatch(/拖动调整/)
    }
    wrapper.unmount()
  })

  it('dragging a header sash changes that column width', async () => {
    // plan coverage: g1.2 / g2.2 — pointer drag updates col style width
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const sash = wrapper.find('[data-testid="token-analytics-runs-col-sash-run"]').element
    const col = wrapper.find('[data-testid="token-analytics-runs-col-run"]')
    expect(col.attributes('style')).toContain('220px')
    firePointer(sash, 'pointerdown', 100)
    firePointer(sash, 'pointermove', 160)
    firePointer(sash, 'pointerup', 160)
    await flushPromises()
    expect(wrapper.find('[data-testid="token-analytics-runs-col-run"]').attributes('style')).toContain('280px')
    wrapper.unmount()
  })

  it('resizes each of the four runs-table columns independently', async () => {
    // plan coverage: g1.2 — run/project/model/total each have their own pixel width
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const drags: Array<[string, number, number]> = [
      ['run', 100, 150],
      ['project', 100, 130],
      ['model', 100, 90],
      ['total', 100, 140],
    ]
    for (const [key, start, end] of drags) {
      const sash = wrapper.find(`[data-testid="token-analytics-runs-col-sash-${key}"]`).element
      firePointer(sash, 'pointerdown', start)
      firePointer(sash, 'pointermove', end)
      firePointer(sash, 'pointerup', end)
    }
    await flushPromises()
    expect(wrapper.find('[data-testid="token-analytics-runs-col-run"]').attributes('style')).toContain('270px')
    expect(wrapper.find('[data-testid="token-analytics-runs-col-project"]').attributes('style')).toContain('170px')
    expect(wrapper.find('[data-testid="token-analytics-runs-col-model"]').attributes('style')).toContain('130px')
    expect(wrapper.find('[data-testid="token-analytics-runs-col-total"]').attributes('style')).toContain('128px')
    wrapper.unmount()
  })

  it('clamps column width at the minimum when dragging inward', async () => {
    // plan coverage: g1.2 / g2.2 — drag below min width is clamped
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const sash = wrapper.find('[data-testid="token-analytics-runs-col-sash-run"]').element
    firePointer(sash, 'pointerdown', 400)
    firePointer(sash, 'pointermove', -4000)
    firePointer(sash, 'pointerup', -4000)
    await flushPromises()
    expect(wrapper.find('[data-testid="token-analytics-runs-col-run"]').attributes('style')).toContain('80px')
    wrapper.unmount()
  })

  it('dragging a column sash does not navigate to the run', async () => {
    // plan coverage: g1.3 / g2.2 — sash pointer drag must not call router.push
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const sash = wrapper.find('[data-testid="token-analytics-runs-col-sash-run"]').element
    firePointer(sash, 'pointerdown', 100)
    firePointer(sash, 'pointermove', 140)
    firePointer(sash, 'pointerup', 140)
    await flushPromises()
    expect(pushMock).not.toHaveBeenCalled()
    const runsTable = wrapper.find('[data-testid="token-analytics-runs-table"]')
    await runsTable.find('tbody button.text-accent-2').trigger('click')
    expect(pushMock).toHaveBeenCalledWith('/runs/r1')
    wrapper.unmount()
  })

  it('defaults to 全部历史 and first request uses window=all (g1.1/g2.2)', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const group = wrapper.find('[data-testid="token-analytics-window"]')
    const labels = group.findAll('button').map((b) => b.text())
    expect(labels).toEqual(['近 24 小时', '近 7 天', '近 30 天', '近 90 天', '全部历史'])
    expect(wrapper.find('[data-testid="token-analytics-window-all"]').classes()).toContain('bg-surface')
    expect(wrapper.find('[data-testid="token-analytics-window-all"]').text()).toBe('全部历史')
    expect(wrapper.find('[data-testid="token-analytics-window-30d"]').classes()).not.toContain('bg-surface')
    expect(api.getGlobalTokenStats).toHaveBeenCalledWith(
      expect.objectContaining({ window: 'all' }),
      expect.anything(),
    )
    wrapper.unmount()
  })

  it('keeps source/project/model filters defaulting to all (g2.1)', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const filters = wrapper.find('[data-testid="token-analytics-filters"]')
    expect(filters.text()).toContain('来源：全部')
    expect(filters.text()).toContain('项目：全部')
    expect(filters.text()).toContain('模型：全部')
    const sourceAll = filters.findAll('button').find((b) => b.text() === '来源：全部')
    expect(sourceAll?.classes()).toContain('bg-accent-dim')
    expect(filters.text()).toContain('工作流：全部')
    expect(filters.text()).toContain('节点类型：全部')
    expect(filters.text()).toContain('状态：全部')
    expect(filters.text()).toContain('阶段：全部')
    const selects = filters.findAll('select')
    expect(selects).toHaveLength(6)
    for (const sel of selects) expect((sel.element as HTMLSelectElement).value).toBe('')
    wrapper.unmount()
  })

  it('puts 近 24 小时 first, still switches windows, and requests window=24h on click (g2.1)', async () => {
    const wrapper = mount(TokenAnalyticsView, { global: { plugins: [i18n] } })
    await flushPromises()
    const group = wrapper.find('[data-testid="token-analytics-window"]')
    const labels = group.findAll('button').map((b) => b.text())
    expect(labels).toEqual(['近 24 小时', '近 7 天', '近 30 天', '近 90 天', '全部历史'])
    expect(wrapper.find('[data-testid="token-analytics-window-all"]').classes()).toContain('bg-surface')

    vi.mocked(api.getGlobalTokenStats).mockClear()
    vi.mocked(api.getGlobalTokenStats).mockResolvedValue({
      ...sampleData,
      window: '24h',
      bucketWidth: 'hour',
      trend: [
        {
          bucket: '2026-07-24T20',
          total: 40,
          workflowTotal: 30,
          pmTotal: 10,
          inputTokens: 20,
          outputTokens: 10,
          cacheReadTokens: 5,
          cacheWriteTokens: 5,
        },
      ],
    })
    await wrapper.find('[data-testid="token-analytics-window-24h"]').trigger('click')
    await flushPromises()
    expect(api.getGlobalTokenStats).toHaveBeenCalledWith(
      expect.objectContaining({ window: '24h' }),
      expect.anything(),
    )
    expect(wrapper.find('[data-testid="token-analytics-window-24h"]').classes()).toContain('bg-surface')
    expect(wrapper.find('[data-testid="token-analytics-window-all"]').classes()).not.toContain('bg-surface')

    vi.mocked(api.getGlobalTokenStats).mockClear()
    vi.mocked(api.getGlobalTokenStats).mockResolvedValue({ ...sampleData, window: '30d' })
    await wrapper.find('[data-testid="token-analytics-window-30d"]').trigger('click')
    await flushPromises()
    expect(api.getGlobalTokenStats).toHaveBeenCalledWith(
      expect.objectContaining({ window: '30d' }),
      expect.anything(),
    )
    wrapper.unmount()
  })
})
