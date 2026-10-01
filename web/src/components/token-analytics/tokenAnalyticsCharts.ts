/** ECharts option builders for 用量统计 and its drill-down modal. */
import {
  STATS_CHART_GRID,
  axisTooltip,
  chartTone,
  fmtCompactAxis,
  pieChartOption,
  statsAxis,
  statsLegend,
  statsTooltip,
  type PieSlice,
} from '@/components/charts/chartTheme'
import {
  TOKEN_PART_COLORS,
  TOKEN_PART_KEYS,
  TOKEN_SOURCE_COLORS,
  formatBucketLabel,
  type TokenPartKey,
} from '@/components/board/token-stats/tokenStatsShared'
import { fmtCompactTokenCount, fmtTokenCount } from '@/lib/run/tokenUsage'
import type {
  GlobalTokenStats,
  GlobalTokenStatsTreeNode,
  TokenStatsBucket,
} from '@/lib/shared/types'
import {
  TOKEN_LEDGER_SOURCE_COLORS,
  fmtCost,
  paletteColor,
  type DrillTarget,
} from './tokenAnalyticsShared'

export type Translate = (key: string, params?: Record<string, unknown>) => string

export type TrendMode = 'total' | 'project' | 'model' | 'cost'
export type AreaMode = 'source' | 'comp' | 'status'

export const BAR_MAX_WIDTH = 28
const BAR_TOP_RADIUS = 4
/** Above this many buckets the trend gets a zoom slider. */
const ZOOM_THRESHOLD = 24

const PART_LABEL_KEYS: Record<TokenPartKey, string> = {
  input: 'pages.executionTimeline.partInput',
  output: 'pages.executionTimeline.partOutput',
  cacheRead: 'pages.executionTimeline.partCacheRead',
  cacheWrite: 'pages.executionTimeline.partCacheWrite',
}

export function partLabel(t: Translate, key: TokenPartKey): string {
  return t(PART_LABEL_KEYS[key])
}

export function sourceLabel(t: Translate, key: string): string {
  if (key === 'workflow') return t('pages.board.tokenStats.workflow')
  if (key === 'pm') return t('pages.board.tokenStats.pm')
  if (key === 'studio') return t('pages.tokenAnalytics.sourceStudio')
  return key
}

export function statusLabel(t: Translate, key: string): string {
  if (key === 'ok' || key === 'failed' || key === 'cancelled') return t(`pages.tokenAnalytics.statuses.${key}`)
  return key
}

export function phaseLabel(t: Translate, key: string): string {
  if (key === 'production' || key === 'interactive' || key === 'chat') return t(`pages.tokenAnalytics.phases.${key}`)
  return key
}

export function bucketPart(b: { inputTokens?: number; outputTokens?: number; cacheReadTokens?: number; cacheWriteTokens?: number }, key: TokenPartKey): number {
  if (key === 'input') return b.inputTokens || 0
  if (key === 'output') return b.outputTokens || 0
  if (key === 'cacheRead') return b.cacheReadTokens || 0
  return b.cacheWriteTokens || 0
}

export function cacheHitRatio(b: { inputTokens?: number; cacheReadTokens?: number; cacheWriteTokens?: number }): number {
  const denom = (b.inputTokens || 0) + (b.cacheReadTokens || 0) + (b.cacheWriteTokens || 0)
  return denom > 0 ? (b.cacheReadTokens || 0) / denom : 0
}

function categoryAxis(labels: string[]) {
  return {
    type: 'category' as const,
    data: labels,
    ...statsAxis(),
    splitLine: { show: false },
    axisLabel: { ...statsAxis().axisLabel, interval: 'auto' },
  }
}

function tokenValueAxis() {
  return {
    type: 'value' as const,
    ...statsAxis(),
    axisLabel: { ...statsAxis().axisLabel, formatter: (v: number) => fmtCompactAxis(v) },
  }
}

function zoomFor(n: number) {
  if (n <= ZOOM_THRESHOLD) return undefined
  const start = Math.max(0, 100 - Math.round((ZOOM_THRESHOLD / n) * 100))
  return [
    { type: 'inside' as const, start, end: 100 },
    { type: 'slider' as const, start, end: 100, height: 14, bottom: 4, brushSelect: false },
  ]
}

function gridFor(n: number) {
  return n > ZOOM_THRESHOLD ? { ...STATS_CHART_GRID, bottom: 64 } : STATS_CHART_GRID
}

function alignSeries(src: TokenStatsBucket[], master: { bucket: string }[]): number[] {
  const map = new Map(src.map((b) => [b.bucket, b.total || 0]))
  return master.map((b) => map.get(b.bucket) || 0)
}

/** Data item carrying the drill path for a trend point. */
function bucketItem(value: number, bucket: string, width: string, extra?: DrillTarget) {
  const path: DrillTarget[] = [{ dim: 'bucket', key: bucket, name: bucket, bucketWidth: width }]
  if (extra) path.push(extra)
  return { value, drill: path }
}

export function trendChartOption(data: GlobalTokenStats | null, mode: TrendMode, t: Translate) {
  if (!data || data.empty || !data.trend.length) return null
  const bw = data.bucketWidth
  const labels = data.trend.map((b) => formatBucketLabel(b.bucket, bw))
  const n = labels.length

  if (mode === 'cost') {
    const currency = data.currency
    return {
      grid: gridFor(n),
      tooltip: statsTooltip({
        trigger: 'axis',
        formatter: (params: Array<{ seriesIndex: number; axisValueLabel: string; value: number; marker: string; seriesName: string }>) => {
          const head = params[0]?.axisValueLabel ?? ''
          const lines = params.map((p) => {
            const v = p.seriesIndex === 0 ? fmtCost(p.value, currency) : `${(p.value || 0).toFixed(1)}%`
            return `${p.marker}${p.seriesName}: ${v}`
          })
          return [head, ...lines].join('<br/>')
        },
      }),
      legend: statsLegend(),
      dataZoom: zoomFor(n),
      xAxis: categoryAxis(labels),
      yAxis: [
        {
          type: 'value' as const,
          ...statsAxis(),
          axisLabel: { ...statsAxis().axisLabel, formatter: (v: number) => fmtCost(v, currency) },
        },
        {
          type: 'value' as const,
          min: 0,
          max: 100,
          ...statsAxis(),
          splitLine: { show: false },
          axisLabel: { ...statsAxis().axisLabel, formatter: '{value}%' },
        },
      ],
      series: [
        {
          type: 'bar',
          name: t('pages.tokenAnalytics.lineModes.cost'),
          barMaxWidth: BAR_MAX_WIDTH,
          itemStyle: { color: '#f59e0b', borderRadius: [BAR_TOP_RADIUS, BAR_TOP_RADIUS, 0, 0] },
          data: data.trend.map((b) => bucketItem(Number((b.cost || 0).toFixed(6)), b.bucket, bw)),
        },
        {
          type: 'line',
          name: t('pages.tokenAnalytics.cacheHit'),
          yAxisIndex: 1,
          smooth: true,
          showSymbol: false,
          lineStyle: { width: 2 },
          itemStyle: { color: '#22a6b3' },
          data: data.trend.map((b) => Number((cacheHitRatio(b) * 100).toFixed(2))),
        },
      ],
    }
  }

  let series: Array<{ name: string; data: unknown[]; color: string; dashed?: boolean; area?: boolean }>
  if (mode === 'total') {
    series = [
      {
        name: t('pages.tokenAnalytics.lineModes.total'),
        data: data.trend.map((b) => bucketItem(b.total || 0, b.bucket, bw)),
        color: TOKEN_SOURCE_COLORS.workflow,
        area: true,
      },
      {
        name: t('pages.tokenAnalytics.linePrevWindow'),
        data: data.prevTrend.map((b) => b.total || 0),
        color: '#c4b5fd',
        dashed: true,
      },
    ]
  } else {
    const groups = mode === 'project' ? data.projectTrends : data.modelTrends
    series = groups.map((s, i) => {
      const values = alignSeries(s.trend, data.trend)
      const extra: DrillTarget | undefined = s.key && !s.key.startsWith('_')
        ? { dim: mode === 'project' ? 'project' : 'model', key: s.key, name: s.name }
        : undefined
      return {
        name: s.name,
        data: values.map((v, j) => bucketItem(v, data.trend[j].bucket, bw, extra)),
        color: paletteColor(i),
      }
    })
  }

  return {
    grid: gridFor(n),
    tooltip: axisTooltip(),
    legend: { ...statsLegend(), type: 'scroll' as const, right: 0 },
    dataZoom: zoomFor(n),
    xAxis: categoryAxis(labels),
    yAxis: tokenValueAxis(),
    series: series.map((s) => ({
      type: 'line',
      name: s.name,
      data: s.data,
      smooth: true,
      showSymbol: true,
      symbolSize: mode === 'total' ? 6 : 4,
      lineStyle: { width: 2, ...(s.dashed ? { type: 'dashed' } : {}) },
      itemStyle: { color: s.color },
      areaStyle: s.area ? { opacity: 0.08 } : undefined,
      emphasis: { focus: mode === 'total' ? 'none' : 'series' },
    })),
  }
}

export function areaChartOption(data: GlobalTokenStats | null, mode: AreaMode, t: Translate) {
  if (!data?.trend.length) return null
  const bw = data.bucketWidth
  const labels = data.trend.map((b) => formatBucketLabel(b.bucket, bw))
  const n = labels.length
  const stacked = (name: string, color: string, values: number[], opacity = 0.55, drill?: DrillTarget) => ({
    type: 'line',
    stack: 'a',
    smooth: true,
    // A lone bucket has no segment to draw; show the point instead of an empty plot.
    showSymbol: n <= 1,
    areaStyle: { opacity },
    name,
    itemStyle: { color },
    data: values.map((v, i) => bucketItem(v, data.trend[i].bucket, bw, drill)),
  })

  let series
  if (mode === 'source') {
    const parts: Array<[string, (b: TokenStatsBucket) => number]> = [
      ['workflow', (b) => b.workflowTotal || 0],
      ['pm', (b) => b.pmTotal || 0],
      ['studio', (b) => b.studioTotal || 0],
    ]
    series = parts
      .filter(([, pick]) => data.trend.some((b) => pick(b) > 0))
      .map(([key, pick]) =>
        stacked(sourceLabel(t, key), TOKEN_LEDGER_SOURCE_COLORS[key], data.trend.map(pick), 0.55, { dim: 'source', key, name: sourceLabel(t, key) }),
      )
  } else if (mode === 'status') {
    series = [
      stacked(t('pages.tokenAnalytics.statuses.ok'), '#10b981', data.trend.map((b) => Math.max(0, (b.total || 0) - (b.failedTotal || 0))), 0.5, { dim: 'status', key: 'ok', name: t('pages.tokenAnalytics.statuses.ok') }),
      stacked(t('pages.tokenAnalytics.statuses.failedOrCancelled'), '#ef4444', data.trend.map((b) => b.failedTotal || 0), 0.6),
    ]
  } else {
    series = TOKEN_PART_KEYS.map((key) =>
      stacked(partLabel(t, key), TOKEN_PART_COLORS[key], data.trend.map((b) => bucketPart(b, key)), 0.7),
    )
  }
  return {
    grid: gridFor(n),
    tooltip: axisTooltip(),
    legend: statsLegend(),
    dataZoom: zoomFor(n),
    xAxis: categoryAxis(labels),
    yAxis: tokenValueAxis(),
    series,
  }
}

export interface StackedBarRow {
  name: string
  input: number
  output: number
  cacheRead: number
  cacheWrite: number
  drill?: DrillTarget
  other: boolean
}

export function stackedBarOption(rows: StackedBarRow[], t: Translate) {
  if (rows.length < 2) return null
  return {
    grid: STATS_CHART_GRID,
    tooltip: axisTooltip({ axisPointer: { type: 'shadow' } }),
    legend: { ...statsLegend(), data: TOKEN_PART_KEYS.map((k) => partLabel(t, k)) },
    xAxis: {
      type: 'category',
      data: rows.map((row) => row.name),
      ...statsAxis(),
      splitLine: { show: false },
      axisLabel: { ...statsAxis().axisLabel, interval: 0, rotate: rows.length > 6 ? 30 : 0, width: 120, overflow: 'truncate' },
    },
    yAxis: tokenValueAxis(),
    series: TOKEN_PART_KEYS.map((key) => ({
      type: 'bar' as const,
      stack: 'total',
      name: partLabel(t, key),
      barMaxWidth: BAR_MAX_WIDTH,
      itemStyle: { color: TOKEN_PART_COLORS[key] },
      data: rows.map((row) => {
        const topKey = [...TOKEN_PART_KEYS].reverse().find((k) => row[k] > 0)
        return {
          value: row[key],
          drill: row.drill ? [row.drill] : undefined,
          other: row.other,
          itemStyle: {
            color: TOKEN_PART_COLORS[key],
            borderRadius: key === topKey ? [BAR_TOP_RADIUS, BAR_TOP_RADIUS, 0, 0] : 0,
          },
        }
      }),
    })),
  }
}

export interface CostBarRow {
  name: string
  cost: number
  total: number
  drill?: DrillTarget
}

/** Horizontal spend ranking; null when nothing is priced. */
export function costBarOption(rows: CostBarRow[], currency: string | undefined, t: Translate) {
  const priced = rows.filter((r) => r.cost > 0).slice(0, 10)
  if (!priced.length) return null
  const ordered = [...priced].reverse()
  return {
    grid: { left: 8, right: 64, top: 8, bottom: 8, containLabel: true },
    tooltip: statsTooltip({
      trigger: 'item',
      formatter: (p: { name: string; data: { value: number; total: number } }) =>
        `${p.name}<br/>${t('pages.tokenAnalytics.kpiCost')}: ${fmtCost(p.data.value, currency)}<br/>${t('pages.tokenAnalytics.tables.colTotal')}: ${fmtCompactTokenCount(p.data.total)}`,
    }),
    xAxis: { type: 'value', ...statsAxis(), axisLabel: { ...statsAxis().axisLabel, formatter: (v: number) => fmtCost(v, currency) } },
    yAxis: {
      type: 'category',
      data: ordered.map((r) => r.name),
      ...statsAxis(),
      splitLine: { show: false },
      axisLabel: { ...statsAxis().axisLabel, width: 140, overflow: 'truncate' },
    },
    series: [
      {
        type: 'bar',
        barMaxWidth: 16,
        data: ordered.map((r) => ({
          value: Number(r.cost.toFixed(6)),
          total: r.total,
          drill: r.drill ? [r.drill] : undefined,
          itemStyle: { color: '#f59e0b', borderRadius: [0, BAR_TOP_RADIUS, BAR_TOP_RADIUS, 0] },
        })),
        label: {
          show: true,
          position: 'right',
          fontSize: 10,
          color: chartTone().pieLabel,
          formatter: (p: { value: number }) => fmtCost(p.value, currency),
        },
      },
    ],
  }
}

export type DrillPieSlice = PieSlice & { drill?: DrillTarget }

/** Pie with drill paths attached to each slice (pieChartOption only forwards name/value/key). */
export function drillPieOption(slices: DrillPieSlice[], donut = false) {
  const opt = pieChartOption(slices, donut)
  if (!opt) return null
  const data = slices.map((s) => ({
    name: s.name,
    value: s.value,
    key: s.key,
    drill: s.drill ? [s.drill] : undefined,
    itemStyle: s.color ? { color: s.color } : undefined,
  }))
  return { ...opt, series: [{ ...opt.series[0], data }] }
}

export function modelProjectHeatmapOption(data: GlobalTokenStats | null) {
  const hm = data?.heatmap
  if (!data || !hm?.rows.length || !hm.cols.length) return null
  const modelKeys = new Map(data.modelRanking.map((m) => [m.name, m.modelKey || '']))
  const projectKeys = new Map(data.projects.map((p) => [p.name, p.projectId]))
  const flat: Array<{ value: [number, number, number]; drill?: DrillTarget[] }> = []
  hm.grid.forEach((row, ri) => {
    row.forEach((v, ci) => {
      const mk = modelKeys.get(hm.rows[ri])
      const pk = projectKeys.get(hm.cols[ci])
      const drill: DrillTarget[] = []
      if (pk) drill.push({ dim: 'project', key: pk, name: hm.cols[ci] })
      if (mk) drill.push({ dim: 'model', key: mk, name: hm.rows[ri] })
      flat.push({ value: [ci, ri, v], drill: drill.length ? drill : undefined })
    })
  })
  const max = Math.max(1, ...flat.map((f) => f.value[2]))
  const tone = chartTone()
  return {
    grid: { left: 8, right: 12, top: 12, bottom: 28, containLabel: true },
    tooltip: statsTooltip({
      position: 'top',
      formatter: (p: { data: { value: [number, number, number] } }) => {
        const [x, y, v] = p.data.value
        return `${hm.rows[y]} × ${hm.cols[x]}<br/>${fmtCompactTokenCount(v)}<br/><span style="opacity:0.75;font-size:11px">${fmtTokenCount(v)}</span>`
      },
    }),
    xAxis: {
      type: 'category',
      data: hm.cols,
      splitArea: { show: true },
      axisLabel: { fontSize: 10, color: tone.axisLabel, width: 96, overflow: 'truncate', interval: 0 },
    },
    yAxis: {
      type: 'category',
      data: hm.rows,
      splitArea: { show: true },
      axisLabel: { fontSize: 10, color: tone.axisLabel, width: 160, overflow: 'truncate' },
    },
    visualMap: { min: 0, max, show: false, inRange: { color: [tone.heatLow, tone.heatHigh] } },
    series: [
      {
        type: 'heatmap',
        data: flat,
        label: {
          show: true,
          color: tone.pieLabel,
          fontSize: 10,
          formatter: (p: { data: { value: [number, number, number] } }) => (p.data.value[2] ? fmtCompactTokenCount(p.data.value[2]) : ''),
        },
        emphasis: { itemStyle: { borderColor: '#5b4dff', borderWidth: 1 } },
      },
    ],
  }
}

export function weekHourHeatmapOption(weekHour: number[][] | undefined, t: Translate) {
  if (!weekHour?.length || !weekHour.some((row) => row.some((v) => v > 0))) return null
  const days = [0, 1, 2, 3, 4, 5, 6].map((d) => t(`pages.tokenAnalytics.weekdays.${d}`))
  const hours = Array.from({ length: 24 }, (_, h) => String(h).padStart(2, '0'))
  const flat: [number, number, number][] = []
  weekHour.forEach((row, d) => row.forEach((v, h) => flat.push([h, d, v])))
  const max = Math.max(1, ...flat.map((f) => f[2]))
  const tone = chartTone()
  return {
    grid: { left: 8, right: 12, top: 8, bottom: 24, containLabel: true },
    tooltip: statsTooltip({
      position: 'top',
      formatter: (p: { data: [number, number, number] }) =>
        `${days[p.data[1]]} ${hours[p.data[0]]}:00<br/>${fmtCompactTokenCount(p.data[2])}`,
    }),
    xAxis: { type: 'category', data: hours, splitArea: { show: true }, axisLabel: { fontSize: 10, color: tone.axisLabel, interval: 1 } },
    yAxis: { type: 'category', data: days, inverse: true, splitArea: { show: true }, axisLabel: { fontSize: 10, color: tone.axisLabel } },
    visualMap: { min: 0, max, show: false, inRange: { color: [tone.heatLow, tone.heatHigh] } },
    series: [{ type: 'heatmap', data: flat, itemStyle: { borderRadius: 2, borderWidth: 1, borderColor: 'transparent' } }],
  }
}

function treeDrill(node: GlobalTokenStatsTreeNode, parents: DrillTarget[], t: Translate): DrillTarget[] {
  const step: DrillTarget | null =
    node.kind === 'project'
      ? node.key ? { dim: 'project', key: node.key, name: node.name } : null
      : node.kind === 'workflow'
        ? node.key && !node.key.startsWith('_') ? { dim: 'workflow', key: node.key, name: node.name } : null
        : node.kind === 'pm' || node.kind === 'studio'
          ? { dim: 'source', key: node.kind, name: sourceLabel(t, node.kind) }
          : node.kind === 'nodeType' && node.key !== 'unknown'
            ? { dim: 'nodeType', key: node.key, name: node.name }
            : null
  return step ? [...parents, step] : parents
}

export function treemapOption(tree: GlobalTokenStatsTreeNode[] | undefined, currency: string | undefined, t: Translate) {
  if (!tree?.length) return null
  const convert = (node: GlobalTokenStatsTreeNode, parents: DrillTarget[], depth: number, idx: number): Record<string, unknown> => {
    const drill = treeDrill(node, parents, t)
    const name = node.kind === 'pm' || node.kind === 'studio' ? sourceLabel(t, node.kind) : node.name
    return {
      name,
      value: node.value,
      cost: node.cost || 0,
      drill: drill.length ? drill : undefined,
      itemStyle: depth === 0 ? { color: paletteColor(idx) } : undefined,
      children: node.children?.map((c, i) => convert(c, drill, depth + 1, i)),
    }
  }
  const tone = chartTone()
  return {
    tooltip: statsTooltip({
      formatter: (p: { name: string; value: number; data: { cost?: number }; treePathInfo?: Array<{ name: string }> }) => {
        const path = (p.treePathInfo || []).slice(1).map((x) => x.name).join(' / ') || p.name
        const cost = p.data?.cost ? ` · ${fmtCost(p.data.cost, currency)}` : ''
        return `${path}<br/>${fmtCompactTokenCount(p.value)}${cost}`
      },
    }),
    series: [
      {
        type: 'treemap',
        roam: false,
        nodeClick: false,
        width: '100%',
        height: '86%',
        top: 0,
        breadcrumb: { show: false },
        leafDepth: 2,
        label: { show: true, fontSize: 11, formatter: (p: { name: string; value: number }) => `${p.name}\n${fmtCompactTokenCount(p.value)}` },
        upperLabel: { show: true, height: 20, fontSize: 11, color: '#fff' },
        itemStyle: { borderColor: tone.tooltipBg, borderWidth: 2, gapWidth: 2 },
        levels: [
          { itemStyle: { borderWidth: 0, gapWidth: 3 } },
          { colorSaturation: [0.35, 0.6], itemStyle: { gapWidth: 2, borderColorSaturation: 0.6 } },
          { colorSaturation: [0.3, 0.5], itemStyle: { gapWidth: 1, borderColorSaturation: 0.5 } },
        ],
        data: tree.map((n, i) => convert(n, [], 0, i)),
      },
    ],
  }
}

/** Extract the drill path ECharts click params carry (set via `data.drill`). */
export function drillFromChartEvent(params: unknown): DrillTarget[] | null {
  const ev = params as { componentType?: string; data?: { drill?: DrillTarget[]; other?: boolean } }
  if (ev?.componentType !== 'series' || ev.data?.other) return null
  const path = ev.data?.drill
  return path && path.length ? path : null
}
