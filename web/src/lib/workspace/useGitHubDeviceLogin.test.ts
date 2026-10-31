import { effectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useGitHubDeviceLogin } from './useGitHubDeviceLogin'

const mocks = vi.hoisted(() => ({ start: vi.fn(), poll: vi.fn(), cancel: vi.fn() }))
vi.mock('@/lib/api/api', () => ({
  api: {
    startGitHubDeviceLogin: mocks.start,
    pollGitHubDeviceLogin: mocks.poll,
    cancelGitHubDeviceLogin: mocks.cancel,
  },
}))

const session = { sessionId: 'ghd-1', userCode: 'WDJB-MJHT', verificationUri: 'https://github.com/login/device', expiresIn: 900, interval: 5 }

function setup(onAuthorized = vi.fn()) {
  const scope = effectScope()
  const login = scope.run(() => useGitHubDeviceLogin({ onAuthorized }))!
  return { scope, login, onAuthorized }
}

describe('useGitHubDeviceLogin', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    mocks.start.mockResolvedValue(session)
    mocks.poll.mockResolvedValue({ status: 'pending', interval: 5 })
    mocks.cancel.mockResolvedValue({ status: 'cancelled' })
  })
  afterEach(() => { vi.useRealTimers() })

  it('shows the code and polls no faster than the server interval until authorized', async () => {
    const { login, onAuthorized, scope } = setup()
    expect(await login.start({ name: ' Mine ' })).toBeNull()
    expect(mocks.start).toHaveBeenCalledWith({ name: 'Mine' })
    expect(login.state.value).toBe('waiting')
    expect(login.userCode.value).toBe('WDJB-MJHT')
    expect(login.remainingSeconds.value).toBe(900)

    await vi.advanceTimersByTimeAsync(4999)
    expect(mocks.poll).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(mocks.poll).toHaveBeenCalledTimes(1)
    expect(mocks.poll).toHaveBeenCalledWith('ghd-1')

    // slow_down widened the interval on the server.
    mocks.poll.mockResolvedValueOnce({ status: 'pending', interval: 10 })
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.poll).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(9000)
    expect(mocks.poll).toHaveBeenCalledTimes(2)

    const credential = { id: 'cred-1', type: 'git', name: 'Mine', configured: true }
    mocks.poll.mockResolvedValueOnce({ status: 'authorized', credential, githubLogin: 'octocat', scopes: ['repo'] })
    await vi.advanceTimersByTimeAsync(1000)
    expect(login.state.value).toBe('authorized')
    expect(login.result.value?.githubLogin).toBe('octocat')
    expect(onAuthorized).toHaveBeenCalledWith(expect.objectContaining({ credential }))
    expect(login.remainingSeconds.value).toBeLessThan(900)

    await vi.advanceTimersByTimeAsync(60_000)
    expect(mocks.poll).toHaveBeenCalledTimes(3)
    expect(mocks.cancel).not.toHaveBeenCalled()
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
    expect(mocks.cancel).toHaveBeenCalledWith('ghd-1')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.poll).not.toHaveBeenCalled()
    scope.stop()
  })

  it('disposing the scope (unmount) cancels the session', async () => {
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    scope.stop()
    expect(mocks.cancel).toHaveBeenCalledWith('ghd-1')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.poll).not.toHaveBeenCalled()
  })

  it('ignores a poll answer that arrives after cancel', async () => {
    const { login, onAuthorized, scope } = setup()
    let resolve!: (value: unknown) => void
    mocks.poll.mockImplementationOnce(() => new Promise((r) => { resolve = r }))
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(5000)
    login.cancel()
    resolve({ status: 'authorized', credential: { id: 'x' } })
    await vi.advanceTimersByTimeAsync(0)
    expect(login.state.value).toBe('idle')
    expect(onAuthorized).not.toHaveBeenCalled()
    scope.stop()
  })

  it('expires locally when the countdown ends', async () => {
    mocks.start.mockResolvedValue({ ...session, expiresIn: 12 })
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(12_000)
    expect(login.state.value).toBe('expired')
    expect(mocks.cancel).toHaveBeenCalledWith('ghd-1')
    const polls = mocks.poll.mock.calls.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.poll.mock.calls.length).toBe(polls)
    scope.stop()
  })

  it.each([
    [{ status: 'denied' }, 'denied', null],
    [{ status: 'expired' }, 'expired', null],
    [{ status: 'error', error: 'alias_taken', alias: 'Mine' }, 'error', { code: 'alias_taken', alias: 'Mine' }],
  ])('stops on a terminal poll result %j', async (reply, state, failure) => {
    mocks.poll.mockResolvedValueOnce(reply)
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(5000)
    expect(login.state.value).toBe(state)
    expect(login.failure.value).toEqual(failure)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.poll).toHaveBeenCalledTimes(1)
    scope.stop()
  })

  it('keeps waiting through transient poll failures and stops on a lost session', async () => {
    mocks.poll.mockRejectedValueOnce(Object.assign(new Error('offline'), { status: 502 }))
    mocks.poll.mockRejectedValueOnce(Object.assign(new Error('gone'), { status: 404, code: 'session_not_found' }))
    const { login, scope } = setup()
    await login.start({ name: 'a' })
    await vi.advanceTimersByTimeAsync(5000)
    expect(login.state.value).toBe('waiting')
    await vi.advanceTimersByTimeAsync(5000)
    expect(login.state.value).toBe('error')
    expect(login.failure.value?.code).toBe('session_not_found')
    scope.stop()
  })

  it('reports a start failure and can retry', async () => {
    mocks.start.mockRejectedValueOnce(Object.assign(new Error('GitHub: device_flow_disabled'), { status: 502, code: 'device_flow_disabled' }))
    const { login, scope } = setup()
    expect(await login.start({ name: 'a' })).toEqual(expect.objectContaining({ code: 'device_flow_disabled' }))
    expect(login.state.value).toBe('error')
    expect(await login.start({ name: 'a' })).toBeNull()
    expect(login.state.value).toBe('waiting')
    scope.stop()
  })
})
