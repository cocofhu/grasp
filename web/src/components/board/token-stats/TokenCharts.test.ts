// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import TokenTrendChart from './TokenTrendChart.vue'
import TokenDonutChart from './TokenDonutChart.vue'
import TokenWorkflowRank from './TokenWorkflowRank.vue'
import { TOKEN_SOURCE_COLORS } from './tokenStatsShared'
import { setTheme } from '@/lib/shared/theme'
import { pieTooltipFormatter, RANK_TRACK_DARK, RANK_TRACK_LIGHT } from '@/components/charts/chartTheme'

vi.mock('vue-echarts', () => ({
  default: {
    name: 'VChart',
    template: '<div data-testid="mock-vchart" class="h-full w-full"><canvas /></div>',
    props: ['option'],
    emits: ['mousemove', 'globalout', 'click'],
  },
}))

vi.mock('@/components/charts/echartsSetup', () => ({
  registerECharts: () => {},
}))

const i18n = () =>
  createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })

function sampleTrend(n: number) {
  return Array.from({ length: n }, (_, i) => {
    const day = String(i + 1).padStart(2, '0')
    const total = i === n - 1 ? 1_500_000 : i * 100
    const pm = Math.floor(total * 0.2)
    return {
      bucket: `2026-07-${day}`,
      total,
      workflowTotal: total - pm,
      pmTotal: pm,
      inputTokens: 40,
      outputTokens: 30,
      cacheReadTokens: 20,
      cacheWriteTokens: 10,
    }
  })
}

type PieOption = {
  legend?: { orient?: string; right?: number; bottom?: number }
  tooltip?: {
    borderRadius?: number
    backgroundColor?: string
    appendToBody?: boolean
    confine?: boolean
    formatter?: (p: unknown) => string
  }
  series?: { type?: string; label?: { show?: boolean; formatter?: string }; center?: string[] }[]
}

type TrendOption = {
  grid?: { left?: number; right?: number; top?: number; containLabel?: boolean }
  legend?: { top?: number; left?: number }
  tooltip?: {
    borderRadius?: number
    backgroundColor?: string
    appendToBody?: boolean
    confine?: boolean
    className?: string
    triggerOn?: string
    axisPointer?: { snap?: boolean }
    position?: (...args: unknown[]) => unknown
    formatter?: (p: unknown) => string
    valueFormatter?: (v: number) => string
  }
  xAxis?: { axisLabel?: { color?: string; maxInterval?: number } }
  yAxis?: { splitLine?: { lineStyle?: { color?: string } }; axisLabel?: { color?: string } }
  series?: { name?: string; lineStyle?: { type?: unknown }; clip?: boolean; symbolSize?: number }[]
}

function expectSquareThemeTooltip(tip: PieOption['tooltip'] | TrendOption['tooltip'], theme: 'dark' | 'light') {
  expect(tip?.borderRadius).toBe(12)
  expect(tip?.appendToBody).toBe(true)
  expect(tip?.confine).toBe(false)
  expect(JSON.stringify(tip)).not.toContain('#1a1d23')
  expect(tip?.backgroundColor).toBe(theme === 'dark' ? '#27272a' : '#ffffff')
}

describe('Token charts (g2.3/g2.4)', () => {
  it('renders ECharts trend from buckets including a zero day (g2.1)', () => {
    const wrapper = mount(TokenTrendChart, {
      props: {
        bucketWidth: 'day',
        trend: [
          {
            bucket: '2026-07-24',
            total: 100,
            workflowTotal: 70,
            pmTotal: 30,
            inputTokens: 40,
            outputTokens: 30,
            cacheReadTokens: 20,
            cacheWriteTokens: 10,
          },
          {
            bucket: '2026-07-25',
            total: 0,
            workflowTotal: 0,
            pmTotal: 0,
            inputTokens: 0,
            outputTokens: 0,
            cacheReadTokens: 0,
            cacheWriteTokens: 0,
          },
        ],
      },
      global: { plugins: [i18n()] },
    })
    expect(wrapper.find('[data-testid="token-trend-wrap"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-trend-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-trend-legend"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="token-trend-chart"] canvas').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-trend-svg"]').exists()).toBe(false)
    expect(wrapper.html()).not.toMatch(/preserveAspectRatio\s*=\s*["']none["']/)
    const exposed = wrapper.vm as unknown as {
      chartData: { datasets: { label: string; data: number[] }[] }
      chartOption: TrendOption
    }
    expect(exposed.chartData.datasets).toHaveLength(2)
    expect(exposed.chartData.datasets.map((d) => d.label)).toEqual(['workflow', 'pm'])
    expect(exposed.chartOption.legend?.top).toBe(0)
    expect(exposed.chartOption.legend?.left).toBe(0)
    expect(exposed.chartOption.series?.map((s) => s.name)).toEqual(['工作流', '项目管理'])
    wrapper.unmount()
  })

  it('trend tooltip is ECharts axis tooltip with i18n compact values (g2.2)', () => {
    setTheme('dark')
    const wrapper = mount(TokenTrendChart, {
      props: {
        bucketWidth: 'day',
        trend: [
          {
            bucket: '2026-07-24',
            total: 100,
            workflowTotal: 70,
            pmTotal: 30,
            inputTokens: 40,
            outputTokens: 30,
            cacheReadTokens: 20,
            cacheWriteTokens: 10,
          },
        ],
      },
      global: { plugins: [i18n()] },
    })
    const option = (wrapper.vm as unknown as { chartOption: TrendOption }).chartOption
    expectSquareThemeTooltip(option.tooltip, 'dark')
    expect(option.tooltip?.className).toMatch(/token-stats-echarts-tooltip/)
    expect(option.tooltip?.className).not.toMatch(/token-trend-tooltip/)
    expect(typeof option.tooltip?.formatter).toBe('function')
    const html = option.tooltip!.formatter!({ dataIndex: 0 })
    expect(html).toContain('07-24')
    expect(html).toContain('工作流')
    expect(html).toContain('项目管理')
    expect(html).toMatch(/data-tip-row="workflow"/)
    expect(html).toMatch(/data-tip-row="pm"/)
    expect(wrapper.find('[data-testid="token-trend-tooltip"]').exists()).toBe(false)
    expect(wrapper.html()).not.toMatch(/rounded-lg/)
    wrapper.unmount()
  })

  it('keeps ~200px wrap height and rejects preserveAspectRatio=none stretch (g2.2)', () => {
    const wrapper = mount(TokenTrendChart, {
      props: {
        bucketWidth: 'day',
        trend: sampleTrend(30),
      },
      global: { plugins: [i18n()] },
      attachTo: document.body,
    })
    const wrap = wrapper.find('[data-testid="token-trend-wrap"]')
    expect(wrap.exists()).toBe(true)
    expect((wrap.element as HTMLElement).style.height).toBe('200px')
    expect(wrapper.find('svg[preserveAspectRatio="none"]').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('preserveAspectRatio="none"')
    ;(wrap.element as HTMLElement).style.width = '1280px'
    expect(wrapper.find('[data-testid="token-trend-chart"] canvas').exists()).toBe(true)
    wrapper.unmount()
  })

  it('trend wrap uses min-w-0 + overflow-x-clip to prevent ECharts flex overflow (g3.1)', () => {
    const wrapper = mount(TokenTrendChart, {
      props: {
        bucketWidth: 'day',
        trend: sampleTrend(7),
      },
      global: { plugins: [i18n()] },
    })
    const wrap = wrapper.find('[data-testid="token-trend-wrap"]')
    expect(wrap.classes()).toEqual(expect.arrayContaining(['min-w-0', 'overflow-x-clip', 'w-full']))
    expect((wrap.element as HTMLElement).style.height).toBe('200px')
    wrapper.unmount()
  })

  it('maps 30-day labels with tick thinning (maxInterval≈8) (g2.2)', () => {
    const wrapper = mount(TokenTrendChart, {
      props: {
        bucketWidth: 'day',
        trend: sampleTrend(30),
      },
      global: { plugins: [i18n()] },
    })
    const exposed = wrapper.vm as unknown as {
      chartOption: TrendOption
      chartData: { labels: string[]; datasets: { data: number[] }[] }
    }
    expect(exposed.chartOption.xAxis?.axisLabel?.maxInterval).toBe(Math.ceil(30 / 8))
    expect(exposed.chartOption.tooltip?.appendToBody).toBe(true)
    expect(exposed.chartData.labels).toHaveLength(30)
    expect(exposed.chartData.datasets[0]?.data).toHaveLength(30)
    wrapper.unmount()
  })

  it('renders donut/pie with right legend, visible labels, no HTML legend or center total (g2.1)', () => {
    const wrapper = mount(TokenDonutChart, {
      props: {
        composition: {
          inputTokens: 70,
          outputTokens: 55,
          cacheReadTokens: 35,
          cacheWriteTokens: 20,
          total: 180,
        },
      },
      global: { plugins: [i18n()] },
    })
    expect(wrapper.find('[data-testid="token-donut-chart"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="token-donut-legend"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('总量')
    expect(wrapper.find('[data-testid="token-donut-chart"]').attributes('aria-label')).toBe('用量构成')
    const option = (wrapper.vm as unknown as { chartOption: PieOption }).chartOption
    expect(option.legend?.orient).toBe('vertical')
    expect(option.legend?.right).toBe(0)
    expect(option.legend?.bottom).toBeUndefined()
    expect(option.series?.[0]?.label?.show).toBe(true)
    expect(option.series?.[0]?.label?.formatter).toBe('{b} {d}%')
    expect(option.series?.[0]?.center?.[0]).toBe('38%')
    const names = (
      option.series?.[0] as unknown as { data: { name: string; value: number }[] }
    ).data.map((d) => d.name)
    expect(names).toEqual(['输入', '输出', '缓存读', '缓存写'])
    expect(names.join(' ')).not.toMatch(/\binput\b/)
    const parts = (wrapper.vm as unknown as { parts: { pct: number }[] }).parts
    expect(parts.map((p) => p.pct)).toEqual([38.9, 30.6, 19.4, 11.1])
    wrapper.unmount()
  })

  it('donut tooltip uses compact i18n formatter and square theme chrome (g2.2)', () => {
    setTheme('light')
    const wrapper = mount(TokenDonutChart, {
      props: {
        composition: {
          inputTokens: 70,
          outputTokens: 55,
          cacheReadTokens: 35,
          cacheWriteTokens: 20,
          total: 180,
        },
      },
      global: { plugins: [i18n()] },
    })
    const option = (wrapper.vm as unknown as { chartOption: PieOption }).chartOption
    expectSquareThemeTooltip(option.tooltip, 'light')
    expect(typeof option.tooltip?.formatter).toBe('function')
    expect(option.tooltip?.formatter?.({ name: '输入', value: 396_400, percent: 38.9 })).toBe(
      pieTooltipFormatter({ name: '输入', value: 396_400, percent: 38.9 }),
    )
    expect(wrapper.find('[data-testid="token-donut-tooltip"]').exists()).toBe(false)
    expect(wrapper.html()).not.toMatch(/#1a1d23/)
    wrapper.unmount()
  })

  it('donut plot is full-width overflow-visible (g4.1/g4.2)', () => {
    const wrapper = mount(TokenDonutChart, {
      props: {
        composition: {
          inputTokens: 70,
          outputTokens: 55,
          cacheReadTokens: 35,
          cacheWriteTokens: 20,
          total: 180,
        },
      },
      global: { plugins: [i18n()] },
    })
    const row = wrapper.find('[data-testid="token-donut-row"]')
    expect(row.classes()).toEqual(expect.arrayContaining(['w-full', 'overflow-visible']))
    const chart = wrapper.find('[data-testid="token-donut-chart"]')
    expect(chart.classes()).toEqual(expect.arrayContaining(['h-[168px]', 'w-full']))
    wrapper.unmount()
  })

  it('renders consumption rank with continuous numeric badges for workflow+PM (no 12PM34)', () => {
    const wrapper = mount(TokenWorkflowRank, {
      props: {
        workflows: [
          { workflowId: 'a', name: 'approve-main', total: 1_020_000, kind: 'workflow' },
          { workflowId: 'b', name: 'doc-review', total: 40_000, kind: 'workflow' },
          { name: 'PM', total: 80_000, kind: 'pm' },
          { name: 'other', total: 20_000, other: true, kind: 'other' },
        ],
      },
      global: { plugins: [i18n()] },
    })
    const items = wrapper.findAll('li')
    expect(items).toHaveLength(4)
    expect(wrapper.find('.token-rank-pm').exists()).toBe(true)
    expect(wrapper.find('.token-rank-other').exists()).toBe(true)
    expect(wrapper.find('[data-kind="pm"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="token-rank-bar"]').length).toBe(4)
    expect(wrapper.findAll('[data-testid="mock-vchart"]').length).toBe(4)
    expect(wrapper.text()).toContain('项目管理')
    expect(wrapper.text()).toContain('其他')

    const pmRow = wrapper.find('[data-kind="pm"]')
    const badges = wrapper.findAll('li').map((li) => li.find('span').text())
    expect(badges).toEqual(['1', '2', '3', '·'])
    expect(pmRow.find('span').text()).toBe('3')
    const pmBadge = pmRow.find('span')
    expect(pmBadge.classes()).toEqual(
      expect.arrayContaining(['w-[22px]', 'bg-accent-dim', 'text-accent-2']),
    )
    expect(pmBadge.classes()).not.toEqual(expect.arrayContaining(['w-auto', 'min-w-[22px]', 'px-1.5']))
    const wfBadge = items[0]!.find('span')
    expect(wfBadge.classes()).toEqual(pmBadge.classes())

    const barColorFn = (
      wrapper.vm as unknown as {
        barColor: (w: { kind?: string; other?: boolean; name: string; total: number }) => {
          colorStops: { color: string }[]
        }
      }
    ).barColor
    const pmGradient = barColorFn({ kind: 'pm', name: 'PM', total: 80_000 })
    expect(pmGradient.colorStops[0]!.color).toBe('#6d5cff')
    expect(pmGradient.colorStops[1]!.color).toBe('#9b8cff')
    expect(pmGradient.colorStops.map((s) => s.color)).not.toContain('#f59e0b')

    const values = wrapper.findAll('[data-testid="token-rank-value"]').map((v) => v.text())
    expect(values).toEqual(['1.02M', '40K', '80K', '20K'])
    expect(wrapper.find('[data-testid="token-rank-value"]').attributes('title')).toBeTruthy()
    expect(wrapper.find('[data-testid="token-rank-tooltip"]').exists()).toBe(false)
    const rowTip = (
      wrapper.vm as unknown as { rowOptions: { tooltip: { borderRadius?: number; backgroundColor?: string } }[] }
    ).rowOptions[0]!.tooltip
    expect(rowTip.borderRadius).toBe(12)
    expect(JSON.stringify(rowTip)).not.toContain('#1a1d23')
    wrapper.unmount()
  })

  it('Demo sample: 4 workflows + PM + other → badges 1→2→3→4→5→· (g3.3)', () => {
    const wrapper = mount(TokenWorkflowRank, {
      props: {
        workflows: [
          { workflowId: '1', name: '自我迭代', total: 1200, kind: 'workflow' },
          { workflowId: '2', name: '自我迭代·轻量', total: 240, kind: 'workflow' },
          { workflowId: '3', name: '调研', total: 140, kind: 'workflow' },
          { workflowId: '4', name: '产品宣传文章', total: 70, kind: 'workflow' },
          { name: 'PM', total: 180, kind: 'pm' },
          { name: 'other', total: 55, other: true, kind: 'other' },
        ],
      },
      global: { plugins: [i18n()] },
    })
    const badges = wrapper.findAll('li').map((li) => li.find('span').text())
    expect(badges).toEqual(['1', '2', '3', '4', '5', '·'])
    expect(wrapper.find('[data-kind="pm"]').find('span').text()).toBe('5')
    expect(wrapper.find('[data-kind="pm"]').find('span').classes()).toEqual(
      expect.arrayContaining(['w-[22px]', 'bg-elevated', 'text-txt3']),
    )
    wrapper.unmount()
  })

  it('hides PM row when absent; no-other still continuous (g3.3)', () => {
    const noPm = mount(TokenWorkflowRank, {
      props: {
        workflows: [
          { workflowId: 'a', name: 'approve-main', total: 100, kind: 'workflow' },
          { name: 'other', total: 20, other: true, kind: 'other' },
        ],
      },
      global: { plugins: [i18n()] },
    })
    expect(noPm.find('[data-kind="pm"]').exists()).toBe(false)
    expect(noPm.findAll('li').map((li) => li.find('span').text())).toEqual(['1', '·'])
    noPm.unmount()

    const noOther = mount(TokenWorkflowRank, {
      props: {
        workflows: [
          { workflowId: 'a', name: 'approve-main', total: 100, kind: 'workflow' },
          { name: 'PM', total: 50, kind: 'pm' },
        ],
      },
      global: { plugins: [i18n()] },
    })
    expect(noOther.find('[data-kind="other"]').exists()).toBe(false)
    expect(noOther.findAll('li').map((li) => li.find('span').text())).toEqual(['1', '2'])
    noOther.unmount()
  })

  it('trend PM dataset shares workflow purple and uses borderDash (g2.2/g3.1)', () => {
    expect(TOKEN_SOURCE_COLORS.pm).toBe(TOKEN_SOURCE_COLORS.workflow)
    expect(TOKEN_SOURCE_COLORS.pm).toBe('#6d5cff')
    expect(TOKEN_SOURCE_COLORS.pm).not.toBe('#f59e0b')

    const wrapper = mount(TokenTrendChart, {
      props: {
        bucketWidth: 'day',
        trend: sampleTrend(7),
      },
      global: { plugins: [i18n()] },
    })
    const exposed = wrapper.vm as unknown as {
      chartData: {
        datasets: {
          label: string
          borderColor: string
          backgroundColor: string
          borderDash?: number[]
        }[]
      }
    }
    const pmDs = exposed.chartData.datasets.find((d) => d.label === 'pm')
    const wfDs = exposed.chartData.datasets.find((d) => d.label === 'workflow')
    expect(pmDs).toBeTruthy()
    expect(wfDs).toBeTruthy()
    expect(pmDs!.borderColor).toBe(wfDs!.borderColor)
    expect(pmDs!.borderColor).toBe('#6d5cff')
    expect(pmDs!.borderDash).toEqual([5, 4])
    expect(wfDs!.borderDash).toBeUndefined()
    expect(String(pmDs!.backgroundColor)).not.toMatch(/245,\s*158,\s*11/)
    const pmSeries = (wrapper.vm as unknown as { chartOption: TrendOption }).chartOption.series?.find(
      (s) => s.name === '项目管理',
    )
    expect(pmSeries?.symbolSize).toBe(7)
    expect(pmSeries?.clip).toBe(false)
    wrapper.unmount()
  })

  it('stacks workflow, PM, and Agent Studio and drops an all-zero source (review v1)', () => {
    const mixed = mount(TokenTrendChart, {
      props: {
        bucketWidth: 'day',
        trend: [
          {
            bucket: '2026-07-24',
            total: 180,
            workflowTotal: 100,
            pmTotal: 20,
            studioTotal: 60,
            inputTokens: 80,
            outputTokens: 50,
            cacheReadTokens: 30,
            cacheWriteTokens: 20,
          },
          {
            bucket: '2026-07-25',
            total: 40,
            workflowTotal: 0,
            pmTotal: 10,
            studioTotal: 30,
            inputTokens: 20,
            outputTokens: 10,
            cacheReadTokens: 10,
            cacheWriteTokens: 0,
          },
        ],
      },
      global: { plugins: [i18n()] },
    })
    const mixedExposed = mixed.vm as unknown as {
      chartData: { datasets: { label: string; data: number[] }[] }
      chartOption: TrendOption
    }
    expect(mixedExposed.chartData.datasets.map((d) => d.label)).toEqual(['workflow', 'pm', 'studio'])
    expect(mixedExposed.chartOption.series?.map((s) => s.name)).toEqual(['工作流', '项目管理', 'Agent Studio'])
    for (const [i, bucketTotal] of [180, 40].entries()) {
      const sum = mixedExposed.chartData.datasets.reduce((s, d) => s + (d.data[i] || 0), 0)
      expect(sum).toBe(bucketTotal)
    }
    mixed.unmount()

    const studioOnly = mount(TokenTrendChart, {
      props: {
        bucketWidth: 'day',
        trend: [
          {
            bucket: '2026-07-24',
            total: 60,
            workflowTotal: 0,
            pmTotal: 0,
            studioTotal: 60,
            inputTokens: 20,
            outputTokens: 20,
            cacheReadTokens: 20,
            cacheWriteTokens: 0,
          },
        ],
      },
      global: { plugins: [i18n()] },
    })
    const studioExposed = studioOnly.vm as unknown as {
      chartData: { datasets: { label: string; data: number[] }[] }
      chartOption: TrendOption
    }
    expect(studioExposed.chartData.datasets.map((d) => d.label)).toEqual(['studio'])
    expect(studioExposed.chartData.datasets[0]?.data).toEqual([60])
    expect(studioExposed.chartOption.series?.map((s) => s.name)).toEqual(['Agent Studio'])
    const html = studioExposed.chartOption.tooltip!.formatter!({ dataIndex: 0 })
    expect(html).toMatch(/data-tip-row="studio"/)
    expect(html).not.toMatch(/data-tip-row="workflow"/)
    expect(html).not.toMatch(/data-tip-row="pm"/)
    studioOnly.unmount()
  })
})

describe('TokenWorkflowRank track color (g1.1/g1.2/g2.1)', () => {
  const workflows = [
    { workflowId: 'a', name: 'approve-main', total: 1_020_000, kind: 'workflow' as const },
    { name: 'other', total: 20_000, other: true, kind: 'other' as const },
  ]

  function rowOption(vm: unknown, idx: number) {
    const opts = (
      vm as {
        rowOptions: {
          series: {
            backgroundStyle?: { color?: string }
            itemStyle?: { color?: { colorStops?: { color: string }[] } }
          }[]
        }[]
      }
    ).rowOptions
    return opts[idx]!.series[0]!
  }

  it('backgroundStyle is theme rgb without var(); purple and other gradients stay (g2.2)', () => {
    document.documentElement.style.removeProperty('--c-elevated')
    document.documentElement.classList.remove('light')
    setTheme('light')

    const wrapper = mount(TokenWorkflowRank, {
      props: { workflows },
      global: { plugins: [i18n()] },
    })

    const light = String(rowOption(wrapper.vm, 0).backgroundStyle?.color)
    expect(light).toMatch(/^rgb\(\d+,\s*\d+,\s*\d+\)$/)
    expect(light).not.toContain('var(')
    expect(light.replace(/\s/g, '')).toBe(RANK_TRACK_LIGHT.replace(/\s/g, ''))
    expect(String(rowOption(wrapper.vm, 1).backgroundStyle?.color).replace(/\s/g, '')).toBe(
      light.replace(/\s/g, ''),
    )
    expect(rowOption(wrapper.vm, 0).itemStyle?.color?.colorStops?.map((s) => s.color)).toEqual([
      '#6d5cff',
      '#9b8cff',
    ])
    expect(rowOption(wrapper.vm, 1).itemStyle?.color?.colorStops?.map((s) => s.color)).toEqual([
      '#94a3b8',
      '#cbd5e1',
    ])

    setTheme('dark')
    const dark = String(rowOption(wrapper.vm, 0).backgroundStyle?.color)
    expect(dark).toMatch(/^rgb\(\d+,\s*\d+,\s*\d+\)$/)
    expect(dark).not.toContain('var(')
    expect(dark.replace(/\s/g, '')).toBe(RANK_TRACK_DARK.replace(/\s/g, ''))
    expect(dark.replace(/\s/g, '')).not.toBe(light.replace(/\s/g, ''))
    expect(rowOption(wrapper.vm, 0).itemStyle?.color?.colorStops?.map((s) => s.color)).toEqual([
      '#6d5cff',
      '#9b8cff',
    ])

    document.documentElement.style.removeProperty('--c-elevated')
    wrapper.unmount()
  })
})

describe('TokenTrendChart tooltip / theme chrome (g2.3/g2.4)', () => {
  it('light and dark option tones follow 用量统计 shell', () => {
    const trend = [
      {
        bucket: '2026-07-11',
        total: 0,
        workflowTotal: 0,
        pmTotal: 0,
        inputTokens: 0,
        outputTokens: 0,
        cacheReadTokens: 0,
        cacheWriteTokens: 0,
      },
    ]

    setTheme('dark')
    const darkWrap = mount(TokenTrendChart, {
      props: { bucketWidth: 'day', trend },
      global: { plugins: [i18n()] },
    })
    const darkOpt = (darkWrap.vm as unknown as { chartOption: TrendOption }).chartOption
    expectSquareThemeTooltip(darkOpt.tooltip, 'dark')
    expect(darkOpt.yAxis?.splitLine?.lineStyle?.color).toBe('rgba(255,255,255,0.08)')
    expect(darkOpt.xAxis?.axisLabel?.color).toBe('#a1a1aa')
    const html = darkOpt.tooltip!.formatter!({ dataIndex: 0 })
    expect(html).toMatch(/07-11\s*·\s*0/)
    expect(html).not.toMatch(/(^|\s)-11\s*·/)
    expect(darkOpt.grid?.left).toBe(42)
    expect(darkOpt.grid?.containLabel).toBe(false)
    expect(darkOpt.tooltip?.triggerOn).toBe('mousemove')
    expect(darkOpt.tooltip?.axisPointer?.snap).toBe(true)
    expect(typeof darkOpt.tooltip?.position).toBe('function')
    // An all-zero window draws no source line (workflow / PM / Studio).
    expect(darkOpt.series).toEqual([])
    darkWrap.unmount()

    setTheme('light')
    const lightWrap = mount(TokenTrendChart, {
      props: { bucketWidth: 'day', trend },
      global: { plugins: [i18n()] },
    })
    const lightOpt = (lightWrap.vm as unknown as { chartOption: TrendOption }).chartOption
    expectSquareThemeTooltip(lightOpt.tooltip, 'light')
    expect(lightOpt.yAxis?.splitLine?.lineStyle?.color).toBe('#eef0f3')
    expect(lightOpt.xAxis?.axisLabel?.color).toBe('#71717a')
    lightWrap.unmount()
  })

  it('DOM .token-trend-tooltip is v-if removed on leave so E2E count hits 0 (g2.2/g4.5)', async () => {
    const trend = [
      {
        bucket: '2026-07-11',
        total: 0,
        workflowTotal: 0,
        pmTotal: 0,
        inputTokens: 0,
        outputTokens: 0,
        cacheReadTokens: 0,
        cacheWriteTokens: 0,
      },
    ]
    const wrapper = mount(TokenTrendChart, {
      props: { bucketWidth: 'day', trend },
      global: { plugins: [i18n()] },
      attachTo: document.body,
    })
    const wrap = wrapper.find('[data-testid="token-trend-wrap"]')
    await wrap.trigger('mousemove', { clientX: 48, clientY: 160 })
    const shown = document.querySelector('.token-trend-tooltip')
    expect(shown).toBeTruthy()
    expect(shown?.textContent).toMatch(/07-11/)
    expect(document.querySelector('[data-testid="token-trend-tooltip"]')).toBeNull()
    await wrap.trigger('mouseleave')
    expect(document.querySelector('.token-trend-tooltip')).toBeNull()
    wrapper.unmount()
  })
})
