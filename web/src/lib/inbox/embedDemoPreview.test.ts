// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EMBED_DEMO_MESSAGE } from './embedChat'
import { createEmbedDemoPreview, embedDemoChannelName, parseEmbedDemoFrameMessage } from './embedDemoPreview'

const channels: BroadcastChannel[] = []

function frameChannel(runId = 'run-1', nodeId = 'ap1') {
  const ch = new BroadcastChannel(embedDemoChannelName(runId, nodeId))
  channels.push(ch)
  return ch
}

afterEach(() => {
  for (const ch of channels.splice(0)) ch.close()
})

describe('embed demo preview channel', () => {
  it('tells the host to open, replace, and close without the demo HTML', async () => {
    const post = vi.fn()
    const api = createEmbedDemoPreview({ runId: 'run-1', nodeId: 'ap1', post })
    const frame = frameChannel()
    const seen: unknown[] = []
    frame.onmessage = (ev) => seen.push(ev.data)

    api.open('<p>secret-demo</p>', '方案 A')
    expect(post.mock.calls[0][0]).toEqual({ type: EMBED_DEMO_MESSAGE, action: 'open', label: '方案 A' })
    expect(JSON.stringify(post.mock.calls)).not.toContain('secret-demo')
    expect(seen.filter((m) => (m as { kind?: string }).kind === 'show')).toEqual([])

    frame.postMessage({ kind: 'ready', runId: 'run-1', nodeId: 'ap1' })
    await vi.waitFor(() => expect(seen.some((m) => (m as { kind?: string }).kind === 'show')).toBe(true))
    api.open('<p>other</p>', '方案 B')
    expect(post.mock.calls.map((c) => c[0])).toEqual([
      { type: EMBED_DEMO_MESSAGE, action: 'open', label: '方案 A' },
      { type: EMBED_DEMO_MESSAGE, action: 'open', label: '方案 B' },
    ])
    await vi.waitFor(() =>
      expect(seen.filter((m) => (m as { kind?: string }).kind === 'show').at(-1)).toEqual({
        kind: 'show',
        runId: 'run-1',
        nodeId: 'ap1',
        html: '<p>other</p>',
        label: '方案 B',
      }),
    )
    api.close()
    expect(post.mock.calls.at(-1)?.[0]).toEqual({ type: EMBED_DEMO_MESSAGE, action: 'close' })
    expect(parseEmbedDemoFrameMessage({ kind: 'nope' })).toBeNull()
    api.dispose()
  })
})
