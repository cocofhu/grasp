import { effectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CODEX_DEVICE_URL, useCodexDeviceLogin } from './useCodexDeviceLogin'

const mocks = vi.hoisted(() => ({ start: vi.fn(), poll: vi.fn(), cancel: vi.fn() }))
vi.mock('@/lib/api/api', () => ({
  api: {
    startCodexDeviceLogin: mocks.start,
    pollCodexDeviceLogin: mocks.poll,
    cancelCodexDeviceLogin: mocks.cancel,
  },
}))

const session = { sessionId: 'cdx-1', userCode: 'ABCD-1234', verificationUri: 'https://auth.openai.com/codex/device', expiresIn: 900, interval: 5 }

function setup(onSuccess = vi.fn()) {
  const scope = effectScope()
  const login = scope.run(() => useCodexDeviceLogin({ onSuccess }))!
  return { scope, login, onSuccess }
}

describe('useCodexDeviceLogin', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    mocks.start.mockResolvedValue(session)
    mocks.poll.mockResolvedValue({ status: 'pending', interval: 5 })
    mocks.cancel.mockResolvedValue({ status: 'cancelled' })
  })
  afterEach(() => { vi.useRealTimers() })

  it('shows the code and polls at the server interval until success', async () => {
    const { login, onSuccess, scope } = setup()
    expect(await login.start({ name: ' Mine ' })).toBeNull()
    expect(mocks.start).toHaveBeenCalledWith({ name: 'Mine' })
    expect(login.state.value).toBe('waiting')
    expect(login.busy.value).toBe(true)
    expect(login.userCode.value).toBe('ABCD-1234')
    expect(login.progress.value).toBe(1)

    await vi.advanceTimersByTimeAsync(4999)
    expect(mocks.poll).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.poll).toHaveBeenCalledWith('cdx-1')

    mocks.poll.mockResolvedValueOnce({ status: 'pending', interval: 10 })
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.poll).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(9000)
    expect(mocks.poll).toHaveBeenCalledTimes(2)

    const credential = { id: 'cred-1', type: 'ai', name: 'Mine', configured: true }
    mocks.poll.mockResolvedValueOnce({ status: 'success', credential, email: 'a@example.com' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(login.state.value).toBe('success')
    expect(login.result.value?.credential).toEqual(credential)
    expect(onSuccess).toHaveBeenCalledWith(expect.objectContaining({ credential }))
    expect(login.progress.value).toBeLessThan(1)

    await vi.advanceTimersByTimeAsync(60_000)
    expect(mocks.poll).toHaveBeenCalledTimes(3)
    expect(mocks.cancel).not.toHaveBeenCalled()
    scope.stop()
  })

  it('never polls faster than the floor and falls back to the default page', async () => {
    mocks.start.mockResolvedValue({ ...session, verificationUri: '', interval: 1, expiresIn: 0 })
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    expect(login.verificationUri.value).toBe(CODEX_DEVICE_URL)
    expect(login.totalSeconds.value).toBe(900)
    await vi.advanceTimersByTimeAsync(2999)
    expect(mocks.poll).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.poll).toHaveBeenCalledTimes(1)
    scope.stop()
  })

  it('re-logs into an existing credential by id', async () => {
    const { login, scope } = setup()
    await login.start({ credentialId: 'cred-9', name: 'ignored' })
    expect(mocks.start).toHaveBeenCalledWith({ credentialId: 'cred-9' })
    scope.stop()
  })

  it('cancel stops polling and drops the server session', async () => {
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    login.cancel()
    expect(login.state.value).toBe('idle')
    expect(mocks.cancel).toHaveBeenCalledWith('cdx-1')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.poll).not.toHaveBeenCalled()
    scope.stop()
  })

  it('disposing the scope cancels the session', async () => {
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    scope.stop()
    expect(mocks.cancel).toHaveBeenCalledWith('cdx-1')
  })

  it('cancels a session that arrives after the user already cancelled', async () => {
    let resolve!: (value: unknown) => void
    mocks.start.mockImplementationOnce(() => new Promise((r) => { resolve = r }))
    const { login, scope } = setup()
    const pending = login.start({ name: 'a' })
    expect(login.state.value).toBe('starting')
    login.cancel()
    resolve(session)
    expect(await pending).toBeNull()
    expect(login.state.value).toBe('idle')
    expect(mocks.cancel).toHaveBeenCalledWith('cdx-1')
    scope.stop()
  })

  it('ignores a poll answer that arrives after cancel', async () => {
    const { login, onSuccess, scope } = setup()
    let resolve!: (value: unknown) => void
    mocks.poll.mockImplementationOnce(() => new Promise((r) => { resolve = r }))
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(5000)
    login.cancel()
    resolve({ status: 'success', credential: { id: 'x' } })
    await vi.advanceTimersByTimeAsync(0)
    expect(login.state.value).toBe('idle')
    expect(onSuccess).not.toHaveBeenCalled()
    scope.stop()
  })

  it('ignores a poll failure that arrives after cancel', async () => {
    const { login, scope } = setup()
    let reject!: (e: unknown) => void
    mocks.poll.mockImplementationOnce(() => new Promise((_, r) => { reject = r }))
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(5000)
    login.cancel()
    reject(Object.assign(new Error('gone'), { status: 404 }))
    await vi.advanceTimersByTimeAsync(0)
    expect(login.state.value).toBe('idle')
    scope.stop()
  })

  it('expires locally when the countdown ends', async () => {
    mocks.start.mockResolvedValue({ ...session, expiresIn: 12 })
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(12_000)
    expect(login.state.value).toBe('expired')
    expect(mocks.cancel).toHaveBeenCalledWith('cdx-1')
    const polls = mocks.poll.mock.calls.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.poll.mock.calls.length).toBe(polls)
    scope.stop()
  })

  it.each([
    [{ status: 'denied' }, 'denied', null],
    [{ status: 'expired' }, 'expired', null],
    [{ status: 'not_enabled' }, 'not_enabled', null],
    [{ status: 'error', error: 'alias_taken', alias: 'Mine' }, 'error', { code: 'alias_taken', alias: 'Mine' }],
    [{ status: 'error' }, 'error', { code: 'unknown', alias: undefined }],
  ])('stops on a terminal poll result %j', async (reply, state, failure) => {
    mocks.poll.mockResolvedValueOnce(reply)
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(5000)
    expect(login.state.value).toBe(state)
    expect(login.failure.value).toEqual(failure)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.poll).toHaveBeenCalledTimes(1)
    expect(mocks.cancel).not.toHaveBeenCalled()
    scope.stop()
  })

  it('keeps waiting through transient poll failures and stops on a lost session', async () => {
    mocks.poll.mockRejectedValueOnce(Object.assign(new Error('offline'), { status: 502 }))
    mocks.poll.mockRejectedValueOnce(Object.assign(new Error('gone'), { status: 404 }))
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(5000)
    expect(login.state.value).toBe('waiting')
    await vi.advanceTimersByTimeAsync(5000)
    expect(login.state.value).toBe('error')
    expect(login.failure.value?.code).toBe('session_not_found')
    scope.stop()
  })

  it('reports start failures, including a disabled device flow, and can retry', async () => {
    mocks.start.mockRejectedValueOnce(Object.assign(new Error('not enabled'), { status: 409, code: 'not_enabled' }))
    mocks.start.mockRejectedValueOnce(new Error('boom'))
    const { login, scope } = setup()
    expect(await login.start({ name: 'a' })).toEqual(expect.objectContaining({ code: 'not_enabled' }))
    expect(login.state.value).toBe('not_enabled')
    expect(await login.start({})).toEqual(expect.objectContaining({ code: 'unknown', message: 'boom' }))
    expect(mocks.start).toHaveBeenLastCalledWith({ name: '' })
    expect(login.state.value).toBe('error')
    expect(await login.start({ name: 'a' })).toBeNull()
    expect(login.state.value).toBe('waiting')
    scope.stop()
  })

  it('works outside an effect scope', async () => {
    const login = useCodexDeviceLogin()
    mocks.poll.mockResolvedValueOnce({ status: 'success', credential: { id: 'c' } })
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(5000)
    expect(login.state.value).toBe('success')
  })
})
