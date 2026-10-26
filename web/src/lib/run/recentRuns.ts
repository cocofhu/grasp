import type { RouteLocationRaw } from 'vue-router'
import { nodeConversationPath, nodeConversationRunId } from '@/lib/run/nodeConversation'
import type { Run, RunCurrentNode } from '@/lib/shared/types'

/**
 * Sidebar「最近」: rows sorted by last activity, marked with the same icons as
 * the 待审批 rows, and routed straight into the node's chat.
 */

export type RecentRun = Run

/** Status primitive on a live sidebar row. */
export type RecentRunMark = {
  icon: { name: 'arc-spinner' | 'breath-dot' | 'user-hold'; toneClass: string }
}

const LIVE_STATES = new Set<RunCurrentNode['state']>(['running', 'stopping', 'waiting_human', 'busy'])

/** The node a running / waiting run is on, or null when the run has none. */
export function recentCurrentNode(run: RecentRun): RunCurrentNode | null {
  const n = run.currentNode
  if (!n || !n.id || !LIVE_STATES.has(n.state)) return null
  if (run.status !== 'running' && run.status !== 'waiting_human') return null
  return n
}

/**
 * A visit the user can only watch (read-only stream + Stop): an auto agent
 * that is running, or one just taken over whose auto call is still unwinding
 * (server: kind=takeover, state=stopping; the chat opens once it waits).
 */
export function isObservableNode(n: RunCurrentNode | null | undefined): boolean {
  if (!n) return false
  if (n.kind === 'agent') return n.mode === 'auto' && (n.state === 'running' || n.state === 'stopping')
  return n.kind === 'takeover' && n.state === 'stopping'
}

/**
 * Live sidebar row: starting or stopping is the arc, a machine at work is the
 * breath dot, and anyone waiting (approval included) is the person-hold mark.
 */
export function recentRunMark(run: RecentRun): RecentRunMark | null {
  const n = recentCurrentNode(run)
  if (!n) return null
  if (n.state === 'stopping' || (n.state === 'running' && n.kind === 'clarify')) {
    return { icon: { name: 'arc-spinner', toneClass: 'nav-row-type-icon--accent' } }
  }
  if (n.state === 'running' || n.state === 'busy') {
    return { icon: { name: 'breath-dot', toneClass: 'nav-row-type-icon--accent' } }
  }
  return { icon: { name: 'user-hold', toneClass: 'nav-row-type-icon--hold' } }
}

export const runLivePath = (runId: string) => `/runs/${encodeURIComponent(runId)}/live`
export const runDetailPath = (runId: string) => `/runs/${encodeURIComponent(runId)}`

/** Click target: the node's chat (live stream for auto, inbox chat otherwise), else run detail. */
export function recentRunTarget(
  run: RecentRun,
  filters?: { wf?: string; tag?: string },
): RouteLocationRaw {
  const n = recentCurrentNode(run)
  if (!n) return runDetailPath(run.id)
  if (isObservableNode(n)) return { path: runLivePath(run.id), query: { node: n.id } }
  const query: Record<string, string> = {}
  if (filters?.wf) query.wf = filters.wf
  if (filters?.tag) query.tag = filters.tag
  const path = nodeConversationPath(run.id, n.id)
  return Object.keys(query).length ? { path, query } : { path }
}

type RouteLike = { path: string; query: Record<string, unknown> }

export function recentRunActive(run: RecentRun, route: RouteLike): boolean {
  if (route.path === runDetailPath(run.id) || route.path === runLivePath(run.id)) return true
  return nodeConversationRunId(route.path) === run.id
}

/** Fields the row renders; an unchanged signature skips the reactive write. */
function rowSignature(run: RecentRun, title: string): string {
  const n = run.currentNode
  return [run.id, title, run.status, n?.id, n?.kind, n?.mode, n?.state].join('\u0001')
}

export function sameRecentRows(a: RecentRun[], b: RecentRun[], titleOf: (r: RecentRun) => string): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (rowSignature(a[i], titleOf(a[i])) !== rowSignature(b[i], titleOf(b[i]))) return false
  }
  return true
}

/**
 * Hold list updates while the user is on the list itself (pointer over it,
 * keyboard focus in it, or a text selection inside it) so a reorder never
 * moves the row under the cursor or drops focus/selection. Updates elsewhere
 * in the page never touch the main composer: the list owns no inputs.
 */
export function createRecentHold(doc: Document = document) {
  let el: HTMLElement | null = null
  let pointerInside = false
  let onRelease: (() => void) | null = null
  const selectionInside = () => {
    const sel = doc.getSelection?.()
    if (!el || !sel || sel.isCollapsed || !sel.rangeCount) return false
    const node = sel.anchorNode
    return !!node && el.contains(node)
  }
  const isHeld = () => !!el && (pointerInside || el.contains(doc.activeElement) || selectionInside())
  const maybeRelease = () => { if (!isHeld()) onRelease?.() }
  const enter = () => { pointerInside = true }
  const leave = () => { pointerInside = false; maybeRelease() }
  const focusOut = () => { queueMicrotask(maybeRelease) }
  const selectionChange = () => maybeRelease()
  return {
    isHeld,
    attach(target: HTMLElement, release: () => void) {
      this.detach()
      el = target
      onRelease = release
      el.addEventListener('pointerenter', enter)
      el.addEventListener('pointerleave', leave)
      el.addEventListener('focusout', focusOut)
      doc.addEventListener('selectionchange', selectionChange)
    },
    detach() {
      if (!el) return
      el.removeEventListener('pointerenter', enter)
      el.removeEventListener('pointerleave', leave)
      el.removeEventListener('focusout', focusOut)
      doc.removeEventListener('selectionchange', selectionChange)
      el = null
      onRelease = null
      pointerInside = false
    },
  }
}

// ---- local event relay (no global WS on the server) -------------------------
// Pages that already hold a run channel (live view, inbox chat) relay state
// changes so the row updates without waiting for the next poll.
type Listener = () => void
const listeners = new Set<Listener>()
let relayTimer: ReturnType<typeof setTimeout> | null = null
/** A notice arrived while a read was already waiting; run once more so it is not dropped. */
let relayFollowUp = false

export const RECENT_RELAY_DEBOUNCE_MS = 300

function flushRecentActivity() {
  relayTimer = null
  const follow = relayFollowUp
  relayFollowUp = false
  for (const fn of [...listeners]) fn()
  if (follow) scheduleRecentActivity()
}

function scheduleRecentActivity() {
  relayTimer = setTimeout(flushRecentActivity, RECENT_RELAY_DEBOUNCE_MS)
}

export function notifyRecentActivity() {
  if (relayTimer) {
    relayFollowUp = true
    return
  }
  scheduleRecentActivity()
}

/** Drop a pending relay. Tests use this so a cleared timer cannot stick the module. */
export function resetRecentActivityRelayForTests(): void {
  if (relayTimer) clearTimeout(relayTimer)
  relayTimer = null
  relayFollowUp = false
}

export function onRecentActivity(fn: Listener): () => void {
  if (typeof fn !== 'function') return () => {}
  listeners.add(fn)
  return () => { listeners.delete(fn) }
}

/** Sidebar poll interval (same cadence as the pending-row poll). */
export const RECENT_POLL_MS = 15_000

/**
 * Reorder slide for the「最近」list. Prototype-only polish: flip to false (or
 * replace RecentReorderList with a plain <div>) to drop it; nothing else
 * depends on it.
 */
export const RECENT_REORDER_MOTION = true
