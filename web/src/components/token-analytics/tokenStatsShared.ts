/** Shared Token stats chart colors aligned with approved page.html Demo. */
export const TOKEN_PART_COLORS = {
  input: '#4f46e5',
  output: '#7c6dff',
  cacheRead: '#a99cff',
  cacheWrite: '#c9c0ff',
} as const

/** Trend/rank source colors (workflow / pm share purple; PM distinguished by dashed line). */
export const TOKEN_SOURCE_COLORS = {
  workflow: '#6d5cff',
  pm: '#6d5cff',
} as const

export type TokenPartKey = keyof typeof TOKEN_PART_COLORS

export const TOKEN_PART_KEYS: TokenPartKey[] = ['input', 'output', 'cacheRead', 'cacheWrite']

export function clientTimezoneParams(): { timezone?: string; utcOffsetMinutes: number } {
  let timezone: string | undefined
  try {
    timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    timezone = undefined
  }
  const utcOffsetMinutes = -new Date().getTimezoneOffset()
  return { timezone, utcOffsetMinutes }
}

export function formatBucketLabel(bucket: string, bucketWidth: string): string {
  if (bucketWidth === 'hour') {
    // 2026-07-24T14 → 14:00 (distinct from day MM-DD / week Wxx)
    const hour = /^(\d{4})-(\d{2})-(\d{2})T(\d{2})$/.exec(bucket)
    if (hour) return `${hour[4]}:00`
    return bucket
  }
  if (bucketWidth === 'week') {
    // 2026-W30 → W30
    const m = /^(\d{4})-W(\d{2})$/.exec(bucket)
    if (m) return `W${m[2]}`
    return bucket
  }
  // 2026-07-25 → 07-25
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(bucket)
  if (m) return `${m[2]}-${m[3]}`
  return bucket
}
