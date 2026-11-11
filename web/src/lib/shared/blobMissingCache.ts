/**
 * Tab-scoped negative cache for orphan blob image loads.
 * Lifecycle = current tab memory only (no localStorage/sessionStorage).
 * Full page refresh clears; SPA nav / Run switch must not clear.
 */

type BlobAutoLoadDecision = 'proceed' | 'blocked_missing' | 'blocked_pending'

type Listener = () => void

const knownMissing = new Set<string>()
const knownLoaded = new Set<string>()
const autoDone = new Set<string>()
const inflight = new Set<string>()
const listeners = new Set<Listener>()

function notify() {
  for (const fn of listeners) {
    try {
      fn()
    } catch {
      /* ignore subscriber errors */
    }
  }
}

/** Parse blob id from `blob:{id}`, `/api/blobs/{id}`, or absolute URL with `/blobs/{id}`. */
export function parseBlobId(srcOrRef: string): string | null {
  const raw = String(srcOrRef || '').trim()
  if (!raw) return null
  if (raw.startsWith('blob:')) {
    const id = raw.slice('blob:'.length).split(/[?#]/)[0]?.trim()
    return id || null
  }
  try {
    const path = raw.includes('://') ? new URL(raw).pathname : raw.split(/[?#]/)[0] || ''
    const m = path.match(/\/blobs\/([^/]+)$/)
    if (m?.[1]) return decodeURIComponent(m[1])
  } catch {
    /* ignore invalid URL */
  }
  return null
}

export function isKnownMissing(blobId: string): boolean {
  return !!blobId && knownMissing.has(blobId)
}

export function isKnownLoaded(blobId: string): boolean {
  return !!blobId && knownLoaded.has(blobId)
}

export function hasAutoAttempted(blobId: string): boolean {
  return !!blobId && autoDone.has(blobId)
}

/** Mark id as known-missing after a failed auto/manual image GET. */
export function markMissing(blobId: string): void {
  if (!blobId) return
  knownMissing.add(blobId)
  knownLoaded.delete(blobId)
  inflight.delete(blobId)
  autoDone.add(blobId)
  notify()
}

/** Successful image load: record loaded, drop missing + inflight, broadcast for cross-surface sync. */
export function markLoaded(blobId: string): void {
  if (!blobId) return
  knownMissing.delete(blobId)
  knownLoaded.add(blobId)
  inflight.delete(blobId)
  autoDone.add(blobId)
  notify()
}

/**
 * Coordinate first automatic GET for a blob id across surfaces.
 * Only one caller gets `proceed` while first attempt is in flight;
 * known-missing always blocks; prior success may remount via browser cache.
 */
export function beginAutoLoad(blobId: string): BlobAutoLoadDecision {
  if (!blobId) return 'proceed'
  if (knownMissing.has(blobId)) return 'blocked_missing'
  if (inflight.has(blobId)) return 'blocked_pending'
  if (autoDone.has(blobId)) {
    // Prior auto attempt finished and id is not missing → safe to paint via cache.
    return 'proceed'
  }
  inflight.add(blobId)
  autoDone.add(blobId)
  return 'proceed'
}

/**
 * Chat-only manual retry: clear missing so the retrying thumb may GET once.
 * Does not notify peers — strips/preview stay on placeholder until markLoaded / markMissing.
 */
export function beginManualRetry(blobId: string): void {
  if (!blobId) return
  knownMissing.delete(blobId)
  knownLoaded.delete(blobId)
  inflight.delete(blobId)
}

export function subscribe(listener: Listener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** Test helper: reset tab memory. */
export function resetBlobMissingCacheForTests(): void {
  knownMissing.clear()
  knownLoaded.clear()
  autoDone.clear()
  inflight.clear()
  notify()
}

/** Test helper: snapshot for assertions. */
export function blobMissingCacheDebug() {
  return {
    knownMissing: [...knownMissing],
    knownLoaded: [...knownLoaded],
    autoDone: [...autoDone],
    inflight: [...inflight],
  }
}
