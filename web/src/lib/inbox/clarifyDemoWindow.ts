/** Page-level enlarge window: centered in the viewport, draggable and resizable. */

export type DemoWindowBox = { x: number; y: number; w: number; h: number }

export type DemoWindowEdge = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw'

export const CLARIFY_DEMO_ESCAPE = 'grasp-clarify-demo-escape'

/** Default inset from each viewport edge. */
export const DEMO_WINDOW_MARGIN = 28
/** Tighter inset when a 28px margin would leave less than DEMO_WINDOW_USEFUL on either axis. */
export const DEMO_WINDOW_TIGHT_MARGIN = 8
const DEMO_WINDOW_USEFUL = 160
export const DEMO_WINDOW_MIN_W = 480
export const DEMO_WINDOW_MIN_H = 320

function roundBox(box: DemoWindowBox): DemoWindowBox {
  return {
    x: Math.round(box.x),
    y: Math.round(box.y),
    w: Math.round(Math.max(0, box.w)),
    h: Math.round(Math.max(0, box.h)),
  }
}

function viewportSize(viewport: { w: number; h: number }) {
  return { w: Math.max(0, viewport.w), h: Math.max(0, viewport.h) }
}

/** Margin is 8 when width−56 or height−56 would be under 160; otherwise 28. */
export function demoWindowMargin(viewport: { w: number; h: number }): number {
  const { w, h } = viewportSize(viewport)
  const usableW = w - DEMO_WINDOW_MARGIN * 2
  const usableH = h - DEMO_WINDOW_MARGIN * 2
  if (usableW < DEMO_WINDOW_USEFUL || usableH < DEMO_WINDOW_USEFUL) return DEMO_WINDOW_TIGHT_MARGIN
  return DEMO_WINDOW_MARGIN
}

/**
 * Default rectangle: viewport minus the margin on every side, so the window is centered.
 * Chat geometry is ignored; the window may cover the conversation.
 */
export function placeClarifyDemoWindow(viewport: { w: number; h: number }): DemoWindowBox {
  const { w: vw, h: vh } = viewportSize(viewport)
  const margin = demoWindowMargin({ w: vw, h: vh })
  return roundBox({
    x: margin,
    y: margin,
    w: vw - margin * 2,
    h: vh - margin * 2,
  })
}

/** Keep an already placed window inside the viewport without restoring the default size. */
export function clampDemoWindow(box: DemoWindowBox, viewport: { w: number; h: number }): DemoWindowBox {
  const { w: vw, h: vh } = viewportSize(viewport)
  const w = Math.min(Math.max(0, box.w), vw)
  const h = Math.min(Math.max(0, box.h), vh)
  const x = Math.min(Math.max(0, box.x), Math.max(0, vw - w))
  const y = Math.min(Math.max(0, box.y), Math.max(0, vh - h))
  return roundBox({ x, y, w, h })
}

/** Move by a pointer delta. Width and height stay put; the box is clamped to the viewport. */
export function moveDemoWindow(
  box: DemoWindowBox,
  dx: number,
  dy: number,
  viewport: { w: number; h: number },
): DemoWindowBox {
  return clampDemoWindow({ ...box, x: box.x + dx, y: box.y + dy }, viewport)
}

/** Floor is 480×320, or the viewport itself when the viewport is smaller. */
export function demoWindowMinSize(viewport: { w: number; h: number }): { w: number; h: number } {
  const { w, h } = viewportSize(viewport)
  return { w: Math.min(DEMO_WINDOW_MIN_W, w), h: Math.min(DEMO_WINDOW_MIN_H, h) }
}

function clampSpan(value: number, min: number, max: number): number {
  const hi = Math.max(0, max)
  const lo = Math.min(Math.max(0, min), hi)
  return Math.min(hi, Math.max(lo, value))
}

/**
 * Resize from one edge or corner. The opposite edge stays fixed.
 * The result stays inside the viewport and does not go under the minimum
 * unless the viewport itself is smaller than that minimum.
 */
export function resizeDemoWindow(
  start: DemoWindowBox,
  edge: DemoWindowEdge,
  dx: number,
  dy: number,
  viewport: { w: number; h: number },
): DemoWindowBox {
  const { w: vw, h: vh } = viewportSize(viewport)
  const { w: minW, h: minH } = demoWindowMinSize({ w: vw, h: vh })
  let { x, y, w, h } = start
  const touchesE = edge === 'e' || edge === 'ne' || edge === 'se'
  const touchesW = edge === 'w' || edge === 'nw' || edge === 'sw'
  const touchesS = edge === 's' || edge === 'se' || edge === 'sw'
  const touchesN = edge === 'n' || edge === 'ne' || edge === 'nw'

  if (touchesE) {
    const maxW = Math.max(0, vw - start.x)
    w = clampSpan(start.w + dx, Math.min(minW, maxW), maxW)
    x = start.x
  }
  if (touchesW) {
    const fixedRight = Math.min(Math.max(0, start.x + start.w), vw)
    const maxW = fixedRight
    const nextW = clampSpan(start.w - dx, Math.min(minW, maxW), maxW)
    x = fixedRight - nextW
    w = nextW
  }
  if (touchesS) {
    const maxH = Math.max(0, vh - start.y)
    h = clampSpan(start.h + dy, Math.min(minH, maxH), maxH)
    y = start.y
  }
  if (touchesN) {
    const fixedBottom = Math.min(Math.max(0, start.y + start.h), vh)
    const maxH = fixedBottom
    const nextH = clampSpan(start.h - dy, Math.min(minH, maxH), maxH)
    y = fixedBottom - nextH
    h = nextH
  }

  return roundBox(clampDemoWindow({ x, y, w, h }, { w: vw, h: vh }))
}

/** Srcdoc bridge so Escape inside the sandboxed preview iframe can close the window. */
export function withDemoEscapeBridge(html: string): string {
  if (!html || html.includes(CLARIFY_DEMO_ESCAPE)) return html
  return (
    html +
    `<script>document.addEventListener('keydown',function(e){if(e.key!=='Escape'&&e.key!=='Esc')return;e.preventDefault();try{parent.postMessage({type:'${CLARIFY_DEMO_ESCAPE}'},'*')}catch(err){}},true);<\/script>`
  )
}

export function isClarifyDemoEscapeMessage(data: unknown): boolean {
  return !!data && typeof data === 'object' && (data as { type?: unknown }).type === CLARIFY_DEMO_ESCAPE
}
