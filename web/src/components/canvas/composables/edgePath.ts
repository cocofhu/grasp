import { getSmoothStepPath, Position } from '@vue-flow/core'
import { coordsFinite, fallbackLinePath, roundedPolyline } from '@/lib/run/canvasPath'

export interface EdgeEnds {
  sourceX: number
  sourceY: number
  targetX: number
  targetY: number
  sourcePosition?: Position
  targetPosition?: Position
}

export interface NodeBox {
  y: number
  height: number
}

export interface EdgeGeometry {
  path: string
  labelX: number
  labelY: number
  back: boolean
}

const STUB = 24
/** Short final run into the target so a loop stays clear of the forward edge's midpoint controls. */
const ENTRY = 12
const LANE_GAP = 40
const RADIUS = 10

/** A connection whose target sits left of (or on top of) its source loops back. */
export function isBackward(e: EdgeEnds): boolean {
  return e.targetX <= e.sourceX + STUB
}

/**
 * Rounded orthogonal path. Forward edges use a smooth step; backward edges leave
 * right, drop below both nodes, run left in that lane and enter the target from
 * the left, so a loop never crosses the main path.
 */
export function edgeGeometry(e: EdgeEnds, source?: NodeBox, target?: NodeBox): EdgeGeometry {
  const { sourceX: sx, sourceY: sy, targetX: tx, targetY: ty } = e
  if (!coordsFinite(sx, sy, tx, ty)) {
    const [path, labelX, labelY] = fallbackLinePath(sx, sy, tx, ty)
    return { path, labelX, labelY, back: false }
  }
  if (isBackward(e)) {
    const bottom = Math.max(
      source ? source.y + source.height : sy,
      target ? target.y + target.height : ty,
      sy,
      ty,
    )
    const lane = bottom + LANE_GAP
    const path = roundedPolyline(
      [
        [sx, sy],
        [sx + STUB, sy],
        [sx + STUB, lane],
        [tx - ENTRY, lane],
        [tx - ENTRY, ty],
        [tx, ty],
      ],
      RADIUS,
    )
    return { path: path || fallbackLinePath(sx, sy, tx, ty)[0], labelX: (sx + tx) / 2, labelY: lane, back: true }
  }
  const [path, labelX, labelY] = getSmoothStepPath({
    sourceX: sx,
    sourceY: sy,
    targetX: tx,
    targetY: ty,
    sourcePosition: e.sourcePosition ?? Position.Right,
    targetPosition: e.targetPosition ?? Position.Left,
    borderRadius: RADIUS,
    offset: STUB,
  })
  if (!path || /NaN/.test(path)) {
    const [p, lx, ly] = fallbackLinePath(sx, sy, tx, ty)
    return { path: p, labelX: lx, labelY: ly, back: false }
  }
  return { path, labelX, labelY, back: false }
}
