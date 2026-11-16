import { computed, getCurrentScope, onScopeDispose, ref, type Ref } from 'vue'

export interface DeviceLoginFailure {
  /** Server or upstream error code; `unknown` when none was given. */
  code: string
  message?: string
  alias?: string
}

export interface DeviceLoginTarget {
  name?: string
  credentialId?: string
}

interface DeviceLoginSession {
  sessionId: string
  userCode: string
  verificationUri: string
  interval?: number
  expiresIn?: number
}

interface DeviceLoginPoll {
  status: string
  interval?: number
  error?: string
  alias?: string
}

type BaseState = 'idle' | 'starting' | 'waiting' | 'expired' | 'error'

interface DeviceLoginProvider<P extends DeviceLoginPoll, S extends string> {
  start: (body: DeviceLoginTarget) => Promise<DeviceLoginSession>
  poll: (sessionId: string) => Promise<P>
  cancel: (sessionId: string) => Promise<unknown>
  /** Polls never run more often than this, whatever the server returns. */
  minIntervalSeconds: number
  defaultVerificationUri: string
  successStatus: P['status'] & S
  /** Poll statuses besides success that end the flow in the state of the same name. */
  terminalStatuses: readonly (P['status'] & S)[]
  /** State a failed start lands in; defaults to `error`. */
  startFailureState?: (failure: DeviceLoginFailure) => S | 'error'
  onSuccess?: (result: P) => void
}

/**
 * Drives one device-code sign-in: request a code, poll the server at the
 * interval it returns, and stop on success, failure, expiry, cancel or scope
 * disposal. The device secret and tokens never reach the browser.
 */
export function useDeviceLogin<P extends DeviceLoginPoll, S extends string>(provider: DeviceLoginProvider<P, S>) {
  const minInterval = provider.minIntervalSeconds
  const state = ref('idle') as Ref<S | BaseState>
  const userCode = ref('')
  const verificationUri = ref('')
  const totalSeconds = ref(0)
  const remainingSeconds = ref(0)
  const failure = ref<DeviceLoginFailure | null>(null)
  const result = ref(null) as Ref<P | null>
  let sessionId = ''
  let deadline = 0
  let interval = minInterval
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
    if (id) void provider.cancel(id).catch(() => undefined)
  }

  function finish(next: S | BaseState, error: DeviceLoginFailure | null = null) {
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
    pollTimer = setTimeout(() => { void poll(gen) }, Math.max(minInterval, seconds) * 1000)
  }

  async function poll(gen: number) {
    pollTimer = undefined
    if (gen !== generation || !sessionId) return
    let out: P
    try {
      out = await provider.poll(sessionId)
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
    if (out.status === 'pending') {
      if (out.interval) interval = Math.max(minInterval, out.interval)
      schedule(gen, interval)
      return
    }
    sessionId = ''
    if (out.status === provider.successStatus) {
      result.value = out
      finish(provider.successStatus)
      provider.onSuccess?.(out)
      return
    }
    const terminal = provider.terminalStatuses.find((s) => s === out.status)
    if (terminal) finish(terminal)
    else finish('error', { code: out.error || 'unknown', alias: out.alias })
  }

  /** Resolves to the start failure, or null once the code is shown. */
  async function start(target: DeviceLoginTarget): Promise<DeviceLoginFailure | null> {
    cancel()
    const gen = generation
    state.value = 'starting'
    try {
      const session = await provider.start(
        target.credentialId ? { credentialId: target.credentialId } : { name: (target.name || '').trim() },
      )
      if (gen !== generation) {
        void provider.cancel(session.sessionId).catch(() => undefined)
        return null
      }
      sessionId = session.sessionId
      userCode.value = session.userCode
      verificationUri.value = session.verificationUri || provider.defaultVerificationUri
      interval = Math.max(minInterval, session.interval || 0)
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
      finish(provider.startFailureState?.(fail) ?? 'error', fail)
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
