import { ref, computed } from 'vue'
import { api, isPaginated } from '@/lib/api/api'
import { i18n } from '@/lib/shared/i18n'
import { beginRefresh, endRefresh } from '@/lib/shared/refreshChrome'
import { createTimeoutController, isAbortError } from '@/lib/shared/loadingRequest'
import { DEFAULT_LOADING_TIMEOUT_MS } from '@/lib/shared/loadingTypes'
import { applyInboxReplyingState, mergeInboxReplyingFromRemote } from '@/lib/inbox/inboxDisplay'
import type { InboxItem } from '@/lib/shared/types'

export type RefreshSource =
  | 'sidebar-poll'
  | 'visibility'
  | 'focus'
  | 'navigate'
  | 'submit'
  | 'mount'
  | 'manual'

export interface RefreshOptions {
  source?: RefreshSource
  mode?: 'force' | 'peek'
}

export interface PeekOptions {
  source?: RefreshSource
}

export interface PendingMeta {
  added: number
  removed: number
}

export function itemKey(it: InboxItem): string {
  return `${it.runId}:${it.nodeId}`
}

function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message) return err.message
  return String(err || 'load failed')
}

// Module-level singleton
const displayedItems = ref<InboxItem[]>([])
const remoteItems = ref<InboxItem[]>([])
const totalCount = ref(0)
const hasPendingUpdate = ref(false)
const pendingMeta = ref<PendingMeta | null>(null)
const lastRefreshSource = ref<RefreshSource | null>(null)
const lastPeekAt = ref(0)
const error = ref<string | null>(null)
const ariaBusy = ref(false)
/**
 * Keys currently rendered on the Gates inbox page (listItems). Set by
 * syncDisplayedBaseline after each successful loadList; cleared on unmount.
 * Null means "inbox not mounted" — peek falls back to displayedItems only.
 */
let visibleMembershipKeys: Set<string> | null = null

/**
 * Membership diff for the 待刷新 banner.
 * "Added" is relative to the applied peek snapshot ∪ the inbox page's visible
 * listKeys (plan g1.1) so loadList/starting-poll rows are not re-flagged.
 * "Removed" only considers displayedItems — page-N visible keys must not count
 * as removals against the page-1 peek window.
 */
function diffMembership(remote: InboxItem[]): PendingMeta {
  const displayedSet = new Set(displayedItems.value.map(itemKey))
  const remoteSet = new Set(remote.map(itemKey))
  const visible = visibleMembershipKeys
  let added = 0
  let removed = 0
  for (const k of remoteSet) {
    if (displayedSet.has(k) || (visible != null && visible.has(k))) continue
    added++
  }
  for (const k of displayedSet) {
    if (!remoteSet.has(k)) removed++
  }
  return { added, removed }
}

// Backward-compatible alias: list UI binds to displayedItems.
const items = displayedItems
const count = computed(() => totalCount.value)

/** Separate flight slots so force/submit never joins an in-flight peek. */
let peekPromise: Promise<void> | null = null
let forcePromise: Promise<void> | null = null
/**
 * Monotonic generation: bumping invalidates in-flight peek/force writebacks.
 * Force always bumps so a slower peek cannot overwrite with setPending.
 */
let refreshGeneration = 0
let abortCtrl: AbortController | null = null

async function fetchPeek(signal?: AbortSignal): Promise<{ items: InboxItem[]; total: number }> {
  // Align with GatesInbox loadList: use the live URL filter set (plan g2.2).
  // Do not hard-code project/wf/tag — read whatever is currently in the query.
  const filters = readPeekFiltersFromLocation()
  const data = await api.listGates({
    page: 1,
    pageSize: 20,
    signal,
    wf: filters.wf,
    projectId: filters.projectId,
    tag: filters.tag,
  })
  if (isPaginated(data)) {
    return { items: data.items, total: data.total }
  }
  return { items: data, total: data.length }
}

/** URL `?wf=&projectId=&tag=` — same source of truth as the inbox page filters. */
function readPeekFiltersFromLocation(): {
  wf?: string
  projectId?: string
  tag?: string
} {
  if (typeof window === 'undefined') return {}
  try {
    const q = new URLSearchParams(window.location.search)
    const wf = q.get('wf')?.trim() || undefined
    const projectId = q.get('projectId')?.trim() || undefined
    const tag = q.get('tag')?.trim() || undefined
    return { wf, projectId, tag }
  } catch {
    return {}
  }
}

function setPending(remote: InboxItem[], total: number) {
  remoteItems.value = remote
  totalCount.value = total
  const meta = diffMembership(remote)
  if (meta.added > 0 || meta.removed > 0) {
    hasPendingUpdate.value = true
    pendingMeta.value = meta
    return
  }
  // Same membership: fold visible∩remote into displayed, then merge badge-level
  // state (replying) in place. Do not treat session busy as an add/remove.
  hasPendingUpdate.value = false
  pendingMeta.value = null
  adoptVisibleRemoteIntoDisplayed(remote)
  displayedItems.value = mergeInboxReplyingFromRemote(displayedItems.value, remote, itemKey)
}

/** Pull remote rows the visible inbox already shows into the applied snapshot. */
function adoptVisibleRemoteIntoDisplayed(remote: InboxItem[]): void {
  const visible = visibleMembershipKeys
  if (!visible || !visible.size) return
  const displayedKeys = new Set(displayedItems.value.map(itemKey))
  const missing = remote.filter((it) => {
    const k = itemKey(it)
    return visible.has(k) && !displayedKeys.has(k)
  })
  if (missing.length) {
    displayedItems.value = [...displayedItems.value, ...missing]
  }
}

/** Patch one visible/sidebar card's sessionBusy-derived state without peek diffs. */
function patchItemReplying(key: string, busy: boolean): void {
  const apply = (arr: InboxItem[]) => {
    let changed = false
    const next = arr.map((it) => {
      if (itemKey(it) !== key || it.type !== 'clarify') return it
      const patched = applyInboxReplyingState(it, busy)
      if (patched !== it) changed = true
      return patched
    })
    return changed ? next : arr
  }
  displayedItems.value = apply(displayedItems.value)
  remoteItems.value = apply(remoteItems.value)
}

function applyRemoteToDisplayed(remote: InboxItem[], total: number) {
  remoteItems.value = remote
  totalCount.value = total
  displayedItems.value = remote
  hasPendingUpdate.value = false
  pendingMeta.value = null
}

function syncPendingMetaFromDiff() {
  const meta = diffMembership(remoteItems.value)
  if (meta.added > 0 || meta.removed > 0) {
    hasPendingUpdate.value = true
    pendingMeta.value = meta
  } else {
    // Empty membership diff: close banner state without inventing "added N" (g2.1).
    hasPendingUpdate.value = false
    pendingMeta.value = null
  }
}

/**
 * After the inbox page successfully loads/refreshes its visible list, adopt
 * those keys as the peek baseline so subsequent peeks do not re-flag them as
 * "新增" (plan g1.2). Also rewrites displayedItems for keys already present in
 * the remote peek snapshot.
 */
function syncDisplayedBaseline(visible: InboxItem[]): void {
  visibleMembershipKeys = new Set(visible.map(itemKey))
  const remoteByKey = new Map(remoteItems.value.map((it) => [itemKey(it), it]))
  const displayedKeys = new Set(displayedItems.value.map(itemKey))
  const adopted: InboxItem[] = []
  for (const it of visible) {
    const k = itemKey(it)
    if (displayedKeys.has(k)) continue
    // Only pull remote∩visible into displayed — never inflate with page-only
    // rows (that would look like "removed" against the page-1 peek window).
    const remoteRow = remoteByKey.get(k)
    if (!remoteRow) continue
    adopted.push(remoteRow)
    displayedKeys.add(k)
  }
  if (adopted.length) {
    displayedItems.value = [...displayedItems.value, ...adopted]
  }
  syncPendingMetaFromDiff()
  // When membership matches, still merge badge-level state from remote.
  if (!hasPendingUpdate.value && remoteItems.value.length) {
    displayedItems.value = mergeInboxReplyingFromRemote(
      displayedItems.value,
      remoteItems.value,
      itemKey,
    )
  }
}

/** Inbox page left — stop using listItems as a peek baseline. */
function clearVisibleMembership(): void {
  visibleMembershipKeys = null
}

function syncAriaBusy() {
  ariaBusy.value = peekPromise != null || forcePromise != null
}

/**
 * Optimistic local removal after approve/reject/clarify-force is initiated
 * (or after a successful converge). Keeps sidebar totalCount/displayedItems
 * aligned even when force refresh fails or a stale peek would otherwise linger.
 */
function removeItemLocally(key: string): void {
  const inDisplayed = displayedItems.value.some((it) => itemKey(it) === key)
  const inRemote = remoteItems.value.some((it) => itemKey(it) === key)
  if (!inDisplayed && !inRemote) return

  displayedItems.value = displayedItems.value.filter((it) => itemKey(it) !== key)
  remoteItems.value = remoteItems.value.filter((it) => itemKey(it) !== key)
  if (inDisplayed || inRemote) {
    totalCount.value = Math.max(0, totalCount.value - 1)
  }
  syncPendingMetaFromDiff()
}

/**
 * Roll back removeItemLocally when confirm/resume fails so the row is retryable.
 * Inserts at the front of displayed/remote when the snapshot position is unknown.
 */
function restoreItemLocally(item: InboxItem): void {
  const key = itemKey(item)
  const hadDisplayed = displayedItems.value.some((it) => itemKey(it) === key)
  const hadRemote = remoteItems.value.some((it) => itemKey(it) === key)
  if (!hadDisplayed) {
    displayedItems.value = [item, ...displayedItems.value]
  }
  if (!hadRemote) {
    remoteItems.value = [item, ...remoteItems.value]
  }
  if (!hadDisplayed || !hadRemote) {
    totalCount.value = Math.max(totalCount.value, displayedItems.value.length)
  }
  syncPendingMetaFromDiff()
}

async function peek(opts?: PeekOptions): Promise<void> {
  // Prefer awaiting in-flight force: it applies to displayed and is fresher.
  if (forcePromise) return forcePromise
  if (peekPromise) return peekPromise

  const gen = ++refreshGeneration
  abortCtrl?.abort()
  abortCtrl = new AbortController()
  const tc = createTimeoutController(DEFAULT_LOADING_TIMEOUT_MS, abortCtrl.signal)
  // Holder avoids TS2454: const flight = (async () => flight)() is "used before assigned".
  const holder: { flight: Promise<void> | null } = { flight: null }
  holder.flight = (async () => {
    ariaBusy.value = true
    try {
      const { items: remote, total } = await fetchPeek(tc.signal)
      // Discard if a newer peek/force superseded this request.
      if (gen !== refreshGeneration) return
      lastRefreshSource.value = opts?.source ?? null
      error.value = null
      setPending(remote, total)
      lastPeekAt.value = Date.now()
    } catch (err) {
      if (gen !== refreshGeneration) return
      if (isAbortError(err) && !tc.timedOut) return
      error.value = tc.timedOut ? String(i18n.global.t('common.loading.timeout')) : errorMessage(err)
    } finally {
      tc.clear()
      if (peekPromise === holder.flight) peekPromise = null
      syncAriaBusy()
    }
  })()
  peekPromise = holder.flight
  return holder.flight
}

function applyPending(): void {
  if (!hasPendingUpdate.value) return
  displayedItems.value = [...remoteItems.value]
  hasPendingUpdate.value = false
  pendingMeta.value = null
}

async function refresh(opts?: RefreshOptions): Promise<void> {
  if (opts?.mode === 'peek') {
    return peek({ source: opts?.source })
  }

  const force =
    opts?.mode === 'force' ||
    opts?.source === 'submit' ||
    opts?.source === 'navigate' ||
    opts?.source === 'mount' ||
    opts?.source === 'manual'

  if (!force) {
    return peek({ source: opts?.source })
  }

  // Deduplicate concurrent force calls, but never join an in-flight peek.
  if (forcePromise) return forcePromise

  const isManual = opts?.source === 'manual'

  // Invalidate any in-flight peek writeback (setPending) before we apply.
  const gen = ++refreshGeneration
  abortCtrl?.abort()
  abortCtrl = new AbortController()
  const tc = createTimeoutController(DEFAULT_LOADING_TIMEOUT_MS, abortCtrl.signal)
  // Holder avoids TS2454 on self-referential flight cleanup.
  const holder: { flight: Promise<void> | null } = { flight: null }
  holder.flight = (async () => {
    ariaBusy.value = true
    if (isManual) beginRefresh('user_initiated')
    try {
      const { items: remote, total } = await fetchPeek(tc.signal)
      if (gen !== refreshGeneration) return
      lastRefreshSource.value = opts?.source ?? null
      error.value = null
      applyRemoteToDisplayed(remote, total)
    } catch (err) {
      if (gen !== refreshGeneration) return
      if (isAbortError(err) && !tc.timedOut) return
      error.value = tc.timedOut ? String(i18n.global.t('common.loading.timeout')) : errorMessage(err)
    } finally {
      tc.clear()
      if (forcePromise === holder.flight) forcePromise = null
      syncAriaBusy()
      if (isManual) endRefresh()
    }
  })()
  forcePromise = holder.flight
  return holder.flight
}

export function usePendingGates() {
  return {
    items,
    displayedItems,
    remoteItems,
    totalCount,
    count,
    hasPendingUpdate,
    pendingMeta,
    lastRefreshSource,
    lastPeekAt,
    error,
    ariaBusy,
    refresh,
    peek,
    applyPending,
    removeItemLocally,
    restoreItemLocally,
    patchItemReplying,
    syncDisplayedBaseline,
    clearVisibleMembership,
    itemKey,
  }
}
