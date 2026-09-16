import { isGrasp } from '@/lib/shared/clarifyInteractive'
import { isStartingInboxItem } from '@/lib/inbox/inboxDisplay'
import { clipRunTitle } from '@/lib/run/runTitle'
import type { ClarifyInboxItem, InboxItem, Run } from '@/lib/shared/types'

/** The approval a just-started run is expected to park on. */
export type IncomingApproval = { runId: string; nodeId: string }

/**
 * The approval the inbox is waiting for, from `?run=&node=` or the pending home
 * handoff. The published-graph node id may differ from the parked Approve id
 * (`ap` vs `approve_7gl8`), so nodeId is a hint and runId is the identity.
 */
export function resolveIncomingApproval(
  queryRun: unknown,
  queryNode: unknown,
  seed?: { runId?: string; nodeId?: string } | null,
): IncomingApproval | null {
  const runId = String(queryRun ?? '').trim() || seed?.runId || ''
  if (!runId) return null
  return { runId, nodeId: String(queryNode ?? '').trim() || seed?.nodeId || '' }
}

/** Optimistic loading row shown until listGates returns the real approval. */
export function makeIncomingGhost(
  target: IncomingApproval,
  seedText = '',
  now: string = new Date().toISOString(),
): ClarifyInboxItem {
  const label = clipRunTitle(seedText.trim()) || '…'
  return {
    type: 'clarify',
    state: 'starting',
    runId: target.runId,
    nodeId: target.nodeId || 'grasp',
    iteration: 1,
    workflowName: '',
    runTitle: label,
    label,
    done: false,
    requestedAt: now,
    updatedAt: now,
  }
}

/**
 * Whether a starting approval never came up.
 *
 * A clarify/approve sandbox-setup failure records the failure on the node
 * execution and stops the run *without* marking the run terminal, so run status
 * alone would miss exactly the case this has to detect. A whole-run failure or
 * cancel still counts. When the hinted node id misses, fall back to Approve
 * nodes on `run.nodes` so `ap` vs `approve_7gl6` cannot hide a setup failure.
 */
export function isStartFailedRun(
  run: Pick<Run, 'status'> & { nodeRuns?: Run['nodeRuns']; nodes?: Run['nodes'] },
  nodeId: string,
): boolean {
  if (run.status === 'failed' || run.status === 'cancelled') return true
  const nodeStatus = nodeId ? run.nodeRuns?.[nodeId]?.status : undefined
  if (nodeStatus === 'failed' || nodeStatus === 'cancelled') return true
  if (nodeStatus !== undefined) return false

  const approveIds = (run.nodes || []).filter((n) => isGrasp(n.type)).map((n) => n.id)
  for (const id of approveIds) {
    const st = run.nodeRuns?.[id]?.status
    if (st === 'failed' || st === 'cancelled') return true
  }
  return false
}

const leftBootStatuses = new Set(['waiting_human', 'completed', 'failed', 'cancelled'])

function nodeRunStatus(
  run: { nodeRuns?: Run['nodeRuns'] },
  nodeId: string,
): string | undefined {
  return nodeId ? run.nodeRuns?.[nodeId]?.status : undefined
}

/**
 * Whether an incoming ghost may still represent a live sandbox boot.
 *
 * Used when `?run=&node=` (or a home handoff) would rebuild a starting card
 * even though the run is absent from the pending list — e.g. cold refresh after
 * the approval already parked, completed, or moved on.
 *
 * `nodeId` is only a hint (`ap` vs `approve_7gl6` may differ). Prefer that
 * node's status when present; otherwise consult approve nodes from `run.nodes`
 * so a completed input + still-booting approve is not mistaken for "already
 * moved on", and a completed approve + running implement still drops the ghost.
 */
export function isApproveStillStarting(
  run: Pick<Run, 'status'> & { nodeRuns?: Run['nodeRuns']; nodes?: Run['nodes'] },
  nodeId: string,
): boolean {
  if (run.status === 'failed' || run.status === 'cancelled' || run.status === 'completed') {
    return false
  }

  const hinted = nodeRunStatus(run, nodeId)
  if (hinted) {
    if (leftBootStatuses.has(hinted)) return false
    if (hinted === 'running') return true
  }

  const approveIds = (run.nodes || []).filter((n) => isGrasp(n.type)).map((n) => n.id)
  if (approveIds.length > 0) {
    let sawApprove = false
    for (const id of approveIds) {
      const st = nodeRunStatus(run, id)
      if (!st) continue
      sawApprove = true
      if (st === 'running') return true
      if (leftBootStatuses.has(st)) return false
    }
    // Graph has Approve, but no StateRun yet — still launching.
    if (!sawApprove) return run.status === 'queued' || run.status === 'running'
    return false
  }

  // No graph: only keep the ghost before any node has parked or finished.
  const runs = Object.values(run.nodeRuns || {})
  if (runs.some((n) => n?.status && leftBootStatuses.has(n.status))) return false
  return run.status === 'queued' || run.status === 'running'
}

/**
 * Whether the deep-linked approve/gate is still waiting for a human (parked).
 * Used to pin `?run=&node=` after boot when the filtered list omits the row
 * (cross-project / wf / tag mismatch) — only leave once the node left pending.
 */
export function isApproveAwaitingHuman(
  run: Pick<Run, 'status'> & { nodeRuns?: Run['nodeRuns']; nodes?: Run['nodes'] },
  nodeId: string,
): boolean {
  if (run.status === 'failed' || run.status === 'cancelled' || run.status === 'completed') {
    return false
  }

  const hinted = nodeRunStatus(run, nodeId)
  if (hinted === 'waiting_human') return true
  if (hinted !== undefined) return false

  const approveIds = (run.nodes || []).filter((n) => isGrasp(n.type)).map((n) => n.id)
  for (const id of approveIds) {
    if (nodeRunStatus(run, id) === 'waiting_human') return true
  }
  return run.status === 'waiting_human'
}

/**
 * Starting rows present in `before` but gone from `after`. Each one still needs
 * a run check before it counts as a failure: a filter or page change drops live
 * rows too. `ignoreKey` skips the client-side ghost, which is not a server row.
 */
export function vanishedStartingRows(
  before: InboxItem[],
  after: InboxItem[],
  keyOf: (it: InboxItem) => string,
  ignoreKey = '',
): InboxItem[] {
  if (!before.length) return []
  const present = new Set(after.map(keyOf))
  return before.filter((it) => {
    if (!isStartingInboxItem(it)) return false
    const key = keyOf(it)
    return key !== ignoreKey && !present.has(key)
  })
}
