import { computed } from 'vue'
import { useRoute, useRouter, type RouteLocationNormalizedLoaded, type Router } from 'vue-router'

export const STATUS_FILTER_STORAGE_KEY = 'grasp-runs-status-filter'

const VALID_RUN_STATUSES = new Set([
  'running',
  'waiting_human',
  'queued',
  'completed',
  'failed',
  'cancelled',
])

export const STATUS_ORDER = [
  'running',
  'waiting_human',
  'queued',
  'completed',
  'failed',
  'cancelled',
] as const

export const STATUS_FILTER_OPTIONS = [
  { id: '', labelKey: 'common.status.all', icon: 'clock', cls: 'text-txt2', spin: false },
  { id: 'running', labelKey: 'common.status.running', icon: 'dot', cls: 'text-info', spin: true },
  { id: 'waiting_human', labelKey: 'common.status.waiting_human', icon: 'gate', cls: 'text-warn', spin: false },
  { id: 'queued', labelKey: 'common.status.queued', icon: 'clock', cls: 'text-txt2', spin: false },
  { id: 'completed', labelKey: 'common.status.completed', icon: 'check', cls: 'text-ok', spin: false },
  { id: 'failed', labelKey: 'common.status.failed', icon: 'alert', cls: 'text-err', spin: false },
  { id: 'cancelled', labelKey: 'common.status.cancelled', icon: 'close', cls: 'text-txt3', spin: false },
] as const

/** Parse comma-separated ?status= into valid, deduped statuses (order not preserved). */
export function parseStatusQuery(raw: string): string[] {
  if (!raw) return []
  const seen = new Set<string>()
  const result: string[] = []
  for (const part of raw.split(',')) {
    const s = part.trim()
    if (s && VALID_RUN_STATUSES.has(s) && !seen.has(s)) {
      seen.add(s)
      result.push(s)
    }
  }
  return result
}

/** Serialize statuses to comma-separated string in fixed STATUS_ORDER. */
export function serializeStatusQuery(statuses: string[]): string {
  const set = new Set(statuses)
  return STATUS_ORDER.filter((s) => set.has(s)).join(',')
}

/** Zero selected or all 6 selected → no filter (empty array). */
export function normalizeStatuses(statuses: string[]): string[] {
  if (statuses.length === 0 || statuses.length === STATUS_ORDER.length) {
    return []
  }
  return statuses
}

export function readStatusFilterFromStorage(): string {
  try {
    return localStorage.getItem(STATUS_FILTER_STORAGE_KEY) ?? ''
  } catch {
    return ''
  }
}

export function writeStatusFilterToStorage(value: string): void {
  try {
    localStorage.setItem(STATUS_FILTER_STORAGE_KEY, value)
  } catch {
    /* localStorage unavailable — silent degradation */
  }
}

export function clearStatusFilterStorage(): void {
  try {
    localStorage.removeItem(STATUS_FILTER_STORAGE_KEY)
  } catch {
    /* localStorage unavailable — silent degradation */
  }
}

function syncStatusFilterStorage(normalized: string[]): void {
  if (normalized.length) {
    writeStatusFilterToStorage(serializeStatusQuery(normalized))
  } else {
    clearStatusFilterStorage()
  }
}

/**
 * On /runs load: URL with valid status → sync localStorage (f4); no status → restore
 * from localStorage via router.replace, preserving other query params like ?wf= (f2).
 * Returns true when storage restored the URL (caller should skip duplicate load).
 */
export async function initStatusFilterFromStorage(
  route: RouteLocationNormalizedLoaded,
  router: Router,
): Promise<boolean> {
  const rawStatus = typeof route.query.status === 'string' ? route.query.status : ''

  if (rawStatus) {
    const normalized = normalizeStatuses(parseStatusQuery(rawStatus))
    if (normalized.length) {
      writeStatusFilterToStorage(serializeStatusQuery(normalized))
    }
    return false
  }

  const stored = readStatusFilterFromStorage()
  if (!stored) return false

  const normalized = normalizeStatuses(parseStatusQuery(stored))
  if (!normalized.length) {
    clearStatusFilterStorage()
    return false
  }

  const status = serializeStatusQuery(normalized)
  await router.replace({ query: { ...route.query, status } })
  return true
}

// useStatusFilter exposes selected run statuses as a writable string[] backed by
// the URL query param `?status=` (comma-separated). Empty array = 全部状态.
export function useStatusFilter() {
  const route = useRoute()
  const router = useRouter()

  const selectedStatuses = computed<string[]>({
    get: () => {
      const raw = typeof route.query.status === 'string' ? route.query.status : ''
      return normalizeStatuses(parseStatusQuery(raw))
    },
    set: (val) => {
      const normalized = normalizeStatuses(val)
      const query = { ...route.query }
      if (normalized.length) query.status = serializeStatusQuery(normalized)
      else delete query.status
      router.replace({ query })
      syncStatusFilterStorage(normalized)
    },
  })

  const allSelected = computed(() => selectedStatuses.value.length === 0)

  return { selectedStatuses, allSelected }
}
