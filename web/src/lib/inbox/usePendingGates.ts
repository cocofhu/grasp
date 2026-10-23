import { ref, computed } from 'vue'
import { i18n } from '@/lib/shared/i18n'
import { beginRefresh, endRefresh } from '@/lib/shared/refreshChrome'
import { createTimeoutController, isAbortError } from '@/lib/shared/loadingRequest'
import { DEFAULT_LOADING_TIMEOUT_MS } from '@/lib/shared/loadingTypes'
import {
  applyInboxReplyingState,
  currentInboxStateEpoch,
  keepNewerLocalInboxState,
  mergeInboxReplyingFromRemote,
  noteInboxStatePatch,
} from '@/lib/inbox/inboxDisplay'
import { listAllPendingGates } from '@/lib/inbox/listAllPendingGates'
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
/**
 * Rows the inbox page is holding locally (incoming ghost, start-failed) that
 * the pending snapshot does not include yet. Shown above displayedItems.
 */
const sidebarExtras = ref<InboxItem[]>([])
/** Clarify rows whose sandbox boot failed. They stay listed but do not animate. */
const startFailedKeys = ref<string[]>([])
/** Run:node of the inbox panel selection. Null when the inbox view is unmounted. */
const activeInboxKey = ref<string | null>(null)
const totalCount = ref(0)
const hasPendingUpdate = ref(false)
const pendingMeta = ref<PendingMeta | null>(null)
const lastRefreshSource = ref<RefreshSource | null>(null)
const lastPeekAt = ref(0)
/**
 * Bumped whenever a pending membership change is applied to displayedItems
 * automatically. The inbox page watches it to reload its own page list.
 */
const lastAutoAppliedAt = ref(0)
/**
 * Each mounted inbox page registers its own hold: while any returns true
 * (a non-empty draft in a form/composer/focused field, processingLock)
 * membership changes stay pending and nothing the user is working on
 * re-renders. No holds = no inbox mounted → apply at once so the sidebar never
 * freezes on other pages. Per instance: one inbox unmounting releases only its
 * own hold.
 */
const autoApplyHolds = new Set<() => boolean>()
/**
 * Keys removed locally (approve/reject/force) → time. A lagging peek that still
 * lists them must not resurrect a ghost card now that peeks apply on their own.
 * Cleared once the server stops listing the key, on restore, or after the TTL.
 */
const LOCAL_REMOVAL_TTL_MS = 60_000
const locallyRemoved = new Map<string, { at: number; iteration: number }>()
const iterationOf = (it: InboxItem) => it.iteration ?? 1
/** Same run:node at a later iteration (reject → loop back) is a new gate, not a ghost. */
const isLocallyRemoved = (it: InboxItem) => locallyRemoved.get(itemKey(it))?.iteration === iterationOf(it)
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
/** Epoch captured when the current remote snapshot was requested. */
let lastSnapshotEpoch: number | undefined

function inboxRemoteObservedAt(): number | undefined {
  return lastSnapshotEpoch
}

async function fetchPeek(signal?: AbortSignal): Promise<{ items: InboxItem[]; total: number }> {
  // Align with GatesInbox loadList: use the live URL filter set (plan g2.2).
  // Do not hard-code wf/tag — read whatever is currently in the query.
  // Page through the whole total: the desktop sidebar is the only entry now.
  const filters = readPeekFiltersFromLocation()
  return listAllPendingGates({
    signal,
    wf: filters.wf,
    tag: filters.tag,
  })
}

/** URL `?wf=&tag=` — same source of truth as the inbox page filters. */
function readPeekFiltersFromLocation(): {
  wf?: string
  tag?: string
} {
  if (typeof window === 'undefined') return {}
  try {
    const q = new URLSearchParams(window.location.search)
    const wf = q.get('wf')?.trim() || undefined
    const tag = q.get('tag')?.trim() || undefined
    return { wf, tag }
  } catch {
    return {}
  }
}

function dropLocallyRemoved(remote: InboxItem[]): InboxItem[] {
  if (!locallyRemoved.size) return remote
  const now = Date.now()
  const listed = new Set(remote.filter(isLocallyRemoved).map(itemKey))
  for (const [k, mark] of locallyRemoved) {
    if (!listed.has(k) || now - mark.at > LOCAL_REMOVAL_TTL_MS) locallyRemoved.delete(k)
  }
  return locallyRemoved.size ? remote.filter((it) => !isLocallyRemoved(it)) : remote
}

function setPending(rawRemote: InboxItem[], rawTotal: number, snapshotEpoch?: number) {
  const remote = dropLocallyRemoved(rawRemote)
  if (snapshotEpoch != null) lastSnapshotEpoch = snapshotEpoch
  remoteItems.value = remote
  totalCount.value = Math.max(0, rawTotal - (rawRemote.length - remote.length))
  const meta = diffMembership(remote)
  if (meta.added > 0 || meta.removed > 0) {
    hasPendingUpdate.value = true
    pendingMeta.value = meta
    autoApplyPending()
    return
  }
  // Same membership: fold visible∩remote into displayed, then merge badge-level
  // state (replying) in place. Do not treat session busy as an add/remove.
  hasPendingUpdate.value = false
  pendingMeta.value = null
  adoptVisibleRemoteIntoDisplayed(remote)
  displayedItems.value = mergeInboxReplyingFromRemote(displayedItems.value, remote, itemKey, snapshotEpoch)
  sidebarExtras.value = mergeInboxReplyingFromRemote(sidebarExtras.value, remote, itemKey, snapshotEpoch)
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
  let patched = false
  const apply = (arr: InboxItem[]) => {
    let changed = false
    const next = arr.map((it) => {
      if (itemKey(it) !== key || it.type !== 'clarify') return it
      const updated = applyInboxReplyingState(it, busy)
      if (updated !== it) changed = true
      return updated
    })
    if (changed) patched = true
    return changed ? next : arr
  }
  displayedItems.value = apply(displayedItems.value)
  remoteItems.value = apply(remoteItems.value)
  sidebarExtras.value = apply(sidebarExtras.value)
  if (patched) noteInboxStatePatch(key)
}

function applyRemoteToDisplayed(rawRemote: InboxItem[], rawTotal: number, snapshotEpoch?: number) {
  const dropped = dropLocallyRemoved(rawRemote)
  const remote = keepNewerLocalInboxState(
    dropped,
    [...displayedItems.value, ...sidebarExtras.value],
    itemKey,
    snapshotEpoch,
  )
  if (snapshotEpoch != null) lastSnapshotEpoch = snapshotEpoch
  remoteItems.value = remote
  totalCount.value = Math.max(0, rawTotal - (rawRemote.length - remote.length))
  displayedItems.value = remote
  sidebarExtras.value = mergeInboxReplyingFromRemote(sidebarExtras.value, remote, itemKey, snapshotEpoch)
  hasPendingUpdate.value = false
  pendingMeta.value = null
}

function syncPendingMetaFromDiff() {
  const meta = diffMembership(remoteItems.value)
  if (meta.added > 0 || meta.removed > 0) {
    hasPendingUpdate.value = true
    pendingMeta.value = meta
    autoApplyPending()
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
function syncDisplayedBaseline(visible: InboxItem[], snapshotEpoch?: number): void {
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
      lastSnapshotEpoch,
    )
    sidebarExtras.value = mergeInboxReplyingFromRemote(
      sidebarExtras.value,
      remoteItems.value,
      itemKey,
      lastSnapshotEpoch,
    )
  }
  // loadList's rows are the fresh snapshot. Copy their state onto the sidebar
  // rows already on screen, including placeholders that peek does not list.
  displayedItems.value = mergeInboxReplyingFromRemote(displayedItems.value, visible, itemKey, snapshotEpoch)
  sidebarExtras.value = mergeInboxReplyingFromRemote(sidebarExtras.value, visible, itemKey, snapshotEpoch)
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
  const inExtras = sidebarExtras.value.some((it) => itemKey(it) === key)
  if (!inDisplayed && !inRemote && !inExtras) return

  const removed = [...displayedItems.value, ...remoteItems.value].find((it) => itemKey(it) === key)
  if (removed) locallyRemoved.set(key, { at: Date.now(), iteration: iterationOf(removed) })
  displayedItems.value = displayedItems.value.filter((it) => itemKey(it) !== key)
  remoteItems.value = remoteItems.value.filter((it) => itemKey(it) !== key)
  sidebarExtras.value = sidebarExtras.value.filter((it) => itemKey(it) !== key)
  startFailedKeys.value = startFailedKeys.value.filter((k) => k !== key)
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
  locallyRemoved.delete(key)
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
      const snapshotEpoch = currentInboxStateEpoch()
      const { items: remote, total } = await fetchPeek(tc.signal)
      // Discard if a newer peek/force superseded this request.
      if (gen !== refreshGeneration) return
      lastRefreshSource.value = opts?.source ?? null
      error.value = null
      setPending(remote, total, snapshotEpoch)
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
  displayedItems.value = keepNewerLocalInboxState(
    remoteItems.value,
    [...displayedItems.value, ...sidebarExtras.value],
    itemKey,
    lastSnapshotEpoch,
  )
  sidebarExtras.value = mergeInboxReplyingFromRemote(sidebarExtras.value, displayedItems.value, itemKey, lastSnapshotEpoch)
  hasPendingUpdate.value = false
  pendingMeta.value = null
}

/**
 * Apply a pending membership change unless the mounted inbox holds it.
 * Returns true when applied. The inbox calls it again once its hold ends.
 */
function autoApplyPending(): boolean {
  if (!hasPendingUpdate.value) return false
  for (const hold of autoApplyHolds) if (hold()) return false
  applyPending()
  lastAutoAppliedAt.value = Date.now()
  return true
}

/** Register an inbox page's edit/processing hold; returns its release function. */
function addAutoApplyHold(hold: () => boolean): () => void {
  autoApplyHolds.add(hold)
  return () => { autoApplyHolds.delete(hold) }
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
      const snapshotEpoch = currentInboxStateEpoch()
      const { items: remote, total } = await fetchPeek(tc.signal)
      if (gen !== refreshGeneration) return
      lastRefreshSource.value = opts?.source ?? null
      error.value = null
      applyRemoteToDisplayed(remote, total, snapshotEpoch)
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

function setSidebarExtras(items: InboxItem[]) {
  sidebarExtras.value = items
}

function markStartFailed(key: string) {
  if (startFailedKeys.value.includes(key)) return
  startFailedKeys.value = [...startFailedKeys.value, key]
}

function clearStartFailed(key: string) {
  if (!startFailedKeys.value.includes(key)) return
  startFailedKeys.value = startFailedKeys.value.filter((k) => k !== key)
}

function setActiveInboxKey(key: string | null) {
  activeInboxKey.value = key
}

/** Write a saved run title onto every inbox row of that run (sidebar extras included). */
function patchRunTitle(runId: string, title: string): void {
  const id = runId.trim()
  if (!id) return
  const apply = (arr: InboxItem[]) => {
    let changed = false
    const next = arr.map((it) => {
      if (it.runId !== id || it.runTitle === title) return it
      changed = true
      return { ...it, runTitle: title }
    })
    return changed ? next : arr
  }
  displayedItems.value = apply(displayedItems.value)
  remoteItems.value = apply(remoteItems.value)
  sidebarExtras.value = apply(sidebarExtras.value)
}

export function usePendingGates() {
  return {
    displayedItems,
    remoteItems,
    sidebarExtras,
    startFailedKeys,
    activeInboxKey,
    totalCount,
    count,
    hasPendingUpdate,
    pendingMeta,
    lastRefreshSource,
    lastPeekAt,
    lastAutoAppliedAt,
    error,
    ariaBusy,
    refresh,
    peek,
    applyPending,
    autoApplyPending,
    addAutoApplyHold,
    removeItemLocally,
    restoreItemLocally,
    setSidebarExtras,
    markStartFailed,
    clearStartFailed,
    setActiveInboxKey,
    patchRunTitle,
    patchItemReplying,
    syncDisplayedBaseline,
    inboxRemoteObservedAt,
    clearVisibleMembership,
    itemKey,
  }
}
