import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import {
  allowsScreenshotError,
  buildScreenshotContentFingerprint,
  useTestScreenshotLoad,
} from './useTestScreenshotLoad'
import type { Artifact } from '../shared/types'

vi.mock('@/lib/api/api', () => ({
  api: {
    artifactDownloadUrl: (id: string) => `http://test/api/artifacts/${id}/download`,
  },
}))

function artifact(
  name: string,
  id: string,
  extra: Partial<Pick<Artifact, 'sizeBytes' | 'updatedAt' | 'etag'>> = {},
): Artifact {
  return {
    id,
    name,
    kind: 'image',
    nodeId: 'n1',
    runId: 'r1',
    workflowName: 'wf',
    sizeBytes: extra.sizeBytes ?? 100,
    createdAt: '2026-01-01',
    updatedAt: extra.updatedAt,
    etag: extra.etag,
    content: '',
  }
}

async function flush() {
  await nextTick()
  await new Promise((r) => setTimeout(r, 0))
  await nextTick()
}

describe('allowsScreenshotError', () => {
  it('defaults to terminal when runStatus is omitted', () => {
    expect(allowsScreenshotError(undefined)).toBe(true)
    expect(allowsScreenshotError(null)).toBe(true)
    expect(allowsScreenshotError('')).toBe(true)
  })

  it('allows error only for completed/failed/cancelled', () => {
    expect(allowsScreenshotError('completed')).toBe(true)
    expect(allowsScreenshotError('failed')).toBe(true)
    expect(allowsScreenshotError('cancelled')).toBe(true)
    expect(allowsScreenshotError('running')).toBe(false)
    expect(allowsScreenshotError('waiting_human')).toBe(false)
    expect(allowsScreenshotError('queued')).toBe(false)
  })
})

describe('buildScreenshotContentFingerprint', () => {
  it('includes artifact id|sizeBytes|updatedAt|etag', () => {
    const fp = buildScreenshotContentFingerprint(
      { artifact: 'shot.png', caption: 'c1' },
      [artifact('shot.png', 'a1', { sizeBytes: 12, updatedAt: 't1', etag: 'e1' })],
    )
    expect(fp).toBe('shot.png|a1|12|t1|e1')
  })

  it('marks missing artifacts distinctly', () => {
    expect(buildScreenshotContentFingerprint({ artifact: 'x.png' }, [])).toBe('x.png|missing')
  })
})

describe('useTestScreenshotLoad', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: true,
        blob: async () => new Blob(['png'], { type: 'image/png' }),
      })),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('keeps loading (not error) when artifact is missing in non-terminal run', async () => {
    const screenshots = ref([{ artifact: 'missing.png' }])
    const artifacts = ref<Artifact[]>([])
    const runStatus = ref<string | undefined>('running')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('loading')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('enters error when artifact is missing in terminal run (or omitted status)', async () => {
    const screenshots = ref([{ artifact: 'missing.png' }])
    const artifacts = ref<Artifact[]>([])
    const { states } = useTestScreenshotLoad(screenshots, artifacts)
    await flush()
    expect(states.value[0]?.status).toBe('error')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('keeps loading when download fails in non-terminal run', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 500, blob: async () => new Blob() })),
    )
    const screenshots = ref([{ artifact: 'shot.png' }])
    const artifacts = ref([artifact('shot.png', 'a1')])
    const runStatus = ref('waiting_human')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('loading')
  })

  it('enters error when download fails in terminal run', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 500, blob: async () => new Blob() })),
    )
    const screenshots = ref([{ artifact: 'shot.png' }])
    const artifacts = ref([artifact('shot.png', 'a1')])
    const runStatus = ref('completed')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('error')
  })

  it('promotes soft-failed download to error when run becomes terminal', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: false, status: 500, blob: async () => new Blob() })),
    )
    const screenshots = ref([{ artifact: 'shot.png' }])
    const artifacts = ref([artifact('shot.png', 'a1', { sizeBytes: 10, updatedAt: 't', etag: 'e' })])
    const runStatus = ref<string | undefined>('waiting_human')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('loading')
    expect(fetch).toHaveBeenCalledTimes(1)

    // Same fingerprint poll must not leave terminal stuck on loading (review v1).
    runStatus.value = 'completed'
    await flush()
    expect(states.value[0]?.status).toBe('error')
  })

  it('retries soft-failed download on non-terminal poll and can recover', async () => {
    let calls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        calls++
        if (calls === 1) {
          return { ok: false, status: 500, blob: async () => new Blob() }
        }
        return { ok: true, blob: async () => new Blob(['ok'], { type: 'image/png' }) }
      }),
    )
    const screenshots = ref([{ artifact: 'shot.png' }])
    const artifacts = ref([artifact('shot.png', 'a1', { sizeBytes: 10, updatedAt: 't', etag: 'e' })])
    const runStatus = ref('running')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('loading')
    expect(calls).toBe(1)

    // New array ref, same fingerprint → soft-fail retry (review v2).
    artifacts.value = [artifact('shot.png', 'a1', { sizeBytes: 10, updatedAt: 't', etag: 'e' })]
    await flush()
    expect(calls).toBe(2)
    expect(states.value[0]?.status).toBe('success')
  })

  it('does not refetch or drop success when artifacts array reference changes but fingerprint is unchanged', async () => {
    const screenshots = ref([{ artifact: 'shot.png', caption: 'c' }])
    const artifacts = ref([artifact('shot.png', 'a1', { sizeBytes: 10, updatedAt: 't', etag: 'e' })])
    const runStatus = ref('running')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('success')
    const src =
      states.value[0]?.status === 'success' ? states.value[0].src : ''
    expect(fetch).toHaveBeenCalledTimes(1)

    artifacts.value = [artifact('shot.png', 'a1', { sizeBytes: 10, updatedAt: 't', etag: 'e' })]
    await flush()
    expect(states.value[0]?.status).toBe('success')
    expect(
      states.value[0]?.status === 'success' ? states.value[0].src : '',
    ).toBe(src)
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('keeps previous success frame when artifact briefly disappears (non-terminal)', async () => {
    const screenshots = ref([{ artifact: 'shot.png' }])
    const artifacts = ref([artifact('shot.png', 'a1', { sizeBytes: 10, updatedAt: 't', etag: 'e' })])
    const runStatus = ref('running')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('success')
    const src = states.value[0]?.status === 'success' ? states.value[0].src : ''

    artifacts.value = []
    await flush()
    expect(states.value[0]?.status).toBe('success')
    expect(states.value[0]?.status === 'success' ? states.value[0].src : '').toBe(src)
  })

  it('stays loading (not error) when never-succeeded item is missing for a long time while running', async () => {
    const screenshots = ref([{ artifact: 'late.png' }])
    const artifacts = ref<Artifact[]>([])
    const runStatus = ref('running')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('loading')

    // Simulate another poll with a new empty array reference.
    artifacts.value = []
    await flush()
    expect(states.value[0]?.status).toBe('loading')

    runStatus.value = 'waiting_human'
    await flush()
    expect(states.value[0]?.status).toBe('loading')
  })

  it('promotes long-missing item to error only after run becomes terminal', async () => {
    const screenshots = ref([{ artifact: 'late.png' }])
    const artifacts = ref<Artifact[]>([])
    const runStatus = ref<string | undefined>('running')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('loading')

    runStatus.value = 'failed'
    await flush()
    expect(states.value[0]?.status).toBe('error')
  })

  it('retries soft-failed SWR download while keeping success frame until new blob ready', async () => {
    let calls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        calls++
        if (calls === 1) {
          return { ok: true, blob: async () => new Blob(['v1'], { type: 'image/png' }) }
        }
        if (calls === 2) {
          return { ok: false, status: 500, blob: async () => new Blob() }
        }
        return { ok: true, blob: async () => new Blob(['v2'], { type: 'image/png' }) }
      }),
    )

    const screenshots = ref([{ artifact: 'shot.png' }])
    const artifacts = ref([artifact('shot.png', 'a1', { sizeBytes: 10, updatedAt: 't1', etag: 'e1' })])
    const runStatus = ref('running')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('success')
    const oldSrc = states.value[0]?.status === 'success' ? states.value[0].src : ''
    expect(calls).toBe(1)

    // Identity change: first SWR download fails → keep old frame + softFailed.
    artifacts.value = [artifact('shot.png', 'a2', { sizeBytes: 20, updatedAt: 't2', etag: 'e2' })]
    await flush()
    expect(states.value[0]?.status).toBe('success')
    expect(states.value[0]?.status === 'success' ? states.value[0].src : '').toBe(oldSrc)
    expect(calls).toBe(2)

    // Same fingerprint poll must retry despite success short-circuit (review v1).
    artifacts.value = [artifact('shot.png', 'a2', { sizeBytes: 20, updatedAt: 't2', etag: 'e2' })]
    await flush()
    expect(calls).toBe(3)
    expect(states.value[0]?.status).toBe('success')
    const newSrc = states.value[0]?.status === 'success' ? states.value[0].src : ''
    expect(newSrc).not.toBe(oldSrc)
  })

  it('keeps old success frame until new blob is ready when identity changes', async () => {
    let release!: (v: { ok: boolean; blob: () => Promise<Blob> }) => void
    const gate = new Promise<{ ok: boolean; blob: () => Promise<Blob> }>((r) => {
      release = r
    })
    let calls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        calls++
        if (calls === 1) {
          return { ok: true, blob: async () => new Blob(['v1'], { type: 'image/png' }) }
        }
        return gate
      }),
    )

    const screenshots = ref([{ artifact: 'shot.png' }])
    const artifacts = ref([artifact('shot.png', 'a1', { sizeBytes: 10, updatedAt: 't1', etag: 'e1' })])
    const runStatus = ref('running')
    const { states } = useTestScreenshotLoad(screenshots, artifacts, runStatus)
    await flush()
    expect(states.value[0]?.status).toBe('success')
    const oldSrc = states.value[0]?.status === 'success' ? states.value[0].src : ''

    artifacts.value = [artifact('shot.png', 'a2', { sizeBytes: 20, updatedAt: 't2', etag: 'e2' })]
    await nextTick()
    // Still showing old frame while second fetch is in flight (no loading flash).
    expect(states.value[0]?.status).toBe('success')
    expect(states.value[0]?.status === 'success' ? states.value[0].src : '').toBe(oldSrc)

    release({ ok: true, blob: async () => new Blob(['v2'], { type: 'image/png' }) })
    await flush()
    expect(states.value[0]?.status).toBe('success')
    const newSrc = states.value[0]?.status === 'success' ? states.value[0].src : ''
    expect(newSrc).not.toBe(oldSrc)
    expect(calls).toBe(2)
  })

  it('successIndices excludes loading and error entries', async () => {
    const screenshots = ref([
      { artifact: 'ok.png' },
      { artifact: 'bad.png' },
      { artifact: 'ok2.png' },
    ])
    const artifacts = ref([artifact('ok.png', 'a1'), artifact('ok2.png', 'a2')])
    const { states, successIndices } = useTestScreenshotLoad(screenshots, artifacts)
    await flush()
    expect(states.value[1]?.status).toBe('error')
    expect(successIndices()).toEqual([0, 2])
  })
})
