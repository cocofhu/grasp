import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  SITE_ATTACH_MAX_BYTES,
  attachmentByteLength,
  attachmentKind,
  attachmentMime,
  attachmentSizeLabel,
  attachmentDownloadUrl,
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
  it('allows attachment transports and rejects executable download URLs', () => {
    for (const src of ['/api/blobs/abc', 'blob:https://grasp.test/abc', 'data:audio/wav;base64,QQ==', 'https://grasp.test/file']) expect(attachmentDownloadUrl(src)).toBe(src)
    for (const src of ['javascript:alert(1)', 'java\nscript:alert(1)', 'file:///private/file', 'https://[', '']) expect(attachmentDownloadUrl(src)).toBe('')
  })
  it('uses media MIME types first and falls back to filename for missing/generic MIME', () => {
    expect(attachmentKind({ mimeType: 'AUDIO/MPEG; codecs=mp3', name: 'clip.mp4' })).toBe('audio')
    expect(isImageAttachment({ mimeType: 'audio/mpeg', name: 'renamed.png' })).toBe(false)
    expect(attachmentMime({ mimeType: '', name: 'clip.MP4' })).toBe('video/mp4')
    expect(attachmentKind({ mimeType: 'application/octet-stream', name: 'voice.m4a' })).toBe('audio')
    expect(attachmentKind({ mimeType: 'application/pdf', name: 'doc.pdf' })).toBe('file')
    expect(attachmentKind({ mimeType: 'image/webp', name: 'shot.webp' })).toBe('image')
    expect(attachmentSizeLabel({ mimeType: '', sizeBytes: 0 })).toBe('0 B')
    expect(attachmentSizeLabel({ mimeType: '', sizeBytes: 1024 })).toBe('1.0 KiB')
    expect(attachmentSizeLabel({ mimeType: '', sizeBytes: SITE_ATTACH_MAX_BYTES })).toBe('50.0 MiB')
    expect(attachmentSizeLabel({ mimeType: '' })).toBe('')
  })
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

  it('reports read errors, aborts and synchronous failures without accepting them as finished files', () => {
    class Reader {
      onerror: (() => void) | null = null
      onabort: (() => void) | null = null
      readAsDataURL(file: File) {
        if (file.name === 'throw') throw new Error('read failed')
        if (file.name === 'abort') this.onabort?.()
        else this.onerror?.()
      }
    }
    vi.stubGlobal('FileReader', Reader)
    const onError = vi.fn(), onRead = vi.fn(), onStart = vi.fn()
    readFilesAsAttachments(['throw', 'abort', 'error'].map(name => new File(['x'], name)), { onStart, onError, onRead })
    expect(onStart).toHaveBeenCalledTimes(3)
    expect(onError.mock.calls.map(args => args[0])).toEqual(['throw', 'abort', 'error'])
    expect(onRead).not.toHaveBeenCalled()
  })

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
      { data: 'QQ==', mimeType: 'image/png', url: 'data:image/png;base64,QQ==', name: 'a.png', sizeBytes: 1 },
      { data: 'QQ==', mimeType: 'application/octet-stream', url: 'data:application/octet-stream;base64,QQ==', name: 'attachment-3', sizeBytes: 1 },
      { data: 'no-comma', mimeType: 'application/octet-stream', url: 'no-comma', name: 'raw', sizeBytes: 1 },
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
