// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { previewShareURL, publicAppPreviewClient } from './appPreviewShare'
import { parseShareTokenFromHash } from './gateShareLink'

afterEach(() => vi.restoreAllMocks())

describe('preview share language', () => {
  it('stops the shared main conversation with its scoped credential', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{"ok":true}', { status: 200 }))
    const signal = new AbortController().signal
    await publicAppPreviewClient('preview-session').cancelMain('observed-turn', signal)
    expect(fetch).toHaveBeenCalledWith('/public/approvals/app/main-chat/cancel', expect.objectContaining({
      method: 'POST', signal, credentials: 'omit', body: '{"turnId":"observed-turn"}',
      headers: expect.objectContaining({ 'X-Gate-Share-Token': 'preview-session', 'X-Gate-Share-Requested': '1' }),
    }))
  })

  it('keeps the token and the sharing language when normalizing the API origin or copying again', () => {
    const token = 'a'.repeat(64)
    const first = previewShareURL(`http://api-upstream/public/approvals/app#t=${token}`, 'zh-CN')
    const parsed = new URL(first)
    expect(parsed.origin).toBe(window.location.origin)
    expect(parsed.pathname).toBe('/public/approvals/app')
    expect(parsed.hash).toBe(`#t=${token}&lang=zh-CN`)
    expect(parseShareTokenFromHash(parsed.hash)).toBe(token)
    expect(new URL(previewShareURL(first, 'en')).hash).toBe(`#t=${token}&lang=en`)
  })
})
