/**
 * Fine-grained run-detail chrome merge (g1.1).
 * Patches status / progress / node runs / artifacts without touching dialogue.
 */
import type { Artifact, Run } from '@/lib/shared/types'
import { artifactFingerprint } from '@/lib/run/reactArtifactPreview'

export function artifactsListFingerprint(arts: Artifact[] | undefined): string {
  if (!arts?.length) return ''
  return arts.map((a) => `${a.name}:${artifactFingerprint(a)}`).join('|')
}

/** Canvas / header fields that may update while a clarify session is streaming. */
export function runChromeFingerprint(run: Run): string {
  const nodeRuns = Object.entries(run.nodeRuns || {})
    .map(([k, v]) => `${k}:${v.status}:${v.durationSec ?? ''}:${v.startedAt || ''}`)
    .sort()
    .join(',')
  const exec = Object.entries(run.nodeExecutions || {})
    .map(([k, list]) => `${k}:${list.length}:${list.map((n) => n.status).join('/')}`)
    .sort()
    .join(',')
  return [
    run.status,
    String(run.progress ?? ''),
    run.startedAt,
    String(run.durationSec ?? ''),
    run.currentNodeLabel || '',
    nodeRuns,
    exec,
    artifactsListFingerprint(run.artifacts),
    run.gate?.nodeId || '',
    run.error || '',
    run.failedNode || '',
  ].join('#')
}

export function reactSessionsBusyFingerprint(run: Run): string {
  const sessions = run.reactSessions
  if (!sessions) return ''
  return Object.keys(sessions)
    .sort()
    .map((k) => `${k}:${sessions[k]?.busy ? '1' : '0'}:${sessions[k]?.waiting ?? 0}`)
    .join('|')
}

type ClarifySlot = NonNullable<Run['clarify']>

/**
 * Clarify transcript fingerprint (plan g1.1 / g2.1).
 * Turn count, last-turn text, last-turn thought, and done. Chrome-identical
 * polls still count as changed when a parked empty transcript gains a reply.
 */
function clarifySlotFingerprint(slot: ClarifySlot | undefined): string {
  if (!slot) return ''
  const turns = slot.turns || []
  const last = turns[turns.length - 1]
  return JSON.stringify([
    slot.nodeId || '',
    !!slot.done,
    turns.length,
    last?.text || '',
    last?.thought || '',
  ])
}

/** Top-level clarify plus every clarifyByNode entry. Order of nodes is stable. */
export function clarifyTurnsFingerprint(run: Run): string {
  const byNode = run.clarifyByNode || {}
  const nodes = Object.keys(byNode)
    .sort()
    .map((id) => `${id}:${clarifySlotFingerprint(byNode[id])}`)
    .join('|')
  return `${clarifySlotFingerprint(run.clarify)}#${nodes}`
}

/** [turnCount, last text+thought length, done]. Larger means a more complete transcript. */
function clarifySlotRichness(slot: ClarifySlot | undefined): [number, number, number] {
  const turns = slot?.turns || []
  const last = turns[turns.length - 1]
  const body = (last?.text || '').length + (last?.thought || '').length
  return [turns.length, body, slot?.done ? 1 : 0]
}

function richnessCmp(a: [number, number, number], b: [number, number, number]): number {
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return a[i]! - b[i]!
  }
  return 0
}

function slotHasStreaming(slot: ClarifySlot | undefined): boolean {
  return !!slot?.turns?.some((t) => t.streaming)
}

function nodeSessionBusy(run: Run, nodeId: string | undefined): boolean {
  if (!nodeId) return false
  return !!run.reactSessions?.[nodeId]?.busy
}

/**
 * Local unpersisted stream is ahead of this server slot (plan g1.2).
 * Identical transcripts are not "ahead" — a busy-bit change must still assign.
 */
function slotStreamAhead(
  current: Run,
  snapshot: Run,
  nodeId: string | undefined,
  cur: ClarifySlot | undefined,
  next: ClarifySlot | undefined,
): boolean {
  if (clarifySlotFingerprint(cur) === clarifySlotFingerprint(next)) return false
  const cmp = richnessCmp(clarifySlotRichness(next), clarifySlotRichness(cur))
  // In-progress streaming row: keep it while the snapshot session is still busy,
  // or while the server copy is not strictly more complete.
  if (slotHasStreaming(cur)) {
    if (nodeSessionBusy(snapshot, nodeId)) return true
    return cmp <= 0
  }
  // Both sides still busy and the server transcript is shorter: keep local bubbles.
  if (nodeSessionBusy(current, nodeId) && nodeSessionBusy(snapshot, nodeId)) return cmp < 0
  return false
}

function shouldAdoptClarifySlot(
  current: Run,
  snapshot: Run,
  nodeId: string | undefined,
  cur: ClarifySlot | undefined,
  next: ClarifySlot | undefined,
): boolean {
  if (clarifySlotFingerprint(cur) === clarifySlotFingerprint(next)) return false
  // Shorter server snapshot must not wipe an in-progress bubble (plan g1.2 / g2.2).
  if (slotStreamAhead(current, snapshot, nodeId, cur, next)) return false
  const cmp = richnessCmp(clarifySlotRichness(next), clarifySlotRichness(cur))
  if (cmp > 0) {
    // Busy patch keeps a live streaming row until the snapshot itself is idle
    // or the local row is only the empty panel (plan g1.3).
    if (slotHasStreaming(cur) && nodeSessionBusy(snapshot, nodeId)) return false
    return true
  }
  // Same size, different body: take the server copy only once the session is idle.
  if (cmp < 0) return false
  if (slotHasStreaming(cur) || nodeSessionBusy(current, nodeId) || nodeSessionBusy(snapshot, nodeId)) {
    return false
  }
  return true
}

/**
 * True when any clarify slot still has a local stream the server has not caught
 * up with. Callers must not replace the whole run in that case (plan g1.2).
 */
export function clarifyStreamAheadOfServer(current: Run, snapshot: Run): boolean {
  const clarifyNode = current.clarify?.nodeId || snapshot.clarify?.nodeId
  if (slotStreamAhead(current, snapshot, clarifyNode, current.clarify, snapshot.clarify)) return true
  const ids = new Set([
    ...Object.keys(current.clarifyByNode || {}),
    ...Object.keys(snapshot.clarifyByNode || {}),
  ])
  for (const id of ids) {
    if (slotStreamAhead(current, snapshot, id, current.clarifyByNode?.[id], snapshot.clarifyByNode?.[id])) {
      return true
    }
  }
  return false
}

/**
 * Chrome merge plus persisted clarify slots that are safe to show (plan g1.2 / g1.3).
 * Adopts a server transcript when it is more complete and the local stream is not
 * ahead. A shorter snapshot never replaces a streaming bubble. Other nodes are
 * left untouched.
 */
export function applyPersistedClarify(current: Run, snapshot: Run): Run {
  const base = mergeRunChromeFields(current, snapshot)
  let clarify = current.clarify
  let clarifyByNode = current.clarifyByNode
  let adopted = false

  const clarifyNode = snapshot.clarify?.nodeId || current.clarify?.nodeId
  if (shouldAdoptClarifySlot(current, snapshot, clarifyNode, current.clarify, snapshot.clarify)) {
    clarify = snapshot.clarify
    adopted = true
  }

  const ids = new Set([
    ...Object.keys(current.clarifyByNode || {}),
    ...Object.keys(snapshot.clarifyByNode || {}),
  ])
  if (ids.size) {
    const nextMap: NonNullable<Run['clarifyByNode']> = { ...(current.clarifyByNode || {}) }
    for (const id of ids) {
      const cur = current.clarifyByNode?.[id]
      const next = snapshot.clarifyByNode?.[id]
      if (!shouldAdoptClarifySlot(current, snapshot, id, cur, next)) continue
      if (next) nextMap[id] = next
      else delete nextMap[id]
      adopted = true
    }
    clarifyByNode = nextMap
  }

  if (!adopted) return base
  // Idle catch-up: take the server busy/queue flags with the transcript so a
  // stale busy bit does not keep the empty panel on the chrome-only path.
  const reactSessions =
    !clarifyStreamAheadOfServer(current, snapshot) && snapshot.reactSessions
      ? snapshot.reactSessions
      : base.reactSessions
  return { ...base, clarify, clarifyByNode, reactSessions }
}

/**
 * Merge REST snapshot chrome onto the live run.
 * Keeps reactSessions / clarify turns so a busy dialogue is not overwritten (g1.1).
 */
export function mergeRunChromeFields(current: Run, snapshot: Run): Run {
  const nextArts = snapshot.artifacts ?? current.artifacts
  const artifacts =
    artifactsListFingerprint(current.artifacts) === artifactsListFingerprint(nextArts)
      ? current.artifacts
      : nextArts
  const nodeRuns = snapshot.nodeRuns ?? current.nodeRuns
  const nodeExecutions = snapshot.nodeExecutions ?? current.nodeExecutions
  return {
    ...current,
    status: snapshot.status,
    progress: snapshot.progress,
    startedAt: snapshot.startedAt,
    durationSec: snapshot.durationSec,
    currentNodeLabel: snapshot.currentNodeLabel,
    nodeRuns,
    nodeExecutions,
    artifacts,
    gate: snapshot.gate,
    trace: snapshot.trace ?? current.trace,
    git: snapshot.git !== undefined ? snapshot.git : current.git,
    error: snapshot.error,
    failedNode: snapshot.failedNode,
    priority: snapshot.priority ?? current.priority,
    // reactSessions, clarify, clarifyByNode stay on `current` via spread.
  }
}

export function runSnapshotUnchanged(current: Run, snapshot: Run): boolean {
  return (
    current.id === snapshot.id &&
    runChromeFingerprint(current) === runChromeFingerprint(snapshot) &&
    reactSessionsBusyFingerprint(current) === reactSessionsBusyFingerprint(snapshot) &&
    clarifyTurnsFingerprint(current) === clarifyTurnsFingerprint(snapshot)
  )
}
