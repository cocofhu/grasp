import { Marked, Renderer, Tokenizer, marked } from 'marked'
import type { Tokens } from 'marked'
import DOMPurify from 'dompurify'

marked.setOptions({ breaks: true, gfm: true })

/**
 * RFC 3986 unreserved + reserved + "%". GFM bare links otherwise consume
 * every non-space after the host, so CJK and fullwidth punctuation stick
 * to the anchor. `]` is first so it stays literal inside the class.
 */
const BARE_URL_CHAR = /[[\]A-Za-z0-9._~:/?#@!$&'()*+,;=%-]/
const BARE_URL_SCHEME = /^(?:(?:ftp|https?):\/\/|www\.)/i
/** marked 15 GFM `_backpedal`: drop trailing ASCII punctuation, keep balanced parens. */
const BARE_URL_BACKPEDAL =
  /(?:[^?!.,:;*_'"~()&]+|\([^)]*\)|&(?![a-zA-Z0-9]+;$)|[?!.,:;*_'"~)]+(?!$))+/
/** marked's host shape, plus an IPv6 literal such as `[::1]`. */
const BARE_URL_HOST = /^(?:\[[0-9A-Fa-f:.]+\]|(?:[A-Za-z0-9-]+\.?)+)/

/**
 * Truncate a bare URL at the first non-URL character, then apply marked's
 * trailing-punctuation backpedal. Returns false so email autolinks still
 * use marked's own `url` tokenizer.
 */
function truncateBareUrl(src: string) {
  const scheme = BARE_URL_SCHEME.exec(src)
  if (!scheme) return false
  let end = scheme[0].length
  while (end < src.length && BARE_URL_CHAR.test(src.charAt(end))) end++
  if (end === scheme[0].length) return false

  let raw = src.slice(0, end)
  let prev = ''
  do {
    prev = raw
    raw = BARE_URL_BACKPEDAL.exec(raw)?.[0] ?? ''
  } while (raw !== prev)

  const trimmed = BARE_URL_SCHEME.exec(raw)
  if (!trimmed || !BARE_URL_HOST.test(raw.slice(trimmed[0].length))) return false

  // `www.` keeps marked's existing href prefix; the visible text stays as written.
  const href = trimmed[0] === 'www.' ? `http://${raw}` : raw
  return {
    type: 'link' as const,
    raw,
    text: raw,
    href,
    tokens: [{ type: 'text' as const, raw, text: raw }],
  }
}

/**
 * Han, kana, hangul and CJK / fullwidth punctuation. Curly quotes, `…` and `—`
 * are left out because English uses them too; they still count as punctuation.
 */
const CJK_CHAR =
  /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}\p{Script=Bopomofo}\u3000-\u303F\uFF00-\uFFEF]/u
const PUNCT = /[\p{P}\p{S}]/u
const SPACE = /\s/u
const LOW_SURROGATE = /^[\uDC00-\uDFFF]$/
const ASTERISK_RUN = /\*+/g

function charAfter(s: string, i: number): string {
  const cp = s.codePointAt(i)
  return cp === undefined ? '' : String.fromCodePoint(cp)
}

function charBefore(s: string, i: number): string {
  return Array.from(s.slice(Math.max(0, i - 2), i)).pop() ?? ''
}

/** CommonMark left-flanking. */
function strictOpen(prev: string, next: string): boolean {
  if (!next || SPACE.test(next)) return false
  return !PUNCT.test(next) || !prev || SPACE.test(prev) || PUNCT.test(prev)
}

/** CommonMark right-flanking. */
function strictClose(prev: string, next: string): boolean {
  if (!prev || SPACE.test(prev)) return false
  return !PUNCT.test(prev) || !next || SPACE.test(next) || PUNCT.test(next)
}

/** Left-flanking, also when the punctuation after the run is CJK or follows CJK text. */
function canOpen(prev: string, next: string): boolean {
  return strictOpen(prev, next) || (PUNCT.test(next) && (CJK_CHAR.test(prev) || CJK_CHAR.test(next)))
}

/** Right-flanking, also when the punctuation before the run is CJK or precedes CJK text. */
function canClose(prev: string, next: string): boolean {
  return strictClose(prev, next) || (PUNCT.test(prev) && (CJK_CHAR.test(prev) || CJK_CHAR.test(next)))
}

/**
 * CommonMark will not close `**结论：**正文` or open `是**“关键”**步骤`:
 * a delimiter touching punctuation needs whitespace or punctuation on its
 * other side, which Chinese text never has. Runs only after marked's own
 * emStrong fails, and only takes a pair whose opener or closer needs the
 * relaxed rule; when both are valid CommonMark, marked rejected the pair for
 * another reason (e.g. an unmatched opener), so its result stands.
 */
function cjkEmStrong(
  this: Tokenizer,
  src: string,
  maskedSrc: string,
  prevChar = '',
): Tokens.Em | Tokens.Strong | undefined {
  const native = Tokenizer.prototype.emStrong.call(this, src, maskedSrc, prevChar)
  if (native) return native

  // marked retries from the second `*` of a failed `**`; that is not a new run.
  if (prevChar === '*') return
  const open = /^\*{1,2}(?!\*)/.exec(src)
  if (!open) return
  const n = open[0].length
  const full = maskedSrc ?? src
  const masked = full.slice(-src.length)
  // marked passes one UTF-16 unit; recover a supplementary-plane Han character.
  const prev = LOW_SURROGATE.test(prevChar) ? charBefore(full, full.length - src.length) || prevChar : prevChar
  const first = charAfter(src, n)
  if (!canOpen(prev, first)) return
  const openStrict = strictOpen(prev, first)

  // Same-length runs nest like CommonMark: an inner opener takes the next closer.
  let depth = 0
  ASTERISK_RUN.lastIndex = n
  let run: RegExpExecArray | null
  while ((run = ASTERISK_RUN.exec(masked)) !== null) {
    if (run[0].length !== n) continue
    const at = run.index
    const before = charBefore(src, at)
    const after = charAfter(src, at + n)
    if (!canClose(before, after)) {
      if (canOpen(before, after)) depth++
      continue
    }
    if (depth > 0) {
      depth--
      continue
    }
    if (openStrict && strictClose(before, after)) return
    const text = src.slice(n, at)
    return {
      type: n === 2 ? 'strong' : 'em',
      raw: src.slice(0, at + n),
      text,
      tokens: this.lexer.inlineTokens(text),
    }
  }
}

marked.use({
  tokenizer: {
    url(src: string) {
      return truncateBareUrl(src)
    },
    emStrong: cjkEmStrong,
  },
})

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
  tokenizer: {
    url(src: string) {
      return truncateBareUrl(src)
    },
    emStrong: cjkEmStrong,
  },
  renderer: {
    html({ text }) {
      return escapeHtmlText(text)
    },
    link(token) {
      return Renderer.prototype.link.call(this, token).replace(/^<a /, '<a target="_blank" rel="noopener noreferrer" ')
    },
  },
  extensions: [
    {
      name: 'tightHeading',
      level: 'block',
      // marked passes the paragraph minus its first char; a heading may only
      // interrupt it at a line start, never mid-line as in `@#815`.
      start(src) {
        const m = /\n {0,3}#{1,6}[^\s#]/.exec(src)
        return m ? m.index + 1 : undefined
      },
      tokenizer(src) {
        const cap = /^ {0,3}(#{1,6})([^\s#].*?)(?:\n+|$)/.exec(src)
        if (!cap) return
        let text = cap[2].trim()
        if (/#$/.test(text)) {
          const trimmed = text.replace(/#+$/, '')
          if (!trimmed || / $/.test(trimmed)) text = trimmed.trim()
        }
        return {
          type: 'heading',
          raw: cap[0],
          depth: cap[1].length,
          text,
          tokens: this.lexer.inlineTokens(text),
        }
      },
    },
  ],
})

/** Raw HTML is already text here, so `target` can only come from the link renderer. */
const CHAT_SANITIZE = { ADD_ATTR: ['target'] }

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
  const html = DOMPurify.sanitize(raw, CHAT_SANITIZE)
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
      html = DOMPurify.sanitize(chatMarked.parser(list) as string, CHAT_SANITIZE)
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
