import { onBeforeUnmount, reactive, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import type { ArtifactReferenceCandidate, GitHubReferenceCandidate, ReferenceSearchLane, ReferenceSearchResult, SoloReferenceCandidate } from '@/lib/api/apiTypes'
import type { ReferenceSearchParams } from '@/lib/api/clients/soloClient'
import { parseGitHubReference } from './referenceTokens'

export type ScopedReferenceSearch = typeof api.searchSoloReferences

export type ReferenceTab = 'all' | 'solo' | 'issue' | 'pr' | 'discussion' | 'artifact'
export type GitHubStateFilter = 'open' | 'all'

type LaneName = ReferenceSearchLane
type LaneParams = Omit<ReferenceSearchParams, 'lane' | 'page'>

/**
 * Where candidates come from: an open session, the Solo launch composer's
 * picks, a workflow about to start, or a running workflow node.
 */
export type ReferenceScope =
  | { sessionId: string }
  | { agent: string; projects: string[] }
  | { workflowId: string }
  | { runId: string; nodeId: string }

function scopeKey(scope: ReferenceScope) {
  if ('sessionId' in scope) return `s:${scope.sessionId}`
  if ('workflowId' in scope) return `w:${scope.workflowId}`
  if ('runId' in scope) return `r:${scope.runId}:${scope.nodeId}`
  return `l:${scope.agent}:${scope.projects.join(',')}`
}

function fetchLane(scope: ReferenceScope, params: ReferenceSearchParams, signal: AbortSignal, scopedSearch?: ScopedReferenceSearch) {
  if (scopedSearch) return scopedSearch<any>('sessionId' in scope ? scope.sessionId : '', params, signal)
  if ('sessionId' in scope) return api.searchSoloReferences<any>(scope.sessionId, params, signal)
  if ('workflowId' in scope) return api.searchWorkflowReferences<any>(scope.workflowId, params, signal)
  if ('runId' in scope) return api.searchRunNodeReferences<any>(scope.runId, scope.nodeId, params, signal)
  return api.searchLaunchReferences<any>({ ...params, agent: scope.agent, projects: scope.projects }, signal)
}
type LaneState<T> = {
  items: T[]
  loading: boolean
  paging: boolean
  skipped: boolean
  /** True once any reply (or failure) has landed since the lane was last reset. */
  settled: boolean
  error: string
  errorCode: string
  hint: string
  total?: number
  nextPage?: number
  counts?: { issue?: number; pr?: number }
}

const CACHE_TTL = 5 * 60_000
const DEBOUNCE: Record<LaneName, number> = { solo: 120, artifact: 120, discussion: 250, github: 250 }
const GITHUB_EXACT = /^(?:https?:\/\/github\.com\/|[\w.-]+\/[\w.-]+#\d+$|#\d+$)/i

function emptyLane<T>(): LaneState<T> {
  return { items: [], loading: false, paging: false, skipped: false, settled: false, error: '', errorCode: '', hint: '' }
}

/**
 * Picker search with one lane per source: debounced, cancelled when the query
 * moves on, sequence-checked so a slow reply never overwrites a newer one,
 * and cached per query for five minutes. The previous results, hint or error
 * stay on screen until the new reply lands, so typing does not flash.
 */
export function useReferenceSearch(scope: Ref<ReferenceScope>, scopedSearch?: ScopedReferenceSearch, allowWorkspace = true) {
  const solo = reactive(emptyLane<SoloReferenceCandidate>()) as LaneState<SoloReferenceCandidate>
  const github = reactive(emptyLane<GitHubReferenceCandidate>()) as LaneState<GitHubReferenceCandidate>
  const discussion = reactive(emptyLane<GitHubReferenceCandidate>()) as LaneState<GitHubReferenceCandidate>
  const artifact = reactive(emptyLane<ArtifactReferenceCandidate>()) as LaneState<ArtifactReferenceCandidate>
  const lanes = { solo, github, discussion, artifact }
  const control: Record<LaneName, { seq: number; timer?: ReturnType<typeof setTimeout>; abort?: AbortController }> = {
    solo: { seq: 0 },
    github: { seq: 0 },
    discussion: { seq: 0 },
    artifact: { seq: 0 },
  }
  const cache = new Map<string, { at: number; data: ReferenceSearchResult<any> }>()

  function apply<T>(lane: LaneState<T>, data: ReferenceSearchResult<T>, append = false) {
    lane.items = append ? [...lane.items, ...data.items] : data.items
    lane.total = data.total
    lane.nextPage = data.nextPage
    lane.counts = data.counts ?? (append ? lane.counts : undefined)
    lane.hint = data.hint || ''
    lane.error = data.error?.message || ''
    lane.errorCode = data.error?.code || ''
    lane.settled = true
  }

  function cancel(name: LaneName) {
    const c = control[name]
    clearTimeout(c.timer)
    c.abort?.abort()
    c.seq++
  }

  function run(name: LaneName, params: LaneParams, page = 1) {
    const lane = lanes[name] as LaneState<any>
    const c = control[name]
    const key = `${scopeKey(scope.value)}|${name}|${params.kind || ''}|${params.state || ''}|${params.q.toLowerCase()}`
    cancel(name)
    const my = c.seq
    lane.skipped = false
    const hit = page === 1 ? cache.get(key) : undefined
    if (page === 1) {
      lane.paging = false
      if (hit) {
        apply(lane, hit.data)
        lane.loading = false
        if (Date.now() - hit.at < CACHE_TTL) return
      }
      lane.loading = true
    } else {
      lane.paging = true
    }
    const delay = page > 1 || ((name === 'github' || name === 'discussion') && GITHUB_EXACT.test(params.q)) ? 0 : DEBOUNCE[name]
    c.timer = setTimeout(async () => {
      const abort = new AbortController()
      c.abort = abort
      try {
        const data = await fetchLane(scope.value, { lane: name, ...params, page }, abort.signal, scopedSearch)
        if (my !== c.seq) return
        if (page === 1) cache.set(key, { at: Date.now(), data })
        apply(lane, data, page > 1)
      } catch (e: any) {
        if (abort.signal.aborted || my !== c.seq) return
        if (page === 1) {
          lane.items = []
          lane.counts = undefined
        }
        lane.error = String(e?.message || e)
        lane.errorCode = 'upstream'
        lane.settled = true
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

  function laneParams(name: LaneName, q: string, tab: ReferenceTab, state: GitHubStateFilter): LaneParams {
    if (name === 'github') return { q, state, kind: tab === 'issue' ? 'github_issue' : tab === 'pr' ? 'github_pr' : undefined }
    if (name === 'discussion') return { q, state }
    return { q }
  }

  /** Each tab keeps a result count. An exact GitHub URL searches only its own kind. */
  function search(q: string, tab: ReferenceTab, state: GitHubStateFilter) {
    const query = q.trim()
    const link = /^https?:\/\//i.test(query) ? parseGitHubReference(query) : null
    const local = allowWorkspace && !link && !query.startsWith('#')
    for (const name of ['solo', 'artifact'] as const) {
      if (local) run(name, { q: query })
      else skip(name)
    }
    if (allowWorkspace && (!link || link.kind === 'github_discussion')) run('discussion', laneParams('discussion', query, tab, state))
    else skip('discussion')
    if (link?.kind === 'github_discussion') skip('github')
    else run('github', laneParams('github', query, tab, state))
  }

  function loadMore(name: LaneName, q: string, tab: ReferenceTab, state: GitHubStateFilter) {
    const lane = lanes[name]
    if (!lane.nextPage || lane.loading || lane.paging) return
    run(name, laneParams(name, q.trim(), tab, state), lane.nextPage)
  }

  function reset() {
    for (const name of ['solo', 'github', 'discussion', 'artifact'] as const) {
      cancel(name)
      Object.assign(lanes[name], emptyLane())
    }
  }

  onBeforeUnmount(reset)
  return { ...lanes, search, loadMore, reset }
}
