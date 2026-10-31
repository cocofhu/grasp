/**
 * Client side of the server-owned node dialogue (clarify / review / observed
 * automatic visit). The server decides every row, turn phase and outcome; this
 * module only keeps the newest version and says when to refetch. No Vue.
 */
import type { ClarifyImage, ClarifyTurn, DialogueOutcome, ReactAnnotation } from '../shared/types'

export type DialogueRowPhase = 'running' | 'orphan' | 'sealed'
export type DialogueTurnPhase = 'queued' | 'running' | 'orphan' | 'ended'

export type DialogueRow = ClarifyTurn & {
  id: string
  phase: DialogueRowPhase
  /** Seq at which this row's content last changed. */
  rev: number
}

export type DialogueItem = {
  id?: string
  clientMessageId?: string
  text?: string
  images?: ClarifyImage[]
  annotations?: ReactAnnotation[]
}

export type DialogueTurn = {
  turnId: string
  clientMessageId?: string
  phase: DialogueTurnPhase
  position?: number
  outcome?: DialogueOutcome
  error?: string
  code?: string
  autoRetryOnResume?: boolean
  runningOpId?: string
  item?: DialogueItem
}

export type DialogueOrphan = { runningOpId: string; desynced: boolean }

export type DialogueState = {
  type: 'dialogue'
  runId: string
  nodeId: string
  /** Set on a share visitor's own chat. */
  lane?: string
  iteration: number
  kind?: string
  epoch: number
  seq: number
  mode: 'full' | 'tail'
  prefixLen?: number
  prefixDigest?: string
  rows: DialogueRow[]
  turns: DialogueTurn[]
  orphan?: DialogueOrphan | null
  busy: boolean
  done: boolean
  observe?: boolean
}

export type DialogueVersion = { epoch: number; seq: number }

export function versionLess(a: DialogueVersion, b: DialogueVersion): boolean {
  return a.epoch < b.epoch || (a.epoch === b.epoch && a.seq < b.seq)
}

const utf8 = new TextEncoder()

/** Same digest as the server: `<len>:<fnv1a32 hex of "id@rev|id@rev…">`. */
export function prefixDigest(rows: readonly Pick<DialogueRow, 'id' | 'rev'>[]): string {
  let h = 0x811c9dc5
  const bytes = utf8.encode(rows.map((r) => `${r.id}@${r.rev}`).join('|'))
  for (const b of bytes) {
    h ^= b
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return `${rows.length}:${h.toString(16).padStart(8, '0')}`
}

export type ApplyResult = {
  state: DialogueState | null
  /** The frame could not be placed on the current state; refetch over REST. */
  resync: boolean
  applied: boolean
}

/**
 * Apply one frame. Frames not newer than the current state are ignored; a
 * full frame replaces it; a tail frame replaces the rows after its prefix
 * only when that prefix is exactly the one held here.
 */
export function applyFrame(cur: DialogueState | null, frame: DialogueState): ApplyResult {
  if (cur && !versionLess(cur, frame)) return { state: cur, resync: false, applied: false }
  if (frame.mode !== 'tail') return { state: asFull(frame, frame.rows), resync: false, applied: true }
  const n = frame.prefixLen ?? 0
  if (!cur || cur.iteration !== frame.iteration || cur.rows.length < n || prefixDigest(cur.rows.slice(0, n)) !== frame.prefixDigest) {
    return { state: cur, resync: true, applied: false }
  }
  return { state: asFull(frame, [...cur.rows.slice(0, n), ...frame.rows]), resync: false, applied: true }
}

function asFull(frame: DialogueState, rows: DialogueRow[]): DialogueState {
  const { prefixLen: _len, prefixDigest: _digest, ...rest } = frame
  return { ...rest, mode: 'full', rows }
}

/** A send accepted on this page that the server state does not show yet. */
export type OptimisticSend = {
  clientMessageId: string
  text: string
  images: ClarifyImage[]
  annotations: ReactAnnotation[]
  at: string
}

export function addOptimistic(list: readonly OptimisticSend[], send: OptimisticSend): OptimisticSend[] {
  return [...list.filter((o) => o.clientMessageId !== send.clientMessageId), send]
}

export function rejectOptimistic(list: readonly OptimisticSend[], clientMessageId: string): OptimisticSend[] {
  return list.filter((o) => o.clientMessageId !== clientMessageId)
}

/** Optimistic sends the server state has not taken over yet (by clientMessageId). */
export function pendingOptimistic(state: DialogueState | null, list: readonly OptimisticSend[]): OptimisticSend[] {
  if (!state || !list.length) return [...list]
  const known = new Set<string>()
  for (const r of state.rows) if (r.clientMessageId) known.add(r.clientMessageId)
  for (const t of state.turns) {
    if (t.clientMessageId) known.add(t.clientMessageId)
    if (t.item?.clientMessageId) known.add(t.item.clientMessageId)
  }
  return list.filter((o) => !known.has(o.clientMessageId))
}

export type DialogueQueueRow = {
  id?: string
  clientMessageId?: string
  text: string
  images: ClarifyImage[]
  annotations: ReactAnnotation[]
  /** Sent from this page; the server has not listed it yet. */
  local?: boolean
}

export type DialogueView = {
  rows: DialogueRow[]
  queue: DialogueQueueRow[]
  /** A turn is running (or a send from this page is about to start one). */
  thinking: boolean
  /** Index in rows of the reply currently streaming, or -1. */
  streamingIndex: number
  orphan: DialogueOrphan | null
  busy: boolean
  done: boolean
  observe: boolean
  iteration: number
  kind: string
  /** Turns the server ended without a row (outcome dropped / failed before output). */
  endedTurns: DialogueTurn[]
}

/** What to draw for a state plus this page's optimistic sends. */
export function render(state: DialogueState | null, optimistic: readonly OptimisticSend[] = []): DialogueView {
  const rows: DialogueRow[] = state ? [...state.rows] : []
  const turns = state?.turns ?? []
  const queue: DialogueQueueRow[] = turns
    .filter((t) => t.phase === 'queued')
    .sort((a, b) => (a.position ?? 0) - (b.position ?? 0))
    .map((t) => ({
      id: t.turnId,
      clientMessageId: t.clientMessageId || t.item?.clientMessageId,
      text: t.item?.text ?? '',
      images: t.item?.images ?? [],
      annotations: t.item?.annotations ?? [],
    }))
  const pending = pendingOptimistic(state, optimistic)
  const busy = !!state?.busy
  let thinking = turns.some((t) => t.phase === 'running' || t.phase === 'orphan' || t.phase === 'queued')
  pending.forEach((o, i) => {
    if (i === 0 && !busy && !queue.length) {
      rows.push({
        id: `local:${o.clientMessageId}`,
        phase: 'sealed',
        rev: 0,
        role: 'human',
        clientMessageId: o.clientMessageId,
        text: o.text,
        images: o.images.length ? o.images : undefined,
        annotations: o.annotations.length ? o.annotations : undefined,
        at: o.at,
        localPending: true,
      })
      thinking = true
      return
    }
    queue.push({ clientMessageId: o.clientMessageId, text: o.text, images: o.images, annotations: o.annotations, local: true })
  })
  let streamingIndex = -1
  for (let i = rows.length - 1; i >= 0; i--) {
    const r = rows[i]!
    if (r.role === 'agent' && r.phase !== 'sealed' && r.streaming) {
      streamingIndex = i
      break
    }
  }
  return {
    rows,
    queue,
    thinking,
    streamingIndex,
    orphan: state?.orphan ?? null,
    busy,
    done: !!state?.done,
    observe: !!state?.observe,
    iteration: state?.iteration ?? 0,
    kind: state?.kind ?? '',
    endedTurns: turns.filter((t) => t.phase === 'ended' && !rows.some((r) => r.turnId === t.turnId)),
  }
}
