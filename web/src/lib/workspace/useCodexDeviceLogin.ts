import { computed, getCurrentScope, onScopeDispose, ref } from 'vue'
import { api, type CodexDeviceLoginPoll } from '@/lib/api/api'

export type CodexDeviceLoginState = 'idle' | 'starting' | 'waiting' | 'success' | 'denied' | 'expired' | 'not_enabled' | 'error'

interface CodexDeviceLoginFailure {
  /** Server or OpenAI error code; `unknown` when none was given. */
  code: string
  message?: string
  alias?: string
}

interface CodexDeviceLoginTarget {
  name?: string
  credentialId?: string
}

export const CODEX_DEVICE_URL = 'https://auth.openai.com/codex/device'

/** The server asks OpenAI at most this often; polling faster only returns pending. */
const MIN_INTERVAL_SECONDS = 3

/**
 * Drives one Codex ChatGPT device-code sign-in: request a code, poll the server
 * at the interval it returns, and stop on success, failure, expiry, cancel or
 * scope disposal. The device auth ID and tokens never reach the browser.
 */
export function useCodexDeviceLogin(options: { onSuccess?: (result: CodexDeviceLoginPoll) => void } = {}) {
  const state = ref<CodexDeviceLoginState>('idle')
  const userCode = ref('')
  const verificationUri = ref('')
  const totalSeconds = ref(0)
  const remainingSeconds = ref(0)
  const failure = ref<CodexDeviceLoginFailure | null>(null)
  const result = ref<CodexDeviceLoginPoll | null>(null)
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
    if (id) void api.cancelCodexDeviceLogin(id).catch(() => undefined)
  }

  function finish(next: CodexDeviceLoginState, error: CodexDeviceLoginFailure | null = null) {
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
    let out: CodexDeviceLoginPoll
    try {
      out = await api.pollCodexDeviceLogin(sessionId)
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
      case 'success':
        sessionId = ''
        result.value = out
        finish('success')
        options.onSuccess?.(out)
        return
      case 'denied':
      case 'expired':
      case 'not_enabled':
        sessionId = ''
        finish(out.status)
        return
      default:
        sessionId = ''
        finish('error', { code: out.error || 'unknown', alias: out.alias })
    }
  }

  /** Resolves to the start failure, or null once the code is shown. */
  async function start(target: CodexDeviceLoginTarget): Promise<CodexDeviceLoginFailure | null> {
    cancel()
    const gen = generation
    state.value = 'starting'
    try {
      const session = await api.startCodexDeviceLogin({
        ...(target.credentialId ? { credentialId: target.credentialId } : { name: (target.name || '').trim() }),
      })
      if (gen !== generation) {
        void api.cancelCodexDeviceLogin(session.sessionId).catch(() => undefined)
        return null
      }
      sessionId = session.sessionId
      userCode.value = session.userCode
      verificationUri.value = session.verificationUri || CODEX_DEVICE_URL
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
      finish(fail.code === 'not_enabled' ? 'not_enabled' : 'error', fail)
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
