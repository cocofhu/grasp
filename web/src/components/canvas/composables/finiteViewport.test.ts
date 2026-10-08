import { describe, expect, it } from 'vitest'
import {
  DEFAULT_FLOW_VIEWPORT,
  dotPatternGeometry,
  finiteViewport,
  fitTransform,
  unionBounds,
  type FlowViewport,
} from './finiteViewport'

const previous: FlowViewport = { x: 12, y: -4, zoom: 0.8 }

describe('finiteViewport', () => {
  it('keeps a viewport whose x, y and zoom are all finite and zoom is positive', () => {
    expect(finiteViewport({ x: 3, y: -8, zoom: 1.5 }, previous)).toEqual({ x: 3, y: -8, zoom: 1.5 })
  })

  it('falls back to the whole previous viewport when any component is NaN', () => {
    expect(finiteViewport({ x: Number.NaN, y: 5, zoom: 1 }, previous)).toEqual(previous)
    expect(finiteViewport({ x: 5, y: Number.NaN, zoom: 1 }, previous)).toEqual(previous)
    expect(finiteViewport({ x: 5, y: 6, zoom: Number.NaN }, previous)).toEqual(previous)
  })

  it('falls back to the whole previous viewport when any component is infinite', () => {
    expect(finiteViewport({ x: Number.POSITIVE_INFINITY, y: 1, zoom: 1 }, previous)).toEqual(previous)
    expect(finiteViewport({ x: 1, y: Number.NEGATIVE_INFINITY, zoom: 1 }, previous)).toEqual(previous)
    expect(finiteViewport({ x: 1, y: 1, zoom: Number.POSITIVE_INFINITY }, previous)).toEqual(previous)
  })

  it('falls back when zoom is zero or negative', () => {
    expect(finiteViewport({ x: 9, y: 9, zoom: 0 }, previous)).toEqual(previous)
    expect(finiteViewport({ x: 9, y: 9, zoom: -0.2 }, previous)).toEqual(previous)
  })

  it('uses zero pan and unit zoom when there is no previous viewport', () => {
    expect(finiteViewport({ x: Number.NaN, y: Number.NaN, zoom: Number.NaN }, null)).toEqual(DEFAULT_FLOW_VIEWPORT)
    expect(finiteViewport({ x: 1, y: 2, zoom: 0 })).toEqual(DEFAULT_FLOW_VIEWPORT)
  })
})

describe('fitTransform', () => {
  it('returns a finite transform for a finite box', () => {
    const next = fitTransform({ x: 0, y: 0, width: 200, height: 100 }, 800, 600, 0.25, 1, 0.2)
    expect(next).not.toBeNull()
    expect(Number.isFinite(next!.x)).toBe(true)
    expect(Number.isFinite(next!.y)).toBe(true)
    expect(next!.zoom).toBeGreaterThan(0)
    expect(next!.zoom).toBeLessThanOrEqual(1)
  })

  it('returns null when the box or the pane is not finite', () => {
    expect(fitTransform({ x: Number.NaN, y: 0, width: 200, height: 100 }, 800, 600, 0.25, 1, 0.2)).toBeNull()
    expect(fitTransform({ x: 0, y: 0, width: 200, height: 100 }, Number.NaN, 600, 0.7, 1, 0.2)).toBeNull()
  })

  it('returns null for an empty pane so a zero-size fit is not committed', () => {
    expect(fitTransform({ x: 0, y: 0, width: 200, height: 100 }, 0, 600, 0.25, 1, 0.2)).toBeNull()
  })
})

describe('unionBounds', () => {
  it('rejects the whole union when one box is not finite', () => {
    expect(
      unionBounds([
        { x: 0, y: 0, width: 10, height: 10 },
        { x: Number.NaN, y: 4, width: 10, height: 10 },
      ]),
    ).toBeNull()
  })
})

describe('dotPatternGeometry', () => {
  it('matches the library dots at unit zoom and zero pan', () => {
    expect(dotPatternGeometry({ x: 0, y: 0, zoom: 1 })).toEqual({
      cx: 0.6,
      cy: 0.6,
      r: 0.6,
      x: 0,
      y: 0,
      width: 16,
      height: 16,
      transform: 'translate(-9,-9)',
    })
  })

  it('keeps every length finite when the viewport is not', () => {
    const geo = dotPatternGeometry({ x: Number.NaN, y: Number.POSITIVE_INFINITY, zoom: Number.NaN }, previous)
    expect([geo.cx, geo.cy, geo.r, geo.x, geo.y, geo.width, geo.height].every(Number.isFinite)).toBe(true)
    expect(geo.width).toBe(16 * previous.zoom)
    expect(geo.x).toBe(previous.x % (16 * previous.zoom))
  })
})
