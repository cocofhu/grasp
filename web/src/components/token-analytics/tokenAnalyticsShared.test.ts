// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import {
  activeFilterCount,
  applyDrill,
  bucketDateRange,
  defaultTokenStatsFilters,
  filtersToParams,
  fmtCost,
  fmtHitRatePct,
  fmtPct,
  isDrillable,
  toCsv,
} from './tokenAnalyticsShared'
import { cacheHitRatio, drillFromChartEvent, listDrillBreakdowns, trendChartOption } from './tokenAnalyticsCharts'
import type { GlobalTokenStats } from '@/lib/shared/types'

const t = (k: string) => k

describe('tokenAnalyticsShared', () => {
  it('maps trend buckets to local date ranges', () => {
    expect(bucketDateRange('2026-07-25', 'day')).toEqual({ from: '2026-07-25', to: '2026-07-25', granularity: 'hour' })
    expect(bucketDateRange('2026-07-25T14', 'hour')).toEqual({ from: '2026-07-25', to: '2026-07-25', granularity: 'hour' })
    // ISO week 2026-W01 starts Monday 2025-12-29.
    expect(bucketDateRange('2026-W01', 'week')).toEqual({ from: '2025-12-29', to: '2026-01-04', granularity: 'day' })
    expect(bucketDateRange('garbage', 'day')).toBeNull()
  })

  it('narrows filters along a drill path; later steps win on the same dimension', () => {
    const base = { ...defaultTokenStatsFilters(), window: '30d' as const, source: 'pm' as const }
    const out = applyDrill(base, [
      { dim: 'workflow', key: 'w1', name: 'W1' },
      { dim: 'bucket', key: '2026-07-02', name: '07-02', bucketWidth: 'day' },
    ])
    expect(out).toMatchObject({
      workflowId: 'w1',
      source: 'workflow',
      window: 'custom',
      from: '2026-07-02',
      to: '2026-07-02',
      granularity: 'hour',
    })
    expect(applyDrill(base, [{ dim: 'phase', key: 'chat', name: 'Chat' }]).phase).toBe('chat')
  })

  it('serializes filters to API params', () => {
    const f = { ...defaultTokenStatsFilters(), window: 'custom' as const, from: '2026-07-01', to: '', status: 'failed' as const }
    const p = filtersToParams(f)
    expect(p).toMatchObject({ window: 'custom', from: '2026-07-01', to: '2026-07-01', status: 'failed' })
    expect(filtersToParams({ ...f, from: '' }).window).toBe('all')
    expect(activeFilterCount(f)).toBe(1)
  })

  it('validates drill targets', () => {
    expect(isDrillable({ dim: 'workflow', key: '', name: '' })).toBe(false)
    expect(isDrillable({ dim: 'bucket', key: 'W30', name: '' })).toBe(false)
    expect(isDrillable({ dim: 'model', key: 'm', name: 'm' })).toBe(true)
  })

  it('formats cost and CSV', () => {
    expect(fmtCost(0, 'USD')).toBe('$0')
    expect(fmtCost(0.004, 'USD')).toBe('<$0.01')
    expect(fmtCost(12.345, 'CNY')).toBe('¥12.35')
    expect(fmtCost(12_345, 'USD')).toBe('$12.3K')
    expect(toCsv(['a', 'b'], [['x,y', 'say "hi"'], [1, null]])).toBe('a,b\r\n"x,y","say ""hi"""\r\n1,')
  })

  it('never rounds a cache hit rate below 1 up to 100%', () => {
    expect(fmtHitRatePct(0.9996)).toBe('99.9%')
    expect(fmtHitRatePct(0.9995)).toBe('99.9%')
    expect(fmtHitRatePct(1)).toBe('100.0%')
    expect(fmtHitRatePct(0.969)).toBe('96.9%')
    expect(fmtHitRatePct(0)).toBe('0.0%')
    expect(fmtHitRatePct(undefined)).toBe('0.0%')
    expect(fmtPct(0.9996)).toBe('100.0%')
  })
})

describe('tokenAnalyticsCharts', () => {
  const stats = {
    window: '7d',
    bucketWidth: 'day',
    timezone: 'UTC',
    empty: false,
    currency: 'USD',
    trend: [
      { bucket: '2026-07-01', total: 10, inputTokens: 6, outputTokens: 2, cacheReadTokens: 2, cacheWriteTokens: 0, cost: 1.5 },
      { bucket: '2026-07-02', total: 0, inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 },
    ],
    prevTrend: [],
    modelTrends: [{ key: 'p1', name: 'P1', trend: [{ bucket: '2026-07-02', total: 5, inputTokens: 5, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 }] }],
  } as unknown as GlobalTokenStats

  it('attaches bucket (+ series) drill paths to trend points', () => {
    const opt = trendChartOption(stats, 'model', t) as { series: Array<{ data: Array<{ value: number; drill: unknown }> }> }
    expect(opt.series[0].data.map((d) => d.value)).toEqual([0, 5])
    expect(opt.series[0].data[1].drill).toEqual([
      { dim: 'bucket', key: '2026-07-02', name: '2026-07-02', bucketWidth: 'day' },
      { dim: 'model', key: 'p1', name: 'P1' },
    ])
  })

  it('renders cost mode as cost bars plus cache-hit line on a second axis', () => {
    const opt = trendChartOption(stats, 'cost', t) as { series: Array<{ type: string; yAxisIndex?: number; data: unknown[] }> }
    expect(opt.series.map((s) => s.type)).toEqual(['bar', 'line'])
    expect(opt.series[1].yAxisIndex).toBe(1)
    expect(opt.series[1].data[0]).toBe(25)
  })

  it('formats the cost-mode cache-hit tooltip without reading 100% below a full hit', () => {
    const nearFull = {
      ...stats,
      trend: [{ bucket: '2026-07-01', total: 100_000, inputTokens: 4, outputTokens: 0, cacheReadTokens: 99_996, cacheWriteTokens: 0, cost: 1 }],
    } as unknown as GlobalTokenStats
    type Param = { seriesIndex: number; dataIndex?: number; axisValueLabel: string; value: number; marker: string; seriesName: string }
    const opt = trendChartOption(nearFull, 'cost', t) as { tooltip: { formatter: (p: Param[]) => string }; series: Array<{ data: unknown[] }> }
    expect(opt.series[1].data[0]).toBe(100)
    const line = { seriesIndex: 1, axisValueLabel: '07-01', marker: '', seriesName: 'hit' }
    expect(opt.tooltip.formatter([{ ...line, dataIndex: 0, value: 100 }])).toContain('hit: 99.9%')
    expect(opt.tooltip.formatter([{ ...line, value: 99.96 }])).toContain('hit: 99.9%')
  })

  it('computes cache hit ratio over all input-side tokens', () => {
    expect(cacheHitRatio({ inputTokens: 6, cacheReadTokens: 2, cacheWriteTokens: 0 })).toBe(0.25)
    expect(cacheHitRatio({})).toBe(0)
  })

  it('hides locked drill dimensions and single-bucket axes (g3.3 g4.2)', () => {
    const rich = {
      ...stats,
      modelRanking: [
        { modelKey: 'a', name: 'A', total: 8, inputTokens: 5, outputTokens: 3 },
        { modelKey: 'b', name: 'B', total: 6, inputTokens: 3, outputTokens: 3 },
      ],
      workflows: [],
      nodeTypes: [{ key: 'agent', name: 'agent', total: 10, inputTokens: 6, outputTokens: 4 }],
      sources: [
        { key: 'workflow', name: 'workflow', total: 9, inputTokens: 5, outputTokens: 4 },
        { key: 'studio', name: 'studio', total: 5, inputTokens: 3, outputTokens: 2 },
      ],
      statuses: [
        { key: 'ok', name: 'ok', total: 8, inputTokens: 4, outputTokens: 4 },
        { key: 'failed', name: 'failed', total: 6, inputTokens: 4, outputTokens: 2 },
      ],
      phases: [
        { key: 'production', name: 'production', total: 7, inputTokens: 4, outputTokens: 3 },
        { key: 'chat', name: 'chat', total: 7, inputTokens: 4, outputTokens: 3 },
      ],
      topRuns: [
        { runId: 'r1', title: 'R1', total: 6, inputTokens: 4, outputTokens: 2 },
        { runId: 'r2', title: 'R2', total: 4, inputTokens: 2, outputTokens: 2 },
      ],
    } as unknown as GlobalTokenStats
    const openBreakdowns = listDrillBreakdowns(rich, {}, t)
    const open = openBreakdowns.map((b) => b.id)
    expect(open).toEqual(expect.arrayContaining(['source', 'model', 'status', 'phase', 'run']))
    expect(open).not.toContain('nodeType')
    const phaseRows = openBreakdowns.find((b) => b.id === 'phase')?.rows ?? []
    expect(phaseRows.map((r) => r.drill)).toEqual([
      { dim: 'phase', key: 'production', name: 'pages.tokenAnalytics.phases.production' },
      { dim: 'phase', key: 'chat', name: 'pages.tokenAnalytics.phases.chat' },
    ])
    const locked = listDrillBreakdowns(rich, { source: 'workflow', status: 'ok', runId: 'r1' }, t).map((b) => b.id)
    for (const id of ['source', 'status', 'run']) expect(locked).not.toContain(id)
    expect(locked).toEqual(expect.arrayContaining(['model', 'phase']))
    const phaseLocked = listDrillBreakdowns(rich, { phase: 'chat' }, t).map((b) => b.id)
    expect(phaseLocked).not.toContain('phase')
    expect(phaseLocked).toEqual(expect.arrayContaining(['source', 'status']))
  })

  it('extracts drill paths from chart click events', () => {
    expect(drillFromChartEvent({ componentType: 'series', data: { drill: [{ dim: 'model', key: 'm', name: 'm' }] } })).toHaveLength(1)
    expect(drillFromChartEvent({ componentType: 'series', data: { other: true, drill: [{ dim: 'model', key: 'm', name: 'm' }] } })).toBeNull()
    expect(drillFromChartEvent({ componentType: 'xAxis' })).toBeNull()
  })
})
