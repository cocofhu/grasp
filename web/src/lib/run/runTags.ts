/** Align with server models.NormalizeRunTags (Unicode letter/number + _-./, ≤32 runes). */
const RUN_TAG_PATTERN = /^[\p{L}\p{N}_./-]+$/u

/** Max runes per tag (matches NormalizeRunTags). */
export const MAX_TAG_RUNES = 32

/** Launch-path only; filter path does not enforce this. */
export const MAX_RUN_TAGS = 8

type RunTagValidationCode = 'empty' | 'too_long' | 'invalid'

export function runeCount(value: string): number {
  return Array.from(value).length
}

/** Returns a validation code, or null when the trimmed tag is valid. */
export function validateRunTag(value: string): RunTagValidationCode | null {
  const trimmed = value.trim()
  if (!trimmed) return 'empty'
  if (runeCount(trimmed) > MAX_TAG_RUNES) return 'too_long'
  if (!RUN_TAG_PATTERN.test(trimmed)) return 'invalid'
  return null
}

export function isValidRunTag(value: string): boolean {
  return validateRunTag(value) === null
}

/**
 * Parse comma-separated ?tag= into valid, deduped tags (order preserved).
 * Invalid segments are dropped so UI count matches backend NormalizeRunTags.
 */
export function parseTagQuery(raw: string): string[] {
  if (!raw) return []
  const seen = new Set<string>()
  const out: string[] = []
  for (const part of raw.split(',')) {
    const tag = part.trim()
    if (!tag || seen.has(tag) || !isValidRunTag(tag)) continue
    seen.add(tag)
    out.push(tag)
  }
  return out
}

/** Serialize tags for ?tag= (valid + dedupe); empty → ''. */
export function serializeTagQuery(tags: string[]): string {
  return parseTagQuery(tags.join(',')).join(',')
}
