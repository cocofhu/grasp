import { getAuthState } from '@/lib/composables/useAuth'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

/**
 * Clarify text that could not stay in a composer: a reply refused because the
 * run ended (clarify_visit_ended), or a draft whose composer closed when the
 * run ended. Kept per user+run+node in localStorage so it survives a refresh
 * and comes back when the run resumes and the composer reappears. Cleared once
 * the refilled text is sent or the user empties the composer.
 *
 * The outbox is the per-tab (sessionStorage) record of replies this tab sent
 * and that have not been answered yet, keyed by clientMessageId. Only a
 * clarify_visit_ended frame echoing one of these ids is "ours" to refill; other
 * viewers of the same run never refill or stash.
 */
export type ClarifyDraftStash = {
  text: string
  images: ClarifyImage[]
  annotations: ReactAnnotation[]
}

type Stored = ClarifyDraftStash & { at: number }

const STASH_PREFIX = 'grasp-clarify-stash:'
const OUTBOX_PREFIX = 'grasp-clarify-outbox:'
/** Unsent text older than this is dropped on the next sweep. */
export const STASH_TTL_MS = 7 * 24 * 60 * 60 * 1000
/** Inline (base64) image bytes kept per entry; ref-only images are always kept. */
export const INLINE_IMAGE_BUDGET = 256 * 1024
/** Outbox entries kept per run+node (oldest dropped first). */
const OUTBOX_MAX = 20

function userKey(): string {
  try {
    return getAuthState().user?.username || 'anon'
  } catch {
    return 'anon'
  }
}

function stashKey(runId: string, nodeId: string): string {
  return `${STASH_PREFIX}${userKey()}:${runId}:${nodeId}`
}

function outboxKey(runId: string, nodeId: string): string {
  return `${OUTBOX_PREFIX}${userKey()}:${runId}:${nodeId}`
}

function storage(kind: 'local' | 'session'): Storage | null {
  try {
    return kind === 'local' ? localStorage : sessionStorage
  } catch {
    return null
  }
}

function isEmpty(s: ClarifyDraftStash): boolean {
  return !s.text.trim() && s.images.length === 0 && s.annotations.length === 0
}

/**
 * Keep ref-only (already uploaded) images; keep inline ones while they fit
 * the budget. Text and annotations are always kept.
 */
export function budgetImages(images: ClarifyImage[], budget = INLINE_IMAGE_BUDGET): ClarifyImage[] {
  let used = 0
  const out: ClarifyImage[] = []
  for (const im of images) {
    if (!im.data) {
      if (im.ref || im.url) out.push(im)
      continue
    }
    if (used + im.data.length > budget) continue
    used += im.data.length
    out.push(im)
  }
  return out
}

function normalize(v: Partial<Stored> | null | undefined): Stored | null {
  if (!v || typeof v !== 'object') return null
  return {
    text: typeof v.text === 'string' ? v.text : '',
    images: Array.isArray(v.images) ? v.images : [],
    annotations: Array.isArray(v.annotations) ? v.annotations : [],
    at: typeof v.at === 'number' ? v.at : 0,
  }
}

function write(store: Storage, k: string, value: unknown): void {
  try {
    store.setItem(k, JSON.stringify(value))
  } catch {
    /* quota / unavailable: the caller degrades below */
    throw new Error('stash-write-failed')
  }
}

let swept = false
/** Drop expired stash / outbox entries (once per page load, and on demand). */
export function sweepClarifyStash(now = Date.now(), force = false): void {
  if (swept && !force) return
  swept = true
  for (const [kind, prefix] of [['local', STASH_PREFIX], ['session', OUTBOX_PREFIX]] as const) {
    const store = storage(kind)
    if (!store) continue
    const keys: string[] = []
    for (let i = 0; i < store.length; i++) {
      const k = store.key(i)
      if (k && k.startsWith(prefix)) keys.push(k)
    }
    for (const k of keys) {
      try {
        const raw = JSON.parse(store.getItem(k) || 'null')
        if (prefix === STASH_PREFIX) {
          const s = normalize(raw)
          if (!s || now - s.at > STASH_TTL_MS) store.removeItem(k)
        } else {
          const box = raw && typeof raw === 'object' ? (raw as Record<string, Partial<Stored>>) : {}
          const kept = Object.fromEntries(
            Object.entries(box).filter(([, e]) => {
              const s = normalize(e)
              return !!s && now - s.at <= STASH_TTL_MS
            }),
          )
          if (Object.keys(kept).length) store.setItem(k, JSON.stringify(kept))
          else store.removeItem(k)
        }
      } catch {
        store.removeItem(k)
      }
    }
  }
}

export function readClarifyStash(runId: string, nodeId: string): ClarifyDraftStash | null {
  if (!runId || !nodeId) return null
  sweepClarifyStash()
  const store = storage('local')
  if (!store) return null
  try {
    const s = normalize(JSON.parse(store.getItem(stashKey(runId, nodeId)) || 'null'))
    if (!s || Date.now() - s.at > STASH_TTL_MS) return null
    const out = { text: s.text, images: s.images, annotations: s.annotations }
    return isEmpty(out) ? null : out
  } catch {
    return null
  }
}

export function clearClarifyStash(runId: string, nodeId: string): void {
  if (!runId || !nodeId) return
  storage('local')?.removeItem(stashKey(runId, nodeId))
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

/** Add to whatever is already stashed for user+run+node (never overwrites it). */
export function stashClarifyDraft(runId: string, nodeId: string, add: ClarifyDraftStash): void {
  if (!runId || !nodeId) return
  const store = storage('local')
  if (!store) return
  const prev = readClarifyStash(runId, nodeId)
  const next: Stored = {
    text: mergeClarifyText(prev?.text ?? '', add.text),
    images: budgetImages(uniqueByJson([...(prev?.images ?? []), ...add.images])),
    annotations: uniqueByJson([...(prev?.annotations ?? []), ...add.annotations]),
    at: Date.now(),
  }
  if (isEmpty(next)) return
  const k = stashKey(runId, nodeId)
  try {
    write(store, k, next)
  } catch {
    // Quota: keep at least the text (and ref-only images).
    try {
      write(store, k, { ...next, images: next.images.filter((im) => !im.data) })
    } catch {
      /* storage full: nothing more we can do without evicting others */
    }
  }
}

/* ---------------- outbox (per tab) ---------------- */

/**
 * A fresh UUID (v4) for one clarify send, echoed back by clarify_visit_ended
 * frames. The server rejects non-UUID ids, and crypto.randomUUID is missing on
 * plain-http (non-secure) origins, so build one from getRandomValues there.
 */
export function newClientMessageId(): string {
  const c = typeof crypto !== 'undefined' ? crypto : undefined
  if (c && typeof c.randomUUID === 'function') {
    try {
      return c.randomUUID()
    } catch {
      /* fall through */
    }
  }
  const b = new Uint8Array(16)
  if (c && typeof c.getRandomValues === 'function') c.getRandomValues(b)
  else for (let i = 0; i < 16; i++) b[i] = Math.floor(Math.random() * 256)
  b[6] = (b[6] & 0x0f) | 0x40
  b[8] = (b[8] & 0x3f) | 0x80
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

function readOutbox(runId: string, nodeId: string): Record<string, Stored> {
  const store = storage('session')
  if (!store) return {}
  try {
    const raw = JSON.parse(store.getItem(outboxKey(runId, nodeId)) || 'null')
    if (!raw || typeof raw !== 'object') return {}
    const out: Record<string, Stored> = {}
    for (const [id, e] of Object.entries(raw as Record<string, Partial<Stored>>)) {
      const s = normalize(e)
      if (s) out[id] = s
    }
    return out
  } catch {
    return {}
  }
}

function writeOutbox(runId: string, nodeId: string, box: Record<string, Stored>): void {
  const store = storage('session')
  if (!store) return
  const k = outboxKey(runId, nodeId)
  const ids = Object.keys(box)
  if (!ids.length) {
    store.removeItem(k)
    return
  }
  try {
    write(store, k, box)
  } catch {
    try {
      const textOnly = Object.fromEntries(
        Object.entries(box).map(([id, e]) => [id, { ...e, images: e.images.filter((im) => !im.data) }]),
      )
      write(store, k, textOnly)
    } catch {
      /* storage full */
    }
  }
}

/** Record a send this tab made; kept until answered, refused or expired. */
export function putClarifyOutbox(runId: string, nodeId: string, id: string, entry: ClarifyDraftStash): void {
  if (!runId || !nodeId || !id) return
  sweepClarifyStash()
  const box = readOutbox(runId, nodeId)
  box[id] = {
    text: entry.text,
    images: budgetImages(entry.images),
    annotations: entry.annotations,
    at: Date.now(),
  }
  const ids = Object.keys(box).sort((a, b) => box[a].at - box[b].at)
  while (ids.length > OUTBOX_MAX) delete box[ids.shift()!]
  writeOutbox(runId, nodeId, box)
}

/** The outbox entry for `id`, removed; null when this tab did not send it. */
export function takeClarifyOutbox(runId: string, nodeId: string, id: string | undefined | null): ClarifyDraftStash | null {
  if (!runId || !nodeId || !id) return null
  const box = readOutbox(runId, nodeId)
  const e = box[id]
  if (!e || Date.now() - e.at > STASH_TTL_MS) return null
  delete box[id]
  writeOutbox(runId, nodeId, box)
  return { text: e.text, images: e.images, annotations: e.annotations }
}

/** Forget a send (failed for another reason). */
export function dropClarifyOutbox(runId: string, nodeId: string, id: string | undefined | null): void {
  if (!runId || !nodeId || !id) return
  const box = readOutbox(runId, nodeId)
  if (!(id in box)) return
  delete box[id]
  writeOutbox(runId, nodeId, box)
}

/** A turn for this text was answered: drop the oldest matching outbox entry. */
export function ackClarifyOutboxText(runId: string, nodeId: string, text: string): void {
  if (!runId || !nodeId) return
  const box = readOutbox(runId, nodeId)
  const t = text.trim()
  const hit = Object.keys(box)
    .filter((id) => box[id].text.trim() === t)
    .sort((a, b) => box[a].at - box[b].at)[0]
  if (!hit) return
  delete box[hit]
  writeOutbox(runId, nodeId, box)
}

/**
 * Mirror the composer into the stash while it holds rescued text, so a refresh
 * keeps the user's edits; an empty composer clears it.
 */
export function replaceClarifyStash(runId: string, nodeId: string, next: ClarifyDraftStash): void {
  if (!runId || !nodeId) return
  clearClarifyStash(runId, nodeId)
  stashClarifyDraft(runId, nodeId, next)
}
