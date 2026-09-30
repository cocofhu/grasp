import { afterEach, describe, expect, it, vi } from 'vitest'
import { createEmbedLiveContext, EMBED_LIVE_CONTEXT_REQUEST, EMBED_LIVE_CONTEXT_RESULT, liveRequestId } from './embedLiveContext'

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('preview Live context handshake', () => {
  it('matches concurrent responses by nonce and ignores unrelated or stale messages', async () => {
    vi.useFakeTimers()
    const post = vi.fn()
    const bridge = createEmbedLiveContext(post)
    const first = bridge.request()
    const second = bridge.request()
    const [a, b] = post.mock.calls.map(([message]) => message)
    expect(a).toMatchObject({ type: EMBED_LIVE_CONTEXT_REQUEST, nonce: expect.stringMatching(/^[0-9a-f]{32}$/) })
    expect(a.nonce).not.toBe(b.nonce)
    expect(bridge.onResult(null)).toBe(false)
    expect(bridge.onResult('invalid')).toBe(false)
    expect(bridge.onResult({ type: 'other', nonce: a.nonce })).toBe(false)
    expect(bridge.onResult({ type: EMBED_LIVE_CONTEXT_RESULT, nonce: 'stale', ok: true, url: 'http://preview.test/old' })).toBe(true)
    expect(vi.getTimerCount()).toBe(2)
    bridge.onResult({ type: EMBED_LIVE_CONTEXT_RESULT, nonce: b.nonce, ok: true, url: 'http://preview.test/second' })
    await expect(second).resolves.toEqual({ url: 'http://preview.test/second' })
    expect(vi.getTimerCount()).toBe(1)
    bridge.onResult({ type: EMBED_LIVE_CONTEXT_RESULT, nonce: a.nonce, ok: true, url: 'https://preview.test/first?tab=design' })
    await expect(first).resolves.toEqual({ url: 'https://preview.test/first?tab=design' })
    expect(vi.getTimerCount()).toBe(0)
    expect(bridge.onResult({ type: EMBED_LIVE_CONTEXT_RESULT, nonce: a.nonce, ok: false })).toBe(true)
  })

  it.each([
    { ok: false, url: 'https://preview.test/' },
    { ok: 'true', url: 'https://preview.test/' },
    { ok: true, url: 'javascript:alert(1)' },
    { ok: true, url: 'file:///tmp/index.html' },
    { ok: true, url: 123 },
    { ok: true, url: 'https://preview.test/' + 'x'.repeat(2048) },
    { ok: true },
  ])('rejects unavailable or invalid preview responses: %j', async (payload) => {
    vi.useFakeTimers()
    const post = vi.fn()
    const bridge = createEmbedLiveContext(post)
    const result = bridge.request()
    bridge.onResult({ type: EMBED_LIVE_CONTEXT_RESULT, nonce: post.mock.calls[0][0].nonce, ...payload })
    await expect(result).rejects.toThrow('Preview Live controls are unavailable')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('times out missing acknowledgements and releases the pending request', async () => {
    vi.useFakeTimers()
    const post = vi.fn()
    const bridge = createEmbedLiveContext(post)
    const result = bridge.request()
    const rejection = expect(result).rejects.toThrow('Preview Live controls did not respond')
    await vi.advanceTimersByTimeAsync(3000)
    await rejection
    expect(vi.getTimerCount()).toBe(0)
    bridge.onResult({ type: EMBED_LIVE_CONTEXT_RESULT, nonce: post.mock.calls[0][0].nonce, ok: true, url: 'http://preview.test/' })
    bridge.dispose()
  })

  it('rejects every pending request when the drawer disconnects', async () => {
    vi.useFakeTimers()
    const bridge = createEmbedLiveContext(vi.fn())
    const first = expect(bridge.request()).rejects.toThrow('Preview disconnected')
    const second = expect(bridge.request()).rejects.toThrow('Preview disconnected')
    bridge.dispose()
    await Promise.all([first, second])
    expect(vi.getTimerCount()).toBe(0)
    bridge.dispose()
  })
})

describe('Live request IDs on direct HTTP previews', () => {
  it('uses getRandomValues without requiring randomUUID', () => {
    const getRandomValues = vi.fn((bytes: Uint8Array) => bytes.fill(15))
    vi.stubGlobal('crypto', { getRandomValues })
    expect(liveRequestId()).toBe('0f'.repeat(16))
    expect(getRandomValues).toHaveBeenCalledOnce()
  })

  it.each([undefined, {}])('still generates a valid ID without Web Crypto: %j', (crypto) => {
    vi.stubGlobal('crypto', crypto)
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    expect(liveRequestId()).toBe('80'.repeat(16))
  })
})
