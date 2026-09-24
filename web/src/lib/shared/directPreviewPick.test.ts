import { describe, expect, it } from 'vitest'
import {
  DIRECT_PREVIEW_PICKED,
  DIRECT_PREVIEW_READY,
  acceptsPreviewFrameMessage,
  iframeOrigin,
  isCurrentPreviewFrameSource,
  isDirectPreviewOrigin,
  isSameOriginPreviewPath,
  parseDirectPreviewMessage,
  resolveDirectPreviewGoto,
  resolvePreviewFrameGoto,
} from './directPreviewPick'
import { toPreviewDocumentURL } from './previewDocumentOrigin'

describe('directPreviewPick', () => {
  it('iframeOrigin parses http URL', () => {
    expect(iframeOrigin('http://10.0.0.8:18081/app')).toBe('http://10.0.0.8:18081')
    expect(iframeOrigin('not a url')).toBe('')
  })

  it('isDirectPreviewOrigin requires exact origin match', () => {
    const url = 'http://127.0.0.1:18081/'
    expect(isDirectPreviewOrigin(url, 'http://127.0.0.1:18081')).toBe(true)
    expect(isDirectPreviewOrigin(url, 'http://127.0.0.1:18082')).toBe(false)
    expect(isDirectPreviewOrigin(url, 'https://127.0.0.1:18081')).toBe(false)
  })

  it('resolvePreviewFrameGoto stays on the preview document host', () => {
    const page = { protocol: 'http:', hostname: 'app.example.com', port: '' }
    const embed = toPreviewDocumentURL('/preview/run-1/node-a/18081/', page)
    const direct = 'http://10.0.0.8:18081/'
    expect(embed).toBe('http://pv.app.example.com/preview/run-1/node-a/18081/')
    expect(new URL(embed).origin).not.toBe('http://app.example.com')
    expect(isSameOriginPreviewPath('/preview/run-1/node-a/18081/')).toBe(true)
    expect(isSameOriginPreviewPath(embed)).toBe(false)
    expect(resolvePreviewFrameGoto(embed, direct, '/dash')).toBe(
      'http://pv.app.example.com/preview/run-1/node-a/18081/dash',
    )
    expect(resolvePreviewFrameGoto(embed, direct, '/preview/run-1/node-a/18081/home')).toBe(
      'http://pv.app.example.com/preview/run-1/node-a/18081/home',
    )
    expect(resolvePreviewFrameGoto(embed, direct, 'http://10.0.0.8:18081/account?x=1')).toBe(
      'http://pv.app.example.com/preview/run-1/node-a/18081/account?x=1',
    )
    expect(resolvePreviewFrameGoto(embed, direct, 'http://evil.example/')).toBeNull()
    expect(resolvePreviewFrameGoto(embed, direct, 'http://app.example.com/preview/run-1/node-a/18081/home')).toBeNull()
    expect(resolvePreviewFrameGoto('', direct, '/dash')).toBe('http://10.0.0.8:18081/dash')
  })

  it('acceptsPreviewFrameMessage only from the preview document origin', () => {
    const embed = 'http://pv.app.example.com/preview/run-1/node-a/18081/'
    const direct = 'http://10.0.0.8:18081/'
    expect(acceptsPreviewFrameMessage(embed, direct, 'http://pv.app.example.com')).toBe(true)
    expect(acceptsPreviewFrameMessage(embed, direct, 'http://app.example.com')).toBe(false)
    expect(acceptsPreviewFrameMessage(embed, direct, 'http://10.0.0.8:18081')).toBe(false)
    expect(acceptsPreviewFrameMessage(embed, direct, 'http://evil.example')).toBe(false)
    expect(acceptsPreviewFrameMessage('', direct, 'http://10.0.0.8:18081')).toBe(true)
    const frame = {} as Window
    const other = {} as Window
    expect(isCurrentPreviewFrameSource(frame, frame)).toBe(true)
    expect(isCurrentPreviewFrameSource(other, frame)).toBe(false)
    expect(isCurrentPreviewFrameSource(null, frame)).toBe(false)
  })

  it('resolveDirectPreviewGoto keeps same origin', () => {
    const base = 'http://127.0.0.1:18081/'
    expect(resolveDirectPreviewGoto(base, '/dash')).toBe('http://127.0.0.1:18081/dash')
    expect(resolveDirectPreviewGoto(base, 'http://127.0.0.1:18081/x?q=1')).toBe(
      'http://127.0.0.1:18081/x?q=1',
    )
    expect(resolveDirectPreviewGoto(base, 'http://evil.example/')).toBeNull()
    expect(resolveDirectPreviewGoto(base, 'javascript:alert(1)')).toBeNull()
    expect(resolveDirectPreviewGoto(base, '')).toBeNull()
    expect(resolveDirectPreviewGoto('bad', '/x')).toBeNull()
  })

  it('parseDirectPreviewMessage accepts protocol shapes', () => {
    expect(parseDirectPreviewMessage({ type: DIRECT_PREVIEW_READY, url: 'http://x/' })).toEqual({
      type: DIRECT_PREVIEW_READY,
      url: 'http://x/',
    })
    expect(parseDirectPreviewMessage({ type: 'direct-preview-canceled' })).toEqual({
      type: 'direct-preview-canceled',
    })
    expect(
      parseDirectPreviewMessage({
        type: DIRECT_PREVIEW_PICKED,
        selector: 'button',
        tagName: 'button',
        outerHTML: '<button></button>',
        url: 'http://x/a',
      }),
    ).toMatchObject({ selector: 'button', url: 'http://x/a' })
    expect(parseDirectPreviewMessage({ type: DIRECT_PREVIEW_READY })).toBeNull()
    expect(parseDirectPreviewMessage(null)).toBeNull()
    expect(parseDirectPreviewMessage({ type: 'other' })).toBeNull()
  })
})
