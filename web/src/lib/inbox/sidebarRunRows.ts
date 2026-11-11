import type { ClarifyInboxItem, InboxItem, Run, RunCurrentNode, WFEdge, WFNode } from '@/lib/shared/types'
import { isGated } from '@/lib/workflow/agentCapabilities'

/** Run statuses listed under「正在执行」; everything else is history. */
export const RUNNING_RUN_STATUSES = 'queued,running,waiting_human'
export const HISTORY_RUN_STATUSES = 'completed,failed,cancelled'
/**
 * Largest page the run list accepts. Callers walk every page, so this cap
 * cannot hide a run that is still going.
 */
export const RUN_LIST_PAGE_SIZE = 100
/** Stop a broken total from paging forever (100 × 100). */
const RUN_LIST_MAX_PAGES = 100

type PauseShape = { status?: string; workspaceSave?: unknown }

/** Paused only to save the workspace. A user pause has no workspaceSave. */
export function isWorkspaceSavePause(run: PauseShape | null | undefined): boolean {
  return !!run && run.status === 'paused' && !!run.workspaceSave
}

/** User pause. It is not part of the current view. */
export function isManualPause(run: PauseShape | null | undefined): boolean {
  return !!run && run.status === 'paused' && !run.workspaceSave
}

/**
 * Still on screen in「工作流」and「进行中」: executing, or a workspace save
 * that has not finished. A user pause is not this.
 */
export function isCurrentViewRun(run: PauseShape | null | undefined): boolean {
  if (!run) return false
  return run.status === 'running' || isWorkspaceSavePause(run)
}

/**
 * Run-list「进行中」, including the older link that also named queued.
 * Queued rows stay out of that table; the sidebar「排队中」section keeps them.
 */
export function isInProgressListQuery(status: string | undefined): boolean {
  const parts = (status ?? '').split(',').map((part) => part.trim()).filter(Boolean).sort()
  const key = parts.join(',')
  return key === 'running' || key === 'queued,running'
}

export interface RunListPage<T> {
  items?: T[]
  total?: number
  hasMore?: boolean
}

export interface RunListQuery {
  status?: string
  tag?: string
  wf?: string
  page?: number
  pageSize?: number
  sort?: string
  order?: string
  signal?: AbortSignal
}

/**
 * Every item of one status query. A full page continues while the server
 * still has more; a short page ends the walk.
 */
export async function listAllRunPages<T extends { id?: string }>(
  fetchPage: (page: number, pageSize: number) => Promise<RunListPage<T>>,
  pageSize = RUN_LIST_PAGE_SIZE,
): Promise<T[]> {
  const items: T[] = []
  const seen = new Set<string>()
  for (let page = 1; page <= RUN_LIST_MAX_PAGES; page++) {
    const data = await fetchPage(page, pageSize)
    if (!Array.isArray(data?.items)) throw new Error('Invalid run list')
    let added = 0
    for (const item of data.items) {
      if (item?.id) {
        if (seen.has(item.id)) continue
        seen.add(item.id)
      }
      items.push(item)
      added++
    }
    const total = typeof data.total === 'number' && Number.isFinite(data.total) ? data.total : items.length
    if (added === 0 || items.length >= total || data.hasMore === false) break
    if (data.items.length < pageSize && data.hasMore !== true) break
  }
  return items
}

function dedupeRuns(runs: Run[]): Run[] {
  const seen = new Set<string>()
  const out: Run[] = []
  for (const run of runs) {
    if (!run?.id || seen.has(run.id)) continue
    seen.add(run.id)
    out.push(run)
  }
  return out
}

type ListRuns = (params: RunListQuery) => Promise<RunListPage<Run>>

/**
 * Sidebar rows that are not finished: every running, waiting, and queued run,
 * plus every workspace-save pause. User pauses are read on their own pages
 * and then left out, so they cannot take a running run's place.
 */
export async function listSidebarLiveRuns(listRuns: ListRuns, opts: { signal?: AbortSignal } = {}): Promise<Run[]> {
  const base = { sort: 'last_activity', order: 'desc' as const, signal: opts.signal }
  const [live, paused] = await Promise.all([
    listAllRunPages((page, pageSize) => listRuns({ ...base, status: RUNNING_RUN_STATUSES, page, pageSize })),
    listAllRunPages((page, pageSize) => listRuns({ ...base, status: 'paused', page, pageSize })),
  ])
  const saves = paused.filter(isWorkspaceSavePause)
  return dedupeRuns([...live, ...saves]).filter((run) => !isManualPause(run))
}

/**
 * Run-list「进行中」: every running workflow and every unfinished workspace
 * save. Queued and waiting runs are not part of this result.
 */
export async function listCurrentViewRuns(
  listRuns: ListRuns,
  query: Omit<RunListQuery, 'page' | 'pageSize' | 'status'> = {},
  opts: { includeQueued?: boolean } = {},
): Promise<Run[]> {
  const [running, paused, queued] = await Promise.all([
    listAllRunPages((page, pageSize) => listRuns({ ...query, status: 'running', page, pageSize })),
    listAllRunPages((page, pageSize) => listRuns({ ...query, status: 'paused', page, pageSize })),
    opts.includeQueued
      ? listAllRunPages((page, pageSize) => listRuns({ ...query, status: 'queued', page, pageSize }))
      : Promise.resolve([] as Run[]),
  ])
  const live = running.filter((run) => run.status === 'running')
  const saves = paused.filter(isWorkspaceSavePause)
  const waitingQueue = queued.filter((run) => run.status === 'queued')
  return dedupeRuns([...live, ...saves, ...waitingQueue])
}

/**
 * 「自动过程」: the run is on an automatic agent that is running, stopping,
 * or already chosen and not started yet (entering). Takeover, waiting for a
 * person, a queued run, and Solo are not this group.
 */
export function isAutoProcessNode(n: RunCurrentNode | null | undefined): boolean {
  return !!n && n.kind === 'agent' && n.mode === 'auto' && (n.state === 'running' || n.state === 'stopping' || n.state === 'entering')
}

export function isAutoProcessRun(run: Run | null | undefined): boolean {
  return isAutoProcessNode(run?.currentNode)
}

/** Current automatic node name, or '' when the run is not in「自动过程」or the name is blank. */
export function autoProcessNodeName(run: Run | null | undefined): string {
  if (!isAutoProcessRun(run)) return ''
  return run?.currentNode?.name?.trim() || ''
}

/** Automatic node this handoff already settled on, before the run list catches up. */
export interface SidebarAutoClaim {
  fromNodeId: string
  confirmedAuto: boolean
  autoNodeId?: string
  autoNodeName?: string
  /**
   * Confirm window is still on screen. The row stays where it was so the
   * fly into「自动过程」cannot start before the window leaves.
   */
  held?: boolean
}

const HUMAN_CURRENT_KINDS = new Set(['gate', 'clarify', 'review', 'takeover'])

/**
 * The list has left the node this click confirmed and landed on a person
 * (gate, clarify, review, takeover, or any other non-agent manual node).
 * A missing node, the node just left, or an automatic agent that is not
 * running yet is not this.
 */
export function sidebarLandedOnHuman(node: RunCurrentNode | null | undefined, fromNodeId: string | undefined): boolean {
  if (!node?.id || !fromNodeId || node.id === fromNodeId) return false
  if (HUMAN_CURRENT_KINDS.has(node.kind)) return true
  return node.mode === 'manual' && node.kind !== 'agent'
}

/**
 * 「自动过程」when the snapshot already shows a running automatic agent, or
 * this handoff has settled on one and the snapshot has not clearly landed on
 * a different human node. Still being on the node just left, having no
 * current node, or not yet marking the automatic agent as running all stay.
 */
export function sidebarRunGroup(run: Run | null | undefined, claim: SidebarAutoClaim | null | undefined): 'running' | 'auto' {
  if (claim?.held) {
    // Already inside「自动过程」on the node being confirmed: stay there.
    // A snapshot that jumped to the next automatic node waits until release.
    if (isAutoProcessRun(run) && run?.currentNode?.id && run.currentNode.id === claim.fromNodeId) return 'auto'
    return 'running'
  }
  if (isAutoProcessRun(run)) return 'auto'
  if (!claim?.confirmedAuto) return 'running'
  if (sidebarLandedOnHuman(run?.currentNode, claim.fromNodeId)) return 'running'
  return 'auto'
}

/** Node name for an automatic row: the snapshot once it has caught up, otherwise the settled handoff. */
export function sidebarAutoNodeName(run: Run | null | undefined, claim: SidebarAutoClaim | null | undefined): string {
  if (isAutoProcessRun(run)) return autoProcessNodeName(run)
  if (sidebarRunGroup(run, claim) !== 'auto') return ''
  return claim?.autoNodeName?.trim() || ''
}

export interface SidebarAutoTarget {
  nodeId: string
  name: string
}

/**
 * Outlet this positive confirmation leaves through.
 * A human gate leaves through the action. An agent leaves through the plain
 * outlet, unless a verdict product chooses pass or fail from this confirm's
 * model output — then the outlet is not known yet.
 */
export function positiveHandoffHandle(node: Pick<WFNode, 'type' | 'caps'> | null | undefined, action?: string): string | null {
  if (!node?.type) return null
  if (node.type === 'human_gate') return action ?? ''
  if (node.type !== 'agent') return null
  if (isGated(node.caps)) return null
  return ''
}

function guardCertainty(when: string | undefined): 'pass' | 'fail' | 'unknown' {
  const w = (when ?? '').trim()
  if (w === '' || w === 'default' || w === 'true') return 'pass'
  if (w === 'false') return 'fail'
  return 'unknown'
}

/**
 * First success edge on `handle` that this confirmation cannot rule out.
 * An unknown guard, or a rollback, means the destination is not settled.
 * A guard that is certainly false is skipped. Later edges do not matter
 * once an earlier one always passes — that is the edge the engine takes.
 */
function certainSuccessTarget(edges: readonly WFEdge[], from: string, handle: string): string | null {
  for (const edge of edges) {
    if (edge.source !== from || (edge.sourceHandle ?? '') !== handle || edge.kind === 'failure') continue
    const guard = guardCertainty(edge.when)
    if (guard === 'fail') continue
    if (guard === 'unknown' || edge.kind === 'rollback') return null
    return edge.target
  }
  return null
}

const PLUMBING_NODE_TYPES = new Set(['input', 'output'])

/**
 * The one automatic agent this positive handoff enters, when the loaded graph
 * already says so. Null when the graph has no edges, several destinations
 * stay possible, or the one destination is not an automatic agent.
 * input/output are skipped; they do not count as the next real node.
 */
export function uniqueAutoDestination(
  nodes: readonly WFNode[] | undefined,
  edges: readonly WFEdge[] | undefined,
  fromNodeId: string,
  handle: string | null,
): SidebarAutoTarget | null {
  if (!fromNodeId || handle == null || !nodes?.length || !edges?.length) return null
  const byId = new Map(nodes.map((node) => [node.id, node]))
  let cursor = fromNodeId
  let hopHandle = handle
  const seen = new Set<string>()
  for (let hop = 0; hop <= nodes.length; hop++) {
    const key = `${cursor}\0${hopHandle}`
    if (seen.has(key)) return null
    seen.add(key)
    const targetId = certainSuccessTarget(edges, cursor, hopHandle)
    if (!targetId) return null
    const node = byId.get(targetId)
    if (!node) return null
    if (PLUMBING_NODE_TYPES.has(node.type)) {
      cursor = node.id
      hopHandle = ''
      continue
    }
    if (node.type !== 'agent' || node.caps?.interaction !== 'auto') return null
    return { nodeId: node.id, name: node.label?.trim() || '' }
  }
  return null
}

/** Click-time destination for one positive handoff. No edges, or more than one possible target: null. */
export function sidebarAutoTarget(
  nodes: readonly WFNode[] | undefined,
  edges: readonly WFEdge[] | undefined,
  fromNodeId: string,
  action?: string,
): SidebarAutoTarget | null {
  const node = nodes?.find((item) => item.id === fromNodeId)
  return uniqueAutoDestination(nodes, edges, fromNodeId, positiveHandoffHandle(node, action))
}

/**
 * Whether this confirmation enters an automatic process.
 * True when the one settled destination is an automatic agent.
 * False when that destination is not (a person, a branch, or the graph ends after plumbing).
 * Null when the loaded graph does not settle one destination — still unknown, not “not automatic”.
 */
export function handoffProcessAuto(
  nodes: readonly WFNode[] | undefined,
  edges: readonly WFEdge[] | undefined,
  fromNodeId: string,
  action?: string,
): boolean | null {
  const from = nodes?.find((item) => item.id === fromNodeId)
  const handle = positiveHandoffHandle(from, action)
  if (!fromNodeId || handle == null || !nodes?.length || !edges?.length) return null
  const byId = new Map(nodes.map((node) => [node.id, node]))
  let cursor = fromNodeId
  let hopHandle = handle
  const seen = new Set<string>()
  for (let hop = 0; hop <= nodes.length; hop++) {
    const key = `${cursor}\0${hopHandle}`
    if (seen.has(key)) return null
    seen.add(key)
    const targetId = certainSuccessTarget(edges, cursor, hopHandle)
    if (!targetId) return cursor === fromNodeId ? null : false
    const node = byId.get(targetId)
    if (!node) return null
    if (PLUMBING_NODE_TYPES.has(node.type)) {
      cursor = node.id
      hopHandle = ''
      continue
    }
    return node.type === 'agent' && node.caps?.interaction === 'auto'
  }
  return null
}

/**
 * Click-time three-state for the boot gate.
 * A snapshot that has already left this node wins; otherwise the loaded graph.
 */
export function resolveHandoffProcessAuto(input: {
  nodes?: readonly WFNode[]
  edges?: readonly WFEdge[]
  fromNodeId: string
  action?: string
  current?: { id?: string; mode?: string } | null
}): boolean | null {
  const cur = input.current
  if (cur?.id && input.fromNodeId && cur.id !== input.fromNodeId) {
    if (cur.mode === 'auto') return true
    if (cur.mode === 'manual') return false
  }
  return handoffProcessAuto(input.nodes, input.edges, input.fromNodeId, input.action)
}

const itemKeyOf = (it: InboxItem) => `${it.runId}:${it.nodeId}`

/** Sidebar row for a running run with no pending item: same shape as a pending row. */
export function runRowFromRun(run: Run): ClarifyInboxItem {
  const n = run.currentNode
  const at = run.lastActivityAt || run.startedAt || run.createdAt || ''
  return {
    type: 'clarify',
    runId: run.id,
    nodeId: n?.id || '',
    iteration: n?.iteration ?? 1,
    workflowId: run.workflowId,
    workflowName: run.workflowName,
    runTitle: run.title,
    label: n?.name || run.currentNodeLabel || '',
    done: false,
    requestedAt: at,
    updatedAt: at,
  }
}

/**
 * One row per unfinished run, in that list's order. A pending item only
 * decorates a run already in the list (the open item wins, else the first).
 * Inbox runs outside the unfinished list do not add a row.
 */
export function mergeRunRows(pending: InboxItem[], running: Run[], activeKey: string | null): InboxItem[] {
  const pendingByRun = new Map<string, InboxItem>()
  for (const it of pending) {
    const current = pendingByRun.get(it.runId)
    if (!current) {
      pendingByRun.set(it.runId, it)
      continue
    }
    if (activeKey && itemKeyOf(it) === activeKey) pendingByRun.set(it.runId, it)
  }
  const rows: InboxItem[] = []
  const seen = new Set<string>()
  for (const run of running) {
    if (!run.id || seen.has(run.id)) continue
    seen.add(run.id)
    rows.push(pendingByRun.get(run.id) ?? runRowFromRun(run))
  }
  return rows
}

/**
 * Keep rows already on screen in their previous relative order (a run moving
 * from waiting to running must not jump); new runs take their natural slot.
 */
export function keepRowOrder<T extends { runId: string }>(rows: T[], previous: readonly string[]): T[] {
  const at = new Map(previous.map((id, i) => [id, i]))
  const known = rows.filter((r) => at.has(r.runId)).sort((a, b) => at.get(a.runId)! - at.get(b.runId)!)
  let k = 0
  return rows.map((r) => (at.has(r.runId) ? known[k++]! : r))
}

/** Pin keys were `run:node` before rows were per run; both forms pin the run. */
export function pinRunId(key: string): string {
  const i = key.indexOf(':')
  return i < 0 ? key : key.slice(0, i)
}
