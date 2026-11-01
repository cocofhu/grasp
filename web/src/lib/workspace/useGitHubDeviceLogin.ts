import { computed, getCurrentScope, onScopeDispose, ref } from 'vue'
import { api, type GitHubDeviceLoginPoll } from '@/lib/api/api'

export type GitHubDeviceLoginState = 'idle' | 'starting' | 'waiting' | 'authorized' | 'denied' | 'expired' | 'error'

export interface GitHubDeviceLoginFailure {
  /** Server or GitHub error code; `unknown` when none was given. */
  code: string
  message?: string
  alias?: string
}

export interface GitHubDeviceLoginTarget {
  name?: string
  credentialId?: string
}

/** GitHub's Octicon mark (16×16 viewBox). */
export const GITHUB_MARK = 'M8 0a8 8 0 0 0-2.53 15.59c.4.07.55-.17.55-.38v-1.33c-2.23.48-2.7-1.07-2.7-1.07-.36-.92-.89-1.17-.89-1.17-.73-.5.05-.49.05-.49.8.06 1.23.83 1.23.83.72 1.22 1.87.87 2.33.66.07-.52.28-.87.5-1.07-1.78-.2-3.65-.89-3.65-3.95 0-.87.31-1.59.83-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.22 2.2.82a7.6 7.6 0 0 1 4 0c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.28.82 2.15 0 3.07-1.87 3.75-3.66 3.95.29.25.54.73.54 1.48v2.2c0 .21.15.46.55.38A8 8 0 0 0 8 0Z'

/** GitHub asks clients to wait at least this long between token polls. */
const MIN_INTERVAL_SECONDS = 5

/**
 * Drives one GitHub device-flow sign-in: request a code, poll the server at the
 * interval it returns, and stop on success, failure, expiry, cancel or scope
 * disposal. The device code and token never reach the browser.
 */
export function useGitHubDeviceLogin(options: { onAuthorized?: (result: GitHubDeviceLoginPoll) => void } = {}) {
  const state = ref<GitHubDeviceLoginState>('idle')
  const userCode = ref('')
  const verificationUri = ref('')
  const totalSeconds = ref(0)
  const remainingSeconds = ref(0)
  const failure = ref<GitHubDeviceLoginFailure | null>(null)
  const result = ref<GitHubDeviceLoginPoll | null>(null)
  let sessionId = ''
  let deadline = 0
  let interval = MIN_INTERVAL_SECONDS
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let tickTimer: ReturnType<typeof setInterval> | undefined
  // Bumped on every start/cancel so responses from an abandoned session are ignored.
  let generation = 0

  const progress = computed(() => totalSeconds.value > 0 ? Math.max(0, Math.min(1, remainingSeconds.value / totalSeconds.value)) : 0)
  const busy = computed(() => state.value === 'starting' || state.value === 'waiting')

  function stopTimers() {
    if (pollTimer !== undefined) clearTimeout(pollTimer)
    if (tickTimer !== undefined) clearInterval(tickTimer)
    pollTimer = undefined
    tickTimer = undefined
  }

  function releaseSession() {
    const id = sessionId
    sessionId = ''
    if (id) void api.cancelGitHubDeviceLogin(id).catch(() => undefined)
  }

  function finish(next: GitHubDeviceLoginState, error: GitHubDeviceLoginFailure | null = null) {
    stopTimers()
    state.value = next
    failure.value = error
  }

  function tick() {
    remainingSeconds.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
    if (remainingSeconds.value === 0 && state.value === 'waiting') {
      generation++
      releaseSession()
      finish('expired')
    }
  }

  function schedule(gen: number, seconds: number) {
    if (pollTimer !== undefined) clearTimeout(pollTimer)
    pollTimer = setTimeout(() => { void poll(gen) }, Math.max(MIN_INTERVAL_SECONDS, seconds) * 1000)
  }

  async function poll(gen: number) {
    pollTimer = undefined
    if (gen !== generation || !sessionId) return
    let out: GitHubDeviceLoginPoll
    try {
      out = await api.pollGitHubDeviceLogin(sessionId)
    } catch (e: unknown) {
      if (gen !== generation) return
      const err = e as { status?: number; code?: string; message?: string }
      if (err.status === 404) {
        sessionId = ''
        finish('error', { code: err.code || 'session_not_found', message: err.message })
        return
      }
      // Transient network or server trouble: keep waiting until the code expires.
      schedule(gen, interval)
      return
    }
    if (gen !== generation) return
    switch (out.status) {
      case 'pending':
        if (out.interval) interval = Math.max(MIN_INTERVAL_SECONDS, out.interval)
        schedule(gen, interval)
        return
      case 'authorized':
        sessionId = ''
        result.value = out
        finish('authorized')
        options.onAuthorized?.(out)
        return
      case 'denied':
      case 'expired':
        sessionId = ''
        finish(out.status)
        return
      default:
        sessionId = ''
        finish('error', { code: out.error || 'unknown', alias: out.alias })
    }
  }

  /** Resolves to the start failure, or null once the code is shown. */
  async function start(target: GitHubDeviceLoginTarget): Promise<GitHubDeviceLoginFailure | null> {
    cancel()
    const gen = generation
    state.value = 'starting'
    try {
      const session = await api.startGitHubDeviceLogin({
        ...(target.credentialId ? { credentialId: target.credentialId } : { name: (target.name || '').trim() }),
      })
      if (gen !== generation) {
        void api.cancelGitHubDeviceLogin(session.sessionId).catch(() => undefined)
        return null
      }
      sessionId = session.sessionId
      userCode.value = session.userCode
      verificationUri.value = session.verificationUri || 'https://github.com/login/device'
      interval = Math.max(MIN_INTERVAL_SECONDS, session.interval || 0)
      totalSeconds.value = Math.max(1, session.expiresIn || 900)
      deadline = Date.now() + totalSeconds.value * 1000
      remainingSeconds.value = totalSeconds.value
      state.value = 'waiting'
      tickTimer = setInterval(tick, 1000)
      schedule(gen, interval)
      return null
    } catch (e: unknown) {
      if (gen !== generation) return null
      const err = e as { code?: string; alias?: string; message?: string }
      const fail = { code: err.code || 'unknown', alias: err.alias, message: err.message }
      finish('error', fail)
      return fail
    }
  }

  /** Stops waiting and drops the server session; safe to call at any time. */
  function cancel() {
    generation++
    stopTimers()
    releaseSession()
    state.value = 'idle'
    failure.value = null
    result.value = null
    userCode.value = ''
    remainingSeconds.value = 0
  }

  if (getCurrentScope()) onScopeDispose(cancel)

  return { state, userCode, verificationUri, totalSeconds, remainingSeconds, progress, failure, result, busy, start, cancel }
}

export type GitHubDeviceLogin = ReturnType<typeof useGitHubDeviceLogin>
