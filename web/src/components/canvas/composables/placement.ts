import { GRID, snap } from './graphOps'
import type { Point, Size } from './useAutoLayout'

export interface Rect {
  x: number
  y: number
  width: number
  height: number
}

/** Minimum clear space kept between a placed node and its neighbours. */
export const PLACE_GAP = 24

export function rectsOverlap(a: Rect, b: Rect, gap = 0): boolean {
  return a.x < b.x + b.width + gap && b.x < a.x + a.width + gap && a.y < b.y + b.height + gap && b.y < a.y + a.height + gap
}

export interface FreeSpotOptions {
  gap?: number
  /** Search step in flow px; defaults to three grid cells. */
  step?: number
  /** Rings searched before giving up and returning the requested spot. */
  maxRings?: number
}

/**
 * Nearest top-left at or around `at` where a `size` box clears every rect in `others`
 * by `gap`. Searches square rings of `step` outward, nearest candidate first; the
 * result is grid-snapped.
 */
export function findFreeSpot(at: Point, size: Size, others: Rect[], opts: FreeSpotOptions = {}): Point {
  const gap = opts.gap ?? PLACE_GAP
  const step = opts.step ?? GRID * 3
  const maxRings = opts.maxRings ?? 40
  const grid = (v: number) => snap(v) || 0
  const origin = { x: grid(at.x), y: grid(at.y) }
  const free = (p: Point) => !others.some((o) => rectsOverlap({ ...p, ...size }, o, gap))
  if (free(origin)) return origin
  for (let r = 1; r <= maxRings; r++) {
    const ring: Point[] = []
    for (let i = -r; i <= r; i++) {
      ring.push({ x: i, y: -r }, { x: i, y: r })
      if (i !== -r && i !== r) ring.push({ x: -r, y: i }, { x: r, y: i })
    }
    ring.sort((a, b) => a.x * a.x + a.y * a.y - (b.x * b.x + b.y * b.y))
    for (const d of ring) {
      const p = { x: grid(origin.x + d.x * step), y: grid(origin.y + d.y * step) }
      if (free(p)) return p
    }
  }
  return origin
}
