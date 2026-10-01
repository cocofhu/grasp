import type {
  GlobalTokenStats,
  TokenPricing,
  TokenStatsWindow,
  TokenUsageEventsPage,
} from '@/lib/shared/types'
import { req } from '../httpCore'

export type GlobalTokenStatsParams = {
  window?: TokenStatsWindow | string
  /** Local date YYYY-MM-DD (inclusive); overrides window when set. */
  from?: string
  /** Local date YYYY-MM-DD (inclusive). */
  to?: string
  granularity?: 'hour' | 'day' | 'week' | string
  timezone?: string
  utcOffsetMinutes?: number
  source?: 'all' | 'workflow' | 'pm' | 'studio' | string
  status?: 'ok' | 'failed' | 'cancelled' | string
  projectId?: string
  modelKey?: string
  workflowId?: string
  nodeType?: string
  runId?: string
}

export type TokenUsageEventsParams = GlobalTokenStatsParams & {
  page?: number
  pageSize?: number
  sort?: 'time' | 'total' | 'cost' | string
}

function tokenStatsQuery(params: GlobalTokenStatsParams): URLSearchParams {
  const q = new URLSearchParams()
  q.set('window', params.window || 'all')
  if (params.from) q.set('from', params.from)
  if (params.to) q.set('to', params.to)
  if (params.granularity) q.set('granularity', params.granularity)
  if (params.timezone) q.set('timezone', params.timezone)
  if (params.utcOffsetMinutes != null && Number.isFinite(params.utcOffsetMinutes)) {
    q.set('utcOffsetMinutes', String(Math.round(params.utcOffsetMinutes)))
  }
  if (params.source && params.source !== 'all') q.set('source', params.source)
  if (params.status) q.set('status', params.status)
  if (params.projectId) q.set('projectId', params.projectId)
  if (params.modelKey) q.set('modelKey', params.modelKey)
  if (params.workflowId) q.set('workflowId', params.workflowId)
  if (params.nodeType) q.set('nodeType', params.nodeType)
  if (params.runId) q.set('runId', params.runId)
  return q
}

export const statsClient = {
  getGlobalTokenStats: (params: GlobalTokenStatsParams, opts?: { signal?: AbortSignal }) => {
    const q = tokenStatsQuery(params)
    return req<GlobalTokenStats>(`/stats/token?${q}`, opts?.signal ? { signal: opts.signal } : undefined)
  },
  listTokenUsageEvents: (params: TokenUsageEventsParams, opts?: { signal?: AbortSignal }) => {
    const q = tokenStatsQuery(params)
    if (params.page) q.set('page', String(params.page))
    if (params.pageSize) q.set('pageSize', String(params.pageSize))
    if (params.sort) q.set('sort', params.sort)
    return req<TokenUsageEventsPage>(`/stats/token/events?${q}`, opts?.signal ? { signal: opts.signal } : undefined)
  },
  getTokenPricing: () => req<TokenPricing>('/stats/token/pricing'),
  updateTokenPricing: (body: TokenPricing) =>
    req<TokenPricing>('/stats/token/pricing', { method: 'PUT', body: JSON.stringify(body) }),
}
