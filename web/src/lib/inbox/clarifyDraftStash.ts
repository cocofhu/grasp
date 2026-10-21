import { getAuthState } from '@/lib/composables/useAuth'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

/**
 * Clarify text that could not stay in a composer: a reply refused because the
 * run ended (clarify_visit_ended), or a draft whose composer closed when the
 * run ended. Kept per user+run+node in this tab's sessionStorage, so it
 * survives a refresh of the same tab and comes back when the run resumes and
 * the composer reappears. Only the tab that sent a message ever gets it back:
 * nothing is shared through localStorage, and closing that tab loses it
 * (accepted, like a tab closed before the refusal arrived). Cleared as soon as
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

/** One rescued (refused) message at the head of a draft, by send time. */
export type RescuedPart = { text: string; at: number }

/** What a read returns: the content plus who owns it and how it was trimmed. */
export type ClarifyStashRead = ClarifyDraftStash & {
  /** Rescued messages at the head of `text`, oldest send first. */
  parts?: RescuedPart[]
  /** Inline images left out because they exceeded the size budget. */
  imagesDropped?: number
}

type Stored = ClarifyDraftStash & { at: number; parts?: RescuedPart[]; imagesDropped?: number }

const STASH_PREFIX = 'grasp-clarify-stash:'
const OUTBOX_PREFIX = 'grasp-clarify-outbox:'
const TAB_ID_KEY = 'grasp-clarify-tab-id'
/** Earlier builds kept these in localStorage (shared by tabs); swept on load. */
const LEGACY_LOCAL_PREFIXES = ['grasp-clarify-stash:', 'grasp-clarify-tab-alive:']
const TAB_CHANNEL = 'grasp-clarify-tab'
/** How long a starting tab waits for another tab to claim the same id. */
export const TAB_CHECK_MS = 150
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
  return budgetImagesCounted(images, budget).kept
}

/** budgetImages plus how many inline images did not fit. */
export function budgetImagesCounted(
  images: ClarifyImage[],
  budget = INLINE_IMAGE_BUDGET,
): { kept: ClarifyImage[]; dropped: number } {
  let dropped = 0
  let used = 0
  const out: ClarifyImage[] = []
  for (const im of images) {
    if (!im.data) {
      if (im.ref || im.url) out.push(im)
      continue
    }
    if (used + im.data.length > budget) {
      dropped += 1
      continue
    }
    used += im.data.length
    out.push(im)
  }
  return { kept: out, dropped }
}

function normalize(v: Partial<Stored> | null | undefined): Stored | null {
  if (!v || typeof v !== 'object') return null
  return {
    text: typeof v.text === 'string' ? v.text : '',
    images: Array.isArray(v.images) ? v.images : [],
    annotations: Array.isArray(v.annotations) ? v.annotations : [],
    at: typeof v.at === 'number' ? v.at : 0,
    ...(Array.isArray(v.parts)
      ? {
          parts: v.parts.filter(
            (p): p is RescuedPart => !!p && typeof p.text === 'string' && typeof p.at === 'number',
          ),
        }
      : {}),
    ...(typeof v.imagesDropped === 'number' && v.imagesDropped > 0 ? { imagesDropped: v.imagesDropped } : {}),
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
  const ls = storage('local')
  if (ls) {
    const legacy: string[] = []
    for (let i = 0; i < ls.length; i++) {
      const k = ls.key(i)
      if (k && LEGACY_LOCAL_PREFIXES.some((p) => k.startsWith(p))) legacy.push(k)
    }
    for (const k of legacy) ls.removeItem(k)
  }
  for (const [kind, prefix] of [['session', STASH_PREFIX], ['session', OUTBOX_PREFIX]] as const) {
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

function readKey(store: Storage, k: string): ClarifyStashRead | null {
  try {
    const s = normalize(JSON.parse(store.getItem(k) || 'null'))
    if (!s || Date.now() - s.at > STASH_TTL_MS) return null
    const out: ClarifyStashRead = { text: s.text, images: s.images, annotations: s.annotations }
    if (isEmpty(out)) return null
    if (s.parts?.length) out.parts = s.parts
    if (s.imagesDropped) out.imagesDropped = s.imagesDropped
    return out
  } catch {
    return null
  }
}

/** This tab's stash for user+run+node (null until the duplicate-tab check is done). */
export function readClarifyStash(runId: string, nodeId: string): ClarifyStashRead | null {
  if (!tabChecked) return null
  return readOwnStash(runId, nodeId)
}

/** Ungated read, for adding to the stash (writes never refill anything). */
function readOwnStash(runId: string, nodeId: string): ClarifyStashRead | null {
  if (!runId || !nodeId) return null
  sweepClarifyStash()
  const store = storage('session')
  if (!store) return null
  return readKey(store, stashKey(runId, nodeId))
}

export function clearClarifyStash(runId: string, nodeId: string): void {
  if (!runId || !nodeId) return
  storage('session')?.removeItem(stashKey(runId, nodeId))
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

/**
 * Put a rescued message back at the head of `current`, keeping rescued
 * messages in send order (oldest first) and anything typed since after them.
 * Refusals can arrive out of order (a 409 for a later send before the
 * turn-start drop of an earlier one), so order by `at`, not arrival.
 */
export function insertRescued(
  current: string,
  head: RescuedPart[],
  item: RescuedPart,
): { text: string; head: RescuedPart[] } {
  const t = item.text.trim()
  if (!t) return { text: current, head }
  if (current.includes(t)) return { text: current, head }
  const headText = head.map((p) => p.text).join('\n')
  if (head.length && current.startsWith(headText)) {
    const next = [...head, { text: t, at: item.at }].sort((a, b) => a.at - b.at)
    return { text: next.map((p) => p.text).join('\n') + current.slice(headText.length), head: next }
  }
  const next = [{ text: t, at: item.at }]
  return { text: current.trim() ? `${t}\n${current}` : t, head: next }
}

/**
 * Add to whatever is already stashed for user+run+node (never overwrites it).
 * `at` (send time) orders rescued messages; `tab` marks this tab as the owner.
 * Returns how many inline images were left out for size.
 */
export function stashClarifyDraft(
  runId: string,
  nodeId: string,
  add: ClarifyDraftStash,
  opts: { at?: number; parts?: RescuedPart[] } = {},
): { imagesDropped: number } {
  if (!runId || !nodeId) return { imagesDropped: 0 }
  const store = storage('session')
  if (!store) return { imagesDropped: 0 }
  const prev = readOwnStash(runId, nodeId)
  let text: string
  let parts: RescuedPart[] | undefined
  if (opts.parts) {
    text = add.text
    parts = opts.parts
  } else if (typeof opts.at === 'number') {
    const r = insertRescued(prev?.text ?? '', prev?.parts ?? [], { text: add.text, at: opts.at })
    text = r.text
    parts = r.head
  } else {
    text = mergeClarifyText(prev?.text ?? '', add.text)
    parts = prev?.parts
  }
  const budget = budgetImagesCounted(uniqueByJson([...(prev?.images ?? []), ...add.images]))
  const next: Stored = {
    text,
    images: budget.kept,
    annotations: uniqueByJson([...(prev?.annotations ?? []), ...add.annotations]),
    at: Date.now(),
    ...(parts?.length ? { parts } : {}),
  }
  const imagesDropped = budget.dropped + (prev?.imagesDropped ?? 0)
  if (imagesDropped) next.imagesDropped = imagesDropped
  if (isEmpty(next)) return { imagesDropped: 0 }
  const k = stashKey(runId, nodeId)
  try {
    write(store, k, next)
  } catch {
    // Quota: keep at least the text (and ref-only images).
    const inline = next.images.filter((im) => !!im.data).length
    try {
      write(store, k, {
        ...next,
        images: next.images.filter((im) => !im.data),
        imagesDropped: imagesDropped + inline,
      })
      return { imagesDropped: budget.dropped + inline }
    } catch {
      /* storage full: nothing more we can do without evicting others */
    }
  }
  return { imagesDropped: budget.dropped }
}

/* ---------------- duplicate tabs (S22) ---------------- */

/**
 * "Duplicate tab" copies sessionStorage, so the copy would hold the sender's
 * outbox and stash and refill them a second time. Each tab keeps an id in
 * sessionStorage; on start it asks the other tabs over a BroadcastChannel
 * whether anyone already has that id. A live tab answering means this tab is
 * the copy: it takes a new id and drops the copied outbox and stash. Until the
 * check is done (TAB_CHECK_MS, or at once without BroadcastChannel) nothing is
 * read from them.
 */
let tabIdCache: string | null = null
let tabChecked = false
let tabStarted = false
let channel: BroadcastChannel | null = null
let checkDone: Promise<void> = Promise.resolve()

function storedTabId(): string {
  try {
    let id = sessionStorage.getItem(TAB_ID_KEY) || ''
    if (!id) {
      id = newClientMessageId()
      sessionStorage.setItem(TAB_ID_KEY, id)
    }
    return id
  } catch {
    return tabIdCache || newClientMessageId()
  }
}

function dropCopiedState() {
  const store = storage('session')
  if (!store) return
  const keys: string[] = []
  for (let i = 0; i < store.length; i++) {
    const k = store.key(i)
    if (k && (k.startsWith(STASH_PREFIX) || k.startsWith(OUTBOX_PREFIX))) keys.push(k)
  }
  for (const k of keys) store.removeItem(k)
}

/** This tab's id (unique among open tabs once the start-up check has run). */
export function clarifyTabId(): string {
  if (!tabIdCache) tabIdCache = storedTabId()
  return tabIdCache
}

/**
 * Start the duplicate-tab check and keep answering other tabs' checks. Called
 * once at app start (and lazily by the chat). Resolves when reads are safe.
 */
export function initClarifyTab(): Promise<void> {
  if (tabStarted) return checkDone
  tabStarted = true
  const id = clarifyTabId()
  const BC = typeof BroadcastChannel !== 'undefined' ? BroadcastChannel : null
  if (!BC) {
    tabChecked = true
    return checkDone
  }
  try {
    channel = new BC(TAB_CHANNEL)
  } catch {
    tabChecked = true
    return checkDone
  }
  const ch = channel
  ch.onmessage = (e: MessageEvent) => {
    const m = e.data as { type?: string; id?: string } | null
    if (!m || typeof m.id !== 'string') return
    if (m.type === 'ping' && m.id === clarifyTabId()) ch.postMessage({ type: 'taken', id: m.id })
  }
  checkDone = new Promise<void>((resolve) => {
    let done = false
    const finish = () => {
      if (done) return
      done = true
      if (channel === ch) tabChecked = true
      resolve()
    }
    const onTaken = (e: MessageEvent) => {
      const m = e.data as { type?: string; id?: string } | null
      if (!m || m.type !== 'taken' || m.id !== id || done) return
      // Another open tab owns this id: this tab is its copy.
      dropCopiedState()
      tabIdCache = newClientMessageId()
      try {
        sessionStorage.setItem(TAB_ID_KEY, tabIdCache)
      } catch {
        /* ignore */
      }
      ch.removeEventListener('message', onTaken)
      finish()
    }
    ch.addEventListener('message', onTaken)
    ch.postMessage({ type: 'ping', id })
    setTimeout(() => {
      ch.removeEventListener('message', onTaken)
      finish()
    }, TAB_CHECK_MS)
  })
  return checkDone
}

/** Whether the duplicate-tab check is done (reads are safe). */
export function clarifyTabReady(): boolean {
  return tabChecked
}

/** Test hook: a fresh tab (forget id and check state). */
export function resetClarifyTabForTest(opts: { checked?: boolean } = {}): void {
  channel?.close()
  channel = null
  tabIdCache = null
  tabStarted = false
  tabChecked = !!opts.checked
  checkDone = Promise.resolve()
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
  const budget = budgetImagesCounted(entry.images)
  box[id] = {
    text: entry.text,
    images: budget.kept,
    annotations: entry.annotations,
    at: Date.now(),
    ...(budget.dropped ? { imagesDropped: budget.dropped } : {}),
  }
  const ids = Object.keys(box).sort((a, b) => box[a].at - box[b].at)
  while (ids.length > OUTBOX_MAX) delete box[ids.shift()!]
  writeOutbox(runId, nodeId, box)
}

/** The outbox entry for `id`, removed; null when this tab did not send it. */
export function takeClarifyOutbox(
  runId: string,
  nodeId: string,
  id: string | undefined | null,
  /**
   * `own`: the caller answers a request this very page sent (HTTP 409), so the
   * duplicate-tab check is not needed. WS frames pass nothing and wait for it.
   */
  opts: { own?: boolean } = {},
): (ClarifyDraftStash & { at: number; imagesDropped?: number }) | null {
  if (!runId || !nodeId || !id || (!tabChecked && !opts.own)) return null
  const box = readOutbox(runId, nodeId)
  const e = box[id]
  if (!e || Date.now() - e.at > STASH_TTL_MS) return null
  delete box[id]
  writeOutbox(runId, nodeId, box)
  const out: ClarifyDraftStash & { at: number; imagesDropped?: number } = {
    text: e.text,
    images: e.images,
    annotations: e.annotations,
    at: e.at,
  }
  if (e.imagesDropped) out.imagesDropped = e.imagesDropped
  return out
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
export function replaceClarifyStash(
  runId: string,
  nodeId: string,
  next: ClarifyDraftStash,
  parts: RescuedPart[] = [],
): { imagesDropped: number } {
  if (!runId || !nodeId) return { imagesDropped: 0 }
  clearClarifyStash(runId, nodeId)
  return stashClarifyDraft(runId, nodeId, next, { parts })
}
