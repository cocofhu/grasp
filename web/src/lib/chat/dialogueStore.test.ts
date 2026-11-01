import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import {
  addOptimisticSend,
  clearDialogues,
  dialogueKey,
  dialogueView,
  getDialogue,
  ingestDialogue,
  ingestDialogues,
  rejectOptimisticSend,
  requestDialogueResync,
  setDialogueFetcher,
  useDialogue,
} from './dialogueStore'
import { prefixDigest, type DialogueState } from './dialogueState'

vi.mock('../api/clients/runsClient', () => ({ runsClient: { nodeDialogue: vi.fn(async () => null) } }))

function frame(seq: number, rows: string[], extra: Partial<DialogueState> = {}): DialogueState {
  return {
    type: 'dialogue',
    runId: 'r1',
    nodeId: 'n1',
    iteration: 1,
    epoch: 1,
    seq,
    mode: 'full',
    rows: rows.map((id) => ({ id, rev: 1, phase: 'sealed', role: 'human', text: id, at: '' })),
    turns: [],
    busy: false,
    done: false,
    ...extra,
  }
}

const key = dialogueKey('r1', 'n1')

afterEach(() => {
  clearDialogues()
  setDialogueFetcher(null)
  setDialogueFetcher(null, key)
  vi.useRealTimers()
})

describe('dialogueStore', () => {
  it('keeps only the newest version per node', async () => {
    const view = useDialogue(() => key)
    expect(ingestDialogue(frame(2, ['a', 'b']))).toBe(true)
    expect(ingestDialogue(frame(1, ['a']))).toBe(false)
    await nextTick()
    expect(view.value.rows.map((r) => r.id)).toEqual(['a', 'b'])
    expect(ingestDialogue({ type: 'review', seq: 3 })).toBe(false)
  })

  it('ingests REST snapshots by node id', () => {
    ingestDialogues({ n1: frame(4, ['x']), n2: { ...frame(1, ['y']), nodeId: 'n2' }, bad: null }, 'r1')
    expect(getDialogue(key)!.seq).toBe(4)
    expect(getDialogue(dialogueKey('r1', 'n2'))!.rows[0]!.id).toBe('y')
  })

  it('refetches once when a tail does not fit and coalesces repeats', async () => {
    ingestDialogue(frame(1, ['a']))
    let release!: (s: DialogueState) => void
    const fetcher = vi.fn(() => new Promise<DialogueState>((res) => (release = res)))
    setDialogueFetcher(fetcher, key)
    const bad = { ...frame(3, ['c']), mode: 'tail' as const, prefixLen: 1, prefixDigest: prefixDigest([{ id: 'a', rev: 9 }]) }
    expect(ingestDialogue(bad)).toBe(false)
    ingestDialogue({ ...bad, seq: 4 })
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(fetcher).toHaveBeenCalledWith('r1', 'n1')
    release(frame(4, ['a2', 'c']))
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2))
    release(frame(4, ['a2', 'c']))
    await vi.waitFor(() => expect(getDialogue(key)!.rows.map((r) => r.id)).toEqual(['a2', 'c']))
  })

  it('backs off after a failed refetch', async () => {
    vi.useFakeTimers()
    const fetcher = vi.fn().mockRejectedValueOnce(new Error('down')).mockResolvedValue(frame(2, ['z']))
    setDialogueFetcher(fetcher)
    requestDialogueResync(key)
    await vi.advanceTimersByTimeAsync(0)
    expect(fetcher).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(400)
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(getDialogue(key)!.seq).toBe(2)
  })

  it('drops an optimistic send once the server lists its clientMessageId', () => {
    ingestDialogue(frame(1, []))
    addOptimisticSend(key, { clientMessageId: 'c1', text: 'hi', images: [], annotations: [], at: 't' })
    expect(dialogueView(key).rows.map((r) => r.localPending)).toEqual([true])
    const next = frame(2, [])
    next.rows = [{ id: 't1:h', rev: 2, phase: 'sealed', role: 'human', text: 'hi', at: '', clientMessageId: 'c1', turnId: 't1' }]
    next.turns = [{ turnId: 't1', clientMessageId: 'c1', phase: 'running' }]
    next.busy = true
    ingestDialogue(next)
    const v = dialogueView(key)
    expect(v.rows.map((r) => [r.id, !!r.localPending])).toEqual([['t1:h', false]])
    expect(v.queue).toEqual([])
    addOptimisticSend(key, { clientMessageId: 'c2', text: 'more', images: [], annotations: [], at: 't' })
    expect(dialogueView(key).queue.map((q) => q.text)).toEqual(['more'])
    rejectOptimisticSend(key, 'c2')
    expect(dialogueView(key).queue).toEqual([])
  })

  it('clears by run', () => {
    ingestDialogue(frame(1, ['a']))
    ingestDialogue({ ...frame(1, ['b']), runId: 'r2' })
    clearDialogues({ runId: 'r1' })
    expect(getDialogue(key)).toBeNull()
    expect(getDialogue(dialogueKey('r2', 'n1'))).not.toBeNull()
  })
})
