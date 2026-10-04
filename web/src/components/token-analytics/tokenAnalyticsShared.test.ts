// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import {
  activeFilterCount,
  applyDrill,
  boardQueryFromFilters,
  bucketDateRange,
  defaultTokenStatsFilters,
  filtersToParams,
  fmtCost,
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
      { dim: 'project', key: 'p1', name: 'P1' },
      { dim: 'workflow', key: 'w1', name: 'W1' },
      { dim: 'bucket', key: '2026-07-02', name: '07-02', bucketWidth: 'day' },
      { dim: 'project', key: 'p2', name: 'P2' },
    ])
    expect(out).toMatchObject({
      projectId: 'p2',
      workflowId: 'w1',
      source: 'workflow',
      window: 'custom',
      from: '2026-07-02',
      to: '2026-07-02',
      granularity: 'hour',
    })
    expect(applyDrill(base, [{ dim: 'phase', key: 'chat', name: 'Chat' }]).phase).toBe('chat')
    expect(base.projectId).toBe('')
  })

  it('serializes filters to API params', () => {
    const f = { ...defaultTokenStatsFilters(), window: 'custom' as const, from: '2026-07-01', to: '', status: 'failed' as const }
    const p = filtersToParams(f)
    expect(p).toMatchObject({ window: 'custom', from: '2026-07-01', to: '2026-07-01', status: 'failed' })
    expect(filtersToParams({ ...f, from: '' }).window).toBe('all')
    expect(activeFilterCount(f)).toBe(1)
  })

  it('validates drill targets', () => {
    expect(isDrillable({ dim: 'project', key: '', name: '' })).toBe(false)
    expect(isDrillable({ dim: 'bucket', key: 'W30', name: '' })).toBe(false)
    expect(isDrillable({ dim: 'model', key: 'm', name: 'm' })).toBe(true)
  })

  it('carries the current window onto the project board (g3.4)', () => {
    expect(boardQueryFromFilters(defaultTokenStatsFilters())).toEqual({ tab: 'board', window: 'all' })
    expect(boardQueryFromFilters({ ...defaultTokenStatsFilters(), window: 'custom', from: '2026-07-01', to: '2026-07-03', granularity: 'day' })).toEqual({
      tab: 'board',
      window: 'custom',
      from: '2026-07-01',
      to: '2026-07-03',
      granularity: 'day',
    })
  })

  it('formats cost and CSV', () => {
    expect(fmtCost(0, 'USD')).toBe('$0')
    expect(fmtCost(0.004, 'USD')).toBe('<$0.01')
    expect(fmtCost(12.345, 'CNY')).toBe('¥12.35')
    expect(fmtCost(12_345, 'USD')).toBe('$12.3K')
    expect(toCsv(['a', 'b'], [['x,y', 'say "hi"'], [1, null]])).toBe('a,b\r\n"x,y","say ""hi"""\r\n1,')
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
    projectTrends: [{ key: 'p1', name: 'P1', trend: [{ bucket: '2026-07-02', total: 5, inputTokens: 5, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 }] }],
    modelTrends: [],
  } as unknown as GlobalTokenStats

  it('attaches bucket (+ series) drill paths to trend points', () => {
    const opt = trendChartOption(stats, 'project', t) as { series: Array<{ data: Array<{ value: number; drill: unknown }> }> }
    expect(opt.series[0].data.map((d) => d.value)).toEqual([0, 5])
    expect(opt.series[0].data[1].drill).toEqual([
      { dim: 'bucket', key: '2026-07-02', name: '2026-07-02', bucketWidth: 'day' },
      { dim: 'project', key: 'p1', name: 'P1' },
    ])
  })

  it('renders cost mode as cost bars plus cache-hit line on a second axis', () => {
    const opt = trendChartOption(stats, 'cost', t) as { series: Array<{ type: string; yAxisIndex?: number; data: unknown[] }> }
    expect(opt.series.map((s) => s.type)).toEqual(['bar', 'line'])
    expect(opt.series[1].yAxisIndex).toBe(1)
    expect(opt.series[1].data[0]).toBe(25)
  })

  it('computes cache hit ratio over all input-side tokens', () => {
    expect(cacheHitRatio({ inputTokens: 6, cacheReadTokens: 2, cacheWriteTokens: 0 })).toBe(0.25)
    expect(cacheHitRatio({})).toBe(0)
  })

  it('hides locked drill dimensions and single-bucket axes (g3.3 g4.2)', () => {
    const rich = {
      ...stats,
      projects: [
        { projectId: 'p1', name: 'P1', total: 10, inputTokens: 6, outputTokens: 4 },
        { projectId: 'p2', name: 'P2', total: 4, inputTokens: 2, outputTokens: 2 },
      ],
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
    expect(open).toEqual(expect.arrayContaining(['project', 'source', 'model', 'status', 'phase', 'run']))
    expect(open).not.toContain('nodeType')
    const phaseRows = openBreakdowns.find((b) => b.id === 'phase')?.rows ?? []
    expect(phaseRows.map((r) => r.drill)).toEqual([
      { dim: 'phase', key: 'production', name: 'pages.tokenAnalytics.phases.production' },
      { dim: 'phase', key: 'chat', name: 'pages.tokenAnalytics.phases.chat' },
    ])
    const locked = listDrillBreakdowns(rich, { projectId: 'p1', source: 'workflow', status: 'ok', runId: 'r1' }, t).map((b) => b.id)
    for (const id of ['project', 'source', 'status', 'run']) expect(locked).not.toContain(id)
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
