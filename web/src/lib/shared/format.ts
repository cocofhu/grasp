import { i18n } from './i18n'

/** Run-state trigger codes → i18n keys (not Triggers product page). */
const TRIGGER_LABEL_KEYS: Record<string, string> = {
  manual: 'common.runTrigger.manual',
  api: 'common.runTrigger.api',
  pm_mcp: 'common.runTrigger.pmMcp',
}

export function formatTrigger(raw: string): string {
  const labelKey = TRIGGER_LABEL_KEYS[raw]
  return labelKey ? i18n.global.t(labelKey) : raw
}

export function fmtTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

export function fmtDuration(sec: number): string {
  if (sec == null) return '—'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = Math.floor(sec % 60)
  const p = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${p(h)}:${p(m)}:${p(s)}` : `${p(m)}:${p(s)}`
}

/**
 * Wall-clock for one agent reply, same compact grammar as tool durations:
 * 42s, 3m 34s, 4m, 1h 3m. Under a second is 1s. Empty when the value is unusable.
 * The UI prefixes「用时」; this function does not.
 */
export function fmtTurnElapsed(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return ''
  const whole = Math.round(sec)
  const s = whole < 1 ? 1 : whole
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return s % 60 ? `${m}m ${s % 60}s` : `${m}m`
  const h = Math.floor(m / 60)
  const rem = m % 60
  return rem ? `${h}h ${rem}m` : `${h}h`
}

/** Seconds between two ISO timestamps. Null when either is missing or end is before start. */
export function wallClockSec(start?: string | null, end?: string | null): number | null {
  if (!start || !end) return null
  const a = Date.parse(start)
  const b = Date.parse(end)
  if (!Number.isFinite(a) || !Number.isFinite(b) || b < a) return null
  return (b - a) / 1000
}

/**
 * Compact duration for Run stats KPI main values (Demo-aligned).
 * 0 → "0s"; ≥1h → two decimals + "h" (3703→1.03h); else one decimal + "m" (3458→57.6m).
 */
export function fmtCompactDuration(sec: number): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  const n = Math.max(0, sec)
  if (n === 0) return '0s'
  if (n >= 3600) return `${(n / 3600).toFixed(2)}h`
  return `${(n / 60).toFixed(1)}m`
}

/**
 * Multi-run「平均总耗时」main value (F2): under 1h → mm:ss; ≥1h → x.xxh; 0 → 00:00.
 * Do not reuse fmtCompactDuration (x.xm / 0s) for this KPI.
 */
export function fmtMultiAvgDuration(sec: number): string {
  if (sec == null || !Number.isFinite(sec)) return '00:00'
  const n = Math.max(0, sec)
  if (n <= 0) return '00:00'
  if (n >= 3600) return `${(n / 3600).toFixed(2)}h`
  return fmtDuration(Math.round(n))
}

export function truncateText(s: string, maxLen: number): string {
  if (!s || s.length <= maxLen) return s
  return s.slice(0, maxLen) + '…'
}

export function relTime(iso: string): string {
  if (!iso) return ''
  const diff = (Date.now() - new Date(iso).getTime()) / 1000
  if (diff < 60) return i18n.global.t('common.relTime.justNow')
  if (diff < 3600) {
    return i18n.global.t('common.relTime.minutesAgo', { n: Math.floor(diff / 60) })
  }
  if (diff < 86400) {
    return i18n.global.t('common.relTime.hoursAgo', { n: Math.floor(diff / 3600) })
  }
  return i18n.global.t('common.relTime.daysAgo', { n: Math.floor(diff / 86400) })
}
