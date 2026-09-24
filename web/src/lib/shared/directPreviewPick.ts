/** postMessage types for IP-direct preview cooperative pick.js. */

export const DIRECT_PREVIEW_READY = 'direct-preview-ready'
export const DIRECT_PREVIEW_URL = 'direct-preview-url'
export const DIRECT_PREVIEW_PICKED = 'direct-preview-picked'
export const DIRECT_PREVIEW_CANCELED = 'direct-preview-canceled'
export const DIRECT_PREVIEW_INSPECT = 'direct-preview-inspect'
export const DIRECT_PREVIEW_NAV = 'direct-preview-nav'
/** Parent asks the page to re-announce ready; answers with DIRECT_PREVIEW_READY. */
export const DIRECT_PREVIEW_PING = 'direct-preview-ping'
/**
 * Grasp frame hello. Until the page receives it from its parent, the in-page
 * Pick bar keeps picks local (standalone window or an unknown embedder).
 */
export const DIRECT_PREVIEW_HOST = 'direct-preview-host'
/** Page reports that its own Pick toggle turned inspect mode on/off. */
export const DIRECT_PREVIEW_INSPECT_STATE = 'direct-preview-inspect-state'

export type DirectPreviewNavAction = 'back' | 'forward' | 'reload'

export type DirectPreviewReadyMessage = {
  type: typeof DIRECT_PREVIEW_READY
  url: string
}

export type DirectPreviewUrlMessage = {
  type: typeof DIRECT_PREVIEW_URL
  url: string
}

export type DirectPreviewPickedMessage = {
  type: typeof DIRECT_PREVIEW_PICKED
  selector: string
  tagName: string
  outerHTML: string
  text?: string
  url?: string
}

export type DirectPreviewCanceledMessage = {
  type: typeof DIRECT_PREVIEW_CANCELED
}

export type DirectPreviewInspectStateMessage = {
  type: typeof DIRECT_PREVIEW_INSPECT_STATE
  on: boolean
}

export type DirectPreviewMessage =
  | DirectPreviewReadyMessage
  | DirectPreviewUrlMessage
  | DirectPreviewPickedMessage
  | DirectPreviewCanceledMessage
  | DirectPreviewInspectStateMessage

export function iframeOrigin(directUrl: string): string {
  try {
    return new URL(directUrl).origin
  } catch {
    return ''
  }
}

export function isDirectPreviewOrigin(directUrl: string, origin: string): boolean {
  const want = iframeOrigin(directUrl)
  return !!want && origin === want
}

/** Resolve address-bar input to a same-origin http(s) URL, or null. */
export function resolveDirectPreviewGoto(directUrl: string, input: string): string | null {
  const origin = iframeOrigin(directUrl)
  if (!origin) return null
  const raw = (input || '').trim()
  if (!raw) return null
  let next: URL
  try {
    next = raw.startsWith('/') ? new URL(raw, origin) : new URL(raw)
  } catch {
    return null
  }
  if (next.origin !== origin) return null
  if (next.protocol !== 'http:' && next.protocol !== 'https:') return null
  return next.href
}

export function parseDirectPreviewMessage(data: unknown): DirectPreviewMessage | null {
  if (!data || typeof data !== 'object') return null
  const msg = data as Record<string, unknown>
  const type = msg.type
  if (type === DIRECT_PREVIEW_READY || type === DIRECT_PREVIEW_URL) {
    const url = typeof msg.url === 'string' ? msg.url : ''
    if (!url) return null
    return { type, url } as DirectPreviewReadyMessage | DirectPreviewUrlMessage
  }
  if (type === DIRECT_PREVIEW_CANCELED) {
    return { type: DIRECT_PREVIEW_CANCELED }
  }
  if (type === DIRECT_PREVIEW_INSPECT_STATE) {
    return { type: DIRECT_PREVIEW_INSPECT_STATE, on: msg.on === true }
  }
  if (type === DIRECT_PREVIEW_PICKED) {
    const selector = typeof msg.selector === 'string' ? msg.selector : ''
    const tagName = typeof msg.tagName === 'string' ? msg.tagName : ''
    const outerHTML = typeof msg.outerHTML === 'string' ? msg.outerHTML : ''
    if (!selector && !tagName) return null
    const url = typeof msg.url === 'string' ? msg.url : undefined
    const text = typeof msg.text === 'string' && msg.text ? msg.text : undefined
    return { type: DIRECT_PREVIEW_PICKED, selector, tagName, outerHTML, text, url }
  }
  return null
}
