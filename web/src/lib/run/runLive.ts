import type { Run, RunCurrentNode } from '@/lib/shared/types'
import { enteringAutoNode, isObservableNode, recentCurrentNode } from './recentRuns'

/** Pure helpers for the live (read-only) view of an auto node. */

/**
 * Where the live view should be: watch this node, the chat, another live node,
 * or run detail. iteration is absent while the engine routes between nodes
 * (the caller keeps the one it has).
 */
export type LiveDecision =
  | { kind: 'observe'; nodeId: string; iteration?: number; stopping?: boolean; task?: string }
  | { kind: 'chat'; nodeId: string }
  | { kind: 'follow'; nodeId: string }
  | { kind: 'wait'; nodeId: string; label: string }
  | { kind: 'detail' }

/**
 * The auto node the run is on now (server currentNode: kind=agent, mode=auto,
 * state=running), or null. A confirm can name the next automatic agent before
 * its visit exists (state=entering). That visit is not observable yet; the
 * live page waits for it instead of leaving.
 */
export function runningAutoNode(run: Run): RunCurrentNode | null {
  const n = recentCurrentNode(run)
  return n && n.kind === 'agent' && n.mode === 'auto' && n.state === 'running' ? n : null
}

/**
 * Decide what the live view shows for `requested` (route ?node=, may be empty),
 * from the server's run.currentNode (GET /runs/:id and the WS snapshot frame).
 * A taken-over node still unwinding (kind=takeover, state=stopping) keeps the
 * stream with the spinner; a node parked for a person goes to its chat; a
 * finished node follows the run to its next auto node; a chosen visit that
 * has not started waits on this page; a finished run leaves for detail.
 * useRunLive only acts on follow / detail / another node's chat while it is
 * still routing the page it was opened on; once a node is on screen, the
 * page stays put and shows that node as finished.
 */
export function decideLive(run: Run, requested: string): LiveDecision {
  const entering = enteringAutoNode(run)
  if (entering) {
    // Chosen, visit not started. Stay on the conversation and show 准备中.
    return { kind: 'wait', nodeId: entering.id, label: entering.name?.trim() || '' }
  }
  const cur = recentCurrentNode(run)
  if (cur) {
    if (isObservableNode(cur)) {
      if (requested && requested !== cur.id) return { kind: 'follow', nodeId: cur.id }
      const task = typeof cur.task === 'string' ? cur.task.trim() : ''
      return {
        kind: 'observe',
        nodeId: cur.id,
        iteration: cur.iteration || 1,
        ...(cur.state === 'stopping' ? { stopping: true } : {}),
        ...(task ? { task } : {}),
      }
    }
    // gate / clarify / review / takeover parked for a person (or busy in a manual turn).
    return { kind: 'chat', nodeId: cur.id }
  }
  // Between nodes (the engine is routing): keep a requested visit, otherwise
  // wait on this page. An active run must not fall through to the canvas.
  if (requested && (run.status === 'running' || run.status === 'queued')) return { kind: 'observe', nodeId: requested }
  if (run.status === 'running' || run.status === 'waiting_human') {
    return { kind: 'wait', nodeId: requested, label: '' }
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

/** 409 body `current` (the server's re-read view of the run). */
export interface TakeoverConflictCurrent {
  runStatus?: string
  nodeId?: string
  iteration?: number
  mode?: string
}

/** current.runStatus of a 409 (run_not_running carries the re-read value). */
export function takeoverConflictRunStatus(e: unknown): string {
  const cur = e && typeof e === 'object' ? (e as { current?: TakeoverConflictCurrent }).current : undefined
  return typeof cur?.runStatus === 'string' ? cur.runStatus : ''
}

/**
 * Codes where the node moved on by itself: refresh and follow instead of
 * failing. node_finishing only means the node is settling / switching.
 */
export function takeoverRaceLost(code: TakeoverErrorCode | null): boolean {
  return code === 'node_finishing' || code === 'node_not_current'
}

/** Run WS frame shapes this view consumes. */
export type LiveFrame =
  | { type: 'acp'; runId?: string; nodeId?: string; events?: unknown[]; busy?: boolean }
  | { type: 'node_takeover'; runId?: string; nodeId?: string; iteration?: number; mode?: 'manual'; state?: 'stopping' | 'waiting_human' }
  | { type: 'snapshot'; run?: Run }
  | { type: string; [k: string]: unknown }
