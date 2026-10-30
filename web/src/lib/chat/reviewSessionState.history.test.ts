import { afterEach, describe, expect, it, vi } from 'vitest'
import { createReviewSessionState, type ReviewSessionFrame, type ReviewSessionSnapshot } from './reviewSessionState'

afterEach(() => vi.useRealTimers())

const item = (id: string) => ({ id, clientMessageId: `send-${id}`, text: '继续' })
const view = (session?: ReviewSessionSnapshot) => ({
  busy: !!session?.busy,
  active: session?.activeItem?.id ?? null,
  queued: session?.items?.map(row => row.id) ?? [],
  waiting: session?.waiting ?? 0,
})
const pending = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

/** Delivery permutations are deterministic; the expected FIFO is a fixed
 * product history, independent of the controller's reducer implementation. */
function shuffled<T>(values: readonly T[], seed: number): T[] {
  const out = [...values]
  let state = seed >>> 0
  for (let index = out.length - 1; index > 0; index--) {
    state ^= state << 13
    state ^= state >>> 17
    state ^= state << 5
    const pick = (state >>> 0) % (index + 1)
    ;[out[index], out[pick]] = [out[pick]!, out[index]!]
  }
  return out
}

// 2882fbea / f4ac5af9: repeated text must never identify a different send.
// 1971431e: clearing dropped waiters precedes the active turn's terminal.
const history: ReviewSessionFrame[] = [
  { event: 'queue_state', busy: true, activeItem: item('a'), waiting: 2, items: [item('b'), item('c')] },
  { event: 'turn_done', busy: false, item: item('a'), waiting: 2, items: [item('b'), item('c')] },
  { event: 'turn_begin', busy: true, item: item('b'), activeItem: item('b'), waiting: 1, items: [item('c')] },
  { event: 'queue_state', busy: true, activeItem: item('b'), waiting: 2, items: [item('c'), item('d')] },
  { event: 'turn_done', busy: false, item: item('b'), waiting: 2, items: [item('c'), item('d')] },
  { event: 'turn_begin', busy: true, item: item('c'), activeItem: item('c'), waiting: 1, items: [item('d')] },
  { event: 'queue_state', busy: true, activeItem: item('c'), waiting: 0, items: [] },
  { event: 'turn_done', busy: false, item: item('c'), waiting: 0, items: [], interrupted: true },
].map((frame, index) => ({ ...frame, nodeId: 'chat', sessionGeneration: 12, revision: index + 1 }))

const expectedHistory = [
  { busy: true, active: 'a', queued: ['b', 'c'], waiting: 2 },
  { busy: false, active: null, queued: ['b', 'c'], waiting: 2 },
  { busy: true, active: 'b', queued: ['c'], waiting: 1 },
  { busy: true, active: 'b', queued: ['c', 'd'], waiting: 2 },
  { busy: false, active: null, queued: ['c', 'd'], waiting: 2 },
  { busy: true, active: 'c', queued: ['d'], waiting: 1 },
  { busy: true, active: 'c', queued: [], waiting: 0 },
  { busy: false, active: null, queued: [], waiting: 0 },
]

describe('workflow queue history delivery', () => {
  describe.each(['ws', 'rest', 'mixed'] as const)('%s authority', delivery => {
    it.each([0x2882fbea, 0xf4ac5af9, 0x1971431e, 0x83640547])('converges identical sends under delayed and duplicate delivery, seed %i', seed => {
      const loop = createReviewSessionState()
      const deliveries = shuffled([...history, ...history.slice(0, 6)], seed)
      let latest = 0
      for (const [index, frame] of deliveries.entries()) {
        const revision = frame.revision!
        if (delivery === 'rest' || (delivery === 'mixed' && index % 2 === 0)) {
          const { event: _event, nodeId: _node, item: _item, ...snapshot } = frame
          loop.reconcileSnapshot({ reviewSessionGeneration: 12,
            reactSessions: { chat: snapshot } }, undefined, ['chat'])
        } else {
          loop.acceptServer(frame)
        }
        latest = Math.max(latest, revision)
        expect(view(loop.session('chat')), `delivery ${index}, revision ${revision}`).toEqual(expectedHistory[latest - 1])
      }
    })
  })

  it.each(['before-cancel', 'after-terminal', 'after-replacement'] as const)('cannot resurrect removed waiters when an old snapshot arrives %s', arrival => {
    const loop = createReviewSessionState()
    const initial = history[5]!
    loop.acceptServer(initial)
    if (arrival === 'before-cancel') loop.reconcileSnapshot({ reactSessions: { chat: initial } })
    loop.acceptServer(history[6]!)
    expect(view(loop.session('chat'))).toEqual(expectedHistory[6])
    loop.acceptServer(history[7]!)
    if (arrival === 'after-terminal') loop.reconcileSnapshot({ reactSessions: { chat: initial } })
    loop.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 13, revision: 1,
      busy: true, waiting: 1, item: item('e'), activeItem: item('e'), items: [item('f')] })
    if (arrival === 'after-replacement') loop.reconcileSnapshot({ reactSessions: { chat: initial } })
    expect(view(loop.session('chat'))).toEqual({ busy: true, active: 'e', queued: ['f'], waiting: 1 })
  })

  it('retires only the captured generation when an unrelated session advances the global watermark', () => {
    const loop = createReviewSessionState()
    loop.reconcileSnapshot({ reviewSessionGeneration: 100, reactSessions: {
      chat: { sessionGeneration: 4, revision: 8, busy: true, activeItem: item('a'), waiting: 1, items: [item('b')] },
    } })
    loop.acceptServer({ event: 'turn_begin', nodeId: 'later', sessionGeneration: 101, revision: 1,
      busy: true, waiting: 0, items: [], item: item('new') })
    loop.reconcileSnapshot({ reviewSessionGeneration: 100 }, undefined, ['chat', 'later'])
    expect(view(loop.session('chat'))).toEqual({ busy: false, active: null, queued: [], waiting: 0 })
    expect(view(loop.session('later'))).toEqual({ busy: true, active: 'new', queued: [], waiting: 0 })
    expect(loop.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 4, revision: 900,
      item: item('retired') })).toBe(false)
    expect(loop.acceptServer({ event: 'queue_state', nodeId: 'later', sessionGeneration: 101, revision: 2,
      busy: true, activeItem: item('new'), waiting: 1, items: [item('next')] })).toBe(true)
    expect(view(loop.session('later'))).toEqual({ busy: true, active: 'new', queued: ['next'], waiting: 1 })
  })

  it('keeps a valid recovery for one node while another node streams and rejects only its stale half', () => {
    const loop = createReviewSessionState()
    loop.reconcileSnapshot({ reviewSessionGeneration: 8, reactSessions: {
      a: { sessionGeneration: 7, revision: 1, busy: true, activeItem: item('a') },
      b: { sessionGeneration: 8, revision: 1, busy: true, activeItem: item('b') },
    } })
    const checkpoint = loop.capture()
    loop.noteAcp('a')
    const frames = loop.reconcileSnapshot({ reviewSessionGeneration: 8 }, checkpoint, ['a', 'b'])
    expect(frames.map(frame => frame.nodeId)).toEqual(['b'])
    expect(view(loop.session('a'))).toEqual({ busy: true, active: 'a', queued: [], waiting: 0 })
    expect(view(loop.session('b'))).toEqual({ busy: false, active: null, queued: [], waiting: 0 })
    expect(loop.acceptServer({ event: 'queue_state', nodeId: 'a', sessionGeneration: 7, revision: 2,
      busy: true, activeItem: item('a'), waiting: 1, items: [item('next')] })).toBe(true)
    expect(loop.acceptServer({ event: 'queue_state', nodeId: 'b', sessionGeneration: 8, revision: 2,
      busy: true, activeItem: item('b') })).toBe(false)
  })

  it.each(['resolve-first', 'ws-first', 'deadline-first', 'reset-first', 'dispose-first'] as const)('does not replay a prior same-text turn across recovery: %s', order => {
    return (async () => {
      vi.useFakeTimers()
      const loop = createReviewSessionState({ recoveryTimeoutMs: 100 })
      loop.acceptServer(history[0]!)
      const response = pending<{ reactSessions: { chat: ReviewSessionSnapshot } }>()
      const read = loop.requestRecovery(async checkpoint => {
        const snapshot = await response.promise
        loop.reconcileSnapshot(snapshot, checkpoint, ['chat'])
      })
      const oldSnapshot = { reactSessions: { chat: {
        sessionGeneration: 12, revision: 1, busy: true, activeItem: item('a'), waiting: 2, items: [item('b'), item('c')],
      } } }
      if (order === 'resolve-first') { response.resolve(oldSnapshot); await read }
      if (order === 'deadline-first') { await vi.advanceTimersByTimeAsync(100); await read }
      if (order === 'reset-first' || order === 'dispose-first') {
        loop[order === 'reset-first' ? 'reset' : 'dispose']()
        await read
      }
      if (order !== 'dispose-first') {
        loop.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 13, revision: 1,
          busy: true, waiting: 1, item: item('b'), activeItem: item('b'), items: [item('c')] })
      }
      response.resolve(oldSnapshot)
      await read
      await Promise.resolve()
      expect(view(loop.session('chat'))).toEqual(order === 'dispose-first'
        ? { busy: false, active: null, queued: [], waiting: 0 }
        : { busy: true, active: 'b', queued: ['c'], waiting: 1 })
      expect(vi.getTimerCount()).toBe(0)
    })()
  })
})
