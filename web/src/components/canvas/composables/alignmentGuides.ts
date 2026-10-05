// Alignment guides shown while dragging: edges and centers of the dragged node
// that line up (within a tolerance) with other nodes. Flow coordinates.

export type GuideRect = { id: string; x: number; y: number; width: number; height: number }

export type AlignmentGuides = { vertical: number[]; horizontal: number[] }

export const GUIDE_TOLERANCE = 4

function xs(r: GuideRect): number[] {
  return [r.x, r.x + r.width / 2, r.x + r.width]
}

function ys(r: GuideRect): number[] {
  return [r.y, r.y + r.height / 2, r.y + r.height]
}

function matches(own: number[], other: number[], tolerance: number, out: Set<number>) {
  for (const a of own) {
    for (const b of other) {
      if (Math.abs(a - b) <= tolerance) out.add(b)
    }
  }
}

export function alignmentGuides(
  dragged: GuideRect,
  others: GuideRect[],
  tolerance = GUIDE_TOLERANCE,
): AlignmentGuides {
  const vertical = new Set<number>()
  const horizontal = new Set<number>()
  const ownX = xs(dragged)
  const ownY = ys(dragged)
  for (const o of others) {
    if (o.id === dragged.id) continue
    matches(ownX, xs(o), tolerance, vertical)
    matches(ownY, ys(o), tolerance, horizontal)
  }
  return {
    vertical: [...vertical].sort((a, b) => a - b),
    horizontal: [...horizontal].sort((a, b) => a - b),
  }
}
