// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { previewShareURL } from './appPreviewShare'
import { parseShareTokenFromHash } from './gateShareLink'

afterEach(() => vi.restoreAllMocks())

describe('preview share language', () => {
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
