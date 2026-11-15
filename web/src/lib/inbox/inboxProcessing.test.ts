import { describe, expect, it, vi } from 'vitest'
import { createInboxContextFlight, createInboxProcessingState } from './inboxProcessing'
import type { InboxItem } from '@/lib/shared/types'

const item = (runId: string, nodeId = 'n', iteration = 1) =>
  ({ runId, nodeId, iteration, type: 'clarify' }) as unknown as InboxItem

describe('createInboxProcessingState', () => {
  it('tracks intents and processed triples per item', () => {
    const s = createInboxProcessingState()
    const a = item('a')
    expect(s.processingLock.value).toBe(false)
    s.addProcessingIntent('a:n:1')
    expect(s.processingLock.value).toBe(true)
    expect(s.isProcessingIntent(a)).toBe(true)
    expect(s.isItemCardDisabled(a)).toBe(true)
    s.removeProcessingIntent('a:n:1')
    s.removeProcessingIntent('a:n:1')
    expect(s.processingLock.value).toBe(false)
    expect(s.isItemCardDisabled(a)).toBe(false)
    s.markProcessed(a)
    expect(s.isProcessedTriple(a)).toBe(true)
    expect(s.isItemCardDisabled(a)).toBe(true)
    s.unmarkProcessed(a)
    expect(s.isProcessedTriple(a)).toBe(false)
    expect(s.isProcessedTriple(null)).toBe(false)
    expect(s.isProcessingIntent(undefined)).toBe(false)
  })

  it('unmarks a processed triple only after it was seen absent, then reappears', () => {
    const s = createInboxProcessingState()
    const a = item('a')
    s.markProcessed(a)
    s.reconcileProcessedWithList([a])
    expect(s.isProcessedTriple(a)).toBe(true)
    s.reconcileProcessedWithList([])
    expect(s.isProcessedTriple(a)).toBe(true)
    expect(s.confirmedAbsentTriples.size).toBe(1)
    s.reconcileProcessedWithList([a])
    expect(s.isProcessedTriple(a)).toBe(false)
    expect(s.confirmedAbsentTriples.size).toBe(0)
  })
})

describe('createInboxContextFlight', () => {
  it('keeps one fetch in flight and aborts the previous triple on switch', () => {
    const f = createInboxContextFlight(() => {})
    const first = f.acquire('a')!
    expect(first).toBeTruthy()
    expect(f.acquire('a')).toBeNull()
    const second = f.acquire('b')!
    expect(first.aborted).toBe(true)
    expect(second.aborted).toBe(false)
    expect([...f.aborts.keys()]).toEqual(['b'])
    f.release('b', second)
    expect(f.aborts.size).toBe(0)
    expect(f.acquire('b')).toBeTruthy()
    f.abortAll()
    expect(f.aborts.size).toBe(0)
  })

  it('runs one trailing refresh after the owning fetch, never for a stale signal', async () => {
    const onTrailing = vi.fn()
    const f = createInboxContextFlight(onTrailing)
    f.retainTrailingRefresh('a')
    const signal = f.acquire('a')!
    f.retainTrailingRefresh('a')
    f.retainTrailingRefresh('a')
    f.release('a', signal)
    expect(onTrailing).not.toHaveBeenCalled()
    await Promise.resolve()
    expect(onTrailing).toHaveBeenCalledTimes(1)
    expect(onTrailing).toHaveBeenCalledWith('a')

    const stale = f.acquire('a')!
    f.retainTrailingRefresh('a')
    const fresh = f.acquire('b')!
    f.release('a', stale)
    f.release('b', fresh)
    await Promise.resolve()
    expect(onTrailing).toHaveBeenCalledTimes(1)
  })

  it('drops the trailing refresh when the fetch was aborted', async () => {
    const onTrailing = vi.fn()
    const f = createInboxContextFlight(onTrailing)
    const signal = f.acquire('a')!
    f.retainTrailingRefresh('a')
    f.abort('a')
    f.release('a', signal)
    await Promise.resolve()
    expect(onTrailing).not.toHaveBeenCalled()
  })
})
