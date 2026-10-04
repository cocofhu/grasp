import type { GlobalTokenStatsParams } from '@/lib/api/clients/statsClient'
import type { TokenStatsWindow } from '@/lib/shared/types'
import { clientTimezoneParams } from '@/components/board/token-stats/tokenStatsShared'

/** Categorical palette for project/model/workflow series (distinct hues, purple first). */
export const TOKEN_CHART_PALETTE = [
  '#5b4dff',
  '#22a6b3',
  '#f59e0b',
  '#ec4899',
  '#10b981',
  '#818cf8',
  '#f97316',
  '#0ea5e9',
  '#a855f7',
  '#94a3b8',
] as const

export function paletteColor(i: number): string {
  return TOKEN_CHART_PALETTE[i % TOKEN_CHART_PALETTE.length]
}

export const TOKEN_LEDGER_SOURCE_COLORS: Record<string, string> = {
  workflow: '#6d5cff',
  pm: '#8b9cf7',
  studio: '#22a6b3',
}

export const TOKEN_LEDGER_STATUS_COLORS: Record<string, string> = {
  ok: '#10b981',
  failed: '#ef4444',
  cancelled: '#f59e0b',
}

export const TOKEN_LEDGER_PHASE_COLORS: Record<string, string> = {
  production: '#5b4dff',
  interactive: '#a99cff',
  chat: '#22a6b3',
}

export type TokenSourceFilter = 'all' | 'workflow' | 'pm' | 'studio'
export type TokenStatusFilter = '' | 'ok' | 'failed' | 'cancelled'
export type TokenGranularityFilter = '' | 'hour' | 'day' | 'week'

/** Page-level filter state; also the base scope of every drill-down. */
export interface TokenStatsFilters {
  window: TokenStatsWindow | 'custom'
  from: string
  to: string
  granularity: TokenGranularityFilter
  source: TokenSourceFilter
  status: TokenStatusFilter
  projectId: string
  modelKey: string
  workflowId: string
  nodeType: string
  runId: string
}

export function defaultTokenStatsFilters(): TokenStatsFilters {
  return {
    window: 'all',
    from: '',
    to: '',
    granularity: '',
    source: 'all',
    status: '',
    projectId: '',
    modelKey: '',
    workflowId: '',
    nodeType: '',
    runId: '',
  }
}

export function filtersToParams(f: TokenStatsFilters): GlobalTokenStatsParams {
  const tz = clientTimezoneParams()
  const custom = f.window === 'custom' && !!f.from
  return {
    window: custom ? 'custom' : f.window === 'custom' ? 'all' : f.window,
    from: custom ? f.from : undefined,
    to: custom ? f.to || f.from : undefined,
    granularity: f.granularity || undefined,
    timezone: tz.timezone,
    utcOffsetMinutes: tz.utcOffsetMinutes,
    source: f.source,
    status: f.status || undefined,
    projectId: f.projectId || undefined,
    modelKey: f.modelKey || undefined,
    workflowId: f.workflowId || undefined,
    nodeType: f.nodeType || undefined,
    runId: f.runId || undefined,
  }
}

/** Query carried onto the project board so its Token stats open on the same range. */
export function boardQueryFromFilters(f: TokenStatsFilters): Record<string, string> {
  const query: Record<string, string> = { tab: 'board' }
  if (f.window === 'custom' && f.from) {
    query.window = 'custom'
    query.from = f.from
    if (f.to) query.to = f.to
  } else if (f.window) {
    query.window = f.window
  }
  if (f.granularity) query.granularity = f.granularity
  return query
}

/** Number of narrowing filters beyond the time range (for the "clear" affordance). */
export function activeFilterCount(f: TokenStatsFilters): number {
  return [
    f.source !== 'all',
    !!f.status,
    !!f.projectId,
    !!f.modelKey,
    !!f.workflowId,
    !!f.nodeType,
    !!f.runId,
  ].filter(Boolean).length
}

export type DrillDim = 'project' | 'model' | 'workflow' | 'nodeType' | 'source' | 'status' | 'run' | 'bucket'

/** One step of a drill-down path (clicked chart element / table row). */
export interface DrillTarget {
  dim: DrillDim
  key: string
  name: string
  /** For dim=bucket: backend bucket width of `key`. */
  bucketWidth?: string
}

/** Local YYYY-MM-DD range covered by a trend bucket (day `2026-07-25`, hour `2026-07-25T14`, week `2026-W30`). */
export function bucketDateRange(bucket: string, width: string): { from: string; to: string; granularity: TokenGranularityFilter } | null {
  const day = /^(\d{4}-\d{2}-\d{2})(?:T\d{2})?$/.exec(bucket)
  if (day) return { from: day[1], to: day[1], granularity: 'hour' }
  const week = /^(\d{4})-W(\d{2})$/.exec(bucket)
  if (week && width === 'week') {
    const start = isoWeekStart(Number(week[1]), Number(week[2]))
    const end = new Date(start)
    end.setUTCDate(start.getUTCDate() + 6)
    return { from: ymd(start), to: ymd(end), granularity: 'day' }
  }
  return null
}

function isoWeekStart(year: number, week: number): Date {
  const jan4 = new Date(Date.UTC(year, 0, 4))
  const dow = (jan4.getUTCDay() + 6) % 7
  const monday = new Date(jan4)
  monday.setUTCDate(jan4.getUTCDate() - dow + (week - 1) * 7)
  return monday
}

function ymd(d: Date): string {
  return d.toISOString().slice(0, 10)
}

/** Narrow `base` by every step of `path`; later steps override earlier ones on the same dimension. */
export function applyDrill(base: TokenStatsFilters, path: DrillTarget[]): TokenStatsFilters {
  const out: TokenStatsFilters = { ...base }
  for (const step of path) {
    switch (step.dim) {
      case 'project':
        out.projectId = step.key
        break
      case 'model':
        out.modelKey = step.key
        break
      case 'workflow':
        out.workflowId = step.key
        out.source = 'workflow'
        break
      case 'nodeType':
        out.nodeType = step.key
        break
      case 'source':
        out.source = (step.key as TokenSourceFilter) || 'all'
        break
      case 'status':
        out.status = (step.key as TokenStatusFilter) || ''
        break
      case 'run':
        out.runId = step.key
        break
      case 'bucket': {
        const r = bucketDateRange(step.key, step.bucketWidth || 'day')
        if (r) {
          out.window = 'custom'
          out.from = r.from
          out.to = r.to
          out.granularity = r.granularity
        }
        break
      }
    }
  }
  return out
}

/** A drill target is only actionable when the backend can filter on its key. */
export function isDrillable(t: Partial<DrillTarget> | null | undefined): t is DrillTarget {
  if (!t?.dim || !t.key) return false
  if (t.dim === 'bucket') return !!bucketDateRange(t.key, t.bucketWidth || 'day')
  return true
}

const CURRENCY_SYMBOL: Record<string, string> = { USD: '$', CNY: '¥' }

export function currencySymbol(currency?: string): string {
  return CURRENCY_SYMBOL[(currency || 'USD').toUpperCase()] ?? ''
}

/** Compact spend label: `$12.34`, `<$0.01`, `$1.2K`. */
export function fmtCost(value: number | null | undefined, currency?: string): string {
  const sym = currencySymbol(currency)
  const v = Number(value) || 0
  if (v === 0) return `${sym}0`
  if (v < 0.01) return `<${sym}0.01`
  if (v >= 1_000_000) return `${sym}${(v / 1_000_000).toFixed(2)}M`
  if (v >= 10_000) return `${sym}${(v / 1000).toFixed(1)}K`
  return `${sym}${v.toFixed(2)}`
}

export function fmtPct(ratio: number | null | undefined, digits = 1): string {
  return `${((Number(ratio) || 0) * 100).toFixed(digits)}%`
}

export function fmtDeltaPct(d: number | null | undefined): string | null {
  if (d == null || !Number.isFinite(d)) return null
  return `${d >= 0 ? '▲' : '▼'} ${Math.abs(d).toFixed(1)}%`
}

function csvCell(v: unknown): string {
  const s = v == null ? '' : String(v)
  return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

export function toCsv(header: string[], rows: unknown[][]): string {
  return [header, ...rows].map((r) => r.map(csvCell).join(',')).join('\r\n')
}

/** Download CSV with a UTF-8 BOM so spreadsheet apps keep CJK names intact. */
export function downloadCsv(filename: string, csv: string): void {
  const blob = new Blob(['\ufeff', csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
