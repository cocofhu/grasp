import { describe, expect, it, beforeEach, vi } from 'vitest'
import {
  beginAutoLoad,
  beginManualRetry,
  blobMissingCacheDebug,
  isKnownMissing,
  markLoaded,
  markMissing,
  parseBlobId,
  resetBlobMissingCacheForTests,
  subscribe,
} from './blobMissingCache'

describe('blobMissingCache', () => {
  beforeEach(() => {
    resetBlobMissingCacheForTests()
  })

  describe('parseBlobId', () => {
    it('parses blob: ref and /api/blobs/:id URLs', () => {
      expect(parseBlobId('blob:e54381fb9ce8471dbe0765d99fc0239f')).toBe(
        'e54381fb9ce8471dbe0765d99fc0239f',
      )
      expect(parseBlobId('/api/blobs/5b32f70529a64bdebafade19ca497a35')).toBe(
        '5b32f70529a64bdebafade19ca497a35',
      )
      expect(parseBlobId('https://x.example/api/blobs/abc123?x=1')).toBe('abc123')
      expect(parseBlobId('data:image/png;base64,AA')).toBeNull()
      expect(parseBlobId('')).toBeNull()
    })
  })

  it('beginAutoLoad: same id auto GET permission ≤1 across callers (g1.2)', () => {
    expect(beginAutoLoad('id-a')).toBe('proceed')
    expect(beginAutoLoad('id-a')).toBe('blocked_pending')

    markMissing('id-a')
    expect(isKnownMissing('id-a')).toBe(true)
    expect(beginAutoLoad('id-a')).toBe('blocked_missing')
  })

  it('markLoaded clears missing and allows cache remount paint', () => {
    expect(beginAutoLoad('id-b')).toBe('proceed')
    markLoaded('id-b')
    expect(isKnownMissing('id-b')).toBe(false)
    expect(beginAutoLoad('id-b')).toBe('proceed')
  })

  it('manual retry clears missing then re-marks on failure (g3.1)', () => {
    markMissing('id-c')
    beginManualRetry('id-c')
    expect(isKnownMissing('id-c')).toBe(false)
    // peers not notified until markLoaded/markMissing
    markMissing('id-c')
    expect(isKnownMissing('id-c')).toBe(true)
  })

  it('subscribe notifies on missing/loaded; memory is tab-scoped API (g1.3)', () => {
    const spy = vi.fn()
    const unsub = subscribe(spy)
    markMissing('id-d')
    markLoaded('id-d')
    expect(spy.mock.calls.length).toBeGreaterThanOrEqual(2)
    unsub()
    markMissing('id-d')
    const calls = spy.mock.calls.length
    markMissing('id-e')
    expect(spy.mock.calls.length).toBe(calls)

    // reset simulates full page refresh clearing memory
    resetBlobMissingCacheForTests()
    expect(blobMissingCacheDebug().knownMissing).toEqual([])
    expect(beginAutoLoad('id-d')).toBe('proceed')
  })
})
