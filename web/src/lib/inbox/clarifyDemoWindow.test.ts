import { describe, expect, it } from 'vitest'
import {
  CLARIFY_DEMO_ESCAPE,
  demoWindowOverlapsChat,
  isClarifyDemoEscapeMessage,
  placeClarifyDemoWindow,
  withDemoEscapeBridge,
} from './clarifyDemoWindow'

describe('placeClarifyDemoWindow', () => {
  it('sits to the left of a right-hand chat column without covering it', () => {
    const chat = { x: 860, y: 0, w: 420, h: 900 }
    const box = placeClarifyDemoWindow({ w: 1280, h: 900 }, chat)
    expect(box.x + box.w).toBeLessThanOrEqual(chat.x)
    expect(box.w).toBeGreaterThan(0)
    expect(box.y).toBeGreaterThanOrEqual(16)
    expect(demoWindowOverlapsChat(box, chat)).toBe(false)
  })

  it('sits to the right when the chat column is on the left', () => {
    const chat = { x: 0, y: 40, w: 420, h: 800 }
    const box = placeClarifyDemoWindow({ w: 1280, h: 900 }, chat)
    expect(box.x).toBeGreaterThanOrEqual(chat.x + chat.w)
    expect(demoWindowOverlapsChat(box, chat)).toBe(false)
  })

  it('uses the free band above a full-width bottom chat drawer', () => {
    const chat = { x: 0, y: 480, w: 390, h: 420 }
    const box = placeClarifyDemoWindow({ w: 390, h: 900 }, chat)
    expect(box.y + box.h).toBeLessThanOrEqual(chat.y)
    expect(box.w).toBeGreaterThan(0)
    expect(demoWindowOverlapsChat(box, chat)).toBe(false)
  })

  it('does not invent a covering window when the chat fills the viewport', () => {
    const chat = { x: 0, y: 0, w: 1280, h: 900 }
    const box = placeClarifyDemoWindow({ w: 1280, h: 900 }, chat)
    expect(box.w).toBe(0)
    expect(demoWindowOverlapsChat(box, chat)).toBe(false)
  })
})

describe('withDemoEscapeBridge', () => {
  it('appends one Escape postMessage and does not repeat it', () => {
    const once = withDemoEscapeBridge('<p>preview-c</p>')
    expect(once).toContain('<p>preview-c</p>')
    expect(once).toContain(CLARIFY_DEMO_ESCAPE)
    expect(once).toContain("e.key!=='Escape'")
    expect(withDemoEscapeBridge(once)).toBe(once)
    expect(isClarifyDemoEscapeMessage({ type: CLARIFY_DEMO_ESCAPE })).toBe(true)
    expect(isClarifyDemoEscapeMessage({ type: 'other' })).toBe(false)
  })
})
