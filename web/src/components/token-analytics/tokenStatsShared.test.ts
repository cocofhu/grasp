// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { formatBucketLabel } from './tokenStatsShared'

describe('formatBucketLabel', () => {
  it('formats day buckets as MM-DD and week buckets as Wxx', () => {
    expect(formatBucketLabel('2026-07-11', 'day')).toBe('07-11')
    expect(formatBucketLabel('2026-W30', 'week')).toBe('W30')
  })

  it('formats hour buckets as HH:00 distinct from day/week (g2.4)', () => {
    expect(formatBucketLabel('2026-07-24T14', 'hour')).toBe('14:00')
    expect(formatBucketLabel('2026-07-25T00', 'hour')).toBe('00:00')
    expect(formatBucketLabel('2026-07-24T14', 'hour')).not.toBe('07-24')
    expect(formatBucketLabel('2026-07-24T14', 'day')).toBe('2026-07-24T14')
  })
})
