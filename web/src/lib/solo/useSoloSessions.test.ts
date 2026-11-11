// @vitest-environment happy-dom
import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SoloSession, SoloSessionList } from '@/lib/api/apiTypes'

const api = vi.hoisted(() => ({ listSoloSessions: vi.fn(), closeSoloSession: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api }))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

const session = (id: string, status: SoloSession['status'] = 'running'): SoloSession => ({ id, agentName: 'Demo', title: id, status, busy: false, waiting: 0, restarts: 0, createdAt: '', updatedAt: '' })

describe('Solo session closing', () => {
  beforeEach(() => {
    vi.resetModules()
    api.listSoloSessions.mockReset().mockResolvedValue({ sessions: [], maxSandboxes: 5 })
    api.closeSoloSession.mockReset()
  })

  it('shares one close per session across callers while allowing another session to close', async () => {
    const { useSoloSessions } = await import('./useSoloSessions')
    const first = useSoloSessions(), second = useSoloSessions()
    const one = deferred<SoloSession>(), two = deferred<SoloSession>()
    api.closeSoloSession.mockImplementation(id => id === 'one' ? one.promise : two.promise)
    first.upsert(session('one'))
    first.upsert(session('two'))
    const closingOne = first.close('one')
    expect(second.close('one')).toBe(closingOne)
    const closingTwo = second.close('two')
    expect([...first.closing.value]).toEqual(['one', 'two'])
    expect(first.active.value).toHaveLength(2)
    expect(api.closeSoloSession).toHaveBeenCalledTimes(2)
    two.resolve(session('two', 'closed'))
    await closingTwo
    expect([...first.closing.value]).toEqual(['one'])
    one.resolve(session('one', 'closed'))
    await closingOne
    expect(first.active.value).toHaveLength(0)
    expect(first.closing.value.size).toBe(0)
  })

  it('does not resurrect a closed session when an older list response arrives last', async () => {
    const { useSoloSessions } = await import('./useSoloSessions')
    const store = useSoloSessions()
    store.upsert(session('one'))
    const oldList = deferred<SoloSessionList>()
    api.listSoloSessions.mockReturnValueOnce(oldList.promise)
    const refresh = store.refresh()
    api.closeSoloSession.mockResolvedValue(session('one', 'closed'))
    await store.close('one')
    oldList.resolve({ sessions: [session('one')], maxSandboxes: 5 })
    await refresh
    expect(store.active.value).toHaveLength(0)
    expect(store.ended.value.map(row => row.id)).toEqual(['one'])
  })

  it('reads the actual result after a failed close and an already running list request', async () => {
    const { useSoloSessions } = await import('./useSoloSessions')
    const store = useSoloSessions()
    store.upsert(session('one'))
    const oldList = deferred<SoloSessionList>()
    api.listSoloSessions.mockReturnValueOnce(oldList.promise).mockResolvedValue({ sessions: [session('one', 'closed')], maxSandboxes: 5 })
    const refresh = store.refresh()
    api.closeSoloSession.mockRejectedValue(new Error('stop pending'))
    await expect(store.close('one')).rejects.toThrow('stop pending')
    expect(store.closing.value.size).toBe(0)
    expect(store.sessions.value[0]?.status).toBe('running')
    oldList.resolve({ sessions: [session('one')], maxSandboxes: 5 })
    await refresh
    await flushPromises()
    expect(api.listSoloSessions).toHaveBeenCalledTimes(2)
    expect(store.sessions.value[0]?.status).toBe('closed')
    expect(api.closeSoloSession).toHaveBeenCalledTimes(1)
  })

  it('does not let an older session list undo a stop that already settled', async () => {
    const { useSoloSessions } = await import('./useSoloSessions')
    const store = useSoloSessions()
    store.upsert(session('one'))
    const oldList = deferred<SoloSessionList>()
    api.listSoloSessions.mockReturnValueOnce(oldList.promise)
    const refresh = store.refresh()
    const pending = store.changeEnvironment('one', 'stopping', () => Promise.resolve(session('one', 'stopped')))
    expect(store.environmentChanges.value.get('one')).toBe('stopping')
    await pending
    expect(store.environmentChanges.value.size).toBe(0)
    expect(store.sessions.value[0]?.status).toBe('stopped')
    oldList.resolve({ sessions: [session('one')], maxSandboxes: 5 })
    await refresh
    expect(store.sessions.value[0]?.status).toBe('stopped')
  })

  it('preserves the last known state on network failure and permits an explicit retry', async () => {
    const { useSoloSessions } = await import('./useSoloSessions')
    const store = useSoloSessions()
    store.upsert(session('one'))
    api.listSoloSessions.mockRejectedValue(new Error('offline'))
    api.closeSoloSession.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(session('one', 'closed'))
    await expect(store.close('one')).rejects.toThrow('offline')
    await flushPromises()
    expect(store.active.value).toHaveLength(1)
    expect(store.closing.value.size).toBe(0)
    await store.close('one')
    expect(store.active.value).toHaveLength(0)
  })
})
