/** Lightweight JSON tokenizer + validity probe (aligned with page.html demo). */

export function escapeHtml(s: string): string {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/**
 * Syntax-highlight JSON source as HTML spans.
 * Does not pretty-print or alter whitespace — colors the original string only.
 */
export function highlightJson(src: string): string {
  let i = 0
  const n = src.length
  let out = ''

  function takeString(): string {
    const start = i
    i++
    while (i < n) {
      const c = src[i]
      if (c === '\\') {
        i += 2
        continue
      }
      if (c === '"') {
        i++
        break
      }
      i++
    }
    return src.slice(start, i)
  }

  while (i < n) {
    const ch = src[i]
    if (ch === '"') {
      const str = takeString()
      let j = i
      while (j < n && /\s/.test(src[j]!)) j++
      const isKey = src[j] === ':'
      out += `<span class="${isKey ? 'tok-key' : 'tok-str'}">${escapeHtml(str)}</span>`
      continue
    }
    if (/[0-9\-]/.test(ch!)) {
      const startN = i
      i++
      while (i < n && /[0-9.eE+\-]/.test(src[i]!)) i++
      out += `<span class="tok-num">${escapeHtml(src.slice(startN, i))}</span>`
      continue
    }
    if (/[a-zA-Z_]/.test(ch!)) {
      const startW = i
      i++
      while (i < n && /[a-zA-Z0-9_]/.test(src[i]!)) i++
      const word = src.slice(startW, i)
      const cls =
        word === 'true' || word === 'false' ? 'tok-bool' : word === 'null' ? 'tok-null' : 'tok-plain'
      out += `<span class="${cls}">${escapeHtml(word)}</span>`
      continue
    }
    if ('{}[]:,'.includes(ch!)) {
      out += `<span class="tok-punc">${escapeHtml(ch!)}</span>`
      i++
      continue
    }
    out += escapeHtml(ch!)
    i++
  }
  return out
}

type JsonParseState = {
  ok: boolean
  html: string
}

/** Probe with JSON.parse; on failure return escaped plain text (no throw). */
export function parseJsonState(content: string): JsonParseState {
  try {
    JSON.parse(content)
    return { ok: true, html: highlightJson(content) }
  } catch {
    return { ok: false, html: escapeHtml(content) }
  }
}

/** kind===json or name ending with .json (case-insensitive). */
export function isJsonArtifact(artifact: { kind?: string; name?: string } | null | undefined): boolean {
  if (!artifact) return false
  if (artifact.kind === 'json') return true
  const name = artifact.name ?? ''
  return name.toLowerCase().endsWith('.json')
}
