import { ref } from 'vue'
import { isPlumbingNode, isRunTerminal, type ChainNodeMeta } from '@/lib/inbox/runConversationChain'
import type { SidebarAutoTarget } from '@/lib/inbox/sidebarRunRows'

/** One move from the old row box to the new one. Matches the sidebar ease, under 600ms. */
export const SIDEBAR_FLIGHT_MS = 480

/** Drop a click mark that never resolved so a later, unrelated enter does not fly. */
export const SIDEBAR_HANDOFF_TTL_MS = 60_000

/**
 * How long an automatic enter may sit before the click is recorded and still
 * count as this handoff's next node. The node being confirmed publishes its
 * own enter as auto; anything older than this is a previous visit.
 */
export const SIDEBAR_EARLY_AUTO_MS = 2_000

export type SidebarRunGroup = 'running' | 'auto'

export type SidebarFlightLanding = 'row' | 'heading'

export type SidebarFlightDecision =
  | { type: 'stay' }
  | { type: 'place' }
  | { type: 'leave' }
  | { type: 'fly-in'; landing: SidebarFlightLanding }
  | { type: 'fly-back'; landing: SidebarFlightLanding }

export interface SidebarFlightInput {
  /** Group last painted, or null when this run was not on screen. */
  previous: SidebarRunGroup | null
  /** Group it belongs in now, or null when it left the unfinished list. */
  next: SidebarRunGroup | null
  /** This click's 确认并流转 is still armed. */
  handoffArmed: boolean
  /** node_enter or the run snapshot says the next node is an automatic process. */
  confirmedAuto: boolean
  reducedMotion: boolean
  /** The destination section is folded, so the move ends on its heading. */
  targetCollapsed: boolean
}

/**
 * When a sidebar row changes group. Fly into「自动过程」only for this click
 * once the next node is confirmed automatic. Fly back when a row that was
 * already in「自动过程」is waiting again. Everything else just lands.
 */
export function decideSidebarFlight(input: SidebarFlightInput): SidebarFlightDecision {
  const { previous, next, handoffArmed, confirmedAuto, reducedMotion, targetCollapsed } = input
  if (previous === next) return { type: 'stay' }
  if (previous == null) return { type: 'place' }
  if (next == null) return { type: 'leave' }
  const landing: SidebarFlightLanding = targetCollapsed ? 'heading' : 'row'
  if (previous === 'running' && next === 'auto') {
    if (handoffArmed && confirmedAuto && !reducedMotion) return { type: 'fly-in', landing }
    return { type: 'place' }
  }
  if (previous === 'auto' && next === 'running') {
    if (reducedMotion) return { type: 'place' }
    return { type: 'fly-back', landing }
  }
  return { type: 'place' }
}

export interface SidebarHandoffMark {
  fromNodeId: string
  confirmedAuto: boolean
  /** Automatic node this handoff settled on, when already known. */
  autoNodeId?: string
  autoNodeName?: string
  at: number
}

export interface SidebarEnterTarget {
  nodeId?: string
  name?: string
}

const marks = new Map<string, SidebarHandoffMark>()
/** Next-node enter that raced ahead of the click. Not the visit already on screen. */
const earlyAuto = new Map<string, SidebarEnterTarget & { at: number }>()
/** Run finished during the handoff: hide it from both unfinished groups until the list drops it. */
const dismissed = new Set<string>()

/** Bumped whenever a mark changes so the sidebar can re-read it. */
export const sidebarHandoffEpoch = ref(0)

function touch() {
  sidebarHandoffEpoch.value += 1
}

/** A different node's enter that arrived just before this click. The graph target wins when both exist. */
function freshEarlyDestination(runId: string, fromNodeId: string, now: number) {
  const early = earlyAuto.get(runId)
  earlyAuto.delete(runId)
  if (!early?.nodeId || early.nodeId === fromNodeId) return undefined
  if (early.at > now || now - early.at > SIDEBAR_EARLY_AUTO_MS) return undefined
  return early
}

export function armSidebarHandoff(
  runId: string,
  fromNodeId: string,
  now = Date.now(),
  target?: SidebarAutoTarget | null,
) {
  if (!runId) return
  const early = freshEarlyDestination(runId, fromNodeId, now)
  dismissed.delete(runId)
  const confirmedAuto = !!target || !!early
  marks.set(runId, {
    fromNodeId,
    confirmedAuto,
    autoNodeId: target?.nodeId || early?.nodeId,
    autoNodeName: target ? target.name : early?.name,
    at: now,
  })
  touch()
}

export function noteSidebarHandoffOutcome(
  runId: string,
  outcome: 'auto' | 'human' | 'ended',
  target?: SidebarEnterTarget,
  now = Date.now(),
) {
  if (!runId) return
  if (outcome === 'ended') {
    earlyAuto.delete(runId)
    const had = marks.delete(runId)
    const seen = dismissed.has(runId)
    dismissed.add(runId)
    if (had || !seen) touch()
    return
  }
  if (dismissed.has(runId)) return
  if (outcome === 'auto') {
    const mark = marks.get(runId)
    if (!mark) {
      // No click in progress. Remember only a named enter, briefly, so the
      // node already on screen cannot confirm the next click.
      if (!target?.nodeId) return
      earlyAuto.set(runId, { nodeId: target.nodeId, name: target.name, at: now })
      return
    }
    // Replaying the node this click is leaving is not the next real node.
    if (target?.nodeId && target.nodeId === mark.fromNodeId) return
    const autoNodeId = target?.nodeId || mark.autoNodeId
    const autoNodeName = target?.name || mark.autoNodeName
    if (mark.confirmedAuto && mark.autoNodeId === autoNodeId && mark.autoNodeName === autoNodeName) return
    marks.set(runId, { ...mark, confirmedAuto: true, autoNodeId, autoNodeName })
    touch()
    return
  }
  earlyAuto.delete(runId)
  if (marks.delete(runId)) touch()
}

/** node_enter: plumbing does not move the row; an automatic agent settles it; anything else takes it back. */
export function applySidebarEnter(
  runId: string,
  frame: { nodeId?: string; name?: string; mode?: string },
  nodes?: ChainNodeMeta,
  now = Date.now(),
) {
  if (!runId) return
  if (frame.nodeId && isPlumbingNode(nodes, frame.nodeId)) return
  if (frame.mode === 'auto') {
    noteSidebarHandoffOutcome(runId, 'auto', { nodeId: frame.nodeId, name: frame.name }, now)
    return
  }
  if (typeof frame.mode === 'string' && frame.mode) noteSidebarHandoffOutcome(runId, 'human', undefined, now)
}

export interface SidebarChainView {
  status?: unknown
  current?: { id?: string; name?: string; mode?: string } | null
}

/**
 * What a chain snapshot says about the handoff in progress.
 * The node still being confirmed is not a negation. input/output are not a destination.
 */
export function sidebarChainHandoff(
  chain: SidebarChainView,
  fromNodeId?: string,
  nodes?: ChainNodeMeta,
): { outcome: 'auto' | 'human' | 'ended'; target?: SidebarEnterTarget } | null {
  if (typeof chain.status === 'string' && isRunTerminal(chain.status)) return { outcome: 'ended' }
  const cur = chain.current
  if (fromNodeId && cur?.id && cur.id === fromNodeId) return null
  if (cur?.id && isPlumbingNode(nodes, cur.id)) return null
  if (cur?.mode === 'auto') return { outcome: 'auto', target: { nodeId: cur.id, name: cur.name } }
  if (cur?.mode) return { outcome: 'human' }
  return null
}

/** A finished run leaves both unfinished groups. Other statuses do not. */
export function noteSidebarRunStatus(runId: string, status: unknown) {
  if (typeof status === 'string' && isRunTerminal(status)) noteSidebarHandoffOutcome(runId, 'ended')
}

export function isSidebarRunDismissed(runId: string): boolean {
  return dismissed.has(runId)
}

export function sidebarTrackedRunIds(): string[] {
  return [...new Set([...marks.keys(), ...dismissed])]
}

export function clearSidebarHandoff(runId: string) {
  earlyAuto.delete(runId)
  if (!runId) return
  const had = marks.delete(runId)
  const gone = dismissed.delete(runId)
  if (had || gone) touch()
}

export function consumeSidebarHandoff(runId: string) {
  clearSidebarHandoff(runId)
}

export function readSidebarHandoff(runId: string, now = Date.now()): SidebarHandoffMark | null {
  const mark = marks.get(runId)
  if (!mark) return null
  if (now - mark.at > SIDEBAR_HANDOFF_TTL_MS) {
    marks.delete(runId)
    touch()
    return null
  }
  return mark
}

export function __resetSidebarHandoffForTests() {
  marks.clear()
  earlyAuto.clear()
  dismissed.clear()
  sidebarHandoffEpoch.value = 0
}
