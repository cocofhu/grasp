import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  HOME_COMPOSER_DRAFT_KEY,
  clearHomeComposerDraft,
  loadHomeComposerDraft,
  saveHomeComposerDraft,
} from './homeComposerDraft'
import {
  __resetDraftIdbForTests,
  __setDraftIdbBackendForTests,
  createMemoryDraftIdb,
  type DraftIdbBackend,
} from './draftIdb'

describe('homeComposerDraft', () => {
  let store: Record<string, string>
  let backend: DraftIdbBackend

  beforeEach(() => {
    store = {}
    backend = createMemoryDraftIdb()
    __setDraftIdbBackendForTests(backend)
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store[k] ?? null,
      setItem: (k: string, v: string) => {
        store[k] = v
      },
      removeItem: (k: string) => {
        delete store[k]
      },
      get length() {
        return Object.keys(store).length
      },
      key: (i: number) => Object.keys(store)[i] ?? null,
    })
  })

  afterEach(() => {
    __resetDraftIdbForTests()
    vi.unstubAllGlobals()
  })

  it('saves and loads a full draft payload via IDB (plan g1.1 / g1.2)', async () => {
    const result = await saveHomeComposerDraft(
      '做登录页',
      [{ data: btoa('abc'), mimeType: 'image/png', name: 'shot.png' }],
      'wf-ap',
    )
    expect(result).toBe('ok')
    const draft = await loadHomeComposerDraft()
    expect(draft).toMatchObject({
      schemaVersion: '1',
      pipelineId: 'wf-ap',
      text: '做登录页',
      attachments: [{ data: btoa('abc'), mimeType: 'image/png', name: 'shot.png' }],
    })
    expect(typeof draft?.savedAt).toBe('number')
    expect(store[HOME_COMPOSER_DRAFT_KEY]).toBeUndefined()
  })

  it('clears instead of writing an empty shell (plan g1.2 / F5)', async () => {
    await saveHomeComposerDraft('x', [], 'wf-ap')
    expect(await loadHomeComposerDraft()).not.toBeNull()
    const result = await saveHomeComposerDraft('   ', [], 'wf-ap')
    expect(result).toBe('ok')
    expect(await loadHomeComposerDraft()).toBeNull()
  })

  it('keeps draft when only attachments remain', async () => {
    await saveHomeComposerDraft('', [{ data: btoa('x'), mimeType: 'image/png' }], 'wf-ap')
    expect((await loadHomeComposerDraft())?.attachments).toHaveLength(1)
  })

  it('returns null when IDB empty and text fallback corrupt', async () => {
    store[HOME_COMPOSER_DRAFT_KEY] = '{not json'
    expect(await loadHomeComposerDraft()).toBeNull()
  })

  it('returns null for missing required fields in text fallback', async () => {
    store[HOME_COMPOSER_DRAFT_KEY] = JSON.stringify({ text: 'ok' })
    expect(await loadHomeComposerDraft()).toBeNull()
  })

  it('clearHomeComposerDraft removes the record', async () => {
    await saveHomeComposerDraft('x', [], 'wf-ap')
    await clearHomeComposerDraft()
    expect(await loadHomeComposerDraft()).toBeNull()
  })

  it('falls back to text-only when IDB put throws QuotaExceededError (plan g1.2)', async () => {
    const failing: DraftIdbBackend = {
      ...backend,
      putHome: async () => {
        const err = new Error('quota') as Error & { name: string; code: number }
        err.name = 'QuotaExceededError'
        err.code = 22
        throw err
      },
    }
    __setDraftIdbBackendForTests(failing)
    const result = await saveHomeComposerDraft(
      'keep text',
      [{ data: btoa('huge'), mimeType: 'image/png' }],
      'wf-ap',
    )
    expect(result).toBe('quota_exceeded')
    expect(store[HOME_COMPOSER_DRAFT_KEY]).toBeTruthy()
    const parsed = JSON.parse(store[HOME_COMPOSER_DRAFT_KEY])
    expect(parsed.text).toBe('keep text')
    expect(parsed.attachments).toEqual([])
    expect(parsed.pipelineId).toBe('wf-ap')
  })

  it('returns partial when IDB unavailable but text fallback works', async () => {
    const failing: DraftIdbBackend = {
      ...backend,
      putHome: async () => {
        throw new Error('idb down')
      },
    }
    __setDraftIdbBackendForTests(failing)
    const result = await saveHomeComposerDraft(
      'text only path',
      [{ data: btoa('pic'), mimeType: 'image/png' }],
      'wf-ap',
    )
    expect(result).toBe('partial')
  })

  it('quota put failure keeps prior IDB attachments intact (review v1)', async () => {
    await saveHomeComposerDraft(
      'old draft',
      [{ data: btoa('keep-me'), mimeType: 'image/png', name: 'old.png' }],
      'wf-ap',
    )
    const failing: DraftIdbBackend = {
      ...backend,
      putHome: async () => {
        const err = new Error('quota') as Error & { name: string; code: number }
        err.name = 'QuotaExceededError'
        err.code = 22
        throw err
      },
    }
    __setDraftIdbBackendForTests(failing)
    const result = await saveHomeComposerDraft(
      'newer text',
      [{ data: btoa('huge'), mimeType: 'image/png' }],
      'wf-ap',
    )
    expect(result).toBe('quota_exceeded')
    // Direct IDB read still has previous attachments (atomic put rolled back / never mutated).
    const packed = await backend.getHome()
    expect(packed?.record.text).toBe('old draft')
    expect(packed?.attachments).toHaveLength(1)
    expect(await blobLikeToB64(packed!.attachments[0]!.data)).toBe(btoa('keep-me'))
  })

  it('load prefers newer LS text-fallback over stale IDB draft (review v2 / F4)', async () => {
    await saveHomeComposerDraft(
      'stale idb',
      [{ data: btoa('img'), mimeType: 'image/png' }],
      'wf-old',
    )
    const packed = await backend.getHome()
    expect(packed).not.toBeNull()
    // Simulate quota fallback writing newer text to LS while IDB keeps old complete draft.
    store[HOME_COMPOSER_DRAFT_KEY] = JSON.stringify({
      schemaVersion: '1',
      savedAt: (packed!.record.savedAt || 0) + 1000,
      pipelineId: 'wf-new',
      text: 'fresh after quota',
      attachments: [],
    })
    const draft = await loadHomeComposerDraft()
    expect(draft?.text).toBe('fresh after quota')
    expect(draft?.pipelineId).toBe('wf-new')
    expect(draft?.attachments).toEqual([])
  })

  it('saves attachment within 50MiB gate without quota toast path (plan g3.1)', async () => {
    // ~64KiB payload — well under SITE_ATTACH_MAX_BYTES; proves IDB accepts binary attachments
    const data = btoa('x'.repeat(64 * 1024))
    const result = await saveHomeComposerDraft('big-enough', [{ data, mimeType: 'image/png' }], 'wf-ap')
    expect(result).toBe('ok')
    const draft = await loadHomeComposerDraft()
    expect(draft?.attachments[0]?.data).toBe(data)
  })
})

async function blobLikeToB64(blob: Blob): Promise<string> {
  const buf = await blob.arrayBuffer()
  const bytes = new Uint8Array(buf)
  let binary = ''
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]!)
  return btoa(binary)
}
