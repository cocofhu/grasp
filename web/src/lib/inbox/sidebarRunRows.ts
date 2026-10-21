import type { ClarifyInboxItem, InboxItem, Run } from '@/lib/shared/types'

/** Run statuses listed under「正在执行」; everything else is history. */
export const RUNNING_RUN_STATUSES = 'queued,running,waiting_human'
export const HISTORY_RUN_STATUSES = 'completed,failed,cancelled'

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
 * One row per run. A run with several pending items shows the one the inbox
 * has open (else the first listed); running runs without a pending item follow.
 */
export function mergeRunRows(pending: InboxItem[], running: Run[], activeKey: string | null): InboxItem[] {
  const byRun = new Map<string, InboxItem>()
  const order: string[] = []
  for (const it of pending) {
    if (!byRun.has(it.runId)) {
      byRun.set(it.runId, it)
      order.push(it.runId)
    } else if (activeKey && itemKeyOf(it) === activeKey) {
      byRun.set(it.runId, it)
    }
  }
  for (const run of running) {
    if (byRun.has(run.id)) continue
    byRun.set(run.id, runRowFromRun(run))
    order.push(run.id)
  }
  return order.map((id) => byRun.get(id)!)
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
