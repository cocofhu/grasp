import { describe, expect, it } from 'vitest'
import { formatRemaining } from './idleCountdown'

describe('sandbox idle countdown', () => {
  it('formats time remaining and distinguishes due or unknown deadlines', () => {
    expect(formatRemaining(0, 'soon')).toBe('soon')
    expect(formatRemaining(45_000, 'soon')).toBe('45s')
    expect(formatRemaining(12 * 60_000 + 5_000, 'soon')).toBe('12m 5s')
    expect(formatRemaining(65 * 60_000, 'soon')).toBe('1h 05m')
    expect(formatRemaining(65 * 60_000, 'soon', (h, m, s) => `${h}:${m}:${s}`)).toBe('1:5:0')
    expect(formatRemaining(Number.NaN, 'soon')).toBe('')
  })
})
