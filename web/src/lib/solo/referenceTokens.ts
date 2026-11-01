import type { MessageReference } from '@/lib/api/apiTypes'

/** A picked reference and the `@label` span [start, end) it owns in the text. */
export type ReferenceToken = { ref: MessageReference; start: number; end: number }

export const MAX_REFERENCES = 5

const GITHUB_URL = /^https?:\/\/github\.com\/([\w.-]+)\/([\w.-]+)\/(issues|pull)\/(\d+)/i
const GITHUB_SHORT = /^([\w.-]+)\/([\w.-]+)#(\d+)$/

/** A pasted GitHub URL or owner/repo#n, normalized to the server's target form. */
export function parseGitHubReference(text: string): ReferenceInput | null {
  const t = text.trim()
  let m = t.match(GITHUB_URL)
  if (m) return { kind: m[3]!.toLowerCase() === 'pull' ? 'github_pr' : 'github_issue', target: `${m[1]}/${m[2]}#${Number(m[4])}` }
  m = t.match(GITHUB_SHORT)
  if (m) return { kind: 'github_issue', target: `${m[1]}/${m[2]}#${Number(m[3])}` }
  return null
}
type ReferenceInput = Pick<MessageReference, 'kind' | 'target'>

/** Text inserted after @: a session title, or #n for the session's own repo. */
export function referenceLabel(ref: Pick<MessageReference, 'kind' | 'target' | 'title'>, defaultRepo = ''): string {
  if (ref.kind === 'solo') return ref.title.replace(/\s+/g, ' ').trim() || ref.target
  const [repo, n] = ref.target.split('#')
  return defaultRepo && repo?.toLowerCase() === defaultRepo.toLowerCase() ? `#${n}` : ref.target
}

/**
 * Shift token spans across one edit. An edit that touches a token's span
 * (even one character of it) removes that reference.
 */
export function applyEdit(tokens: ReferenceToken[], before: string, after: string): ReferenceToken[] {
  if (before === after || !tokens.length) return tokens
  let prefix = 0
  const limit = Math.min(before.length, after.length)
  while (prefix < limit && before[prefix] === after[prefix]) prefix++
  let suffix = 0
  while (suffix < limit - prefix && before[before.length - 1 - suffix] === after[after.length - 1 - suffix]) suffix++
  const editEnd = before.length - suffix
  const delta = after.length - before.length
  const out: ReferenceToken[] = []
  for (const token of tokens) {
    if (token.end <= prefix) out.push(token)
    else if (token.start >= editEnd) out.push({ ...token, start: token.start + delta, end: token.end + delta })
  }
  return out
}

/** Replace the `@query` at [start, end) with `@label ` and track the new span. */
export function insertToken(text: string, start: number, end: number, ref: MessageReference, label: string) {
  const lead = start > 0 && !/\s/.test(text[start - 1]!) ? ' ' : ''
  const mention = `@${label}`
  const next = text.slice(0, start) + lead + mention + ' ' + text.slice(end).replace(/^ /, '')
  const tokenStart = start + lead.length
  return { text: next, token: { ref, start: tokenStart, end: tokenStart + mention.length }, caret: tokenStart + mention.length + 1 }
}

/**
 * Rebuild tokens for a sent message: each reference claims the first free
 * `@label` in the text; one without a match is appended so it is not lost.
 */
export function restoreTokens(text: string, refs: MessageReference[], labelOf: (ref: MessageReference) => string) {
  let out = text
  const tokens: ReferenceToken[] = []
  const missing: { ref: MessageReference; label: string }[] = []
  for (const ref of refs.slice(0, MAX_REFERENCES)) {
    const label = labelOf(ref)
    const mention = `@${label}`
    let start = out.indexOf(mention)
    while (start >= 0) {
      const end = start + mention.length
      const free = !tokens.some((t) => start < t.end && end > t.start)
      const before = start === 0 || /\s/.test(out[start - 1]!)
      const after = end === out.length || !/[\p{L}\p{N}_]/u.test(out[end]!)
      if (free && before && after) break
      start = out.indexOf(mention, start + 1)
    }
    if (start >= 0) tokens.push({ ref, start, end: start + mention.length })
    else missing.push({ ref, label })
  }
  for (const { ref, label } of missing) {
    const res = insertToken(out, out.length, out.length, ref, label)
    out = res.text
    tokens.push(res.token)
  }
  return { text: out, tokens: tokens.sort((a, b) => a.start - b.start) }
}

/** Remove one token's `@label` (and one following space) from the text. */
export function removeToken(text: string, tokens: ReferenceToken[], index: number) {
  const token = tokens[index]
  if (!token) return { text, tokens }
  const end = text[token.end] === ' ' ? token.end + 1 : token.end
  const next = text.slice(0, token.start) + text.slice(end)
  const rest = tokens.filter((_, i) => i !== index)
  return { text: next, tokens: applyEdit(rest, text, next) }
}

/**
 * The `@query` being typed right before the caret, or null. The `@` must follow
 * the start, whitespace, a Han character or CJK/fullwidth punctuation, so
 * `a@b` (emails, handles) never opens the picker.
 */
export function activeTrigger(text: string, caret: number): { start: number; query: string } | null {
  const m = text.slice(0, caret).match(/(^|[\s\p{Script=Han}\u3000-\u303F\uFF00-\uFFEF])@([^\s@]*)$/u)
  if (!m) return null
  return { start: caret - m[2]!.length - 1, query: m[2]! }
}

/** True when the caret sits inside an already picked token. */
export function insideToken(tokens: ReferenceToken[], caret: number) {
  return tokens.some((t) => caret > t.start && caret <= t.end)
}
