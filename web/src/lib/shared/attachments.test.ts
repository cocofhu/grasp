import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  SITE_ATTACH_MAX_BYTES,
  attachmentByteLength,
  attachmentDisplayName,
  filesFromClipboard,
  findOversizedAttachments,
  readFilesAsAttachments,
  formatSelectRejectMessage,
  formatSendRejectMessage,
  inferImageMimeFromUrl,
  isImageAttachment,
  isLikelyImageUrl,
} from './attachments'

describe('attachments helpers', () => {
  it('classifies image vs non-image and preserves display names', () => {
    expect(isImageAttachment({ mimeType: 'image/png', name: 'a.png' })).toBe(true)
    expect(isImageAttachment({ mimeType: 'application/pdf', name: 'doc.pdf' })).toBe(false)
    expect(attachmentDisplayName({ data: 'x', mimeType: 'application/pdf', name: '需求.pdf' })).toBe('需求.pdf')
    expect(attachmentDisplayName({ data: 'x', mimeType: 'application/pdf' }, 2)).toBe('attachment-3')
  })

  it('treats data:image / http(s) image URLs as images even with empty mimeType', () => {
    expect(isLikelyImageUrl('data:image/png;base64,AAA')).toBe(true)
    expect(isLikelyImageUrl('https://cdn.example/shot.png')).toBe(true)
    expect(isLikelyImageUrl('https://cdn.example/note.pdf')).toBe(false)
    expect(isImageAttachment({ mimeType: '', name: '', url: 'data:image/jpeg;base64,BBB' })).toBe(true)
    expect(isImageAttachment({ mimeType: '', name: '', url: 'https://x.test/a.webp' })).toBe(true)
    expect(inferImageMimeFromUrl('data:image/jpeg;base64,BBB')).toBe('image/jpeg')
    expect(inferImageMimeFromUrl('https://x.test/a.webp')).toBe('image/webp')
    expect(inferImageMimeFromUrl('https://x.test/screenshot')).toBe('image/png')
  })

  it('detects oversized base64 payloads for send-stage gate', () => {
    const overB64 = 'A'.repeat(Math.ceil(((SITE_ATTACH_MAX_BYTES + 1024) * 4) / 3))
    const over = { data: overB64, mimeType: 'application/octet-stream', name: 'big.bin' }
    expect(attachmentByteLength(over)).toBeGreaterThan(SITE_ATTACH_MAX_BYTES)
    expect(findOversizedAttachments([over])).toHaveLength(1)
    expect(formatSelectRejectMessage(['big.bin'])).toContain('50 MiB')
    expect(formatSendRejectMessage(['big.bin'])).toContain('发送已阻止')
  })
})

describe('readFilesAsAttachments / filesFromClipboard', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('size-gates, names, and reads accepted files as data URLs', () => {
    class Reader {
      result: string | null = null
      onload: (() => void) | null = null
      readAsDataURL(file: File) {
        this.result = file.name === 'raw' ? 'no-comma' : `data:${file.type || 'application/octet-stream'};base64,QQ==`
        this.onload?.()
      }
    }
    vi.stubGlobal('FileReader', Reader)
    const big = new File(['x'], 'big.bin')
    Object.defineProperty(big, 'size', { value: 11 })
    const onRead = vi.fn()
    const res = readFilesAsAttachments(
      [new File(['a'], 'a.png', { type: 'image/png' }), big, new File(['b'], '', { type: '' }), new File(['c'], 'raw')],
      { maxBytes: 10, onRead },
    )
    expect(res).toEqual({ rejected: ['big.bin'], accepted: 3 })
    expect(onRead.mock.calls.map((c) => c[0])).toEqual([
      { data: 'QQ==', mimeType: 'image/png', url: 'data:image/png;base64,QQ==', name: 'a.png' },
      { data: 'QQ==', mimeType: 'application/octet-stream', url: 'data:application/octet-stream;base64,QQ==', name: 'attachment-3' },
      { data: 'no-comma', mimeType: 'application/octet-stream', url: 'no-comma', name: 'raw' },
    ])
  })

  it('defaults to the site limit', () => {
    const huge = new File(['x'], 'huge.bin')
    Object.defineProperty(huge, 'size', { value: SITE_ATTACH_MAX_BYTES + 1 })
    expect(readFilesAsAttachments([huge], { onRead: vi.fn() })).toEqual({ rejected: ['huge.bin'], accepted: 0 })
  })

  it('collects only file clipboard items', () => {
    const f = new File(['a'], 'p.png')
    const ev = (items: unknown) => ({ clipboardData: items === undefined ? null : { items } }) as unknown as ClipboardEvent
    expect(filesFromClipboard(ev(undefined))).toEqual([])
    expect(
      filesFromClipboard(
        ev([
          { kind: 'string', getAsFile: () => null },
          { kind: 'file', getAsFile: () => null },
          { kind: 'file', getAsFile: () => f },
        ]),
      ),
    ).toEqual([f])
  })
})
