import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

/**
 * Clarify text that could not stay in a composer: a reply refused because the
 * run ended (clarify_visit_ended), or a draft whose composer closed when the
 * run ended. Kept per run+node in localStorage so it survives a refresh and
 * comes back when the run resumes and the composer reappears.
 */
export type ClarifyDraftStash = {
  text: string
  images: ClarifyImage[]
  annotations: ReactAnnotation[]
}

const PREFIX = 'grasp-clarify-stash:'

function key(runId: string, nodeId: string): string {
  return `${PREFIX}${runId}:${nodeId}`
}

function isEmpty(s: ClarifyDraftStash): boolean {
  return !s.text.trim() && s.images.length === 0 && s.annotations.length === 0
}

export function readClarifyStash(runId: string, nodeId: string): ClarifyDraftStash | null {
  if (!runId || !nodeId) return null
  try {
    const raw = localStorage.getItem(key(runId, nodeId))
    if (!raw) return null
    const v = JSON.parse(raw) as Partial<ClarifyDraftStash>
    const s: ClarifyDraftStash = {
      text: typeof v.text === 'string' ? v.text : '',
      images: Array.isArray(v.images) ? v.images : [],
      annotations: Array.isArray(v.annotations) ? v.annotations : [],
    }
    return isEmpty(s) ? null : s
  } catch {
    return null
  }
}

export function clearClarifyStash(runId: string, nodeId: string): void {
  if (!runId || !nodeId) return
  try {
    localStorage.removeItem(key(runId, nodeId))
  } catch {
    /* storage unavailable */
  }
}

/** Text joined first-in-first; duplicates (already contained) are skipped. */
export function mergeClarifyText(base: string, add: string): string {
  const a = add.trim()
  if (!a) return base
  if (!base.trim()) return a
  if (base.includes(a)) return base
  if (a.includes(base.trim())) return a
  return `${base}\n${a}`
}

function uniqueByJson<T>(list: T[]): T[] {
  const seen = new Set<string>()
  return list.filter((x) => {
    const k = JSON.stringify(x)
    if (seen.has(k)) return false
    seen.add(k)
    return true
  })
}

/** Add to whatever is already stashed for run+node (never overwrites it). */
export function stashClarifyDraft(runId: string, nodeId: string, add: ClarifyDraftStash): void {
  if (!runId || !nodeId) return
  const prev = readClarifyStash(runId, nodeId)
  const next: ClarifyDraftStash = {
    text: mergeClarifyText(prev?.text ?? '', add.text),
    images: uniqueByJson([...(prev?.images ?? []), ...add.images]),
    annotations: uniqueByJson([...(prev?.annotations ?? []), ...add.annotations]),
  }
  if (isEmpty(next)) return
  try {
    localStorage.setItem(key(runId, nodeId), JSON.stringify(next))
  } catch {
    // Quota (large inline images): keep at least the text.
    try {
      localStorage.setItem(key(runId, nodeId), JSON.stringify({ ...next, images: [] }))
    } catch {
      /* storage unavailable */
    }
  }
}
