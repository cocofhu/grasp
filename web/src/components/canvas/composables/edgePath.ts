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

export interface LaneNode {
  id: string
  x: number
  y: number
  /** Card width. When omitted, the widest standard card is assumed. */
  width?: number
}

export interface LaneEdge {
  id: string
  source: string
  target: string
}

/** Forward smooth-step offset. Backward edges no longer use this as a rightward stub. */
const STUB = 24
/** Short lead out of the source port, long enough for the corner and no further. */
const EXIT = 12
/** Short run into the target, sitting in the gap beside the card. */
const ENTRY = 32
const LANE_GAP = 40
const LANE_PITCH = 36
const RISE_STAGGER = 16
const RADIUS = 10
/** Nodes this close in y can share a horizontal channel only when their strokes miss. */
const LANE_Y_BAND = 160
/** Widest standard card. A back stroke runs under the source card, past its top-left corner. */
const CARD_WIDTH = 240

/** A connection whose target port sits to the left of its source. */
export function isBackward(e: EdgeEnds): boolean {
  return Number.isFinite(e.sourceX) && Number.isFinite(e.targetX) && e.targetX < e.sourceX
}

function laneIndexOf(laneIndex: number): number {
  if (!Number.isFinite(laneIndex) || laneIndex <= 0) return 0
  return Math.floor(laneIndex)
}

function step(x: number, y: number, pos: Position | undefined, dist: number): [number, number] {
  switch (pos) {
    case Position.Left:
      return [x - dist, y]
    case Position.Top:
      return [x, y - dist]
    case Position.Bottom:
      return [x, y + dist]
    default:
      return [x + dist, y]
  }
}

function dedupeCollinear(pts: [number, number][]): [number, number][] {
  const out: [number, number][] = []
  for (const p of pts) {
    const n = out.length
    if (n >= 2) {
      const a = out[n - 2]!
      const b = out[n - 1]!
      const cross = (b[0] - a[0]) * (p[1] - b[1]) - (b[1] - a[1]) * (p[0] - b[0])
      if (Math.abs(cross) < 1e-6) {
        out[n - 1] = p
        continue
      }
    }
    const prev = out[n - 1]
    if (prev && prev[0] === p[0] && prev[1] === p[1]) continue
    out.push(p)
  }
  return out
}

export interface BackRoute {
  points: [number, number][]
  lane: number
}

/**
 * Orthogonal back route: a short exit stub, one drop to a lane under both cards,
 * a run to the gap beside the target, then a short entry. Higher `laneIndex`
 * values sit on a lower lane and rise on a different x so the strokes do not stack.
 */
export function backEdgeRoute(e: EdgeEnds, source?: NodeBox, target?: NodeBox, laneIndex = 0): BackRoute {
  const { sourceX: sx, sourceY: sy, targetX: tx, targetY: ty } = e
  const index = laneIndexOf(laneIndex)
  const bottom = Math.max(source ? source.y + source.height : sy, target ? target.y + target.height : ty, sy, ty)
  const lane = bottom + LANE_GAP + index * LANE_PITCH
  const srcPos = e.sourcePosition ?? Position.Right
  const tgtPos = e.targetPosition ?? Position.Left
  const [ox, oy] = step(sx, sy, srcPos, EXIT)
  const [ax, ay] = step(tx, ty, tgtPos, ENTRY + index * RISE_STAGGER)
  const points = dedupeCollinear([
    [sx, sy],
    [ox, oy],
    [ox, lane],
    [ax, lane],
    [ax, ay],
    [tx, ty],
  ])
  return { points, lane }
}

function laneMidX(points: [number, number][], lane: number, sx: number, tx: number): number {
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1]!
    const b = points[i]!
    if (a[1] === lane && b[1] === lane && a[0] !== b[0]) return (a[0] + b[0]) / 2
  }
  return (sx + tx) / 2
}

/**
 * Rounded orthogonal path. Forward edges keep the smooth step. Backward edges
 * leave the source by a short stub, drop below both cards, and enter the target
 * from the gap on its near side.
 */
export function edgeGeometry(e: EdgeEnds, source?: NodeBox, target?: NodeBox, laneIndex = 0): EdgeGeometry {
  const { sourceX: sx, sourceY: sy, targetX: tx, targetY: ty } = e
  if (!coordsFinite(sx, sy, tx, ty)) {
    const [path, labelX, labelY] = fallbackLinePath(sx, sy, tx, ty)
    return { path, labelX, labelY, back: false }
  }
  if (isBackward(e)) {
    const route = backEdgeRoute(e, source, target, laneIndex)
    const path = roundedPolyline(route.points, RADIUS)
    return {
      path: path || fallbackLinePath(sx, sy, tx, ty)[0],
      labelX: laneMidX(route.points, route.lane, sx, tx),
      labelY: route.lane,
      back: true,
    }
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

function rangesOverlap(a0: number, a1: number, b0: number, b1: number): boolean {
  const lo = Math.max(Math.min(a0, a1), Math.min(b0, b1))
  const hi = Math.min(Math.max(a0, a1), Math.max(b0, b1))
  // Touching counts: a shared endpoint still stacks the two strokes on one line.
  return hi - lo >= -0.5
}

function cardWidth(n: LaneNode): number {
  return n.width && n.width > 0 ? n.width : CARD_WIDTH
}

/**
 * X range of the horizontal stroke, not the two top-left corners.
 * The stroke drops just past the source port (right edge of the source card)
 * and rises in the entry gap left of the target port.
 */
function backStrokeX(source: LaneNode, target: LaneNode): { x0: number; x1: number } {
  const left = target.x - ENTRY
  const right = source.x + cardWidth(source) + EXIT
  return left <= right ? { x0: left, x1: right } : { x0: right, x1: left }
}

/**
 * Lane index for each edge whose target node sits left of its source.
 * Strokes that overlap or meet on the same row get distinct indexes; separated strokes reuse 0.
 */
export function backLaneIndexes(nodes: LaneNode[], edges: LaneEdge[]): Map<string, number> {
  const pos = new Map(nodes.map((n) => [n.id, n]))
  const back = edges.filter((e) => {
    const s = pos.get(e.source)
    const t = pos.get(e.target)
    return !!s && !!t && t.x < s.x
  })
  back.sort((a, b) => {
    const as = pos.get(a.source)!
    const at = pos.get(a.target)!
    const bs = pos.get(b.source)!
    const bt = pos.get(b.target)!
    const ay = Math.min(as.y, at.y)
    const by = Math.min(bs.y, bt.y)
    if (ay !== by) return ay - by
    if (as.x !== bs.x) return as.x - bs.x
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0
  })
  const used: { x0: number; x1: number; y0: number; y1: number; lane: number }[] = []
  const out = new Map<string, number>()
  for (const e of back) {
    const s = pos.get(e.source)!
    const t = pos.get(e.target)!
    const { x0, x1 } = backStrokeX(s, t)
    const y0 = Math.min(s.y, t.y)
    const y1 = Math.max(s.y, t.y)
    const taken = new Set<number>()
    for (const o of used) {
      const yClose = y0 <= o.y1 + LANE_Y_BAND && y1 >= o.y0 - LANE_Y_BAND
      if (yClose && rangesOverlap(x0, x1, o.x0, o.x1)) taken.add(o.lane)
    }
    let lane = 0
    while (taken.has(lane)) lane += 1
    used.push({ x0, x1, y0, y1, lane })
    out.set(e.id, lane)
  }
  return out
}
