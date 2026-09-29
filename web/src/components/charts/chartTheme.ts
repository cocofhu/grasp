/** Shared ECharts chart chrome aligned with Token Analytics (用量统计). */
import { theme } from '@/lib/shared/theme'
import { fmtCompactTokenCount } from '@/lib/run/tokenUsage'
import { cssTokenColor } from '@/components/run/mermaidTheme'

/** Fallbacks match `--c-elevated` in global.css when the token cannot be read. */
export const RANK_TRACK_LIGHT = 'rgb(244, 244, 245)'
export const RANK_TRACK_DARK = 'rgb(28, 28, 33)'

/**
 * Unfilled rank-bar track for canvas. `rgb(var(--c-elevated))` is not a color
 * ECharts can paint, so resolve the channel token (or the theme fallback) to rgb.
 * Reads `theme` so chart option computeds rerun after a light/dark toggle.
 */
export function rankTrackColor(): string {
  const light = theme.value === 'light'
  return cssTokenColor('--c-elevated', light ? RANK_TRACK_LIGHT : RANK_TRACK_DARK)
}

export const CHART_AXIS = {
  axisLine: { show: false },
  axisTick: { show: false },
  axisLabel: { color: '#9aa1ad', fontSize: 10 },
  splitLine: { lineStyle: { color: '#eef0f3' } },
}

export const CHART_GRID = {
  left: 42,
  right: 12,
  top: 12,
  bottom: 28,
  containLabel: false,
}

/** Axis-chart plot padding used by 用量统计 (room for top-left legend). */
export const STATS_CHART_GRID = {
  left: 56,
  right: 16,
  top: 40,
  bottom: 52,
  containLabel: true,
}

/**
 * Board trend plot. Keep CHART_GRID left/right (containLabel:false) so the first
 * category sits in the left-edge hover band (canvas x≈40–96, g4.5 / 07-11 · 0).
 * top is raised for the shared top-left legend.
 */
export const TREND_CHART_GRID = {
  left: CHART_GRID.left,
  right: CHART_GRID.right,
  top: 40,
  bottom: CHART_GRID.bottom,
  containLabel: false,
}

export const ECHARTS_TOOLTIP_CLASS = 'token-stats-echarts-tooltip'

export type ChartTone = {
  axisLabel: string
  splitLine: string
  legend: string
  pieLabel: string
  heatLow: string
  heatHigh: string
  tooltipBg: string
  tooltipBorder: string
  tooltipText: string
}

/** Light/dark tones for axis, legend, pie labels, heatmap, and tooltip. */
export function chartTone(): ChartTone {
  const dark = theme.value === 'dark'
  return {
    axisLabel: dark ? '#a1a1aa' : '#71717a',
    splitLine: dark ? 'rgba(255,255,255,0.08)' : '#eef0f3',
    legend: dark ? '#a1a1aa' : '#8b8b96',
    pieLabel: dark ? '#d4d4d8' : '#52525b',
    heatLow: dark ? '#1f1f36' : '#efeaff',
    heatHigh: '#5b4dff',
    tooltipBg: dark ? '#27272a' : '#ffffff',
    tooltipBorder: dark ? '#3f3f46' : '#e4e4e7',
    tooltipText: dark ? '#e4e4e7' : '#27272a',
  }
}

export function statsAxis() {
  const tone = chartTone()
  return {
    ...CHART_AXIS,
    axisLabel: { ...CHART_AXIS.axisLabel, color: tone.axisLabel, hideOverlap: true },
    splitLine: { lineStyle: { color: tone.splitLine } },
  }
}

/** Axis-chart legend: top-left squares, matching 用量统计. */
export function statsLegend() {
  return {
    top: 0,
    left: 0,
    itemWidth: 12,
    itemHeight: 8,
    itemStyle: { borderRadius: 4 },
    textStyle: { fontSize: 11, color: chartTone().legend },
  }
}

/** Pie/donut legend: vertical, flush right. */
export function pieLegend() {
  return {
    show: true,
    orient: 'vertical' as const,
    right: 0,
    top: 'middle' as const,
    type: 'scroll' as const,
    itemWidth: 10,
    itemHeight: 8,
    itemStyle: { borderRadius: 4 },
    textStyle: { fontSize: 10, color: chartTone().legend },
  }
}

export function statsTooltip<T extends Record<string, unknown> = Record<string, never>>(extra?: T) {
  const tone = chartTone()
  return {
    borderRadius: 12,
    backgroundColor: tone.tooltipBg,
    borderColor: tone.tooltipBorder,
    textStyle: { color: tone.tooltipText, fontSize: 12 },
    confine: false,
    appendToBody: true,
    extraCssText: 'z-index: 1000; border-radius: 12px;',
    className: ECHARTS_TOOLTIP_CLASS,
    ...(extra ?? ({} as T)),
  }
}

export function axisTooltip<T extends Record<string, unknown> = Record<string, never>>(extra?: T) {
  return statsTooltip({
    trigger: 'axis',
    valueFormatter: (v: number) => fmtCompactTokenCount(v),
    ...(extra ?? ({} as T)),
  })
}

export function pieTooltipFormatter(params: unknown): string {
  const p = params as { name?: string; value?: number; percent?: number }
  const name = p.name ?? ''
  const value = fmtCompactTokenCount(p.value ?? 0)
  const pct = p.percent != null ? p.percent.toFixed(1) : '0'
  return `${name}: ${value} (${pct}%)`
}

export type PieSlice = { name: string; value: number; key?: string; color?: string }

/** Shared pie/donut option: right legend + visible name+percent labels. */
export function pieChartOption(slices: PieSlice[], donut = false) {
  if (!slices.length) return null
  const tone = chartTone()
  const colors = slices.map((s, i) => s.color || ['#4f46e5', '#7c6dff', '#a99cff', '#94a3b8'][i % 4])
  return {
    tooltip: statsTooltip({ trigger: 'item', formatter: pieTooltipFormatter }),
    legend: pieLegend(),
    series: [
      {
        type: 'pie' as const,
        radius: donut ? (['36%', '54%'] as [string, string]) : '54%',
        center: ['38%', '50%'],
        data: slices.map((s) => ({
          name: s.name,
          value: s.value,
          key: s.key,
          itemStyle: s.color ? { color: s.color } : undefined,
        })),
        itemStyle: { borderWidth: 0 },
        color: colors,
        label: {
          show: true,
          formatter: '{b} {d}%',
          fontSize: 10,
          color: tone.pieLabel,
        },
        labelLayout: { hideOverlap: true },
      },
    ],
  }
}

export function fmtCompactAxis(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1000) return `${(n / 1000).toFixed(1)}K`
  return String(n)
}
