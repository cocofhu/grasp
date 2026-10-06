import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ClarifyImage } from '../shared/types'
import {
  __resetDraftIdbForTests,
  __setDraftIdbBackendForTests,
  createMemoryDraftIdb,
  type DraftIdbBackend,
} from './draftIdb'
import {
  clearRunDraft,
  loadRunDraft,
  mergeRunDraft,
  saveRunDraft,
} from './runDraft'

const WF = 'wf-test-001'

function seed() {
  return {
    inputs: { a: 'default-a', b: 'default-b' },
    images: { p: [] as ClarifyImage[] },
    keys: ['a', 'b', 'p'],
  }
}

describe('runDraft (IndexedDB)', () => {
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

  it('merges draft values by key over defaults (plan g1.3)', async () => {
    await saveRunDraft(WF, { a: 'draft-a', b: 'draft-b' }, {
      p: [{ data: btoa('x'), mimeType: 'image/png' }],
    })
    const { inputs, images, restored } = await mergeRunDraft(
      WF,
      seed().inputs,
      seed().images,
      seed().keys,
    )
    expect(restored).toBe(true)
    expect(inputs.a).toBe('draft-a')
    expect(inputs.b).toBe('draft-b')
    expect(images.p?.[0]).toMatchObject({ data: btoa('x'), mimeType: 'image/png' })
  })

  it('ignores extra keys in draft not present in current fields', async () => {
    await saveRunDraft(WF, { a: 'x', removed: 'gone' }, {})
    const { inputs, restored } = await mergeRunDraft(WF, seed().inputs, seed().images, ['a', 'b'])
    expect(restored).toBe(true)
    expect(inputs.a).toBe('x')
    expect(inputs).not.toHaveProperty('removed')
    expect(inputs.b).toBe('default-b')
  })

  it('uses defaults for fields missing in draft', async () => {
    await saveRunDraft(WF, { a: 'only-a' }, {})
    const { inputs, images } = await mergeRunDraft(WF, seed().inputs, seed().images, seed().keys)
    expect(inputs.b).toBe('default-b')
    expect(images.p).toEqual([])
  })

  it('silently degrades when the text fallback JSON is corrupt', async () => {
    store[`run-draft:${WF}`] = '{not json'
    const { inputs, restored } = await mergeRunDraft(WF, seed().inputs, seed().images, seed().keys)
    expect(restored).toBe(false)
    expect(inputs).toEqual(seed().inputs)
  })

  it('clears empty draft instead of writing a shell (plan F5)', async () => {
    await saveRunDraft(WF, { a: 'filled' }, {})
    const result = await saveRunDraft(WF, { a: '', b: '' }, { p: [] })
    expect(result).toBe('ok')
    expect(await loadRunDraft(WF)).toBeNull()
  })

  it('returns restored=false when no draft exists', async () => {
    const { restored } = await mergeRunDraft(WF, seed().inputs, seed().images, seed().keys)
    expect(restored).toBe(false)
  })

  it('clearRunDraft removes stored draft', async () => {
    await saveRunDraft(WF, { a: 'x' }, {})
    await clearRunDraft(WF)
    expect(await loadRunDraft(WF)).toBeNull()
  })

  it('falls back to text when IDB quota exceeded (plan g1.3)', async () => {
    const failing: DraftIdbBackend = {
      ...backend,
      putRun: async () => {
        const err = new Error('quota') as Error & { name: string; code: number }
        err.name = 'QuotaExceededError'
        err.code = 22
        throw err
      },
    }
    __setDraftIdbBackendForTests(failing)
    const result = await saveRunDraft(
      WF,
      { a: 'keep' },
      { p: [{ data: btoa('huge'), mimeType: 'image/png' }] },
    )
    expect(result).toBe('quota_exceeded')
    const raw = store[`run-draft:${WF}`]
    expect(raw).toBeTruthy()
    const parsed = JSON.parse(raw)
    expect(parsed.inputs.a).toBe('keep')
    expect(parsed.images).toEqual({})
  })

  it('quota put failure keeps prior run attachments intact (review v1)', async () => {
    await saveRunDraft(
      WF,
      { a: 'old' },
      { p: [{ data: btoa('keep-run'), mimeType: 'image/png', name: 'r.png' }] },
    )
    const failing: DraftIdbBackend = {
      ...backend,
      putRun: async () => {
        const err = new Error('quota') as Error & { name: string; code: number }
        err.name = 'QuotaExceededError'
        err.code = 22
        throw err
      },
    }
    __setDraftIdbBackendForTests(failing)
    const result = await saveRunDraft(
      WF,
      { a: 'newer' },
      { p: [{ data: btoa('huge'), mimeType: 'image/png' }] },
    )
    expect(result).toBe('quota_exceeded')
    const packed = await backend.getRun(WF)
    expect(packed?.record.inputsJson).toContain('old')
    expect(packed?.attachments).toHaveLength(1)
  })

  it('load prefers newer LS fields over stale IDB run draft (review v2 / F4)', async () => {
    await saveRunDraft(WF, { a: 'stale-idb' }, { p: [{ data: btoa('img'), mimeType: 'image/png' }] })
    const packed = await backend.getRun(WF)
    expect(packed).not.toBeNull()
    store[`run-draft:${WF}`] = JSON.stringify({
      workflowId: WF,
      savedAt: packed!.record.savedAt + 1000,
      inputs: { a: 'fresh-ls' },
      images: {},
    })
    const draft = await loadRunDraft(WF)
    expect(draft?.inputs.a).toBe('fresh-ls')
    expect(draft?.images).toEqual({})
  })
})
