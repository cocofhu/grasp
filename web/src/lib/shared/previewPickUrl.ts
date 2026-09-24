import type { ReactAnnotation } from '@/lib/shared/types'

/**
 * Path (+ search + hash) from a full href for compact pick/chip labels.
 * Returns "" when href is empty or not a valid absolute URL.
 */
export function previewPickPath(href: string | undefined | null): string {
  const raw = (href || '').trim()
  if (!raw) return ''
  try {
    const u = new URL(raw)
    return `${u.pathname}${u.search}${u.hash}` || '/'
  } catch {
    // Relative or opaque — treat as already-path-like.
    if (raw.startsWith('/')) return raw
    return ''
  }
}

/** Chip / annotation label: `path · selector`, or selector when path missing. */
export function previewPickLabel(
  href: string | undefined | null,
  selector: string,
  tagName?: string,
): string {
  const sel = (selector || '').trim() || (tagName || '').trim()
  const path = previewPickPath(href)
  if (path && sel) return `${path} · ${sel}`
  return sel || path
}

export type AppPreviewPickPayload = {
  selector: string
  tagName: string
  outerHTML: string
  /** Visible text excerpt; tells repeated components apart. */
  text?: string
  url?: string
}

/** Matches the server clamp in models.RenderAnnotations. */
export const PICK_TEXT_MAX = 120
export const PICK_HTML_MAX = 1024

function clip(s: string | undefined, max: number): string | undefined {
  const v = (s || '').trim()
  if (!v) return undefined
  return v.length > max ? `${v.slice(0, max)}…` : v
}

/** DOM pick → review annotation (chip label plus the element context the agent needs). */
export function previewPickAnnotation(payload: AppPreviewPickPayload): ReactAnnotation {
  const url = (payload.url || '').trim()
  const tagName = (payload.tagName || '').trim().toLowerCase()
  return {
    selector: payload.selector,
    url: url || undefined,
    label: previewPickLabel(url, payload.selector, payload.tagName),
    tagName: tagName || undefined,
    text: clip(payload.text?.replace(/\s+/g, ' '), PICK_TEXT_MAX),
    outerHTML: clip(payload.outerHTML, PICK_HTML_MAX),
  }
}
