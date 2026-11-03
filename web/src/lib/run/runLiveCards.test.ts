// @vitest-environment happy-dom
import { defineComponent, h, inject, nextTick, provide, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LIVE_CARD_HOST, createLiveStore, type LiveCardHost } from '@/lib/inbox/liveVariants'

const mocks = vi.hoisted(() => ({ liveSessions: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { liveSessions: mocks.liveSessions } }))

import { applyRunLiveFrame, refreshRunLive, useRunLiveCardHost } from './runLiveCards'

let seen: LiveCardHost | null = null
let returned: LiveCardHost | null = null

const Child = defineComponent({
  setup() {
    seen = inject(LIVE_CARD_HOST, null)
    return () => h('div')
  },
})

function mountHost(runId: () => string, nodeId: () => string, above?: LiveCardHost) {
  const Host = defineComponent({
    setup() {
      returned = useRunLiveCardHost(runId, nodeId)
      return () => h(Child)
    },
  })
  const Root = defineComponent({
    setup() {
      if (above) provide(LIVE_CARD_HOST, above)
      return () => h(Host)
    },
  })
  return mount(Root)
}

function session(sid: string, state: string, updatedAt = '2026-11-03T10:00:00Z', extra: Record<string, unknown> = {}) {
  return { sid, state, updatedAt, ...extra }
}

const frame = (runId: string, nodeId: string, s: unknown) => ({ type: 'live', runId, nodeId, session: s })

beforeEach(() => {
  mocks.liveSessions.mockReset()
  mocks.liveSessions.mockResolvedValue({ enabled: true, sessions: [] })
  seen = null
  returned = null
})

afterEach(() => {
  document.body.replaceChildren()
})

describe('runLiveCards', () => {
  it('provides a read-only host and loads the node sessions once', async () => {
    mocks.liveSessions.mockResolvedValue({ enabled: true, sessions: [session('sid001', 'ready', undefined, { variants: [{ n: 1 }] })] })
    const w = mountHost(() => 'run-1', () => 'clarify')
    await flushPromises()
    expect(mocks.liveSessions).toHaveBeenCalledTimes(1)
    expect(mocks.liveSessions).toHaveBeenCalledWith('run-1', 'clarify')
    expect(seen).toBe(returned)
    expect(seen!.interactive).toBe(false)
    expect(seen!.store.enabled).toBe(true)
    expect(seen!.store.sessions.sid001?.state).toBe('ready')
    expect(() => seen!.command('sid001', 'accept', 1)).not.toThrow()
    w.unmount()
  })

  it('merges frames for the same run and node only, keeping newer state', async () => {
    const w = mountHost(() => 'run-1', () => 'clarify')
    await flushPromises()
    expect(applyRunLiveFrame(frame('run-1', 'clarify', session('sid001', 'generating', '2026-11-03T10:00:00Z')))).toBe(true)
    expect(seen!.store.sessions.sid001?.state).toBe('generating')
    expect(applyRunLiveFrame(frame('run-1', 'clarify', session('sid001', 'ready', '2026-11-03T10:00:05Z')))).toBe(true)
    expect(seen!.store.sessions.sid001?.state).toBe('ready')
    applyRunLiveFrame(frame('run-1', 'clarify', session('sid001', 'generating', '2026-11-03T10:00:01Z')))
    expect(seen!.store.sessions.sid001?.state).toBe('ready')
    expect(applyRunLiveFrame(frame('run-1', 'other', session('sid002', 'ready')))).toBe(false)
    expect(applyRunLiveFrame(frame('run-2', 'clarify', session('sid003', 'ready')))).toBe(false)
    expect(applyRunLiveFrame({ type: 'dialogue', runId: 'run-1', nodeId: 'clarify' })).toBe(false)
    expect(applyRunLiveFrame(null)).toBe(false)
    expect(Object.keys(seen!.store.sessions)).toEqual(['sid001'])
    w.unmount()
  })

  it('does not let an older fetch overwrite a newer frame', async () => {
    let resolve!: (v: unknown) => void
    mocks.liveSessions.mockReturnValue(new Promise((r) => (resolve = r)))
    const w = mountHost(() => 'run-1', () => 'clarify')
    applyRunLiveFrame(frame('run-1', 'clarify', session('sid001', 'discarded', '2026-11-03T10:00:09Z')))
    resolve({ enabled: true, sessions: [session('sid001', 'discarding', '2026-11-03T10:00:01Z')] })
    await flushPromises()
    expect(seen!.store.sessions.sid001?.state).toBe('discarded')
    w.unmount()
  })

  it('reloads after a reconnect for the run on screen', async () => {
    const w = mountHost(() => 'run-1', () => 'clarify')
    await flushPromises()
    mocks.liveSessions.mockResolvedValue({ enabled: true, sessions: [session('sid001', 'discarded', '2026-11-03T10:00:09Z')] })
    await refreshRunLive('run-1')
    expect(mocks.liveSessions).toHaveBeenCalledTimes(2)
    expect(seen!.store.sessions.sid001?.state).toBe('discarded')
    await refreshRunLive('run-other')
    expect(mocks.liveSessions).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('loads a missing session once and folds concurrent asks into one more fetch', async () => {
    const w = mountHost(() => 'run-1', () => 'clarify')
    await flushPromises()
    expect(mocks.liveSessions).toHaveBeenCalledTimes(1)
    let resolve!: (v: unknown) => void
    mocks.liveSessions.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    seen!.ensure!('sidA01')
    seen!.ensure!('sidA01')
    seen!.ensure!('sidB01')
    seen!.ensure!('sidC01')
    expect(mocks.liveSessions).toHaveBeenCalledTimes(2)
    resolve({ enabled: true, sessions: [session('sidA01', 'ready')] })
    await flushPromises()
    // B and C asked while the first fetch ran: one follow-up fetch for both.
    expect(mocks.liveSessions).toHaveBeenCalledTimes(3)
    seen!.ensure!('sidA01')
    seen!.ensure!('sidB01')
    await flushPromises()
    expect(mocks.liveSessions).toHaveBeenCalledTimes(3)
    w.unmount()
  })

  it('fetches once on mount when a card asks for a session the first response carries', async () => {
    let resolve!: (v: unknown) => void
    mocks.liveSessions.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    const w = mountHost(() => 'run-1', () => 'clarify')
    seen!.ensure!('sidA01')
    resolve({ enabled: true, sessions: [session('sidA01', 'ready')] })
    await flushPromises()
    expect(mocks.liveSessions).toHaveBeenCalledTimes(1)
    expect(seen!.store.sessions.sidA01?.state).toBe('ready')
    w.unmount()
  })

  it('refetches once after mount when the first response lacks the asked session', async () => {
    let resolve!: (v: unknown) => void
    mocks.liveSessions.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    const w = mountHost(() => 'run-1', () => 'clarify')
    seen!.ensure!('sidA01')
    resolve({ enabled: true, sessions: [] })
    await flushPromises()
    expect(mocks.liveSessions).toHaveBeenCalledTimes(2)
    seen!.ensure!('sidA01')
    await flushPromises()
    expect(mocks.liveSessions).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('shares one fetch between sockets reconnecting together', async () => {
    const w = mountHost(() => 'run-1', () => 'clarify')
    await flushPromises()
    await Promise.all([refreshRunLive('run-1'), refreshRunLive('run-1')])
    expect(mocks.liveSessions).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('refetches when a reconnect lands while the mount fetch is still out', async () => {
    let resolve!: (v: unknown) => void
    mocks.liveSessions.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    const w = mountHost(() => 'run-1', () => 'clarify')
    const refreshed = refreshRunLive('run-1')
    void refreshRunLive('run-1')
    resolve({ enabled: true, sessions: [] })
    await refreshed
    await flushPromises()
    expect(mocks.liveSessions).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('survives a failed fetch', async () => {
    mocks.liveSessions.mockRejectedValue(new Error('boom'))
    const w = mountHost(() => 'run-1', () => 'clarify')
    await flushPromises()
    expect(seen!.store.sessions).toEqual({})
    expect(seen!.store.enabled).toBe(false)
    await expect(refreshRunLive('run-1')).resolves.toBeUndefined()
    w.unmount()
  })

  it('switches store with the run or node and drops it when unmounted', async () => {
    const node = ref('clarify')
    const w = mountHost(() => 'run-1', () => node.value)
    await flushPromises()
    applyRunLiveFrame(frame('run-1', 'clarify', session('sid001', 'ready')))
    node.value = 'review'
    await nextTick()
    await flushPromises()
    expect(mocks.liveSessions).toHaveBeenLastCalledWith('run-1', 'review')
    expect(seen!.store.sessions).toEqual({})
    expect(applyRunLiveFrame(frame('run-1', 'clarify', session('sid001', 'discarded')))).toBe(false)
    expect(applyRunLiveFrame(frame('run-1', 'review', session('sid009', 'ready')))).toBe(true)
    expect(seen!.store.sessions.sid009?.state).toBe('ready')
    w.unmount()
    expect(applyRunLiveFrame(frame('run-1', 'review', session('sid009', 'done')))).toBe(false)
  })

  it('keeps a host provided above (the preview drawer) and skips share pages', async () => {
    const drawer: LiveCardHost = { store: createLiveStore().store, interactive: true, command: vi.fn() }
    const w = mountHost(() => 'run-1', () => 'clarify', drawer)
    expect(returned).toBe(drawer)
    expect(seen).toBe(drawer)
    w.unmount()
    const share = mountHost(() => 'public-share', () => 'public-gate')
    expect(returned).toBeNull()
    expect(seen).toBeNull()
    share.unmount()
    const empty = mountHost(() => 'run-1', () => '')
    expect(seen!.interactive).toBe(false)
    expect(seen!.store.sessions).toEqual({})
    empty.unmount()
    await flushPromises()
    expect(mocks.liveSessions).not.toHaveBeenCalled()
  })
})
