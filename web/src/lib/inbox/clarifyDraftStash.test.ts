// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearClarifyStash, mergeClarifyText, readClarifyStash, stashClarifyDraft } from './clarifyDraftStash'

afterEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('clarifyDraftStash', () => {
  it('adds to an existing stash without overwriting it or duplicating', () => {
    const img = { data: 'aGk=', mimeType: 'image/png' }
    stashClarifyDraft('r', 'n', { text: '第一条', images: [img], annotations: [] })
    stashClarifyDraft('r', 'n', { text: '第二条', images: [img], annotations: [] })
    stashClarifyDraft('r', 'n', { text: '第一条', images: [], annotations: [] })
    expect(readClarifyStash('r', 'n')).toEqual({ text: '第一条\n第二条', images: [img], annotations: [] })
    expect(readClarifyStash('r', 'other')).toBeNull()
    clearClarifyStash('r', 'n')
    expect(readClarifyStash('r', 'n')).toBeNull()
  })

  it('ignores empty payloads, bad JSON and missing ids', () => {
    stashClarifyDraft('r', 'n', { text: '  ', images: [], annotations: [] })
    expect(readClarifyStash('r', 'n')).toBeNull()
    localStorage.setItem('grasp-clarify-stash:r:n', '{oops')
    expect(readClarifyStash('r', 'n')).toBeNull()
    stashClarifyDraft('', 'n', { text: 'x', images: [], annotations: [] })
    expect(readClarifyStash('', 'n')).toBeNull()
  })

  it('keeps the text when images exceed the storage quota', () => {
    const store = new Map<string, string>()
    let calls = 0
    vi.spyOn(localStorage, 'getItem').mockImplementation((k: string) => store.get(k) ?? null)
    vi.spyOn(localStorage, 'setItem').mockImplementation((k: string, v: string) => {
      calls += 1
      if (calls === 1) throw new Error('QuotaExceededError')
      store.set(k, v)
    })
    stashClarifyDraft('r', 'n', { text: '文字', images: [{ data: 'big', mimeType: 'image/png' }], annotations: [] })
    expect(readClarifyStash('r', 'n')).toEqual({ text: '文字', images: [], annotations: [] })
  })

  it('merges text first-in-first', () => {
    expect(mergeClarifyText('', 'a')).toBe('a')
    expect(mergeClarifyText('a', '')).toBe('a')
    expect(mergeClarifyText('a\nb', 'b')).toBe('a\nb')
    expect(mergeClarifyText('b', 'a b')).toBe('a b')
    expect(mergeClarifyText('a', 'c')).toBe('a\nc')
  })
})
