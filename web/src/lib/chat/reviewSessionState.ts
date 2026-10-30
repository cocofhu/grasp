import { createReviewStateCursor, type ReviewStateVersion, type SessionFrameItem } from './sessionQueue'
import type { AcpEvent, Run } from '@/lib/shared/types'

export type ReviewSessionSnapshot = NonNullable<Run['reactSessions']>[string]
export type ReviewSessionRun = Pick<Run, 'reactSessions' | 'reviewSessionGeneration'>
export type ReviewSessionFrame = ReviewStateVersion & Record<string, unknown> & {
  event?: string
  nodeId?: string
  busy?: boolean
  waiting?: number
  items?: SessionFrameItem[]
  activeItem?: SessionFrameItem
  item?: SessionFrameItem
}

type Clock = { dialogue: number; acp: number }
export type ReviewRecoveryCheckpoint = {
  readonly epoch: number
  readonly activity: number
  readonly nodes: ReadonlyMap<string, Clock>
  readonly sessions: ReadonlyMap<string, string>
}
export type ReviewRecoveryTask = (checkpoint: ReviewRecoveryCheckpoint, signal: AbortSignal) => Promise<void>
type ReviewSessionInput =
  | { kind: 'review'; frame: ReviewSessionFrame }
  | { kind: 'acp'; nodeId: string; events?: readonly AcpEvent[] }
  | { kind: 'visit'; nodeId: string }
  | { kind: 'snapshot'; run: ReviewSessionRun; checkpoint?: ReviewRecoveryCheckpoint; nodeIds?: string[] }

export const REVIEW_RECOVERY_TIMEOUT_MS = 60_000
type RecoveryResult = { kind: 'done' | 'cancelled' } | { kind: 'failed'; error: unknown }

/** Dialogue content can start with a tool or plan before any prose arrives. */
export function hasAcpContent(events?: readonly AcpEvent[] | null): boolean {
  return !!events?.some(event => {
    switch (event.kind) {
      case 'message':
      case 'thought':
      case 'plan':
      case 'commands': return !!event.text?.trim() || !!event.title?.trim()
      case 'tool_call': return !!event.title
      case 'timeline': return !!event.parts?.some(part => part.kind === 'tool' || !!part.text)
      default: return false
    }
  })
}

/** ACP pushes cumulative snapshots, including unchanged content on watchdog
 * heartbeats. Only semantic changes invalidate a content recovery read. */
export function acpSemanticFingerprint(events: readonly AcpEvent[]): string | null {
  const semantic = events.filter(event => event.kind !== 'liveness').map(({ t: _time, ...event }) => event)
  return semantic.length ? JSON.stringify(semantic) : null
}

/** Every workflow host projects exactly the same queue contract to its surface. */
export function reviewQueueFrame(nodeId: string, session?: ReviewSessionSnapshot): ReviewSessionFrame {
  return {
    ...session,
    event: 'queue_state',
    nodeId,
    busy: !!session?.busy,
    waiting: session?.waiting ?? 0,
    items: session?.items ?? [],
  }
}

/** Transport heartbeats never change session busy. Hosts own transport and
 * rendering; this controller owns queue authority and recovery ordering. */
export function createReviewSessionState(options: { recoveryTimeoutMs?: number } = {}) {
  const cursor = createReviewStateCursor()
  const sessions = new Map<string, ReviewSessionSnapshot>()
  const clocks = new Map<string, Clock>()
  const acpFingerprints = new Map<string, string>()
  const invalidCheckpoints = new WeakSet<ReviewRecoveryCheckpoint>()
  const timeoutMs = typeof options.recoveryTimeoutMs === 'number' &&
    Number.isFinite(options.recoveryTimeoutMs) && options.recoveryTimeoutMs > 0
    ? options.recoveryTimeoutMs : REVIEW_RECOVERY_TIMEOUT_MS
  let epoch = 0
  let activity = 0
  let disposed = false
  let recovery: { controller: AbortController; next: ReviewRecoveryTask | null; promise: Promise<void> } | null = null

  function identity(session?: ReviewSessionSnapshot) {
    return JSON.stringify([session?.sessionGeneration, session?.revision, !!session?.busy,
      session?.activeItem?.id ?? session?.activeItem?.clientMessageId])
  }

  function touch(nodeId: string, kind: keyof Clock) {
    const clock = clocks.get(nodeId) ?? { dialogue: 0, acp: 0 }
    clocks.set(nodeId, { ...clock, [kind]: clock[kind] + 1 })
    activity++
  }

  function setSession(nodeId: string, session: ReviewSessionSnapshot) {
    const previous = sessions.get(nodeId)
    const activeId = (value?: ReviewSessionSnapshot) => value?.activeItem?.id ?? value?.activeItem?.clientMessageId
    if ((session.sessionGeneration !== undefined && session.sessionGeneration !== previous?.sessionGeneration) ||
      (session.busy && (!previous?.busy || activeId(session) !== activeId(previous)))) acpFingerprints.delete(nodeId)
    sessions.set(nodeId, session)
  }

  function noteAcp(nodeId: string, events?: readonly AcpEvent[]): boolean {
    if (events !== undefined) {
      const fingerprint = acpSemanticFingerprint(events)
      if (fingerprint === null || fingerprint === acpFingerprints.get(nodeId)) return false
      acpFingerprints.set(nodeId, fingerprint)
    }
    touch(nodeId, 'acp')
    return true
  }

  function capture(): ReviewRecoveryCheckpoint {
    return { epoch, activity, nodes: new Map(clocks),
      sessions: new Map([...sessions].map(([nodeId, session]) => [nodeId, identity(session)])) }
  }

  function isCurrent(checkpoint: ReviewRecoveryCheckpoint, nodeId?: string): boolean {
    if (disposed || checkpoint.epoch !== epoch || invalidCheckpoints.has(checkpoint)) return false
    if (nodeId === undefined) return checkpoint.activity === activity &&
      [...new Set([...checkpoint.sessions.keys(), ...sessions.keys()])].every(node =>
        (checkpoint.sessions.get(node) ?? identity()) === identity(sessions.get(node)))
    const before = checkpoint.nodes.get(nodeId)
    const now = clocks.get(nodeId)
    return (before?.dialogue ?? 0) === (now?.dialogue ?? 0) &&
      (before?.acp ?? 0) === (now?.acp ?? 0) &&
      (checkpoint.sessions.get(nodeId) ?? identity()) === identity(sessions.get(nodeId))
  }

  /** A same-visit startup-ready transition can advance while ACP streams.
   * Queue and content recovery must keep using the stricter isCurrent. */
  function isStateCurrent(checkpoint: ReviewRecoveryCheckpoint, nodeId?: string): boolean {
    if (disposed || checkpoint.epoch !== epoch || invalidCheckpoints.has(checkpoint)) return false
    const nodes = nodeId === undefined
      ? new Set([...checkpoint.nodes.keys(), ...clocks.keys(), ...checkpoint.sessions.keys(), ...sessions.keys()])
      : [nodeId]
    return [...nodes].every(node =>
      (checkpoint.nodes.get(node)?.dialogue ?? 0) === (clocks.get(node)?.dialogue ?? 0) &&
      (checkpoint.sessions.get(node) ?? identity()) === identity(sessions.get(node)))
  }

  function reduceServer(frame: ReviewSessionFrame): boolean {
    if (!cursor.acceptServer(frame)) return false
    const nodeId = frame.nodeId ?? ''
    const previous = sessions.get(nodeId)
    const update = { ...frame }
    delete update.event
    delete update.nodeId
    delete update.type
    delete update.item
    if (frame.event === 'queue_state') {
      setSession(nodeId, { ...update, busy: !!frame.busy, waiting: frame.waiting ?? 0, items: frame.items ?? [] })
    } else if (frame.event === 'turn_begin') {
      setSession(nodeId, { ...previous, ...update, busy: frame.busy ?? true, activeItem: frame.activeItem ?? frame.item })
    } else if (frame.event === 'turn_done' || frame.event === 'error') {
      setSession(nodeId, { ...previous, ...update, busy: frame.busy ?? false, activeItem: frame.activeItem })
    }
    touch(nodeId, 'dialogue')
    return true
  }

  function reduceSnapshot(run: ReviewSessionRun, checkpoint?: ReviewRecoveryCheckpoint, nodeIds: string[] = []): ReviewSessionFrame[] {
    if (checkpoint && (checkpoint.epoch !== epoch || invalidCheckpoints.has(checkpoint))) return []
    // An explicit empty map or the server's generation watermark proves
    // absence. A partial DTO with neither carries no queue authority.
    if (run.reactSessions === undefined && typeof run.reviewSessionGeneration !== 'number') return []
    const incoming = run.reactSessions ?? {}
    const install = { ...incoming }
    const frames: ReviewSessionFrame[] = []
    for (const nodeId of new Set([...nodeIds, ...sessions.keys(), ...Object.keys(incoming)])) {
      if (checkpoint && !isCurrent(checkpoint, nodeId)) {
        // A late REST omission must not retire a newer live generation.
        const current = sessions.get(nodeId)
        if (current) install[nodeId] = current
        else delete install[nodeId]
        continue
      }
      const session = incoming[nodeId]
      if (!cursor.acceptSnapshot(nodeId, session, run.reviewSessionGeneration)) {
        const current = sessions.get(nodeId)
        if (current) install[nodeId] = current
        else delete install[nodeId]
        continue
      }
      const frame = reviewQueueFrame(nodeId, session)
      setSession(nodeId, { ...session, busy: frame.busy, waiting: frame.waiting, items: frame.items })
      frames.push(frame)
    }
    cursor.installSnapshot(install, run.reviewSessionGeneration)
    return frames
  }

  // Ingress is synchronous and serialized by the JS event loop. Network work
  // never holds this path: streaming frames can invalidate recovery at once.
  function ingest(input: Extract<ReviewSessionInput, { kind: 'review' }>): boolean
  function ingest(input: Extract<ReviewSessionInput, { kind: 'snapshot' }>): ReviewSessionFrame[]
  function ingest(input: Extract<ReviewSessionInput, { kind: 'acp' }>): boolean
  function ingest(input: Extract<ReviewSessionInput, { kind: 'visit' }>): void
  function ingest(input: ReviewSessionInput): boolean | ReviewSessionFrame[] | void {
    if (disposed) return input.kind === 'snapshot' ? [] : false
    switch (input.kind) {
      case 'review': return reduceServer(input.frame)
      case 'snapshot': return reduceSnapshot(input.run, input.checkpoint, input.nodeIds)
      case 'acp': return noteAcp(input.nodeId, input.events)
      case 'visit': acpFingerprints.delete(input.nodeId); return touch(input.nodeId, 'dialogue')
    }
  }

  async function runRecoveryTask(task: ReviewRecoveryTask, jobSignal: AbortSignal): Promise<RecoveryResult> {
    const checkpoint = capture()
    const controller = new AbortController()
    let stop!: () => void
    const cancelled = new Promise<RecoveryResult>(resolve => {
      stop = () => {
        clearTimeout(timer)
        invalidCheckpoints.add(checkpoint)
        controller.abort()
        resolve({ kind: 'cancelled' })
      }
    })
    const timer = setTimeout(stop, timeoutMs)
    jobSignal.addEventListener('abort', stop, { once: true })
    let read: Promise<RecoveryResult>
    try {
      // Attach both outcomes immediately: a read that ignores abort may reject
      // after its deadline, and must never become an unhandled rejection.
      read = Promise.resolve(task(checkpoint, controller.signal)).then(
        (): RecoveryResult => ({ kind: 'done' }),
        (error: unknown): RecoveryResult => ({ kind: 'failed', error }),
      )
    } catch (error) {
      read = Promise.resolve({ kind: 'failed', error })
    }
    try {
      return await Promise.race([read, cancelled])
    } finally {
      clearTimeout(timer)
      jobSignal.removeEventListener('abort', stop)
    }
  }

  /** At most one recovery runs. Further ticks coalesce into one trailing
   * request. Each read has its own deadline; expiring it leaves the native
   * turn untouched and releases this lane for the latest requested retry. */
  function requestRecovery(task: ReviewRecoveryTask): Promise<void> {
    if (disposed) return Promise.resolve()
    if (recovery) {
      recovery.next = task
      return recovery.promise
    }
    let complete!: () => void
    let fail!: (error: unknown) => void
    const promise = new Promise<void>((resolve, reject) => { complete = resolve; fail = reject })
    const job = { controller: new AbortController(), next: task as ReviewRecoveryTask | null, promise }
    recovery = job
    const drain = async () => {
      let failed = false
      let failure: unknown
      try {
        while (job.next && !job.controller.signal.aborted) {
          const next = job.next
          job.next = null
          const result = await runRecoveryTask(next, job.controller.signal)
          if (result.kind === 'failed' && !job.controller.signal.aborted) {
            failed = true
            failure = result.error
          }
        }
        if (failed && !job.controller.signal.aborted) throw failure
      } finally {
        // Release in the same continuation that sees an empty queue. A tick
        // at the promise-completion boundary must start another drain.
        if (recovery === job) recovery = null
      }
    }
    void drain().then(complete, fail)
    return job.promise
  }

  function reset() {
    epoch++
    activity = 0
    if (recovery) {
      recovery.next = null
      recovery.controller.abort()
      recovery = null
    }
    clocks.clear()
    acpFingerprints.clear()
    sessions.clear()
    cursor.reset()
  }

  return {
    get hasVersion() { return cursor.hasVersion },
    capture,
    isCurrent,
    isStateCurrent,
    hasAcpProgress(checkpoint: ReviewRecoveryCheckpoint, nodeId: string) {
      return checkpoint.epoch !== epoch || invalidCheckpoints.has(checkpoint) ||
        (checkpoint.nodes.get(nodeId)?.acp ?? 0) !== (clocks.get(nodeId)?.acp ?? 0)
    },
    ingest,
    requestRecovery,
    noteAcp(nodeId: string, events?: readonly AcpEvent[]) { return ingest({ kind: 'acp', nodeId, events }) },
    noteVisit(nodeId: string) { ingest({ kind: 'visit', nodeId }) },
    acceptServer(frame: ReviewSessionFrame) { return ingest({ kind: 'review', frame }) },
    reconcileSnapshot(run: ReviewSessionRun, checkpoint?: ReviewRecoveryCheckpoint, nodeIds?: string[]) {
      return ingest({ kind: 'snapshot', run, checkpoint, nodeIds })
    },
    session(nodeId: string) { return sessions.get(nodeId) },
    installSnapshot(snapshot: Run['reactSessions'] = {}, watermark?: number) {
      cursor.installSnapshot(snapshot, watermark)
    },
    reset,
    dispose() { reset(); disposed = true },
  }
}
