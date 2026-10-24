import { ref } from 'vue'

/** One move from the old row box to the new one. Matches the sidebar ease, under 600ms. */
export const SIDEBAR_FLIGHT_MS = 480

/** Drop a click mark that never resolved so a later, unrelated enter does not fly. */
export const SIDEBAR_HANDOFF_TTL_MS = 60_000

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
  at: number
}

const marks = new Map<string, SidebarHandoffMark>()
/** node_enter auto that arrived before the click was recorded. */
const earlyAuto = new Set<string>()

/** Bumped whenever a mark changes so the sidebar can re-read it. */
export const sidebarHandoffEpoch = ref(0)

function touch() {
  sidebarHandoffEpoch.value += 1
}

export function armSidebarHandoff(runId: string, fromNodeId: string, now = Date.now()) {
  if (!runId) return
  const confirmedAuto = earlyAuto.has(runId)
  earlyAuto.delete(runId)
  marks.set(runId, { fromNodeId, confirmedAuto, at: now })
  touch()
}

export function noteSidebarHandoffOutcome(runId: string, outcome: 'auto' | 'human' | 'ended') {
  if (!runId) return
  if (outcome === 'auto') {
    const mark = marks.get(runId)
    if (!mark) {
      earlyAuto.add(runId)
      return
    }
    if (mark.confirmedAuto) return
    marks.set(runId, { ...mark, confirmedAuto: true })
    touch()
    return
  }
  earlyAuto.delete(runId)
  if (marks.delete(runId)) touch()
}

export function clearSidebarHandoff(runId: string) {
  earlyAuto.delete(runId)
  if (!runId) return
  if (marks.delete(runId)) touch()
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
  sidebarHandoffEpoch.value = 0
}
