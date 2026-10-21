// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getAuthState } from '@/lib/composables/useAuth'
import {
  INLINE_IMAGE_BUDGET,
  STASH_TTL_MS,
  ackClarifyOutboxText,
  budgetImages,
  insertRescued,
  resetClarifyTabForTest,
  clearClarifyStash,
  dropClarifyOutbox,
  mergeClarifyText,
  newClientMessageId,
  putClarifyOutbox,
  readClarifyStash,
  stashClarifyDraft,
  sweepClarifyStash,
  takeClarifyOutbox,
} from './clarifyDraftStash'

/** Stash / outbox keys only (the tab id and heartbeat live alongside). */
function countKeys(st: Storage, prefix: string): number {
  let n = 0
  for (let i = 0; i < st.length; i++) if (st.key(i)?.startsWith(prefix)) n++
  return n
}


beforeEach(() => resetClarifyTabForTest({ checked: true }))

afterEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  getAuthState().user = null
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('clarifyDraftStash', () => {
  it('adds to an existing stash without overwriting it or duplicating', () => {
    const img = { data: 'aGk=', mimeType: 'image/png' }
    stashClarifyDraft('r', 'n', { text: '第一条', images: [img], annotations: [] })
    stashClarifyDraft('r', 'n', { text: '第二条', images: [img], annotations: [] })
    stashClarifyDraft('r', 'n', { text: '第一条', images: [], annotations: [] })
    expect(readClarifyStash('r', 'n')).toMatchObject({ text: '第一条\n第二条', images: [img], annotations: [] })
    expect(readClarifyStash('r', 'other')).toBeNull()
    clearClarifyStash('r', 'n')
    expect(readClarifyStash('r', 'n')).toBeNull()
  })

  it('ignores empty payloads, bad JSON and missing ids', () => {
    stashClarifyDraft('r', 'n', { text: '  ', images: [], annotations: [] })
    expect(readClarifyStash('r', 'n')).toBeNull()
    sessionStorage.setItem('grasp-clarify-stash:anon:r:n', '{oops')
    expect(readClarifyStash('r', 'n')).toBeNull()
    stashClarifyDraft('', 'n', { text: 'x', images: [], annotations: [] })
    expect(readClarifyStash('', 'n')).toBeNull()
  })

  it('keeps the text when images exceed the storage quota', () => {
    const store = new Map<string, string>()
    let calls = 0
    const g = vi.spyOn(sessionStorage, 'getItem').mockImplementation((k: string) => store.get(k) ?? null)
    const st = vi.spyOn(sessionStorage, 'setItem').mockImplementation((k: string, v: string) => {
      if (k.startsWith('grasp-clarify-stash:')) {
        calls += 1
        if (calls === 1) throw new Error('QuotaExceededError')
      }
      store.set(k, v)
    })
    try {
      stashClarifyDraft('r', 'n', { text: '文字', images: [{ data: 'big', mimeType: 'image/png' }], annotations: [] })
      expect(readClarifyStash('r', 'n')).toMatchObject({ text: '文字', images: [], annotations: [], imagesDropped: 1 })
    } finally {
      g.mockRestore()
      st.mockRestore()
    }
  })

  it('merges text first-in-first', () => {
    expect(mergeClarifyText('', 'a')).toBe('a')
    expect(mergeClarifyText('a', '')).toBe('a')
    expect(mergeClarifyText('a\nb', 'b')).toBe('a\nb')
    expect(mergeClarifyText('b', 'a b')).toBe('a b')
    expect(mergeClarifyText('a', 'c')).toBe('a\nc')
  })

  it('is per user: another account on the same browser never sees it', () => {
    getAuthState().user = { username: 'alice', expiresAt: '' }
    stashClarifyDraft('r', 'n', { text: 'alice 的', images: [], annotations: [] })
    getAuthState().user = { username: 'bob', expiresAt: '' }
    expect(readClarifyStash('r', 'n')).toBeNull()
    getAuthState().user = { username: 'alice', expiresAt: '' }
    expect(readClarifyStash('r', 'n')?.text).toBe('alice 的')
  })

  it('expires after 7 days and the sweep removes old stash and outbox entries', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-01T00:00:00Z'))
    stashClarifyDraft('r', 'n', { text: '旧的', images: [], annotations: [] })
    putClarifyOutbox('r', 'n', 'id-1', { text: '旧的', images: [], annotations: [] })
    vi.setSystemTime(Date.now() + STASH_TTL_MS + 1)
    expect(readClarifyStash('r', 'n')).toBeNull()
    sweepClarifyStash(Date.now(), true)
    expect(countKeys(sessionStorage, 'grasp-clarify-stash:')).toBe(0)
    expect(countKeys(sessionStorage, 'grasp-clarify-outbox:')).toBe(0)
  })

  it('caps inline image bytes and keeps ref-only images', () => {
    const big = { data: 'x'.repeat(INLINE_IMAGE_BUDGET - 10), mimeType: 'image/png', name: 'big' }
    const second = { data: 'y'.repeat(100), mimeType: 'image/png', name: 'second' }
    const ref = { ref: 'blob:1', mimeType: 'image/png', name: 'ref' }
    expect(budgetImages([big, second, ref]).map((i) => i.name)).toEqual(['big', 'ref'])
    stashClarifyDraft('r', 'n', { text: 't', images: [big, second, ref], annotations: [] })
    expect(readClarifyStash('r', 'n')!.images.map((i) => i.name)).toMatchObject(['big', 'ref'])
  })

  it('outbox: per tab (sessionStorage), take consumes, drop and ack forget', () => {
    const a = newClientMessageId()
    const b = newClientMessageId()
    expect(a).not.toBe(b)
    const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
    expect(a).toMatch(UUID)
    // Non-secure origin (no randomUUID): still a valid v4 UUID (server rejects others).
    const spy = vi.spyOn(crypto, 'randomUUID').mockImplementation(() => {
      throw new Error('insecure context')
    })
    expect(newClientMessageId()).toMatch(UUID)
    spy.mockRestore()
    putClarifyOutbox('r', 'n', a, { text: '一', images: [], annotations: [] })
    putClarifyOutbox('r', 'n', b, { text: '二', images: [], annotations: [] })
    expect(countKeys(sessionStorage, 'grasp-clarify-stash:')).toBe(0)
    expect(takeClarifyOutbox('r', 'n', 'nope')).toBeNull()
    expect(takeClarifyOutbox('r', 'n', undefined)).toBeNull()
    expect(takeClarifyOutbox('r', 'n', a)).toMatchObject({ text: '一', images: [], annotations: [] })
    expect(takeClarifyOutbox('r', 'n', a)).toBeNull()
    ackClarifyOutboxText('r', 'n', '二')
    expect(takeClarifyOutbox('r', 'n', b)).toBeNull()
    putClarifyOutbox('r', 'n', a, { text: '三', images: [], annotations: [] })
    dropClarifyOutbox('r', 'n', a)
    expect(countKeys(sessionStorage, 'grasp-clarify-outbox:')).toBe(0)
  })

  it('insertRescued keeps rescued messages in send order ahead of typing', () => {
    let r = insertRescued('打字中', [], { text: 'b', at: 2 })
    expect(r.text).toBe('b\n打字中')
    r = insertRescued(r.text, r.head, { text: 'a', at: 1 })
    expect(r.text).toBe('a\nb\n打字中')
    r = insertRescued(r.text, r.head, { text: 'a', at: 1 })
    expect(r.text).toBe('a\nb\n打字中')
    // The user rewrote the head: start a fresh head instead of reordering it.
    r = insertRescued('改过了', r.head, { text: 'c', at: 3 })
    expect(r.text).toBe('c\n改过了')
  })

  it('stash ordering by send time when refusals arrive reversed', () => {
    stashClarifyDraft('r', 'n', { text: '第二条', images: [], annotations: [] }, { at: 20 })
    stashClarifyDraft('r', 'n', { text: '第一条', images: [], annotations: [] }, { at: 10 })
    expect(readClarifyStash('r', 'n')!.text).toBe('第一条\n第二条')
  })

  it('reports inline images left out for size', () => {
    const big = { data: 'x'.repeat(INLINE_IMAGE_BUDGET + 1), mimeType: 'image/png' }
    expect(stashClarifyDraft('r', 'n', { text: 't', images: [big], annotations: [] }).imagesDropped).toBe(1)
    expect(readClarifyStash('r', 'n')!.imagesDropped).toBe(1)
    putClarifyOutbox('r', 'n', 'id', { text: 't', images: [big], annotations: [] })
    expect(takeClarifyOutbox('r', 'n', 'id')!.imagesDropped).toBe(1)
  })

})
