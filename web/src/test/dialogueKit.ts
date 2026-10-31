/**
 * Builds server dialogue states for tests: what the server would send for a
 * transcript, a running turn, a queue or an orphan sandbox turn.
 */
import { ingestDialogue } from '@/lib/chat/dialogueStore'
import type { DialogueItem, DialogueOrphan, DialogueRow, DialogueState, DialogueTurn } from '@/lib/chat/dialogueState'
import type { ClarifyTurn, DialogueOutcome } from '@/lib/shared/types'

/** A row as written in a test: a turn plus optional row identity. */
export type RowInput = ClarifyTurn & Partial<Pick<DialogueRow, 'id' | 'phase' | 'rev'>>

export type DialogueSpec = {
  rows?: RowInput[]
  turns?: DialogueTurn[]
  busy?: boolean
  done?: boolean
  iteration?: number
  observe?: boolean
  orphan?: DialogueOrphan | null
  kind?: string
}

let seq = 0

/**
 * Rows with server identity. Streaming agent rows run, the rest are sealed;
 * a sealed reply without an outcome is ok (interrupted when flagged).
 */
export function dialogueRows(turns: RowInput[], iteration = 1): DialogueRow[] {
  const replies: Record<string, number> = {}
  return turns.map((t, i) => {
    const phase = t.phase ?? (t.role === 'agent' && t.streaming ? 'running' : 'sealed')
    let id = t.id
    if (!id && t.turnId) {
      if (t.role === 'human') id = `${t.turnId}:h`
      else {
        const k = replies[t.turnId] ?? 0
        replies[t.turnId] = k + 1
        id = `${t.turnId}:a${k}`
      }
    }
    const row: DialogueRow = { ...t, id: id ?? `i${iteration}:${i}`, phase, rev: t.rev ?? 1 }
    if (t.role === 'agent' && phase === 'sealed' && !t.outcome) row.outcome = t.interrupted ? 'interrupted' : 'ok'
    return row
  })
}

/** A full dialogue frame, newer than every frame built before it. */
export function dialogueFrame(runId: string, nodeId: string, spec: DialogueSpec = {}): DialogueState {
  seq++
  const iteration = spec.iteration ?? 1
  return {
    type: 'dialogue',
    runId,
    nodeId,
    iteration,
    ...(spec.kind ? { kind: spec.kind } : {}),
    epoch: 1,
    seq,
    mode: 'full',
    rows: dialogueRows(spec.rows ?? [], iteration),
    turns: spec.turns ?? [],
    orphan: spec.orphan ?? null,
    busy: !!spec.busy,
    done: !!spec.done,
    ...(spec.observe ? { observe: true } : {}),
  }
}

/** Ingest a frame the way a host does on a WS "dialogue" message. */
export function feedDialogue(runId: string, nodeId: string, spec: DialogueSpec = {}): DialogueState {
  const frame = dialogueFrame(runId, nodeId, spec)
  ingestDialogue(frame)
  return frame
}

export function runningTurn(turnId: string, item?: DialogueItem, extra: Partial<DialogueTurn> = {}): DialogueTurn {
  return { turnId, phase: 'running', ...(item ? { item: { id: turnId, ...item } } : {}), ...extra }
}

export function queuedTurn(turnId: string, position: number, item: DialogueItem, extra: Partial<DialogueTurn> = {}): DialogueTurn {
  return { turnId, phase: 'queued', position, item: { id: turnId, ...item }, ...extra }
}

export function endedTurn(turnId: string, outcome: DialogueOutcome, extra: Partial<DialogueTurn> = {}): DialogueTurn {
  return { turnId, phase: 'ended', outcome, ...extra }
}

export function orphanTurn(runningOpId = ''): DialogueTurn {
  return { turnId: runningOpId ? `orphan:${runningOpId}` : 'orphan', phase: 'orphan', runningOpId }
}
