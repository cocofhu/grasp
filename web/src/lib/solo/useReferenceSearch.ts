import { onBeforeUnmount, reactive, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import type { GitHubReferenceCandidate, ReferenceSearchResult, SoloReferenceCandidate } from '@/lib/api/apiTypes'

export type ReferenceTab = 'all' | 'solo' | 'issue' | 'pr'
export type GitHubStateFilter = 'open' | 'all'

type LaneName = 'solo' | 'github'
type SearchParams = { lane: LaneName; q: string; kind?: string; state?: string; page?: number }

/** Where candidates come from: an open session, or the launch composer's picks. */
export type ReferenceScope = { sessionId: string } | { agent: string; projects: string[] }

function scopeKey(scope: ReferenceScope) {
  return 'sessionId' in scope ? `s:${scope.sessionId}` : `l:${scope.agent}:${scope.projects.join(',')}`
}

function fetchLane(scope: ReferenceScope, params: SearchParams, signal: AbortSignal) {
  return 'sessionId' in scope
    ? api.searchSoloReferences<any>(scope.sessionId, params, signal)
    : api.searchLaunchReferences<any>({ ...params, agent: scope.agent, projects: scope.projects }, signal)
}
export type LaneState<T> = {
  items: T[]
  loading: boolean
  paging: boolean
  skipped: boolean
  error: string
  errorCode: string
  hint: string
  total?: number
  nextPage?: number
}

const CACHE_TTL = 5 * 60_000
const DEBOUNCE: Record<LaneName, number> = { solo: 120, github: 250 }
const GITHUB_EXACT = /^(?:https?:\/\/github\.com\/|[\w.-]+\/[\w.-]+#\d+$|#\d+$)/i

function emptyLane<T>(): LaneState<T> {
  return { items: [], loading: false, paging: false, skipped: false, error: '', errorCode: '', hint: '' }
}

/**
 * Picker search with one lane per source: debounced, cancelled when the query
 * moves on, sequence-checked so a slow reply never overwrites a newer one,
 * and cached per query for five minutes (stale results show while refreshing).
 */
export function useReferenceSearch(scope: Ref<ReferenceScope>) {
  const solo = reactive(emptyLane<SoloReferenceCandidate>()) as LaneState<SoloReferenceCandidate>
  const github = reactive(emptyLane<GitHubReferenceCandidate>()) as LaneState<GitHubReferenceCandidate>
  const lanes = { solo, github }
  const control: Record<LaneName, { seq: number; timer?: ReturnType<typeof setTimeout>; abort?: AbortController; key: string }> = {
    solo: { seq: 0, key: '' },
    github: { seq: 0, key: '' },
  }
  const cache = new Map<string, { at: number; data: ReferenceSearchResult<any> }>()

  function apply<T>(lane: LaneState<T>, data: ReferenceSearchResult<T>, append = false) {
    lane.items = append ? [...lane.items, ...data.items] : data.items
    lane.total = data.total
    lane.nextPage = data.nextPage
    lane.hint = data.hint || ''
    lane.error = data.error?.message || ''
    lane.errorCode = data.error?.code || ''
  }

  function cancel(name: LaneName) {
    const c = control[name]
    clearTimeout(c.timer)
    c.abort?.abort()
    c.seq++
  }

  function run(name: LaneName, params: { q: string; kind?: string; state?: string }, page = 1) {
    const lane = lanes[name] as LaneState<any>
    const c = control[name]
    const key = `${scopeKey(scope.value)}|${name}|${params.kind || ''}|${params.state || ''}|${params.q.toLowerCase()}`
    cancel(name)
    const my = c.seq
    lane.skipped = false
    const hit = page === 1 ? cache.get(key) : undefined
    if (page === 1) {
      c.key = key
      if (hit) {
        apply(lane, hit.data)
        lane.loading = false
        if (Date.now() - hit.at < CACHE_TTL) return
      } else {
        lane.loading = true
        lane.error = ''
        lane.hint = ''
      }
    } else {
      lane.paging = true
    }
    const delay = page > 1 || (name === 'github' && GITHUB_EXACT.test(params.q)) ? 0 : DEBOUNCE[name]
    c.timer = setTimeout(async () => {
      const abort = new AbortController()
      c.abort = abort
      try {
        const data = await fetchLane(scope.value, { lane: name, ...params, page }, abort.signal)
        if (my !== c.seq) return
        if (page === 1) cache.set(key, { at: Date.now(), data })
        apply(lane, data, page > 1)
      } catch (e: any) {
        if (abort.signal.aborted || my !== c.seq) return
        if (page === 1) lane.items = []
        lane.error = String(e?.message || e)
        lane.errorCode = 'upstream'
      } finally {
        if (my === c.seq) {
          lane.loading = false
          lane.paging = false
        }
      }
    }, delay)
  }

  function skip(name: LaneName) {
    cancel(name)
    Object.assign(lanes[name], emptyLane(), { skipped: true })
  }

  /** Query every lane the tab shows. A leading # searches GitHub only. */
  function search(q: string, tab: ReferenceTab, state: GitHubStateFilter) {
    const query = q.trim()
    if (tab === 'issue' || tab === 'pr' || query.startsWith('#')) skip('solo')
    else run('solo', { q: query })
    if (tab === 'solo') skip('github')
    else run('github', { q: query, state, kind: tab === 'issue' ? 'github_issue' : tab === 'pr' ? 'github_pr' : undefined })
  }

  function loadMore(name: LaneName, q: string, tab: ReferenceTab, state: GitHubStateFilter) {
    const lane = lanes[name]
    if (!lane.nextPage || lane.paging) return
    const params = name === 'solo' ? { q: q.trim() } : { q: q.trim(), state, kind: tab === 'issue' ? 'github_issue' : tab === 'pr' ? 'github_pr' : undefined }
    run(name, params, lane.nextPage)
  }

  function reset() {
    cancel('solo')
    cancel('github')
    Object.assign(solo, emptyLane())
    Object.assign(github, emptyLane())
  }

  onBeforeUnmount(reset)
  return { solo, github, search, loadMore, reset }
}
