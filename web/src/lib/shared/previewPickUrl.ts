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


export function appendPreviewPick(picks: ReactAnnotation[], payload: AppPreviewPickPayload, limit: number) {
  if (picks.length >= limit) return
  const pick = previewPickAnnotation({
    ...payload,
    selector: payload.selector.trim().slice(0, 1024),
    url: payload.url?.slice(0, 2048),
    tagName: payload.tagName.slice(0, 32),
  })
  if (pick.selector && !picks.some(row => row.selector === pick.selector && row.url === pick.url)) picks.push(pick)
}

/** Old preview clients saved a machine-generated footer inside the user's text. */
export function readPreviewMessage(text: string, annotations?: ReactAnnotation[]) {
  const message = { text, annotations: annotations || [] }
  if (annotations?.length) return message
  const marker = 'Selected page elements:\n'
  const offset = text.lastIndexOf(marker)
  if (offset < 0 || (offset > 0 && text.slice(offset - 2, offset) !== '\n\n')) return message
  const lines = text.slice(offset + marker.length).trim().split('\n')
  if (lines.length > 8) return message
  const picks: ReactAnnotation[] = []
  for (const line of lines) {
    if (line.startsWith('{')) {
      try {
        const { selector, label } = JSON.parse(line) as ReactAnnotation
        if (typeof selector !== 'string' || !selector || typeof label !== 'string') return message
        picks.push({ selector, label })
      } catch {
        return message
      }
    } else {
      const separator = line.startsWith('/') ? line.indexOf(' · ') : -1
      const selector = (separator >= 0 ? line.slice(separator + 3) : line).trim()
      if (!/^(?:[#.\[]\S|(?:body|html)(?:$|[.# >]))/.test(selector)) return message
      picks.push({ selector, label: line })
    }
  }
  return { text: text.slice(0, offset).trimEnd(), annotations: picks }
}
