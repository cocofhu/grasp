import { describe, expect, it } from 'vitest'
import {
  CLARIFY_DEMO_ESCAPE,
  clampDemoWindow,
  isClarifyDemoEscapeMessage,
  moveDemoWindow,
  placeClarifyDemoWindow,
  resizeDemoWindow,
  withDemoEscapeBridge,
} from './clarifyDemoWindow'

describe('placeClarifyDemoWindow', () => {
  it('centers a viewport-minus-margin window on a wide screen', () => {
    const box = placeClarifyDemoWindow({ w: 1280, h: 900 })
    expect(box).toEqual({ x: 28, y: 28, w: 1224, h: 844 })
    expect(box.w).toBeGreaterThan(560)
    expect(box.h).toBeGreaterThan(520)
  })

  it('keeps the 28px margin on a 390-wide viewport when the usable side is still at least 160', () => {
    const box = placeClarifyDemoWindow({ w: 390, h: 900 })
    expect(box).toEqual({ x: 28, y: 28, w: 334, h: 844 })
  })

  it('uses an 8px margin when a 28px margin would leave less than 160 on either axis', () => {
    const box = placeClarifyDemoWindow({ w: 200, h: 900 })
    expect(box).toEqual({ x: 8, y: 8, w: 184, h: 884 })
  })

  it('still returns a positive centered box when the chat would have filled the viewport', () => {
    const box = placeClarifyDemoWindow({ w: 1280, h: 900 })
    expect(box.w).toBeGreaterThan(0)
    expect(box.h).toBeGreaterThan(0)
    expect(box.x).toBe(28)
    expect(box.y).toBe(28)
  })
})

describe('moveDemoWindow', () => {
  it('changes position and keeps width and height', () => {
    const start = { x: 28, y: 28, w: 1224, h: 844 }
    const moved = moveDemoWindow(start, 20, 20, { w: 1280, h: 900 })
    expect(moved).toEqual({ x: 48, y: 48, w: 1224, h: 844 })
  })

  it('stops at the viewport edge instead of leaving it', () => {
    const start = { x: 28, y: 28, w: 1224, h: 844 }
    const moved = moveDemoWindow(start, 800, 800, { w: 1280, h: 900 })
    expect(moved).toEqual({ x: 56, y: 56, w: 1224, h: 844 })
    expect(moved.x).toBeGreaterThanOrEqual(0)
    expect(moved.y).toBeGreaterThanOrEqual(0)
    expect(moved.x + moved.w).toBeLessThanOrEqual(1280)
    expect(moved.y + moved.h).toBeLessThanOrEqual(900)
  })
})

describe('resizeDemoWindow', () => {
  const viewport = { w: 1280, h: 900 }
  const start = { x: 28, y: 28, w: 1224, h: 844 }

  it('grows and shrinks the right edge while the left edge stays', () => {
    const grown = resizeDemoWindow(start, 'e', 20, 0, viewport)
    expect(grown).toEqual({ x: 28, y: 28, w: 1244, h: 844 })
    const shrunk = resizeDemoWindow(start, 'e', -200, 0, viewport)
    expect(shrunk).toEqual({ x: 28, y: 28, w: 1024, h: 844 })
  })

  it('moves the left edge and keeps the right edge fixed', () => {
    const next = resizeDemoWindow(start, 'w', 100, 0, viewport)
    expect(next.x).toBe(128)
    expect(next.w).toBe(1124)
    expect(next.x + next.w).toBe(start.x + start.w)
    expect(next.y).toBe(start.y)
    expect(next.h).toBe(start.h)
  })

  it('keeps the top edge fixed when the bottom edge moves', () => {
    const next = resizeDemoWindow(start, 's', 0, -120, viewport)
    expect(next).toEqual({ x: 28, y: 28, w: 1224, h: 724 })
  })

  it('keeps the bottom edge fixed when the top edge moves', () => {
    const next = resizeDemoWindow(start, 'n', 0, 80, viewport)
    expect(next.y).toBe(108)
    expect(next.h).toBe(764)
    expect(next.y + next.h).toBe(start.y + start.h)
    expect(next.x).toBe(start.x)
    expect(next.w).toBe(start.w)
  })

  it('changes both axes from a corner', () => {
    const next = resizeDemoWindow(start, 'se', -80, -60, viewport)
    expect(next).toEqual({ x: 28, y: 28, w: 1144, h: 784 })
  })

  it('does not shrink below 480×320 on a large viewport', () => {
    const next = resizeDemoWindow(start, 'se', -2000, -2000, viewport)
    expect(next.w).toBe(480)
    expect(next.h).toBe(320)
    expect(next.x).toBe(28)
    expect(next.y).toBe(28)
  })

  it('follows a viewport smaller than 480×320 and stays inside it', () => {
    const small = { w: 400, h: 300 }
    const fitted = resizeDemoWindow({ x: 8, y: 8, w: 360, h: 260 }, 'se', -200, -200, small)
    expect(fitted.w).toBe(392)
    expect(fitted.h).toBe(292)
    expect(fitted.x).toBe(8)
    expect(fitted.y).toBe(8)
    expect(fitted.x + fitted.w).toBeLessThanOrEqual(400)
    expect(fitted.y + fitted.h).toBeLessThanOrEqual(300)
  })

  it('does not cross the viewport while a corner grows', () => {
    const next = resizeDemoWindow(start, 'nw', -500, -500, viewport)
    expect(next.x).toBe(0)
    expect(next.y).toBe(0)
    expect(next.x + next.w).toBe(start.x + start.w)
    expect(next.y + next.h).toBe(start.y + start.h)
  })
})

describe('clampDemoWindow', () => {
  it('shrinks and shifts a user-sized window into a smaller viewport', () => {
    const clamped = clampDemoWindow({ x: 100, y: 80, w: 900, h: 700 }, { w: 640, h: 480 })
    expect(clamped).toEqual({ x: 0, y: 0, w: 640, h: 480 })
  })

  it('keeps a window that already fits', () => {
    const box = { x: 40, y: 50, w: 600, h: 400 }
    expect(clampDemoWindow(box, { w: 1280, h: 900 })).toEqual(box)
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
