/**
 * Hover-ink geometry: diameter that covers the farthest corner from a landing point.
 * Pure functions — no DOM. Used by the hover-ink directive and unit tests (plan g1.1).
 */

/** Diameter of a circle centered at (x, y) that reaches the farthest corner of a w×h box. */
export function farthestCornerDiameter(width: number, height: number, x: number, y: number): number {
  const w = Math.max(0, width)
  const h = Math.max(0, height)
  const dx = Math.max(x, w - x)
  const dy = Math.max(y, h - y)
  return Math.hypot(dx, dy) * 2
}

/** True when the primary pointer can hover (skip touch-only / coarse pointers). */
export function canHoverInk(matchMedia: (query: string) => MediaQueryList = window.matchMedia): boolean {
  return matchMedia('(hover: hover) and (pointer: fine)').matches
}

export const HOVER_INK_DURATION_MS = 350
