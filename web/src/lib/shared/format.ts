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
