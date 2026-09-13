import { describe, expect, it } from 'vitest'
import { canHoverInk, farthestCornerDiameter, HOVER_INK_DURATION_MS } from './hoverInkGeometry'

describe('hoverInkGeometry (plan g1.1)', () => {
  it('uses landing point as center: diameter is 2× distance to farthest corner', () => {
    // 100×40 box, landing at top-left (0,0) → farthest is (100,40)
    expect(farthestCornerDiameter(100, 40, 0, 0)).toBeCloseTo(Math.hypot(100, 40) * 2)
    // center → farthest is any corner
    expect(farthestCornerDiameter(100, 40, 50, 20)).toBeCloseTo(Math.hypot(50, 20) * 2)
    // near bottom-right
    expect(farthestCornerDiameter(100, 40, 90, 35)).toBeCloseTo(Math.hypot(90, 35) * 2)
  })

  it('covers all four corners from an arbitrary landing (plan g1.1 evidence)', () => {
    const w = 200
    const h = 48
    const x = 30
    const y = 10
    const d = farthestCornerDiameter(w, h, x, y)
    const r = d / 2
    const corners: Array<[number, number]> = [
      [0, 0],
      [w, 0],
      [0, h],
      [w, h],
    ]
    for (const [cx, cy] of corners) {
      expect(Math.hypot(cx - x, cy - y)).toBeLessThanOrEqual(r + 1e-9)
    }
  })

  it('handles zero-size boxes without NaN', () => {
    expect(farthestCornerDiameter(0, 0, 0, 0)).toBe(0)
    expect(Number.isFinite(farthestCornerDiameter(-1, 10, 5, 5))).toBe(true)
  })

  it('exports ~350ms duration token for CSS/tests (plan g1.1)', () => {
    expect(HOVER_INK_DURATION_MS).toBe(350)
  })

  it('canHoverInk requires hover + fine pointer', () => {
    expect(
      canHoverInk((q) => ({ matches: q.includes('hover: hover') && q.includes('pointer: fine') }) as MediaQueryList),
    ).toBe(true)
    expect(canHoverInk(() => ({ matches: false }) as MediaQueryList)).toBe(false)
  })
})
