import { afterEach, describe, expect, it, vi } from 'vitest'
import { acpSemanticFingerprint, createReviewSessionState, hasAcpContent, REVIEW_RECOVERY_TIMEOUT_MS, reviewQueueFrame, type ReviewRecoveryCheckpoint } from './reviewSessionState'
import type { AcpEvent } from '@/lib/shared/types'

afterEach(() => vi.useRealTimers())

describe('reviewSessionState', () => {
  const deferred = () => {
    let resolve!: () => void
    const promise = new Promise<void>(done => { resolve = done })
    return { promise, resolve }
  }

  it.each([
    [{ kind: 'thought', t: 0, text: 'thinking' }, true],
    [{ kind: 'message', t: 0, text: 'reply' }, true],
    [{ kind: 'plan', t: 0, text: '- [pending] implement' }, true],
    [{ kind: 'commands', t: 0, title: '/help' }, true],
    [{ kind: 'tool_call', t: 0, title: 'Read', status: 'running' }, true],
    [{ kind: 'timeline', t: 0, parts: [{ kind: 'tool', title: 'Read', output: 'file body' }] }, true],
    [{ kind: 'timeline', t: 0, parts: [{ kind: 'message', text: 'body' }] }, true],
    [{ kind: 'timeline', t: 0, parts: [{ kind: 'thought', text: 'reasoning' }] }, true],
    [{ kind: 'liveness', t: 0 }, false],
    [{ kind: 'thought', t: 0, text: '' }, false],
    [{ kind: 'message', t: 0, text: ' ' }, false],
    [{ kind: 'tool_call', t: 0 }, false],
    [{ kind: 'timeline', t: 0, parts: [] }, false],
    [{ kind: 'timeline', t: 0, parts: [{ kind: 'thought', text: '' }] }, false],
    [{ kind: 'turn_end', t: 0 }, false],
  ] satisfies [AcpEvent, boolean][])('identifies renderable ACP without requiring prose: %j', (event, expected) => {
    expect(hasAcpContent([event])).toBe(expected)
  })

  it('ignores empty content but fingerprints terminal usage and all detailed tool fields', () => {
    expect(hasAcpContent()).toBe(false)
    expect(hasAcpContent(null)).toBe(false)
    expect(hasAcpContent([])).toBe(false)
    const tool: AcpEvent = { kind: 'timeline', t: 0, parts: [{ kind: 'tool', title: 'Write',
      status: 'completed', summary: 'save file', input: 'a', output: 'b', durationMs: 100 }] }
    for (const [field, value] of [['summary', 'new summary'], ['input', 'new input'], ['output', 'new output'], ['durationMs', 200]] as const) {
      const changed: AcpEvent = { ...tool, parts: [{ ...tool.parts![0]!, [field]: value }] }
      expect(acpSemanticFingerprint([changed])).not.toBe(acpSemanticFingerprint([tool]))
    }
  })

  it.each(['empty', 'liveness-only', 'unchanged-cumulative'] as const)('does not mistake %s ACP packets for content progress during recovery', packet => {
    const state = createReviewSessionState()
    state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 4, revision: 1, item: { id: 'active' } })
    const thought: AcpEvent = { kind: 'thought', t: 0, text: 'working' }
    const heartbeat: AcpEvent = { kind: 'liveness', t: 1,
      liveness: { active: true, cpuMs: 100, ioBytes: 1024, idleSec: 0, limitSec: 1200 } }
    state.noteAcp('chat', [thought])
    const checkpoint = state.capture()
    state.noteAcp('chat', packet === 'empty' ? [] : packet === 'liveness-only' ? [heartbeat] : [thought, heartbeat])
    expect(state.hasAcpProgress(checkpoint, 'chat')).toBe(false)
    expect(state.isCurrent(checkpoint, 'chat')).toBe(true)
    expect(state.reconcileSnapshot({ reviewSessionGeneration: 4 }, checkpoint, ['chat'])).toHaveLength(1)
    expect(state.session('chat')?.busy).toBe(false)
  })

  it.each([
    [{ kind: 'tool_call', t: 0, title: 'Write', status: 'running' }, { kind: 'tool_call', t: 0, title: 'Write', status: 'completed' }],
    [{ kind: 'timeline', t: 0, parts: [{ kind: 'tool', title: 'Write', status: 'running' }] },
      { kind: 'timeline', t: 0, parts: [{ kind: 'tool', title: 'Write', status: 'completed', output: 'saved' }] }],
    [{ kind: 'turn_end', t: 0, usage: { inputTokens: 1, outputTokens: 2, cacheReadTokens: 0, cacheWriteTokens: 0 } },
      { kind: 'turn_end', t: 0, usage: { inputTokens: 1, outputTokens: 3, cacheReadTokens: 0, cacheWriteTokens: 0 } }],
  ] satisfies [AcpEvent, AcpEvent][])('recognizes a semantic update without requiring message text: %j', (before, after) => {
    const state = createReviewSessionState()
    state.noteAcp('chat', [before])
    const checkpoint = state.capture()
    state.noteAcp('chat', [after])
    expect(state.hasAcpProgress(checkpoint, 'chat')).toBe(true)
    expect(state.isCurrent(checkpoint, 'chat')).toBe(false)
  })

  it('recognizes identical content for a different send but ignores an identical replay within the same send', () => {
    const state = createReviewSessionState()
    const content: AcpEvent[] = [{ kind: 'message', t: 0, text: '好的' }]
    state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 4, revision: 1, item: { id: 'a', text: '继续' } })
    state.noteAcp('chat', content)
    const first = state.capture()
    state.noteAcp('chat', [{ ...content[0]!, t: 100 }])
    expect(state.hasAcpProgress(first, 'chat')).toBe(false)
    state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 4, revision: 2, item: { id: 'b', text: '继续' } })
    const second = state.capture()
    state.noteAcp('chat', content)
    expect(state.hasAcpProgress(second, 'chat')).toBe(true)
  })

  it('expires a hung read and runs the latest trailing tick without clearing the real busy state', async () => {
    vi.useFakeTimers()
    const state = createReviewSessionState({ recoveryTimeoutMs: 100 })
    state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 2, revision: 1, item: { id: 'active' } })
    let signal!: AbortSignal
    let checkpoint!: ReviewRecoveryCheckpoint
    let reject!: (reason: unknown) => void
    const first = state.requestRecovery(async (captured, taskSignal) => {
      checkpoint = captured
      signal = taskSignal
      await new Promise<void>((_resolve, fail) => { reject = fail })
    })
    const skipped = vi.fn(async () => {})
    const trailing = vi.fn(async (_captured: ReviewRecoveryCheckpoint, taskSignal: AbortSignal) => {
      expect(taskSignal.aborted).toBe(false)
      expect(taskSignal).not.toBe(signal)
    })
    void state.requestRecovery(skipped)
    void state.requestRecovery(trailing)
    await vi.advanceTimersByTimeAsync(100)
    await first
    expect(signal.aborted).toBe(true)
    expect(state.isCurrent(checkpoint, 'chat')).toBe(false)
    expect(state.isStateCurrent(checkpoint, 'chat')).toBe(false)
    expect(state.session('chat')?.busy).toBe(true)
    expect(skipped).not.toHaveBeenCalled()
    expect(trailing).toHaveBeenCalledOnce()
    reject(new Error('late failed fetch'))
    await Promise.resolve()
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['retired generation', 'newer ACP'] as const)('does not admit rejected snapshot generations after %s', rejection => {
    const state = createReviewSessionState()
    state.installSnapshot({}, 100)
    const checkpoint = state.capture()
    if (rejection === 'newer ACP') state.noteAcp('old', [{ kind: 'thought', t: 0, text: 'newer observation' }])
    expect(state.reconcileSnapshot({ reviewSessionGeneration: 99, reactSessions: {
      old: { sessionGeneration: 99, revision: 1, busy: true, waiting: 0, items: [] },
    } }, checkpoint)).toEqual([])
    expect(state.session('old')).toBeUndefined()
    expect(state.acceptServer({ event: 'turn_begin', nodeId: 'old', sessionGeneration: 99,
      revision: 2, item: { id: 'retired' } })).toBe(false)
    expect(state.session('old')).toBeUndefined()
    expect(state.acceptServer({ event: 'turn_begin', nodeId: 'old', sessionGeneration: 101,
      revision: 1, item: { id: 'fresh' } })).toBe(true)
    expect(state.session('old')?.activeItem?.id).toBe('fresh')
  })

  it('uses the heartbeat window as its default deadline', async () => {
    vi.useFakeTimers()
    const state = createReviewSessionState()
    const done = vi.fn()
    const pending = state.requestRecovery(async () => { await new Promise<void>(() => {}) }).then(done)
    await vi.advanceTimersByTimeAsync(REVIEW_RECOVERY_TIMEOUT_MS - 1)
    expect(done).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await pending
    expect(done).toHaveBeenCalledOnce()
  })

  it.each(['reset', 'dispose'] as const)('releases waiting callers on %s even if the read ignores abort', async action => {
    vi.useFakeTimers()
    const state = createReviewSessionState()
    let signal!: AbortSignal
    const first = state.requestRecovery(async (_checkpoint, taskSignal) => {
      signal = taskSignal
      await new Promise<void>(() => {})
    })
    const trailing = vi.fn(async () => {})
    void state.requestRecovery(trailing)
    state[action]()
    await first
    expect(signal.aborted).toBe(true)
    expect(trailing).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('serializes recovery and keeps one trailing tick while realtime ingress keeps flowing', async () => {
    const state = createReviewSessionState()
    const gate = deferred()
    const calls: string[] = []
    const first = state.requestRecovery(async checkpoint => {
      calls.push('first')
      await gate.promise
      expect(state.isCurrent(checkpoint, 'chat')).toBe(false)
    })
    await Promise.resolve()
    expect(state.ingest({ kind: 'review', frame: { event: 'turn_begin', nodeId: 'chat', sessionGeneration: 2,
      revision: 1, item: { id: 'active' } } })).toBe(true)
    expect(state.session('chat')?.busy).toBe(true)
    expect(state.requestRecovery(async () => { calls.push('replaced') })).toBe(first)
    expect(state.requestRecovery(async () => { calls.push('trailing') })).toBe(first)
    expect(calls).toEqual(['first'])
    gate.resolve()
    await first
    expect(calls).toEqual(['first', 'trailing'])
  })

  it('aborts recovery on reset and drops its queued tick while allowing a new visit', async () => {
    const state = createReviewSessionState()
    const gate = deferred()
    let oldSignal!: AbortSignal
    const first = state.requestRecovery(async (_checkpoint, signal) => { oldSignal = signal; await gate.promise })
    await Promise.resolve()
    const trailing = vi.fn(async () => {})
    void state.requestRecovery(trailing)
    state.reset()
    expect(oldSignal.aborted).toBe(true)
    const current = vi.fn(async () => {})
    await state.requestRecovery(current)
    gate.resolve()
    await first
    expect(trailing).not.toHaveBeenCalled()
    expect(current).toHaveBeenCalledOnce()
    state.dispose()
    await state.requestRecovery(current)
    expect(current).toHaveBeenCalledOnce()
    expect(state.acceptServer({ event: 'turn_begin', nodeId: 'chat' })).toBe(false)
  })

  it('drains an already requested trailing recovery after the first read fails', async () => {
    const state = createReviewSessionState()
    const gate = deferred()
    const first = state.requestRecovery(async () => { await gate.promise; throw new Error('temporary read failure') })
    await Promise.resolve()
    const trailing = vi.fn(async () => {})
    void state.requestRecovery(trailing)
    const result = expect(first).rejects.toThrow('temporary read failure')
    gate.resolve()
    await result
    expect(trailing).toHaveBeenCalledOnce()
  })

  it('starts a fresh recovery for a tick at the previous drain completion boundary', async () => {
    const state = createReviewSessionState()
    const gate = deferred()
    const first = state.requestRecovery(async () => { await gate.promise })
    await Promise.resolve()
    gate.resolve()
    await Promise.resolve()
    const next = vi.fn(async () => {})
    const boundary = state.requestRecovery(next)
    await Promise.all([first, boundary])
    expect(next).toHaveBeenCalledOnce()
  })

  it('does not publish an earlier read failure after the recovery visit was reset', async () => {
    const state = createReviewSessionState()
    const gate = deferred()
    const first = state.requestRecovery(async () => { throw new Error('failed read') })
    void state.requestRecovery(async () => { await gate.promise })
    await Promise.resolve()
    state.reset()
    gate.resolve()
    await expect(first).resolves.toBeUndefined()
  })

  it('projects one queue contract including attachments and bridge state', () => {
    const activeItem = { id: 'a', text: 'active', images: [{ url: 'data:image/png;base64,AA' }] }
    expect(reviewQueueFrame('chat', { sessionGeneration: 2, revision: 5, busy: true, waiting: 1,
      items: [{ id: 'b', text: 'queued' }], activeItem, sandboxBusy: true, sandboxDesynced: false,
      sandboxRunningOpId: 'op' })).toEqual({ event: 'queue_state', nodeId: 'chat',
      sessionGeneration: 2, revision: 5, busy: true, waiting: 1,
      items: [{ id: 'b', text: 'queued' }], activeItem, sandboxBusy: true, sandboxDesynced: false,
      sandboxRunningOpId: 'op' })
    expect(reviewQueueFrame('chat')).toEqual({ event: 'queue_state', nodeId: 'chat', busy: false, waiting: 0, items: [] })
  })

  it('reduces full begin and terminal queue snapshots without retaining dropped waiters', () => {
    const state = createReviewSessionState()
    state.acceptServer({ event: 'queue_state', nodeId: 'chat', sessionGeneration: 2, revision: 1,
      busy: true, waiting: 2, items: [{ id: 'a' }, { id: 'b' }], activeItem: { id: 'first' } })
    state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 2, revision: 2,
      item: { id: 'a' }, waiting: 1, items: [{ id: 'b' }] })
    expect(state.session('chat')).toMatchObject({ busy: true, activeItem: { id: 'a' }, waiting: 1, items: [{ id: 'b' }] })
    state.acceptServer({ event: 'turn_done', nodeId: 'chat', sessionGeneration: 2, revision: 3,
      busy: false, waiting: 0, items: [] })
    expect(state.session('chat')).toEqual({ sessionGeneration: 2, revision: 3, busy: false, waiting: 0, items: [], activeItem: undefined })
    expect(state.acceptServer({ event: 'queue_state', nodeId: 'chat', sessionGeneration: 2, revision: 1,
      busy: true, waiting: 2, items: [{ id: 'a' }, { id: 'b' }] })).toBe(false)
    expect(state.session('chat')?.busy).toBe(false)
  })

  it('uses watermark authority for absent sessions and rejects retired queue frames', () => {
    const state = createReviewSessionState()
    state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 8, revision: 1, item: { id: 'a' } })
    expect(state.reconcileSnapshot({ reviewSessionGeneration: 7 }, undefined, ['chat'])).toEqual([])
    expect(state.reconcileSnapshot({ reviewSessionGeneration: 8 }, undefined, ['chat'])).toEqual([
      { event: 'queue_state', nodeId: 'chat', busy: false, waiting: 0, items: [] },
    ])
    expect(state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 8, revision: 2 })).toBe(false)
    expect(state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 9, revision: 1 })).toBe(true)
  })

  it('rejects stale recovery per node and keeps its live generation admitted', () => {
    const state = createReviewSessionState()
    state.acceptServer({ event: 'turn_begin', nodeId: 'a', sessionGeneration: 4, revision: 1, item: { id: 'first' } })
    const checkpoint = state.capture()
    state.noteAcp('a')
    expect(state.reconcileSnapshot({ reviewSessionGeneration: 4, reactSessions: {} }, checkpoint, ['a', 'b'])).toEqual([
      { event: 'queue_state', nodeId: 'b', busy: false, waiting: 0, items: [] },
    ])
    expect(state.session('a')?.busy).toBe(true)
    expect(state.acceptServer({ event: 'turn_done', nodeId: 'a', sessionGeneration: 4, revision: 2 })).toBe(true)
  })

  it('invalidates content recovery when the active item or revision changes in another snapshot', () => {
    const state = createReviewSessionState()
    state.reconcileSnapshot({ reactSessions: { chat: { sessionGeneration: 3, revision: 1, busy: true, activeItem: { id: 'a' } } } })
    const checkpoint = state.capture()
    state.reconcileSnapshot({ reactSessions: { chat: { sessionGeneration: 3, revision: 2, busy: true, activeItem: { id: 'a' } } } })
    expect(state.isCurrent(checkpoint, 'chat')).toBe(false)
    const revised = state.capture()
    state.reconcileSnapshot({ reactSessions: { chat: { sessionGeneration: 3, revision: 3, busy: true, activeItem: { id: 'b' } } } })
    expect(state.isCurrent(revised, 'chat')).toBe(false)
    const visit = state.capture()
    state.noteVisit('chat')
    expect(state.isCurrent(visit, 'chat')).toBe(false)
  })

  it('separates transport liveness from busy and invalidates earlier visits on reset', () => {
    const state = createReviewSessionState()
    state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 500, revision: 2, item: { id: 'a' } })
    const checkpoint = state.capture()
    expect(state.isCurrent(checkpoint, 'chat')).toBe(true)
    expect(state.session('chat')?.busy).toBe(true)
    state.noteAcp('chat')
    expect(state.hasAcpProgress(checkpoint, 'chat')).toBe(true)
    expect(state.isStateCurrent(checkpoint, 'chat')).toBe(true)
    expect(state.isCurrent(checkpoint, 'chat')).toBe(false)
    const progress = state.capture()
    state.acceptServer({ event: 'queue_state', nodeId: 'chat', sessionGeneration: 500, revision: 3, busy: true })
    expect(state.hasAcpProgress(progress, 'chat')).toBe(false)
    expect(state.isStateCurrent(progress, 'chat')).toBe(false)
    // Heartbeats have no reducer action; a quiet native turn stays busy.
    expect(state.reconcileSnapshot({}, checkpoint, ['chat'])).toEqual([])
    expect(state.session('chat')?.busy).toBe(true)
    state.reset()
    expect(state.isCurrent(checkpoint, 'chat')).toBe(false)
    expect(state.reconcileSnapshot({ reviewSessionGeneration: 500 }, checkpoint, ['chat'])).toEqual([])
    expect(state.acceptServer({ event: 'turn_begin', nodeId: 'chat', sessionGeneration: 1, revision: 1 })).toBe(true)
  })
})
