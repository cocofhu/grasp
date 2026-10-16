/** Page-level enlarge window: sits beside the chat and does not cover it. */

export type DemoWindowBox = { x: number; y: number; w: number; h: number }

export const CLARIFY_DEMO_ESCAPE = 'grasp-clarify-demo-escape'

const SIDE_MARGIN = 28
const CHAT_GAP = 16
const TOP = 72
const BOTTOM = 28
const MAX_W = 560
const MAX_H = 520
const MIN_USEFUL = 200

function roundBox(box: DemoWindowBox): DemoWindowBox {
  return {
    x: Math.round(box.x),
    y: Math.round(box.y),
    w: Math.round(Math.max(0, box.w)),
    h: Math.round(Math.max(0, box.h)),
  }
}

/** Beside the chat when that side is wide enough that the window need not cover it. */
function placeBeside(
  viewport: { w: number; h: number },
  chat: { x: number; y: number; w: number; h: number },
): DemoWindowBox | null {
  const vw = Math.max(0, viewport.w)
  const vh = Math.max(0, viewport.h)
  const top = vh > 160 ? Math.min(TOP, Math.max(16, vh - 160)) : 8
  const leftRoom = Math.max(0, chat.x)
  const rightEdge = chat.x + chat.w
  const rightRoom = Math.max(0, vw - rightEdge)
  const placeLeft = leftRoom >= rightRoom
  const room = placeLeft ? leftRoom : rightRoom
  const usable = Math.max(0, room - CHAT_GAP - SIDE_MARGIN)
  const w = Math.min(MAX_W, usable)
  const h = Math.min(MAX_H, Math.max(0, vh - top - BOTTOM))
  if (w < MIN_USEFUL || h < 160) return null
  let x = placeLeft ? Math.max(SIDE_MARGIN, chat.x - CHAT_GAP - w) : rightEdge + CHAT_GAP
  if (placeLeft && x + w > chat.x - CHAT_GAP) x = Math.max(0, chat.x - CHAT_GAP - w)
  if (!placeLeft && x + w > vw - SIDE_MARGIN) x = Math.max(rightEdge + CHAT_GAP, vw - SIDE_MARGIN - w)
  const box = roundBox({ x, y: top, w, h })
  return demoWindowOverlapsChat(box, chat) ? null : box
}

/** Above or below a full-width chat column (mobile drawer), still outside that column. */
function placeVertical(
  viewport: { w: number; h: number },
  chat: { x: number; y: number; w: number; h: number },
): DemoWindowBox | null {
  const vw = Math.max(0, viewport.w)
  const vh = Math.max(0, viewport.h)
  const above = Math.max(0, chat.y - CHAT_GAP - 16)
  const below = Math.max(0, vh - (chat.y + chat.h) - CHAT_GAP - 16)
  const placeAbove = above >= below
  const room = placeAbove ? above : below
  const h = Math.min(MAX_H, room)
  if (h < 160) return null
  const w = Math.min(MAX_W, Math.max(0, vw - SIDE_MARGIN * 2))
  if (w < MIN_USEFUL) return null
  const x = Math.max(SIDE_MARGIN, (vw - w) / 2)
  const y = placeAbove ? Math.max(16, chat.y - CHAT_GAP - h) : chat.y + chat.h + CHAT_GAP
  const box = roundBox({ x, y, w, h })
  return demoWindowOverlapsChat(box, chat) ? null : box
}

/**
 * Place one window outside the chat column.
 * Side room wins; otherwise the free band above or below the column.
 * There is never a full-page mask, so uncovered chat controls stay clickable.
 */
export function placeClarifyDemoWindow(
  viewport: { w: number; h: number },
  chat: { x: number; y: number; w: number; h: number },
): DemoWindowBox {
  return (
    placeBeside(viewport, chat) ??
    placeVertical(viewport, chat) ??
    roundBox({ x: SIDE_MARGIN, y: TOP, w: 0, h: 0 })
  )
}

export function demoWindowOverlapsChat(
  box: DemoWindowBox,
  chat: { x: number; y: number; w: number; h: number },
): boolean {
  if (box.w <= 0 || box.h <= 0 || chat.w <= 0 || chat.h <= 0) return false
  return box.x < chat.x + chat.w && box.x + box.w > chat.x && box.y < chat.y + chat.h && box.y + box.h > chat.y
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
