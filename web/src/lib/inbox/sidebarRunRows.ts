import type { ClarifyInboxItem, InboxItem, Run, RunCurrentNode } from '@/lib/shared/types'

/** Run statuses listed under「正在执行」; everything else is history. */
export const RUNNING_RUN_STATUSES = 'queued,running,waiting_human'
export const HISTORY_RUN_STATUSES = 'completed,failed,cancelled'

/**
 * 「自动过程」: the run is on an automatic agent that is running or stopping.
 * Takeover, waiting for a person, a queued run, and Solo are not this group.
 */
export function isAutoProcessNode(n: RunCurrentNode | null | undefined): boolean {
  return !!n && n.kind === 'agent' && n.mode === 'auto' && (n.state === 'running' || n.state === 'stopping')
}

export function isAutoProcessRun(run: Run | null | undefined): boolean {
  return isAutoProcessNode(run?.currentNode)
}

/** Current automatic node name, or '' when the run is not in「自动过程」or the name is blank. */
export function autoProcessNodeName(run: Run | null | undefined): string {
  if (!isAutoProcessRun(run)) return ''
  return run?.currentNode?.name?.trim() || ''
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
