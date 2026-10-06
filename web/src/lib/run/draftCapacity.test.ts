import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  SITE_ATTACH_MAX_BYTES,
  attachmentByteLength,
  findOversizedAttachments,
} from '../shared/attachments'
import type { ClarifyImage } from '../shared/types'
import {
  __resetDraftIdbForTests,
  __setDraftIdbBackendForTests,
  createMemoryDraftIdb,
  type DraftIdbBackend,
} from './draftIdb'
import { saveHomeComposerDraft } from './homeComposerDraft'

describe('draft capacity vs send gate (plan g3.1 / g3.2)', () => {
  let store: Record<string, string>

  beforeEach(() => {
    store = {}
    __setDraftIdbBackendForTests(createMemoryDraftIdb())
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

  it('IDB save accepts attachment under SITE_ATTACH_MAX_BYTES', async () => {
    // ~1MiB decoded — under 50MiB gate, would historically blow localStorage
    const raw = 'A'.repeat(1024 * 1024)
    const data = btoa(raw)
    const im: ClarifyImage = { data, mimeType: 'image/png', name: 'shot.png' }
    expect(attachmentByteLength(im)).toBeLessThan(SITE_ATTACH_MAX_BYTES)
    expect(findOversizedAttachments([im])).toEqual([])
    const result = await saveHomeComposerDraft('cap', [im], 'wf-ap')
    expect(result).toBe('ok')
  })

  it('MemoryBackend accepts near-SITE_ATTACH_MAX_BYTES Blob without materializing huge base64 (review v4 / g3.1)', async () => {
    const idb = createMemoryDraftIdb()
    __setDraftIdbBackendForTests(idb)
    // Stub Blob sized near the send gate — avoids allocating ~50MiB or slow btoa (review v4).
    const nearMax = SITE_ATTACH_MAX_BYTES - 1024
    const data = {
      size: nearMax,
      type: 'application/octet-stream',
      arrayBuffer: async () => new ArrayBuffer(0),
      slice() {
        return this
      },
    } as unknown as Blob
    await idb.putHome(
      {
        id: 'current',
        schemaVersion: '1',
        savedAt: Date.now(),
        pipelineId: 'wf-near',
        text: 'near-max',
      },
      [
        {
          id: 'home:current:0',
          ownerKind: 'home',
          ownerId: 'current',
          mimeType: 'application/octet-stream',
          data,
          sizeBytes: nearMax,
          sortIndex: 0,
        },
      ],
    )
    const got = await idb.getHome()
    expect(got?.record.text).toBe('near-max')
    expect(got?.attachments[0]?.sizeBytes).toBe(nearMax)
    expect(got?.attachments[0]?.data.size).toBe(nearMax)
    expect(nearMax).toBeLessThan(SITE_ATTACH_MAX_BYTES)
    expect(nearMax).toBeGreaterThan(SITE_ATTACH_MAX_BYTES * 0.9)
  })

  it('send gate still rejects over SITE_ATTACH_MAX_BYTES', () => {
    const overB64 = 'A'.repeat(Math.ceil(((SITE_ATTACH_MAX_BYTES + 1024) * 4) / 3))
    const over: ClarifyImage = { data: overB64, mimeType: 'image/png' }
    expect(attachmentByteLength(over)).toBeGreaterThan(SITE_ATTACH_MAX_BYTES)
    expect(findOversizedAttachments([over]).length).toBe(1)
  })

  it('repeated quota failures return quota_exceeded each time (caller dedupes toast)', async () => {
    const failing: DraftIdbBackend = {
      ...createMemoryDraftIdb(),
      putHome: async () => {
        const err = new Error('quota') as Error & { name: string; code: number }
        err.name = 'QuotaExceededError'
        err.code = 22
        throw err
      },
    }
    __setDraftIdbBackendForTests(failing)
    const r1 = await saveHomeComposerDraft('a', [{ data: btoa('x'), mimeType: 'image/png' }], 'wf')
    const r2 = await saveHomeComposerDraft('ab', [{ data: btoa('y'), mimeType: 'image/png' }], 'wf')
    expect(r1).toBe('quota_exceeded')
    expect(r2).toBe('quota_exceeded')
  })
})
