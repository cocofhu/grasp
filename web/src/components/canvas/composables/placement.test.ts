import { describe, expect, it } from 'vitest'
import { findFreeSpot, PLACE_GAP, rectsOverlap, type Rect } from './placement'

const size = { width: 200, height: 64 }

describe('rectsOverlap', () => {
  it('detects overlap, touching and gap clearance', () => {
    const a: Rect = { x: 0, y: 0, width: 100, height: 50 }
    expect(rectsOverlap(a, { x: 50, y: 25, width: 100, height: 50 })).toBe(true)
    expect(rectsOverlap(a, { x: 100, y: 0, width: 100, height: 50 })).toBe(false)
    expect(rectsOverlap(a, { x: 110, y: 0, width: 100, height: 50 }, 24)).toBe(true)
    expect(rectsOverlap(a, { x: 130, y: 0, width: 100, height: 50 }, 24)).toBe(false)
  })
})

describe('findFreeSpot', () => {
  it('keeps the requested spot (grid-snapped) when it is free', () => {
    expect(findFreeSpot({ x: 101, y: 205 }, size, [])).toEqual({ x: 104, y: 208 })
    expect(findFreeSpot({ x: 0, y: 0 }, size, [{ x: 600, y: 600, width: 200, height: 64 }])).toEqual({ x: 0, y: 0 })
  })

  it('nudges off an occupied spot to the nearest clear one', () => {
    const others: Rect[] = [{ x: 0, y: 0, width: 200, height: 64 }]
    const p = findFreeSpot({ x: 0, y: 0 }, size, others)
    expect(others.some((o) => rectsOverlap({ ...p, ...size }, o, PLACE_GAP))).toBe(false)
    // Vertical clearance (64 + 24) is shorter than horizontal (200 + 24).
    expect(p.x).toBe(0)
    expect(Math.abs(p.y)).toBeGreaterThanOrEqual(64 + PLACE_GAP)
    expect(Math.abs(p.y)).toBeLessThan(64 + PLACE_GAP + 24)
  })

  it('finds a hole in a crowded area', () => {
    const others: Rect[] = []
    for (let i = -2; i <= 2; i++) for (let j = -2; j <= 2; j++) if (i || j) others.push({ x: i * 240, y: j * 120, width: 200, height: 64 })
    others.push({ x: 8, y: 8, width: 200, height: 64 })
    const p = findFreeSpot({ x: 0, y: 0 }, size, others)
    expect(others.some((o) => rectsOverlap({ ...p, ...size }, o, PLACE_GAP))).toBe(false)
    expect(Math.abs(p.x % 8)).toBe(0)
    expect(Math.abs(p.y % 8)).toBe(0)
  })

  it('falls back to the requested spot when nothing is free within range', () => {
    const wall: Rect[] = [{ x: -10000, y: -10000, width: 20000, height: 20000 }]
    expect(findFreeSpot({ x: 16, y: 16 }, size, wall, { maxRings: 3 })).toEqual({ x: 16, y: 16 })
  })
})
