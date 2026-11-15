/**
 * Solo sandbox boot progress, from the states the live session view exposes
 * (the page polls it while the session is `starting`) — nothing here advances
 * on a timer:
 *
 *   sandbox    sandbox status `pulling` / `creating` until the container is up
 *              (live `containerStatus` running)
 *   workspace  container up, sandbox still `creating`: startup.sh clones or
 *              restores the workspace and starts the ACP bridge; the backend
 *              does not split these
 *   agent      sandbox `running` (stamped after ACP connect) until the session
 *              itself turns `running`
 *
 * Steps match the workflow boot checklist (lib/run/bootRecord.ts).
 */
import { isContainerReady } from '@/lib/run/liveLogBootPhase'
import type { SoloSession } from '@/lib/api/apiTypes'

export type SoloBootStepId = 'sandbox' | 'workspace' | 'agent'
export type SoloBootStepState = 'done' | 'running' | 'pending'

export const SOLO_BOOT_STEPS: SoloBootStepId[] = ['sandbox', 'workspace', 'agent']

/** Index of the step in progress; SOLO_BOOT_STEPS.length once the session runs. */
export function soloBootPhase(session: Pick<SoloSession, 'status' | 'sandbox'> | null | undefined): number {
  if (!session) return 0
  if (session.status === 'running') return SOLO_BOOT_STEPS.length
  const sb = session.sandbox
  const status = (sb?.status || '').trim().toLowerCase()
  if (status === 'running') return 2
  if (status === 'creating' && isContainerReady(sb?.containerStatus)) return 1
  return 0
}

export function soloBootStepStates(phase: number): { id: SoloBootStepId; state: SoloBootStepState }[] {
  return SOLO_BOOT_STEPS.map((id, i) => ({
    id,
    state: i < phase ? 'done' : i === phase ? 'running' : 'pending',
  }))
}

export function isSoloSandboxPulling(session: Pick<SoloSession, 'sandbox'> | null | undefined): boolean {
  return (session?.sandbox?.status || '').trim().toLowerCase() === 'pulling'
}
