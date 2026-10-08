import { Marked, marked } from 'marked'
import DOMPurify from 'dompurify'

marked.setOptions({ breaks: true, gfm: true })

/** Max distinct source strings retained (LRU via Map insertion order). */
const CACHE_MAX = 64

const htmlCache = new Map<string, string>()
const chatHtmlCache = new Map<string, string>()

/** Test/observability: how many times marked+DOMPurify actually ran. */
let parseCount = 0

export function getMarkdownParseCount(): number {
  return parseCount
}

export function resetMarkdownParseCount(): void {
  parseCount = 0
}

export function clearMarkdownCache(): void {
  htmlCache.clear()
  chatHtmlCache.clear()
}

export function markdownCacheSize(): number {
  return htmlCache.size
}

/**
 * Render markdown → sanitized HTML with an LRU cache keyed by source text.
 * Identical inputs reuse the previous HTML and skip marked+DOMPurify.
 */
export function renderMarkdown(src: string): string {
  const key = src ?? ''
  const hit = htmlCache.get(key)
  if (hit !== undefined) {
    // Refresh LRU order
    htmlCache.delete(key)
    htmlCache.set(key, hit)
    return hit
  }
  parseCount += 1
  const raw = marked.parse(key, { async: false }) as string
  const html = DOMPurify.sanitize(raw)
  htmlCache.set(key, html)
  if (htmlCache.size > CACHE_MAX) {
    const oldest = htmlCache.keys().next().value
    if (oldest !== undefined) htmlCache.delete(oldest)
  }
  return html
}

/** Per-stream cache for renderMarkdownBlocks: `type\0raw` → sanitized block HTML. */
export type MarkdownBlockCache = Map<string, string>

/** Unused blocks kept beyond the live set before the cache is pruned. */
const BLOCK_SLACK = 64

/**
 * Split markdown into top-level blocks and render each one on its own.
 * While text streams only the last block keeps changing; every settled block
 * hits `cache` and returns the identical string, so Vue leaves its DOM (text
 * selection, images, diagrams) untouched instead of re-parsing the whole reply.
 */
export function renderMarkdownBlocks(src: string, cache: MarkdownBlockCache): string[] {
  const tokens = marked.lexer(src ?? '')
  const out: string[] = []
  const used = new Set<string>()
  for (const tok of tokens) {
    if (tok.type === 'space') continue
    const key = `${tok.type}\u0000${tok.raw}`
    let html = cache.get(key)
    if (html === undefined) {
      parseCount += 1
      const list = Object.assign([tok], { links: tokens.links }) as unknown as Parameters<typeof marked.parser>[0]
      html = DOMPurify.sanitize(marked.parser(list) as string)
      cache.set(key, html)
    }
    used.add(key)
    if (html) out.push(html)
  }
  if (cache.size > used.size + BLOCK_SLACK) {
    for (const k of cache.keys()) if (!used.has(k)) cache.delete(k)
  }
  return out
}

/** Visible text for a raw HTML token. Quotes included so attributes cannot reopen a tag. */
function escapeHtmlText(src: string): string {
  return src
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/**
 * Same breaks/gfm options as the shared marked instance, but raw HTML tokens
 * become text before DOMPurify. Fenced code, emphasis, lists and links stay
 * markdown. Artifact preview and node output keep using renderMarkdown.
 */
const chatMarked = new Marked({ breaks: true, gfm: true })
chatMarked.use({
  renderer: {
    html({ text }) {
      return escapeHtmlText(text)
    },
  },
})

/**
 * ReAct assistant markdown. Script, style, iframe, noscript and xmp keep
 * their inner text instead of being deleted with the tag.
 */
export function renderChatMarkdown(src: string): string {
  const key = src ?? ''
  const hit = chatHtmlCache.get(key)
  if (hit !== undefined) {
    chatHtmlCache.delete(key)
    chatHtmlCache.set(key, hit)
    return hit
  }
  parseCount += 1
  const raw = chatMarked.parse(key, { async: false }) as string
  const html = DOMPurify.sanitize(raw)
  chatHtmlCache.set(key, html)
  if (chatHtmlCache.size > CACHE_MAX) {
    const oldest = chatHtmlCache.keys().next().value
    if (oldest !== undefined) chatHtmlCache.delete(oldest)
  }
  return html
}

/** Block renderer for ReAct assistant streams. Same escaping as renderChatMarkdown. */
export function renderChatMarkdownBlocks(src: string, cache: MarkdownBlockCache): string[] {
  const tokens = chatMarked.lexer(src ?? '')
  const out: string[] = []
  const used = new Set<string>()
  for (const tok of tokens) {
    if (tok.type === 'space') continue
    const key = `${tok.type}\u0000${tok.raw}`
    let html = cache.get(key)
    if (html === undefined) {
      parseCount += 1
      const list = Object.assign([tok], { links: tokens.links }) as unknown as Parameters<typeof chatMarked.parser>[0]
      html = DOMPurify.sanitize(chatMarked.parser(list) as string)
      cache.set(key, html)
    }
    used.add(key)
    if (html) out.push(html)
  }
  if (cache.size > used.size + BLOCK_SLACK) {
    for (const k of cache.keys()) if (!used.has(k)) cache.delete(k)
  }
  return out
}
