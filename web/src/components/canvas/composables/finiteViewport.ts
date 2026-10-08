/** A canvas viewport. x/y are pan, zoom is the scale. */
export interface FlowViewport {
  x: number
  y: number
  zoom: number
}

/** Used when the canvas has never seen a finite viewport. */
export const DEFAULT_FLOW_VIEWPORT: FlowViewport = { x: 0, y: 0, zoom: 1 }

/** Dot spacing and diameter, matching the previous Background props. */
export const DOT_GAP = 16
export const DOT_SIZE = 1.2

export interface NodeBounds {
  x: number
  y: number
  width: number
  height: number
}

export interface DotGeometry {
  cx: number
  cy: number
  r: number
  x: number
  y: number
  width: number
  height: number
  /** Library offset when the offset prop is 0: 1 + half the scaled gap. */
  transform: string
}

function finite(n: number): boolean {
  return Number.isFinite(n)
}

/**
 * Keep a whole viewport or reject it.
 * One non-finite component, or a zoom that is not positive, falls back to the
 * previous finite viewport so pan and zoom never come from different frames.
 */
export function finiteViewport(input: FlowViewport, previous?: FlowViewport | null): FlowViewport {
  const fallback = previous ?? DEFAULT_FLOW_VIEWPORT
  if (finite(input.x) && finite(input.y) && finite(input.zoom) && input.zoom > 0) {
    return { x: input.x, y: input.y, zoom: input.zoom }
  }
  return { x: fallback.x, y: fallback.y, zoom: fallback.zoom }
}

/** Union of node boxes. Returns null when any edge of the union is not finite. */
export function unionBounds(boxes: NodeBounds[]): NodeBounds | null {
  if (!boxes.length) return null
  let x = Infinity
  let y = Infinity
  let x2 = -Infinity
  let y2 = -Infinity
  for (const box of boxes) {
    if (!finite(box.x) || !finite(box.y) || !finite(box.width) || !finite(box.height)) return null
    x = Math.min(x, box.x)
    y = Math.min(y, box.y)
    x2 = Math.max(x2, box.x + box.width)
    y2 = Math.max(y2, box.y + box.height)
  }
  const width = x2 - x
  const height = y2 - y
  if (!finite(x) || !finite(y) || !finite(width) || !finite(height)) return null
  return { x, y, width, height }
}

/**
 * Same transform @vue-flow/core writes from fitView.
 * Null means the result must not be committed.
 */
export function fitTransform(
  bounds: NodeBounds,
  width: number,
  height: number,
  minZoom: number,
  maxZoom: number,
  padding: number,
): FlowViewport | null {
  if (![bounds.x, bounds.y, bounds.width, bounds.height, width, height, minZoom, maxZoom, padding].every(finite)) {
    return null
  }
  if (bounds.width === 0 || bounds.height === 0 || width <= 0 || height <= 0) return null
  const xZoom = width / (bounds.width * (1 + padding))
  const yZoom = height / (bounds.height * (1 + padding))
  const zoom = Math.min(Math.max(Math.min(xZoom, yZoom), minZoom), maxZoom)
  const centerX = bounds.x + bounds.width / 2
  const centerY = bounds.y + bounds.height / 2
  const x = width / 2 - centerX * zoom
  const y = height / 2 - centerY * zoom
  if (!finite(x) || !finite(y) || !finite(zoom) || zoom <= 0) return null
  return { x, y, zoom }
}

/**
 * Dot pattern geometry for a viewport.
 * Lengths follow @vue-flow/background: gap*zoom (or 1), radius = size*zoom/2,
 * pattern x/y = pan modulo the scaled gap, and the zero-offset translate.
 */
export function dotPatternGeometry(viewport: FlowViewport, previous?: FlowViewport | null): DotGeometry {
  const vp = finiteViewport(viewport, previous)
  const scaledGapX = DOT_GAP * vp.zoom || 1
  const scaledGapY = DOT_GAP * vp.zoom || 1
  const radius = (DOT_SIZE * vp.zoom) / 2
  const offsetX = 1 + scaledGapX / 2
  const offsetY = 1 + scaledGapY / 2
  return {
    cx: radius,
    cy: radius,
    r: radius,
    x: vp.x % scaledGapX,
    y: vp.y % scaledGapY,
    width: scaledGapX,
    height: scaledGapY,
    transform: `translate(-${offsetX},-${offsetY})`,
  }
}
