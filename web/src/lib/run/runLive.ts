import type { NodeRun, Run } from '@/lib/shared/types'

/** Pure helpers for the live (read-only) view of an auto node. */

/** Where the live view should be: watch this node, the chat, another live node, or run detail. */
export type LiveDecision =
  | { kind: 'observe'; nodeId: string; iteration: number; stopping?: boolean }
  | { kind: 'chat'; nodeId: string }
  | { kind: 'follow'; nodeId: string }
  | { kind: 'detail' }

const isManual = (nr?: NodeRun) => nr?.mode === 'manual'

/** First node whose latest visit is running automatically. */
export function runningAutoNodeId(run: Run): string {
  for (const [id, nr] of Object.entries(run.nodeRuns || {})) {
    if (nr?.status === 'running' && !isManual(nr)) return id
  }
  return ''
}

/** First node parked for a person (gate, clarify, review, takeover). */
export function waitingNodeId(run: Run): string {
  for (const [id, nr] of Object.entries(run.nodeRuns || {})) {
    if (nr?.status === 'waiting_human') return id
  }
  return ''
}

/**
 * Decide what the live view shows for `requested` (route ?node=, may be empty).
 * A taken-over node goes to its chat; a finished node follows the run to the
 * next auto node; a run parked elsewhere goes to that chat; else run detail.
 */
export function decideLive(run: Run, requested: string): LiveDecision {
  const nr = requested ? run.nodeRuns?.[requested] : undefined
  if (nr && nr.status === 'running' && !isManual(nr)) {
    return { kind: 'observe', nodeId: requested, iteration: nr.iteration ?? 1 }
  }
  // Taken over but the auto call is still winding down (state=stopping).
  if (nr && isManual(nr) && nr.status === 'running') {
    return { kind: 'observe', nodeId: requested, iteration: nr.iteration ?? 1, stopping: true }
  }
  if (nr && isManual(nr) && nr.status === 'waiting_human') return { kind: 'chat', nodeId: requested }
  if (run.status === 'running' || run.status === 'queued' || run.status === 'waiting_human') {
    const next = runningAutoNodeId(run)
    if (next) return requested ? { kind: 'follow', nodeId: next } : { kind: 'observe', nodeId: next, iteration: run.nodeRuns[next]?.iteration ?? 1 }
    const waiting = waitingNodeId(run)
    if (waiting) return { kind: 'chat', nodeId: waiting }
    // Between nodes (the engine is routing): stay and let the next refresh decide.
    if (requested && run.status === 'running') return { kind: 'observe', nodeId: requested, iteration: nr?.iteration ?? 1 }
  }
  return { kind: 'detail' }
}

/** takeover 409/404 codes the server sends in `error`. */
export const TAKEOVER_ERROR_CODES = [
  'not_auto_node',
  'node_not_current',
  'node_finishing',
  'run_not_running',
  'server_halting',
  'run_not_found',
  'node_not_found',
] as const
export type TakeoverErrorCode = (typeof TAKEOVER_ERROR_CODES)[number]

export function takeoverErrorCode(e: unknown): TakeoverErrorCode | null {
  const msg = e instanceof Error ? e.message : ''
  return (TAKEOVER_ERROR_CODES as readonly string[]).includes(msg) ? (msg as TakeoverErrorCode) : null
}

/** Codes where the node moved on by itself: refresh and follow instead of failing. */
export function takeoverRaceLost(code: TakeoverErrorCode | null): boolean {
  return code === 'node_finishing' || code === 'node_not_current'
}

/** Run WS frame shapes this view consumes. */
export type LiveFrame =
  | { type: 'acp'; runId?: string; nodeId?: string; events?: unknown[]; busy?: boolean }
  | { type: 'node_takeover'; runId?: string; nodeId?: string; iteration?: number; mode?: 'manual'; state?: 'stopping' | 'waiting_human' }
  | { type: string; [k: string]: unknown }
