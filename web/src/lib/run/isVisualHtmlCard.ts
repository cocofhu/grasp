import type { OutputCard } from '@/lib/shared/types'

/** Fields the visual-HTML recognizer needs — keeps the helper a pure function. */
export type VisualHtmlCardInput = Pick<
  OutputCard,
  'outputKey' | 'artifactName' | 'structuredArtifactName' | 'markdown' | 'jsonSnapshot'
>

/** page.html, or any name ending in .html / .htm (case-insensitive). */
export function isHtmlArtifactName(name?: string): boolean {
  if (!name) return false
  const n = name.trim().toLowerCase()
  return n === 'page.html' || n.endsWith('.html') || n.endsWith('.htm')
}

/**
 * Full HTML document sniff: leading whitespace stripped, then
 * `<!doctype html` or `<html` start tag (case-insensitive).
 * Fragments that only start with `<div` must not match.
 */
export function looksLikeFullHtmlDocument(body?: string): boolean {
  if (!body) return false
  const s = body.replace(/^\s+/, '')
  return /^<!doctype\s+html\b/i.test(s) || /^<html[\s>]/i.test(s)
}

export function parseOutputCardDoc(card: Pick<OutputCard, 'jsonSnapshot'>): unknown {
  const raw = card.jsonSnapshot?.trim()
  if (!raw) return null
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

export type IsVisualHtmlCardOptions = {
  /** Already-fetched artifact body, if any. */
  artifactHtml?: string
  /**
   * Pre-parsed jsonSnapshot. Pass `null` when parse failed / missing.
   * Omit to let the helper parse jsonSnapshot itself.
   */
  parsedDoc?: unknown
}

/**
 * Clarification three-rule recognizer: any hit is a visual HTML card.
 * (1) outputKey === page
 * (2) artifactName is page.html or ends with .html/.htm
 * (3) available body (fetched artifact, else markdown) looks like a full HTML document
 *
 * Real structured JSON (structuredArtifactName + successful parse)
 * stays on StructuredArtifactView and is never sniffed into a preview.
 */
export function isVisualHtmlCard(
  card: VisualHtmlCardInput,
  options?: IsVisualHtmlCardOptions,
): boolean {
  const parsed = options && 'parsedDoc' in options ? options.parsedDoc : parseOutputCardDoc(card)
  if (card.structuredArtifactName && parsed != null) return false
  if (card.outputKey === 'page') return true
  if (isHtmlArtifactName(card.artifactName)) return true
  const available = options?.artifactHtml?.trim() ? options.artifactHtml : card.markdown
  return looksLikeFullHtmlDocument(available)
}
