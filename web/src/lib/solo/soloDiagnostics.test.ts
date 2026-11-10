import { afterEach, expect, it, vi } from 'vitest'
import { soloHistoryTiming } from './soloDiagnostics'

afterEach(() => vi.restoreAllMocks())

it('keeps the original history timing when a later poll finishes before it', () => {
  const resource = (session: string, startTime: number, queueMs: number) => ({
    name: `https://example.test/api/solo/sessions/${session}/messages`,
    startTime, fetchStart: startTime, requestStart: startTime + queueMs,
    responseStart: startTime + queueMs + 20,
  })
  vi.spyOn(performance, 'getEntriesByType').mockReturnValue([
    resource('s1', 50, 0),
    resource('other', 100, 1),
    resource('s1', 101, 4000),
    resource('s1', 2600, 0),
  ] as PerformanceEntry[])

  expect(soloHistoryTiming('s1', 100)).toEqual({ queueMs: 4000, responseMs: 20 })
  expect(soloHistoryTiming('s1', 2500)).toEqual({ queueMs: 0, responseMs: 20 })
})
