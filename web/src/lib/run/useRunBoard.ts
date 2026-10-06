import { getCurrentInstance, onUnmounted, ref, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import { i18n } from '@/lib/shared/i18n'
import { isAbortError, createTimeoutController, LoadingTimeoutError } from '@/lib/shared/loadingRequest'
import {
  DEFAULT_LOADING_TIMEOUT_MS,
  type LoadingTerminalState,
  type RefreshIntent,
} from '@/lib/shared/loadingTypes'
import { sortRunsByStartedAtDesc } from '@/lib/run/runBoard'
import type { Run } from '@/lib/shared/types'

export type BoardColumnKey =
  | 'active'
  | 'running'
  | 'waiting_human'
  | 'completed'
  | 'queued'
  | 'failed'
  | 'cancelled'

export type BoardMode = 'dashboard' | 'full'

export interface BoardColumnState {
  key: BoardColumnKey
  items: Run[]
  total: number
  hasMore: boolean
  /** True when server total exceeds returned items (e.g. pageSize capped at 100). */
  truncated: boolean
  /** Set when this column's listRuns request failed. */
  error?: string | null
}

export interface BoardColumnQuery {
  key: BoardColumnKey
  status: string
  pageSize: number
  /** When true, locally re-sort merged multi-status results by startedAt. */
  localSort?: boolean
}

const DASHBOARD_COLUMNS: BoardColumnQuery[] = [
  { key: 'active', status: 'running,waiting_human', pageSize: 5, localSort: true },
  { key: 'completed', status: 'completed', pageSize: 5 },
]

const FULL_MAIN_COLUMNS: BoardColumnQuery[] = [
  { key: 'running', status: 'running', pageSize: 100 },
  { key: 'waiting_human', status: 'waiting_human', pageSize: 100 },
  { key: 'completed', status: 'completed', pageSize: 20 },
]

const EXTRA_COLUMNS: BoardColumnQuery[] = [
  { key: 'queued', status: 'queued', pageSize: 100 },
  { key: 'failed', status: 'failed', pageSize: 100 },
  { key: 'cancelled', status: 'cancelled', pageSize: 100 },
]

function emptyColumn(key: BoardColumnKey): BoardColumnState {
  return { key, items: [], total: 0, hasMore: false, truncated: false, error: null }
}

function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message) return err.message
  return String(err || 'load failed')
}

export type ProjectIdSource = string | Ref<string | null | undefined> | (() => string | null | undefined)

function resolveProjectId(source: ProjectIdSource): string {
  if (typeof source === 'function') return String(source() ?? '').trim()
  if (typeof source === 'object' && source && 'value' in source) return String(source.value ?? '').trim()
  return String(source ?? '').trim()
}

async function fetchColumn(
  query: BoardColumnQuery,
  projectId: string,
  signal?: AbortSignal,
): Promise<BoardColumnState> {
  const data = await api.listRuns({
    status: query.status,
    page: 1,
    pageSize: query.pageSize,
    projectId,
    signal,
  })
  let items = data.items
  const { total, hasMore } = data
  if (query.localSort) {
    items = sortRunsByStartedAtDesc(items)
  }
  return {
    key: query.key,
    items,
    total,
    hasMore,
    truncated: total > items.length || hasMore,
    error: null,
  }
}

export interface UseRunBoardOptions {
  mode: BoardMode
  /**
   * Required project boundary. Without a non-empty projectId, load() must not
   * call listRuns (fail-safe: empty columns + missing_project error).
   */
  projectId: ProjectIdSource
  /** Extra statuses to load on the full board (queued / failed / cancelled). */
  extraStatuses?: Ref<Set<string>> | (() => Set<string>)
  /** Shared loading-layer timeout; does not change global api defaults. */
  timeoutMs?: number
}

export function useRunBoard(options: UseRunBoardOptions) {
  const columns = ref<Record<string, BoardColumnState>>({})
  const loading = ref(false)
  const hasLoaded = ref(false)
  const error = ref<string | null>(null)
  const terminalState = ref<LoadingTerminalState | null>(null)
  let requestSeq = 0
  let abortCtrl: AbortController | null = null
  const timeoutMs = options.timeoutMs ?? DEFAULT_LOADING_TIMEOUT_MS

  function resolveExtras(): Set<string> {
    if (!options.extraStatuses) return new Set()
    return typeof options.extraStatuses === 'function' ? options.extraStatuses() : options.extraStatuses.value
  }

  function queriesForLoad(): BoardColumnQuery[] {
    if (options.mode === 'dashboard') return DASHBOARD_COLUMNS
    const extras = resolveExtras()
    const extraQs = EXTRA_COLUMNS.filter((q) => extras.has(q.key))
    return [...FULL_MAIN_COLUMNS, ...extraQs]
  }

  function abort() {
    requestSeq += 1
    abortCtrl?.abort()
    abortCtrl = null
    loading.value = false
    terminalState.value = 'cancelled'
  }

  async function load(_opts?: { intent?: RefreshIntent }) {
    const localSeq = ++requestSeq
    abortCtrl?.abort()
    abortCtrl = new AbortController()
    const tc = createTimeoutController(timeoutMs, abortCtrl.signal)

    const projectId = resolveProjectId(options.projectId)
    if (!projectId) {
      tc.clear()
      if (localSeq !== requestSeq) return
      columns.value = {}
      error.value = 'missing_project'
      hasLoaded.value = true
      loading.value = false
      terminalState.value = 'error'
      return
    }

    const queries = queriesForLoad()
    loading.value = true
    try {
      const aborted = new Promise<never>((_, reject) => {
        const onAbort = () => {
          if (tc.timedOut) reject(new LoadingTimeoutError(timeoutMs))
          else reject(new DOMException('Aborted', 'AbortError'))
        }
        if (tc.signal.aborted) {
          onAbort()
          return
        }
        tc.signal.addEventListener('abort', onAbort, { once: true })
      })
      const settled = await Promise.race([
        Promise.all(
          queries.map(async (query) => {
            try {
              return await fetchColumn(query, projectId, tc.signal)
            } catch (err) {
              if (isAbortError(err) || tc.timedOut || err instanceof LoadingTimeoutError) throw err
              console.warn('[useRunBoard] listRuns failed', query.key, err)
              const prev = columns.value[query.key]
              return {
                ...(prev ?? emptyColumn(query.key)),
                key: query.key,
                error: errorMessage(err),
              } satisfies BoardColumnState
            }
          }),
        ),
        aborted,
      ])
      if (localSeq !== requestSeq) return
      if (tc.timedOut) {
        error.value = String(i18n.global.t('common.loading.timeout'))
        terminalState.value = 'error'
        hasLoaded.value = true
        return
      }
      const next: Record<string, BoardColumnState> = {}
      const failed: string[] = []
      for (const col of settled) {
        next[col.key] = col
        if (col.error) failed.push(col.key)
      }
      columns.value = next
      error.value = failed.length ? failed.join(',') : null
      hasLoaded.value = true
      terminalState.value = failed.length ? 'error' : 'success'
    } catch (err) {
      if (localSeq !== requestSeq) return
      if (tc.timedOut) {
        error.value = String(i18n.global.t('common.loading.timeout'))
        terminalState.value = 'error'
        hasLoaded.value = true
        return
      }
      if (isAbortError(err) && !tc.timedOut) {
        terminalState.value = 'cancelled'
        return
      }
      error.value = errorMessage(err)
      terminalState.value = 'error'
      hasLoaded.value = true
    } finally {
      tc.clear()
      if (localSeq === requestSeq) loading.value = false
    }
  }

  function column(key: BoardColumnKey): BoardColumnState {
    return columns.value[key] ?? emptyColumn(key)
  }

  if (getCurrentInstance()) {
    onUnmounted(() => abort())
  }

  return {
    columns,
    loading,
    hasLoaded,
    error,
    terminalState,
    load,
    abort,
    column,
    DASHBOARD_COLUMNS,
    FULL_MAIN_COLUMNS,
    EXTRA_COLUMNS,
  }
}


