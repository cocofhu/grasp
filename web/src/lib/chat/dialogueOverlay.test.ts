import { describe, expect, it } from 'vitest'
import type { ClarifyTurn } from '@/lib/shared/types'
import {
  isSettled,
  isStopping,
  pruneQueueOverlay,
  retryAttemptRows,
  retryTakenOver,
  settleRows,
  visibleOrphan,
  visibleQueue,
} from './dialogueOverlay'
import type { DialogueQueueRow, DialogueState } from './dialogueState'

function st(seq: number, extra: Partial<DialogueState> = {}): DialogueState {
  return { type: 'dialogue', runId: 'r', nodeId: 'n', iteration: 1, epoch: 1, seq, mode: 'full', rows: [], turns: [], busy: false, done: false, ...extra }
}

const q = (id: string): DialogueQueueRow => ({ id, text: id, images: [], annotations: [] })

describe('dialogue overlays give way to the server state', () => {
  it('settled: only versions up to the confirm click are held idle', () => {
    expect(isSettled(null, st(3))).toBe(false)
    expect(isSettled({ epoch: 1, seq: 3 }, st(3))).toBe(true)
    expect(isSettled({ epoch: 1, seq: 3 }, null)).toBe(true)
    expect(isSettled({ epoch: 1, seq: 3 }, st(4))).toBe(false)
    expect(isSettled({ epoch: 1, seq: 9 }, st(1, { epoch: 2 }))).toBe(false)
  })

  it('settleRows stops every streaming row', () => {
    const rows: ClarifyTurn[] = [{ role: 'agent', text: 'a', at: '', streaming: true }, { role: 'human', text: 'h', at: '' }]
    expect(settleRows(rows).map((r) => !!r.streaming)).toEqual([false, false])
  })

  it('retry: a trailing failed reply shows as a new attempt; anything else is untouched', () => {
    const failed: ClarifyTurn[] = [
      { role: 'human', text: 'q', at: '' },
      { role: 'agent', text: '(澄清回复失败:x)', at: '', outcome: 'failed', error: 'x' },
    ]
    const out = retryAttemptRows(failed)
    expect(out[1]).toMatchObject({ role: 'agent', text: '', streaming: true, localPending: true, outcome: undefined, error: undefined })
    const ok: ClarifyTurn[] = [{ role: 'agent', text: 'fine', at: '', outcome: 'ok' }]
    expect(retryAttemptRows(ok)).toBe(ok)
  })

  it('retry is taken over once the server is busy or no longer ends on a failure', () => {
    const empty = { id: 't:a0', phase: 'sealed' as const, rev: 1, role: 'agent' as const, text: '', at: '', outcome: 'empty' as const }
    expect(retryTakenOver(st(1, { rows: [empty] }))).toBe(false)
    expect(retryTakenOver(st(2, { rows: [empty], busy: true }))).toBe(true)
    expect(retryTakenOver(st(3, { rows: [{ ...empty, outcome: 'ok', text: 'x' }] }))).toBe(true)
  })

  it('queue: hidden rows and a local order hold until the server queue agrees', () => {
    const rows = [q('a'), q('b'), q('c')]
    expect(visibleQueue(rows, { hidden: ['b'], order: null }).map((r) => r.id)).toEqual(['a', 'c'])
    expect(visibleQueue(rows, { hidden: [], order: ['c', 'a', 'b'] }).map((r) => r.id)).toEqual(['c', 'a', 'b'])
    expect(visibleQueue([...rows, { text: 'local', images: [], annotations: [], local: true }], { hidden: [], order: ['b'] }).map((r) => r.text))
      .toEqual(['b', 'a', 'c', 'local'])

    expect(pruneQueueOverlay({ hidden: ['b', 'gone'], order: ['c', 'a'] }, ['a', 'b', 'c'])).toEqual({ hidden: ['b'], order: ['c', 'a'] })
    expect(pruneQueueOverlay({ hidden: [], order: ['c', 'a', 'b'] }, ['c', 'a', 'b'])).toEqual({ hidden: [], order: null })
    expect(pruneQueueOverlay({ hidden: [], order: ['c', 'a', 'b'] }, ['a', 'b', 'c'])).toEqual({ hidden: [], order: ['c', 'a', 'b'] })
  })

  it('orphan: the op Stop was clicked for is hidden; a different op shows', () => {
    const orphan = { runningOpId: 'op-1', desynced: false }
    expect(visibleOrphan(orphan, 'op-1')).toBeNull()
    expect(visibleOrphan(orphan, 'op-0')).toBe(orphan)
    expect(visibleOrphan(null, 'op-1')).toBeNull()
  })

  it('stopping holds only while the server still runs that turn', () => {
    expect(isStopping('t1', ['t1'])).toBe(true)
    expect(isStopping('t1', ['t2'])).toBe(false)
    expect(isStopping(null, ['t1'])).toBe(false)
  })
})
