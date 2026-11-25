import type { SoloSession, SoloStatus } from '@/lib/api/apiTypes'

/** One user-facing sandbox state, merged from the session, sandbox and wake flags. */
export type SandboxDisplayKind =
  | 'preparing'
  | 'running'
  | 'busy'
  | 'paused'
  | 'waking'
  | 'stopping'
  | 'stopped'
  | 'closed'
  | 'error'

/** Status dot color: ok = green, pending = amber pulse, idle = gray, err = red. */
export type SandboxTone = 'ok' | 'pending' | 'idle' | 'err'

/** The primary action the info card offers for a state. */
export type SandboxAction = 'stop' | 'wake' | 'resume' | 'restart' | null

export interface SandboxDisplayState {
  kind: SandboxDisplayKind
  tone: SandboxTone
  action: SandboxAction
  /** The action exists but cannot run now (stop while a turn or queue is active). */
  actionDisabled: boolean
  /** pulling / creating while preparing. */
  step?: string
}

export interface SandboxDisplayInput {
  status: SoloStatus | string | undefined
  sandboxStatus?: string
  paused: boolean
  waking: boolean
  busy: boolean
  queued: number
}

const TONES: Record<SandboxDisplayKind, SandboxTone> = {
  preparing: 'pending',
  running: 'ok',
  busy: 'ok',
  paused: 'idle',
  waking: 'pending',
  stopping: 'pending',
  stopped: 'idle',
  closed: 'idle',
  error: 'err',
}

function state(kind: SandboxDisplayKind, action: SandboxAction = null, actionDisabled = false, step?: string): SandboxDisplayState {
  return { kind, tone: TONES[kind], action, actionDisabled, ...(step ? { step } : {}) }
}

export function sandboxDisplayState(input: SandboxDisplayInput): SandboxDisplayState {
  const sandboxStatus = (input.sandboxStatus || '').trim().toLowerCase()
  switch (input.status) {
    case 'error':
      return state('error', 'restart')
    case 'closed':
      return state('closed', 'restart')
    case 'stopping':
      return state('stopping')
    case 'stopped':
      return state('stopped', 'resume')
    case 'running':
      if (sandboxStatus === 'error') return state('error', 'restart')
      if (input.waking) return state('waking')
      if (input.paused) return state('paused', 'wake')
      if (input.busy || input.queued > 0) return state('busy', 'stop', true)
      return state('running', 'stop')
    default:
      return state('preparing', null, false, sandboxStatus === 'pulling' || sandboxStatus === 'creating' ? sandboxStatus : undefined)
  }
}

/** How idle reclaim applies to the sandbox right now. */
export type SandboxReclaim = 'countdown' | 'exempt' | 'keepAlive' | 'connected' | 'none'

export interface SandboxLifecycle {
  /** snapshot = E2B (saved and paused when idle), files = Docker/Kubernetes. */
  kind: 'snapshot' | 'files' | 'unknown'
  reclaim: SandboxReclaim
  destroyAt?: Date
  connections: number
  modelActiveAt?: Date
}

function date(value?: string): Date | undefined {
  if (!value) return undefined
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? undefined : d
}

export function sandboxLifecycle(session: Pick<SoloSession, 'sandbox' | 'keepAlive' | 'idleReclaimExempt'>): SandboxLifecycle {
  const sb = session.sandbox
  const recovery = sb?.recoveryKind
  const kind = recovery === 'snapshot' || recovery === 'files' ? recovery : session.idleReclaimExempt ? 'files' : 'unknown'
  const connections = sb?.externalSessions || 0
  const destroyAt = date(sb?.destroyAt)
  let reclaim: SandboxReclaim = 'none'
  if (session.idleReclaimExempt || kind === 'files') reclaim = 'exempt'
  else if (session.keepAlive) reclaim = 'keepAlive'
  else if (connections > 0) reclaim = 'connected'
  else if (destroyAt) reclaim = 'countdown'
  return { kind, reclaim, connections, ...(destroyAt ? { destroyAt } : {}), ...(date(sb?.modelActiveAt) ? { modelActiveAt: date(sb?.modelActiveAt) } : {}) }
}
