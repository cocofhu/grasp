import { computed, ref, watch } from 'vue'
import { api } from '@/lib/api/api'
import type { NotificationListItem } from '@/lib/api/apiTypes'
import type { NotificationReadFilter } from '@/lib/api/clients/notificationsClient'
import type { Run } from '@/lib/shared/types'
import { createTimeoutController, isAbortError } from '@/lib/shared/loadingRequest'
import { DEFAULT_LOADING_TIMEOUT_MS } from '@/lib/shared/loadingTypes'
import { useAuth } from '@/lib/composables/useAuth'

/** Notifications page default page size (matches server). */
export const NOTIFICATION_PAGE_SIZE = 20
/** Dropdown preview hard limit (5 per clarified requirement / Demo). */
export const RUN_TERMINAL_PANEL_LIMIT = 5
/** Align with AppSidebarNav pending-gates peek interval. */
const RUN_TERMINAL_POLL_MS = 15_000

export type RunTerminalStatus = 'completed' | 'failed'

/**
 * Notification projection of a terminal Run.
 * Product object is Notification (inbox event), not Run list row.
 */
export interface RunTerminalNotification {
  runId: string
  status: RunTerminalStatus
  /** Cleaned display title (never progress-noise). */
  title: string
  /** True when title was replaced by the neutral template. */
  titleNeutral: boolean
  workflowName: string
  startedAt: string
  /** Approx terminal time: startedAt + durationSec (ISO). */
  finishedApprox: string
}

/** Pool item enriched with unread + baseline-before flags for UI. */
export type RunTerminalNotificationItem = RunTerminalNotification & {
  unread: boolean
  /** Finished at/before enable baseline — visible but never counts as unread. */
  beforeBaseline: boolean
}

export type RunTerminalRefreshSource =
  | 'mount'
  | 'sidebar-poll'
  | 'visibility'
  | 'focus'
  | 'manual'
  | 'auth'
  | 'page'

export function formatUnreadBadge(n: number): string {
  if (n <= 0) return ''
  if (n >= 99) return '99+'
  return String(n)
}

/** Progress / in-progress wording that must not appear as notification titles. */
export function isNoisyNotificationTitle(title: string): boolean {
  const s = title.trim()
  if (!s) return false
  return /运行中|排队中?|等待人工|等待中|(?:^|[\s·\-_/，,])(queued|running|waiting)(?:[\s·\-_/，,]|$)|waiting[_ ]human|in\s*progress/i.test(
    s,
  )
}

/** Approx enter-terminal timestamp for sort + unread baseline. */
export function finishedApproxIso(run: Run): string {
  const start = (run.startedAt || run.createdAt || '').trim()
  if (!start) return ''
  const dur = typeof run.durationSec === 'number' && run.durationSec > 0 ? run.durationSec : 0
  if (!dur) return start
  const t = Date.parse(start)
  if (Number.isNaN(t)) return start
  return new Date(t + dur * 1000).toISOString()
}

export function mapRunToNotification(run: Run): RunTerminalNotification | null {
  if (run.status !== 'completed' && run.status !== 'failed') return null
  const raw = (run.title || '').trim()
  const workflowName = (run.workflowName || '').trim()
  const finishedApprox = finishedApproxIso(run)
  const startedAt = (run.startedAt || run.createdAt || '').trim()

  let title = raw || workflowName || run.id
  let titleNeutral = false
  if (isNoisyNotificationTitle(title)) {
    const name = workflowName || run.id
    // Neutral template tokens; UI may re-localize via titleNeutral + status.
    title = `${name} · ${run.status === 'failed' ? 'failed' : 'completed'}`
    titleNeutral = true
  }

  return {
    runId: run.id,
    status: run.status,
    title,
    titleNeutral,
    workflowName,
    startedAt,
    finishedApprox: finishedApprox || startedAt,
  }
}

/** True when terminal time is at/before enable baseline (history, not unread). */
export function isBeforeBaseline(
  n: Pick<RunTerminalNotification, 'finishedApprox' | 'startedAt'>,
  baselineIso: string,
): boolean {
  if (!baselineIso) return false
  const finished = n.finishedApprox || n.startedAt
  if (!finished) return false
  const ft = Date.parse(finished)
  const bt = Date.parse(baselineIso)
  if (Number.isNaN(ft) || Number.isNaN(bt)) return false
  return ft <= bt
}

const previewPool = ref<RunTerminalNotificationItem[]>([])
const pageItems = ref<RunTerminalNotificationItem[]>([])
const pageTotal = ref(0)
const pageNumber = ref(1)
const pageFilter = ref<NotificationReadFilter>('all')
const serverAllCount = ref(0)
const serverUnreadCount = ref(0)
/** Shared event sequence: initial hydration and identical polling stay quiet. */
const notificationMotionKey = ref(0)
let unreadCountHydrated = false
let lastServerUnreadCount = 0
let lastNotificationMotionAt = Number.NEGATIVE_INFINITY
const serverReadCount = ref(0)

const usernameKey = ref('anonymous')
const error = ref<string | null>(null)
const lastRefreshSource = ref<RunTerminalRefreshSource | null>(null)
const lastPeekAt = ref(0)
const loading = ref(false)
const pageLoading = ref(false)

let refreshPromise: Promise<void> | null = null
let pagePromise: Promise<void> | null = null
let refreshGeneration = 0
let pageGeneration = 0
let abortCtrl: AbortController | null = null
let pageAbortCtrl: AbortController | null = null
let pollTimer: number | undefined
let listenersAttached = false
let authWatchInstalled = false
let lastSettledName: string | null = null

/**
 * Resolve the username only after auth has settled.
 * Avoid fetching as `anonymous` while /auth/me is in flight.
 */
function resolveUsername(): { name: string; settled: boolean } {
  const { user, ready } = useAuth()
  const name = user.value?.username?.trim() || ''
  if (name) return { name, settled: true }
  // Auth explicitly in flight (ready===false) → wait; do not stamp anonymous.
  if (ready && ready.value === false) return { name: '', settled: false }
  // ready===true with no user, or callers/tests without ready → anonymous fallback.
  return { name: 'anonymous', settled: true }
}

const unreadCount = computed(() => serverUnreadCount.value)

const hasUnreadFailed = computed(() =>
  previewPool.value.some((n) => n.unread && n.status === 'failed'),
)

const badgeLabel = computed(() => formatUnreadBadge(unreadCount.value))

const previewItems = computed(() => previewPool.value.slice(0, RUN_TERMINAL_PANEL_LIMIT))

/** Current notifications-page slice (server-paginated). */
const listItems = computed(() => pageItems.value)

const listTotal = computed(() => pageTotal.value)

const remainingCount = computed(() =>
  Math.max(serverAllCount.value - RUN_TERMINAL_PANEL_LIMIT, 0),
)

const poolTotal = computed(() => serverAllCount.value)

const allCount = computed(() => serverAllCount.value)
const readCount = computed(() => serverReadCount.value)

/**
 * Ensure auth identity is settled before fetch/write.
 * @returns true when auth identity is settled.
 */
function ensureUsername(): boolean {
  const { name, settled } = resolveUsername()
  if (!settled) return false
  if (name !== usernameKey.value) {
    usernameKey.value = name
    unreadCountHydrated = false
    lastServerUnreadCount = 0
    lastNotificationMotionAt = Number.NEGATIVE_INFINITY
    previewPool.value = []
    pageItems.value = []
    serverAllCount.value = 0
    serverUnreadCount.value = 0
    serverReadCount.value = 0
    pageTotal.value = 0
  }
  return true
}

function onAuthIdentityChange() {
  const { name, settled } = resolveUsername()
  if (!settled) {
    lastSettledName = null
    return
  }
  if (name !== usernameKey.value) {
    usernameKey.value = name
    unreadCountHydrated = false
    lastServerUnreadCount = 0
    lastNotificationMotionAt = Number.NEGATIVE_INFINITY
    previewPool.value = []
    pageItems.value = []
    serverAllCount.value = 0
    serverUnreadCount.value = 0
    serverReadCount.value = 0
    pageTotal.value = 0
  }
  // Rehydrate as soon as auth paints — do not wait for focus/15s poll.
  if (lastSettledName !== name) {
    lastSettledName = name
    void refresh({ source: 'auth' })
  }
}

function installAuthWatch() {
  if (authWatchInstalled) return
  authWatchInstalled = true
  const { user, ready } = useAuth()
  watch(
    () =>
      [
        // Treat missing ready (legacy mocks) as settled; only ready===false blocks.
        ready ? ready.value : true,
        user.value?.username?.trim() || '',
      ] as const,
    () => {
      onAuthIdentityChange()
    },
    { flush: 'sync' },
  )
}

function parseListItem(raw: NotificationListItem): RunTerminalNotificationItem | null {
  if (!raw || (raw.status !== 'completed' && raw.status !== 'failed')) return null
  const runId = typeof raw.runId === 'string' ? raw.runId.trim() : ''
  if (!runId) return null
  return {
    runId,
    status: raw.status,
    title: typeof raw.title === 'string' ? raw.title : runId,
    titleNeutral: Boolean(raw.titleNeutral),
    workflowName: typeof raw.workflowName === 'string' ? raw.workflowName : '',
    startedAt: typeof raw.startedAt === 'string' ? raw.startedAt : '',
    finishedApprox: typeof raw.finishedApprox === 'string' ? raw.finishedApprox : '',
    unread: Boolean(raw.unread),
    beforeBaseline: Boolean(raw.beforeBaseline),
  }
}

function applyCounts(data: {
  allCount?: number
  unreadCount?: number
  readCount?: number
  items?: NotificationListItem[]
}) {
  if (typeof data.allCount === 'number') {
    serverAllCount.value = data.allCount
  } else if (Array.isArray(data.items)) {
    serverAllCount.value = data.items.length
  }
  if (typeof data.unreadCount === 'number') {
    if (unreadCountHydrated && data.unreadCount > lastServerUnreadCount) {
      // Merge an arriving burst into the bell's current, finite swing.
      const now = Date.now()
      if (now - lastNotificationMotionAt >= 650) {
        notificationMotionKey.value += 1
        lastNotificationMotionAt = now
      }
    }
    unreadCountHydrated = true
    lastServerUnreadCount = data.unreadCount
    serverUnreadCount.value = data.unreadCount
  }
  if (typeof data.readCount === 'number') {
    serverReadCount.value = data.readCount
  }
}

function mapItems(raw: NotificationListItem[]): RunTerminalNotificationItem[] {
  const mapped: RunTerminalNotificationItem[] = []
  for (const item of raw) {
    const n = parseListItem(item)
    if (n) mapped.push(n)
  }
  return mapped
}

function markReadLocal(runId: string) {
  if (!runId) return
  const pageHit = pageItems.value.find((n) => n.runId === runId)
  const previewHit = previewPool.value.find((n) => n.runId === runId)
  const wasUnread = Boolean(pageHit?.unread || previewHit?.unread)
  previewPool.value = previewPool.value.map((n) =>
    n.runId === runId && n.unread ? { ...n, unread: false } : n,
  )
  pageItems.value = pageItems.value.map((n) =>
    n.runId === runId && n.unread ? { ...n, unread: false } : n,
  )
  if (wasUnread && pageFilter.value === 'unread') {
    pageItems.value = pageItems.value.filter((n) => n.runId !== runId)
    pageTotal.value = Math.max(0, pageTotal.value - 1)
  }
  if (wasUnread && serverUnreadCount.value > 0) {
    serverUnreadCount.value -= 1
    lastServerUnreadCount = serverUnreadCount.value
    serverReadCount.value += 1
  }
}

function markAllReadLocal() {
  previewPool.value = previewPool.value.map((n) => (n.unread ? { ...n, unread: false } : n))
  pageItems.value = pageItems.value.map((n) => (n.unread ? { ...n, unread: false } : n))
  serverUnreadCount.value = 0
  lastServerUnreadCount = 0
  serverReadCount.value = serverAllCount.value
}

function markRead(runId: string) {
  if (!runId) return
  const { settled } = resolveUsername()
  if (!settled) return
  const inPreview = previewPool.value.find((n) => n.runId === runId)
  const inPage = pageItems.value.find((n) => n.runId === runId)
  if (!inPreview?.unread && !inPage?.unread) return
  markReadLocal(runId)
  void (async () => {
    try {
      await api.markNotificationRead(runId)
    } catch (err) {
      error.value = err instanceof Error && err.message ? err.message : String(err || 'mark read failed')
    }
  })()
}

function markAllRead() {
  const { settled } = resolveUsername()
  if (!settled) return
  markAllReadLocal()
  void (async () => {
    try {
      await api.markAllNotificationsRead()
    } catch (err) {
      error.value =
        err instanceof Error && err.message ? err.message : String(err || 'mark all read failed')
    }
  })()
}

async function fetchPreview(signal?: AbortSignal): Promise<void> {
  const data = await api.listNotifications({
    signal,
    page: 1,
    pageSize: RUN_TERMINAL_PANEL_LIMIT,
    filter: 'all',
  })
  const raw = Array.isArray(data?.items) ? data.items : []
  previewPool.value = mapItems(raw)
  applyCounts(data)
}

async function fetchPage(
  page: number,
  filter: NotificationReadFilter,
  signal?: AbortSignal,
): Promise<void> {
  const data = await api.listNotifications({
    signal,
    page,
    pageSize: NOTIFICATION_PAGE_SIZE,
    filter,
  })
  const raw = Array.isArray(data?.items) ? data.items : []
  pageItems.value = mapItems(raw)
  pageTotal.value = typeof data.total === 'number' ? data.total : pageItems.value.length
  pageNumber.value = typeof data.page === 'number' && data.page > 0 ? data.page : page
  pageFilter.value = filter
  applyCounts(data)
}

async function refresh(opts?: { source?: RunTerminalRefreshSource }): Promise<void> {
  installAuthWatch()
  const { settled } = resolveUsername()
  // Auth still in flight: keep pool empty of unread semantics until identity settles.
  if (!settled && opts?.source !== 'auth') {
    return
  }
  if (refreshPromise) {
    // Identity flip must not join the prior flight that may have started pre-auth.
    if (opts?.source === 'auth') {
      refreshGeneration++
      abortCtrl?.abort()
      refreshPromise = null
    } else {
      return refreshPromise
    }
  }

  const gen = ++refreshGeneration
  abortCtrl?.abort()
  abortCtrl = new AbortController()
  const tc = createTimeoutController(DEFAULT_LOADING_TIMEOUT_MS, abortCtrl.signal)

  const holder: { flight: Promise<void> | null } = { flight: null }
  holder.flight = (async () => {
    loading.value = true
    try {
      if (!ensureUsername() && opts?.source !== 'auth') return
      await fetchPreview(tc.signal)
      if (gen !== refreshGeneration) return
      error.value = null
      lastRefreshSource.value = opts?.source ?? null
      lastPeekAt.value = Date.now()
    } catch (err) {
      if (gen !== refreshGeneration) return
      if (isAbortError(err) && !tc.timedOut) return
      error.value = err instanceof Error && err.message ? err.message : String(err || 'load failed')
    } finally {
      tc.clear()
      if (refreshPromise === holder.flight) refreshPromise = null
      loading.value = false
    }
  })()
  refreshPromise = holder.flight
  return holder.flight
}

async function refreshPage(opts?: {
  page?: number
  filter?: NotificationReadFilter
  source?: RunTerminalRefreshSource
}): Promise<void> {
  installAuthWatch()
  const { settled } = resolveUsername()
  if (!settled) return

  const page = opts?.page ?? pageNumber.value
  const filter = opts?.filter ?? pageFilter.value

  if (pagePromise) {
    return pagePromise
  }

  const gen = ++pageGeneration
  pageAbortCtrl?.abort()
  pageAbortCtrl = new AbortController()
  const tc = createTimeoutController(DEFAULT_LOADING_TIMEOUT_MS, pageAbortCtrl.signal)

  const holder: { flight: Promise<void> | null } = { flight: null }
  holder.flight = (async () => {
    pageLoading.value = true
    try {
      if (!ensureUsername()) return
      await fetchPage(page, filter, tc.signal)
      if (gen !== pageGeneration) return
      error.value = null
      lastRefreshSource.value = opts?.source ?? 'page'
      lastPeekAt.value = Date.now()
    } catch (err) {
      if (gen !== pageGeneration) return
      if (isAbortError(err) && !tc.timedOut) return
      error.value = err instanceof Error && err.message ? err.message : String(err || 'load failed')
    } finally {
      tc.clear()
      if (pagePromise === holder.flight) pagePromise = null
      pageLoading.value = false
    }
  })()
  pagePromise = holder.flight
  return holder.flight
}

function onVisibility() {
  if (typeof document !== 'undefined' && document.visibilityState === 'visible') {
    void refresh({ source: 'visibility' })
  }
}

function onFocus() {
  void refresh({ source: 'focus' })
}

function startPolling() {
  installAuthWatch()
  stopPolling()
  // If auth is not ready yet, skip mount fetch; auth watch will refresh on settle.
  void refresh({ source: 'mount' })
  if (typeof window === 'undefined') return
  pollTimer = window.setInterval(() => {
    void refresh({ source: 'sidebar-poll' })
  }, RUN_TERMINAL_POLL_MS)
  if (!listenersAttached) {
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('focus', onFocus)
    listenersAttached = true
  }
}

function stopPolling() {
  if (pollTimer != null) {
    clearInterval(pollTimer)
    pollTimer = undefined
  }
  if (listenersAttached && typeof window !== 'undefined') {
    document.removeEventListener('visibilitychange', onVisibility)
    window.removeEventListener('focus', onFocus)
    listenersAttached = false
  }
  abortCtrl?.abort()
  abortCtrl = null
  pageAbortCtrl?.abort()
  pageAbortCtrl = null
}

/** Test helper: reset module singleton state. */
export function __resetRunTerminalNotificationsForTests() {
  stopPolling()
  previewPool.value = []
  pageItems.value = []
  pageTotal.value = 0
  pageNumber.value = 1
  pageFilter.value = 'all'
  serverAllCount.value = 0
  serverUnreadCount.value = 0
  serverReadCount.value = 0
  usernameKey.value = 'anonymous'
  unreadCountHydrated = false
  lastServerUnreadCount = 0
  lastNotificationMotionAt = Number.NEGATIVE_INFINITY
  notificationMotionKey.value = 0
  error.value = null
  lastRefreshSource.value = null
  lastPeekAt.value = 0
  loading.value = false
  pageLoading.value = false
  refreshPromise = null
  pagePromise = null
  refreshGeneration = 0
  pageGeneration = 0
  lastSettledName = null
  // Keep authWatchInstalled: watch is idempotent and must survive test resets.
}

export function useRunTerminalNotifications() {
  return {
    pool: previewPool,
    poolTotal,
    previewItems,
    listItems,
    listTotal,
    remainingCount,
    unreadCount,
    notificationMotionKey,
    allCount,
    readCount,
    hasUnreadFailed,
    badgeLabel,
    error,
    loading,
    pageLoading,
    pageNumber,
    pageFilter,
    lastRefreshSource,
    lastPeekAt,
    markRead,
    markAllRead,
    refresh,
    refreshPage,
    startPolling,
    stopPolling,
    ensureUsername,
  }
}
