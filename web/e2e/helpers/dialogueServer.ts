/**
 * A fake owner of one node dialogue for e2e API mocks: tests change the
 * transcript / turns, and every change becomes a newer full dialogue state —
 * the frame the platform pushes on the run socket and returns from REST.
 */

export type FakeRow = {
  role: 'human' | 'agent'
  text: string
  turnId?: string
  clientMessageId?: string
  at?: string
  images?: unknown[]
  thought?: string
  parts?: unknown[]
  questions?: unknown[]
  /** A reply still being written. */
  streaming?: boolean
  outcome?: 'ok' | 'empty' | 'failed' | 'interrupted' | 'dropped'
  /** Any other persisted message field (usage, startedAt, …). */
  [field: string]: unknown
}

export type FakeItem = { text: string; clientMessageId?: string; images?: unknown[] }

export type FakeTurn =
  | { turnId: string; phase: 'running'; item?: FakeItem }
  | { turnId: string; phase: 'queued'; item: FakeItem }
  | { turnId: string; phase: 'ended'; outcome: NonNullable<FakeRow['outcome']>; clientMessageId?: string }

export type FakeDialogue = { rows: FakeRow[]; turns?: FakeTurn[]; done?: boolean; observe?: boolean }

export function createDialogueServer(runId: string, nodeId: string, initial: FakeDialogue = { rows: [] }) {
  let epoch = 1
  let seq = 1
  let current: FakeDialogue = initial

  function frame() {
    const replies: Record<string, number> = {}
    const rows = current.rows.map((r, i) => {
      let id = `r${i}`
      if (r.turnId && r.role === 'human') id = `${r.turnId}:h`
      else if (r.turnId) {
        const k = replies[r.turnId] ?? 0
        replies[r.turnId] = k + 1
        id = `${r.turnId}:a${k}`
      }
      const running = r.role === 'agent' && !!r.streaming
      return {
        ...r,
        at: r.at ?? '2026-10-21T01:00:00Z',
        id,
        rev: seq,
        phase: running ? 'running' : 'sealed',
        ...(r.role === 'agent' && !running ? { outcome: r.outcome ?? 'ok' } : {}),
      }
    })
    let position = 0
    const turns = (current.turns ?? []).map((t) => {
      if (t.phase === 'ended') return t
      const item = t.item ? { id: t.turnId, ...t.item } : undefined
      return {
        turnId: t.turnId,
        phase: t.phase,
        ...(t.phase === 'queued' ? { position: position++ } : {}),
        ...(item ? { item, clientMessageId: item.clientMessageId } : {}),
      }
    })
    return {
      type: 'dialogue',
      runId,
      nodeId,
      iteration: 1,
      epoch,
      seq,
      mode: 'full',
      rows,
      turns,
      orphan: null,
      busy: turns.some((t) => t.phase === 'running'),
      done: !!current.done,
      ...(current.observe ? { observe: true } : {}),
    }
  }

  return {
    /** Replace the dialogue; the next frame is newer than every earlier one. */
    set(next: FakeDialogue) {
      current = next
      seq++
    },
    /** A server restart: a newer epoch while seq starts over. */
    restart(next: FakeDialogue) {
      current = next
      epoch++
      seq = 1
    },
    get: () => current,
    frame,
    /** The context / chain field that carries this node's state. */
    byNode: () => ({ [nodeId]: frame() }),
  }
}
