/** Boot-stage progress for LiveLogPanel empty state (frontend heuristic only). */

export type BootStageId = 'pulling' | 'creating' | 'acp_ready' | 'first_event'
export type BootStageState = 'pending' | 'active' | 'done' | 'timeout'

/** Persisted boot dwell state so tab switches (log ↔ sandbox) do not reset timeout. */
export type LiveLogBootSession = {
  confirmedPhase: number | null
  stageEnteredAt: number | null
  timedOut: boolean
}

export const BOOT_STAGE_ORDER: BootStageId[] = ['pulling', 'creating', 'acp_ready', 'first_event']

/** Product wait ceiling (~120s) for create/ACP stages — not used while pulling (g3.4). */
export const BOOT_STAGE_TIMEOUT_MS = 120_000

type SandboxPhaseSignal = {
  status?: string | null
  containerStatus?: string | null
} | null | undefined

function norm(s: string | null | undefined): string {
  return (s || '').trim().toLowerCase()
}

/** Container looks ready enough to treat Create as done (ACP wait may still run). */
export function isContainerReady(containerStatus: string | null | undefined): boolean {
  const c = norm(containerStatus)
  return c === 'running' || c === 'up'
}

/** True while gateway reports image pull in progress (no 120s dwell timeout). */
export function isPullingSandbox(sandbox: SandboxPhaseSignal): boolean {
  return norm(sandbox?.status) === 'pulling'
}

/**
 * Raw phase index from sandbox + timeline emptiness.
 * 0 = pulling image, 1 = creating, 2 = ACP ready, 3 = waiting first event.
 * Returns null when boot progress should not be shown.
 */
export function deriveBootPhaseIndex(
  nodeStatus: string | undefined,
  sandbox: SandboxPhaseSignal,
  hasTimelineContent: boolean,
): number | null {
  if (nodeStatus !== 'running' || hasTimelineContent) return null

  const sbStatus = norm(sandbox?.status)
  if (sbStatus === 'running') return 3
  if (sbStatus === 'creating' && isContainerReady(sandbox?.containerStatus)) return 2
  if (sbStatus === 'pulling') return 0
  // No row, creating without ready container, or uncertain → creating stage.
  return 1
}

/** Monotonic ratchet: never go backwards while still in boot empty state. */
export function ratchetBootPhaseIndex(confirmed: number | null, derived: number | null): number | null {
  if (derived == null) return null
  if (confirmed == null) return derived
  return Math.max(confirmed, derived)
}

export function buildBootStageStates(activeIndex: number, timedOut: boolean): BootStageState[] {
  return BOOT_STAGE_ORDER.map((_, i) => {
    if (i < activeIndex) return 'done'
    if (i === activeIndex) return timedOut ? 'timeout' : 'active'
    return 'pending'
  })
}

export function stageIcon(state: BootStageState): string {
  switch (state) {
    case 'done':
      return 'check'
    case 'active':
      return 'spinner'
    case 'timeout':
      return 'alert'
    default:
      return 'dot'
  }
}
