import { describe, it, expect } from 'vitest'
import {
  isNovncRfbModule,
  stripNovncSecureContextCheck,
  SECURE_CONTEXT_SNIPPET,
} from '../../../vite-plugins/stripNovncSecureContext'

describe('stripNovncSecureContextCheck', () => {
  it('removes the isSecureContext Log.Error block from noVNC rfb.js shape', () => {
    const sample = `
    if (!urlOrChannel) {
      throw new Error("Must specify URL, WebSocket or RTCDataChannel");
    }

    // We rely on modern APIs which might not be available in an
    // insecure context
    if (!window.isSecureContext) {
      Log.Error("noVNC requires a secure context (TLS). Expect crashes!");
    }
    _this = _callSuper(this, RFB);
`
    const out = stripNovncSecureContextCheck(sample)
    expect(out).not.toBeNull()
    expect(out!).not.toContain(SECURE_CONTEXT_SNIPPET)
    expect(out!).not.toContain('isSecureContext')
    expect(out!).toContain('_this = _callSuper(this, RFB)')
  })

  it('removes the block from the dev prebundle shape, where esbuild dropped the comments', () => {
    const sample = `
        if (!urlOrChannel) {
          throw new Error("Must specify URL, WebSocket or RTCDataChannel");
        }
        if (!window.isSecureContext) {
          Log.Error("noVNC requires a secure context (TLS). Expect crashes!");
        }
        _this = _callSuper(this, RFB2);
`
    const out = stripNovncSecureContextCheck(sample)
    expect(out).not.toBeNull()
    expect(out!).not.toContain('isSecureContext')
    expect(out!).toContain('_this = _callSuper(this, RFB2)')
  })

  it('matches the rfb module in both source and prebundled paths', () => {
    expect(isNovncRfbModule('/app/node_modules/@novnc/novnc/lib/rfb.js')).toBe(true)
    expect(isNovncRfbModule('/app/node_modules/.vite/deps/@novnc_novnc_lib_rfb__js.js?v=4d4890b9')).toBe(true)
    expect(isNovncRfbModule('/app/node_modules/@novnc/novnc/lib/display.js')).toBe(false)
    expect(isNovncRfbModule('/app/src/components/run/NovncPreviewPanel.vue')).toBe(false)
  })

  it('returns null when the snippet is absent', () => {
    expect(stripNovncSecureContextCheck('const x = 1')).toBeNull()
  })
})
