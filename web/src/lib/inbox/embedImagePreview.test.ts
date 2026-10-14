// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { EMBED_IMAGE_MESSAGE } from './embedChat'
import { isKnownMissing, markMissing, resetBlobMissingCacheForTests } from '@/lib/shared/blobMissingCache'
import { createEmbedImagePreview, embedImageChannelName, parseEmbedImageFrameMessage } from './embedImagePreview'

const channels: BroadcastChannel[] = []

function frameChannel(runId = 'run-1', nodeId = 'ap1') {
  const ch = new BroadcastChannel(embedImageChannelName(runId, nodeId))
  channels.push(ch)
  return ch
}

beforeEach(() => {
  resetBlobMissingCacheForTests()
})

afterEach(() => {
  for (const ch of channels.splice(0)) ch.close()
})

describe('embed image preview channel', () => {
  it('tells the host to open, replace, and close without the picture address', async () => {
    const post = vi.fn()
    const api = createEmbedImagePreview({ runId: 'run-1', nodeId: 'ap1', post })
    const frame = frameChannel()
    const seen: unknown[] = []
    frame.onmessage = (ev) => seen.push(ev.data)

    const secret = 'https://grasp.example/public/gate-approvals/images/0?token=s3cret'
    api.open(secret, 'image.png')
    api.open('data:image/png;base64,BBB', 'other.png')
    expect(post.mock.calls.map((c) => c[0])).toEqual([
      { type: EMBED_IMAGE_MESSAGE, action: 'open', label: 'image.png' },
      { type: EMBED_IMAGE_MESSAGE, action: 'open', label: 'other.png' },
    ])
    expect(JSON.stringify(post.mock.calls)).not.toContain('s3cret')
    expect(JSON.stringify(post.mock.calls)).not.toContain('base64')
    expect(seen.filter((m) => (m as { kind?: string }).kind === 'show')).toEqual([])

    frame.postMessage({ kind: 'ready', runId: 'run-1', nodeId: 'ap1' })
    await vi.waitFor(() => expect(seen.filter((m) => (m as { kind?: string }).kind === 'show')).toHaveLength(1))
    expect(seen.filter((m) => (m as { kind?: string }).kind === 'show')).toEqual([
      {
        kind: 'show',
        runId: 'run-1',
        nodeId: 'ap1',
        src: 'data:image/png;base64,BBB',
        label: 'other.png',
        missing: false,
      },
    ])

    api.close()
    expect(post).toHaveBeenCalledTimes(3)
    expect(post.mock.calls[2][0]).toEqual({ type: EMBED_IMAGE_MESSAGE, action: 'close' })
    expect(JSON.stringify(post.mock.calls[2])).not.toContain('base64')
    api.dispose()
  })

  it('marks a known-missing blob so the picture frame does not request it', async () => {
    markMissing('blob-1')
    const post = vi.fn()
    const api = createEmbedImagePreview({ runId: 'run-1', nodeId: 'ap1', post })
    const frame = frameChannel()
    const seen: unknown[] = []
    frame.onmessage = (ev) => seen.push(ev.data)
    api.open('/api/blobs/blob-1', 'gone.png')
    frame.postMessage({ kind: 'ready', runId: 'run-1', nodeId: 'ap1' })
    await vi.waitFor(() => expect(seen.some((m) => (m as { kind?: string }).kind === 'show')).toBe(true))
    const show = seen.find((m) => (m as { kind?: string }).kind === 'show') as { missing: boolean }
    expect(show.missing).toBe(true)
    expect(post.mock.calls[0][0]).toEqual({ type: EMBED_IMAGE_MESSAGE, action: 'open', label: 'gone.png' })
    api.dispose()
  })

  it('learns a failed load from the picture frame', async () => {
    const api = createEmbedImagePreview({ runId: 'run-1', nodeId: 'ap1', post: vi.fn() })
    const frame = frameChannel()
    frame.postMessage({ kind: 'missing', runId: 'run-1', nodeId: 'ap1', blobId: 'blob-2' })
    await vi.waitFor(() => expect(isKnownMissing('blob-2')).toBe(true))
    api.dispose()
  })

  it('drops a picture address that is not a same-origin frame message', () => {
    expect(parseEmbedImageFrameMessage({ kind: 'show', runId: 'run-1', nodeId: 'ap1' })).toBeNull()
    expect(parseEmbedImageFrameMessage({ kind: 'ready', runId: 'run-1', nodeId: 'ap1' })).toEqual({
      kind: 'ready',
      runId: 'run-1',
      nodeId: 'ap1',
    })
  })
})
