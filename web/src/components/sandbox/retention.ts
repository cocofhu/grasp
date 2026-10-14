/** Countdown text for a sandbox deadline: "1h 05m", "12m 30s", "40s", or `soon` once due. */
export function formatRemaining(ms: number, soon: string): string {
  if (!Number.isFinite(ms)) return ''
  if (ms <= 0) return soon
  const totalSec = Math.floor(ms / 1000)
  const h = Math.floor(totalSec / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const s = totalSec % 60
  if (h > 0) return `${h}h ${String(m).padStart(2, '0')}m`
  return m > 0 ? `${m}m ${s}s` : `${s}s`
}
