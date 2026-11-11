import { describe, expect, it, vi } from 'vitest'
import {
  INLINE_FALLBACK_HEIGHT,
  RESIZE_DEBOUNCE_MS,
  RESIZE_HEIGHT_EPSILON,
  RESIZE_MESSAGE_TYPE,
  INSPECT_MESSAGE_TYPE,
  INSPECT_COMMAND_TYPE,
  INSPECT_CANCELED_TYPE,
  SANDBOX_ATTR,
  CONTENT_FIT_PREVIEW_MAX_VH,
  CONTENT_FIT_REVIEWING_STRIP_PX,
  HTML_PREVIEW_DEFAULT_TOOLBAR_PX,
  contentFitPreviewCapPx,
  createInstanceId,
  injectInlineResizeScript,
  injectInlineInspectScript,
  injectPreviewScripts,
  isValidResizeMessage,
  parseResizeMessage,
  isValidInspectPickMessage,
  parseInspectPickMessage,
  isValidInspectCanceledMessage,
  dataUrlToImageParts,
  buildInspectCommand,
} from './htmlPreviewSandbox'

const TEST_ID = 'test-instance-uuid'

describe('createInstanceId', () => {
  it('returns crypto.randomUUID when available', () => {
    const mockUuid = 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'
    vi.stubGlobal('crypto', { randomUUID: () => mockUuid })

    expect(createInstanceId()).toBe(mockUuid)

    vi.unstubAllGlobals()
  })

  it('returns hp- prefixed fallback when randomUUID is unavailable', () => {
    vi.stubGlobal('crypto', {})

    expect(createInstanceId()).toMatch(/^hp-\d+-[a-z0-9]+$/)

    vi.unstubAllGlobals()
  })
})

describe('htmlPreviewSandbox constants', () => {
  it('SANDBOX_ATTR allows scripts and forms without same-origin', () => {
    expect(SANDBOX_ATTR).toBe('allow-scripts allow-forms')
    expect(SANDBOX_ATTR).toContain('allow-scripts')
    expect(SANDBOX_ATTR).toContain('allow-forms')
    expect(SANDBOX_ATTR).not.toContain('allow-same-origin')
  })

  it('does not combine allow-scripts with allow-same-origin', () => {
    const tokens = SANDBOX_ATTR.split(/\s+/)
    expect(tokens).toContain('allow-scripts')
    expect(tokens).not.toContain('allow-same-origin')
  })

  it('INLINE_FALLBACK_HEIGHT defaults to 120', () => {
    expect(INLINE_FALLBACK_HEIGHT).toBe(120)
  })

  it('RESIZE_HEIGHT_EPSILON and RESIZE_DEBOUNCE_MS are defined', () => {
    expect(RESIZE_HEIGHT_EPSILON).toBeGreaterThanOrEqual(4)
    expect(RESIZE_HEIGHT_EPSILON).toBeLessThanOrEqual(8)
    expect(RESIZE_DEBOUNCE_MS).toBeGreaterThanOrEqual(50)
    expect(RESIZE_DEBOUNCE_MS).toBeLessThanOrEqual(100)
  })

  it('CONTENT_FIT_PREVIEW_MAX_VH is 60 and cap converts vh to px', () => {
    expect(CONTENT_FIT_PREVIEW_MAX_VH).toBe(60)
    expect(CONTENT_FIT_REVIEWING_STRIP_PX).toBe(28)
    expect(HTML_PREVIEW_DEFAULT_TOOLBAR_PX).toBe(37)
    expect(contentFitPreviewCapPx(1000)).toBe(600)
    expect(contentFitPreviewCapPx(800)).toBe(480)
    expect(contentFitPreviewCapPx(1000, 50)).toBe(500)
  })
})

describe('injectInlineResizeScript', () => {
  it('returns script-only output for empty html', () => {
    const result = injectInlineResizeScript('', TEST_ID)
    expect(result).toContain(TEST_ID)
    expect(result).toContain('postMessage')
    expect(result).toContain(RESIZE_MESSAGE_TYPE)
    expect(result).toContain('ResizeObserver')
  })

  it('inserts before </body> when body is closed', () => {
    const html = '<!doctype html><html><head></head><body><p>hi</p></body></html>'
    const result = injectInlineResizeScript(html, TEST_ID)
    expect(result).toContain(`<p>hi</p><script>`)
    expect(result.indexOf(TEST_ID)).toBeLessThan(result.toLowerCase().indexOf('</body>'))
  })

  it('inserts after <body> when body is open but not closed', () => {
    const html = '<!doctype html><html><body><p>hi</p></html>'
    const result = injectInlineResizeScript(html, TEST_ID)
    expect(result).toMatch(/<body[^>]*>[\s\S]*postMessage/)
  })

  it('wraps with body when html exists but body is missing', () => {
    const html = '<!doctype html><html><head></head></html>'
    const result = injectInlineResizeScript(html, TEST_ID)
    expect(result).toContain('<body><script>')
    expect(result).toContain(TEST_ID)
  })

  it('prepends script when no html/body structure', () => {
    const html = '<div>fragment</div>'
    const result = injectInlineResizeScript(html, TEST_ID)
    expect(result.startsWith('<script>')).toBe(true)
    expect(result.endsWith(html)).toBe(true)
  })

  it('escapes instanceId for safe embedding', () => {
    const id = "id-with-'quote"
    const result = injectInlineResizeScript('<body></body>', id)
    expect(result).toContain("instanceId='id-with-\\'quote'")
  })

  it('debounces subsequent resize reports', () => {
    const result = injectInlineResizeScript('<body></body>', TEST_ID)
    expect(result).toContain(`debounceMs=${RESIZE_DEBOUNCE_MS}`)
    expect(result).toContain('hasReported')
    expect(result).toContain('setTimeout')
  })
})

describe('isValidResizeMessage', () => {
  it('accepts valid resize messages', () => {
    expect(
      isValidResizeMessage({ type: RESIZE_MESSAGE_TYPE, id: TEST_ID, height: 200 }),
    ).toBe(true)
  })

  it('rejects wrong type', () => {
    expect(isValidResizeMessage({ type: 'other', id: TEST_ID, height: 200 })).toBe(false)
  })

  it('rejects missing or empty id', () => {
    expect(isValidResizeMessage({ type: RESIZE_MESSAGE_TYPE, id: '', height: 200 })).toBe(false)
    expect(isValidResizeMessage({ type: RESIZE_MESSAGE_TYPE, height: 200 })).toBe(false)
  })

  it('rejects invalid height', () => {
    expect(isValidResizeMessage({ type: RESIZE_MESSAGE_TYPE, id: TEST_ID, height: -1 })).toBe(false)
    expect(isValidResizeMessage({ type: RESIZE_MESSAGE_TYPE, id: TEST_ID, height: NaN })).toBe(false)
    expect(isValidResizeMessage({ type: RESIZE_MESSAGE_TYPE, id: TEST_ID, height: '200' })).toBe(false)
  })

  it('rejects non-object payloads', () => {
    expect(isValidResizeMessage(null)).toBe(false)
    expect(isValidResizeMessage('resize')).toBe(false)
  })
})

describe('parseResizeMessage', () => {
  it('returns parsed message with height floored to fallback minimum', () => {
    expect(parseResizeMessage({ type: RESIZE_MESSAGE_TYPE, id: TEST_ID, height: 80 })).toEqual({
      type: RESIZE_MESSAGE_TYPE,
      id: TEST_ID,
      height: INLINE_FALLBACK_HEIGHT,
    })
  })

  it('preserves height when above fallback minimum', () => {
    expect(parseResizeMessage({ type: RESIZE_MESSAGE_TYPE, id: TEST_ID, height: 240 })).toEqual({
      type: RESIZE_MESSAGE_TYPE,
      id: TEST_ID,
      height: 240,
    })
  })

  it('returns null for invalid messages', () => {
    expect(parseResizeMessage({ type: 'bad', id: TEST_ID, height: 200 })).toBeNull()
  })
})

describe('injectInlineInspectScript', () => {
  it('injects inspect message types, Esc cancel, and pickScript-style path helpers', () => {
    const html = '<!doctype html><html><body><p id="x">hi</p></body></html>'
    const result = injectInlineInspectScript(html, TEST_ID)
    expect(result).toContain(INSPECT_MESSAGE_TYPE)
    expect(result).toContain(INSPECT_COMMAND_TYPE)
    expect(result).toContain(INSPECT_CANCELED_TYPE)
    expect(result).toContain("ev.key!=='Escape'")
    expect(result).toContain('keydown')
    expect(result).toContain(TEST_ID)
    expect(result).toContain('nth-of-type')
    expect(result).toContain('foreignObject')
    expect(result).toContain('getComputedStyle')
    expect(result).toContain('Stay in inspect after pick')
    expect(result.indexOf(TEST_ID)).toBeLessThan(result.toLowerCase().indexOf('</body>'))
  })

  it('derives opaque ancestor fill and does not hardcode wrapper/canvas #fff (g2.1/g2.2/g3.1)', () => {
    const result = injectInlineInspectScript('<body></body>', TEST_ID)
    expect(result).toContain('function resolveOpaqueFillColor')
    expect(result).toContain('function isOpaqueFillColor')
    expect(result).toContain('backgroundImage')
    expect(result).toContain('var pad=6')
    expect(result).toContain('innerW+2*pad')
    expect(result).toContain('ctx.fillStyle=fill')
    expect(result).toContain("background:'+fill")
    expect(result).not.toContain('background:#fff')
    expect(result).not.toContain("fillStyle='#fff'")
    expect(result).not.toContain('fillStyle="#fff"')
    expect(result).toContain('wrapper.appendChild(clone)')
  })
})

describe('injectPreviewScripts', () => {
  it('can inject both resize and inspect', () => {
    const result = injectPreviewScripts('<body></body>', TEST_ID, { resize: true, inspect: true })
    expect(result).toContain(RESIZE_MESSAGE_TYPE)
    expect(result).toContain(INSPECT_MESSAGE_TYPE)
  })
})

describe('isValidInspectPickMessage / parseInspectPickMessage', () => {
  it('accepts valid pick messages', () => {
    const msg = {
      type: INSPECT_MESSAGE_TYPE,
      id: TEST_ID,
      selector: 'p:nth-of-type(1)',
      tagName: 'p',
      imageDataUrl: 'data:image/png;base64,abc',
    }
    expect(isValidInspectPickMessage(msg)).toBe(true)
    expect(parseInspectPickMessage(msg)).toEqual(msg)
  })

  it('accepts optional bounds, currentText, and style', () => {
    const msg = {
      type: INSPECT_MESSAGE_TYPE,
      id: TEST_ID,
      selector: 'div.hero',
      tagName: 'div',
      imageDataUrl: '',
      bounds: { left: 1, top: 2, width: 3, height: 4 },
      currentText: 'hello',
      style: {
        color: 'rgb(26, 26, 26)',
        fontSize: '118px',
        fontWeight: '700',
        fontFamily: '"Bodoni Moda", serif',
        lineHeight: '159.3px',
      },
    }
    expect(isValidInspectPickMessage(msg)).toBe(true)
    expect(parseInspectPickMessage(msg)).toEqual(msg)
  })

  it('rejects empty selector or missing imageDataUrl field', () => {
    expect(
      isValidInspectPickMessage({
        type: INSPECT_MESSAGE_TYPE,
        id: TEST_ID,
        selector: '  ',
        tagName: 'p',
        imageDataUrl: '',
      }),
    ).toBe(false)
    expect(
      isValidInspectPickMessage({
        type: INSPECT_MESSAGE_TYPE,
        id: TEST_ID,
        selector: '#x',
        tagName: 'p',
      }),
    ).toBe(false)
  })
})

describe('dataUrlToImageParts', () => {
  it('splits data URL into ClarifyImage parts', () => {
    expect(dataUrlToImageParts('data:image/png;base64,QUJD')).toEqual({
      data: 'QUJD',
      mimeType: 'image/png',
    })
  })

  it('returns null for invalid input', () => {
    expect(dataUrlToImageParts('')).toBeNull()
    expect(dataUrlToImageParts('not-a-data-url')).toBeNull()
  })
})

describe('buildInspectCommand', () => {
  it('builds parent→iframe command', () => {
    expect(buildInspectCommand(TEST_ID, true)).toEqual({
      type: INSPECT_COMMAND_TYPE,
      id: TEST_ID,
      enabled: true,
    })
  })
})

describe('isValidInspectCanceledMessage', () => {
  it('accepts iframe Esc cancel messages', () => {
    expect(isValidInspectCanceledMessage({ type: INSPECT_CANCELED_TYPE, id: TEST_ID })).toBe(true)
  })

  it('rejects missing id or wrong type', () => {
    expect(isValidInspectCanceledMessage({ type: INSPECT_CANCELED_TYPE, id: '' })).toBe(false)
    expect(isValidInspectCanceledMessage({ type: INSPECT_MESSAGE_TYPE, id: TEST_ID })).toBe(false)
  })
})
