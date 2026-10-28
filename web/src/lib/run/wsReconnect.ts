/**
 * Lightweight WS auto-reconnect scheduler for Inbox / Run dialogue sockets.
 * Exponential backoff with jitter; intentional close skips reconnect.
 */

export type WsReconnectOptions = {
  /** Build a new WebSocket for the current target. */
  connect: () => void
  /** Still want a live socket for this target? */
  shouldReconnect: () => boolean
  /** Base delay ms (default 800). */
  baseDelayMs?: number
  /** Max delay ms (default 10000). */
  maxDelayMs?: number
  schedule?: (fn: () => void, ms: number) => ReturnType<typeof setTimeout>
  clearSchedule?: (id: ReturnType<typeof setTimeout>) => void
}

export function createWsReconnectController(opts: WsReconnectOptions) {
  const schedule = opts.schedule ?? ((fn, ms) => setTimeout(fn, ms))
  const clearSchedule = opts.clearSchedule ?? ((id) => clearTimeout(id))
  const base = opts.baseDelayMs ?? 800
  const max = opts.maxDelayMs ?? 10_000

  let attempt = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let intentionalClose = false

  function clearTimer() {
    if (timer !== undefined) {
      clearSchedule(timer)
      timer = undefined
    }
  }

  return {
    /** A replacement must retry even when its handshake fails before open. */
    markConnecting() {
      intentionalClose = false
      clearTimer()
    },
    /** Call before closing the socket on purpose (navigate away / hard load). */
    markIntentionalClose() {
      intentionalClose = true
      clearTimer()
    },
    /** Call when opening a new socket (resets backoff). */
    markOpened() {
      intentionalClose = false
      attempt = 0
      clearTimer()
    },
    /** Call from onclose — schedules reconnect unless intentional. */
    onClose() {
      if (intentionalClose) {
        intentionalClose = false
        return false
      }
      if (!opts.shouldReconnect()) return false
      clearTimer()
      const exp = Math.min(max, base * Math.pow(2, attempt))
      const jitter = Math.floor(Math.random() * Math.min(200, exp * 0.2))
      const delay = exp + jitter
      attempt += 1
      timer = schedule(() => {
        timer = undefined
        if (!opts.shouldReconnect()) return
        opts.connect()
      }, delay)
      return true
    },
    reset() {
      clearTimer()
      attempt = 0
      intentionalClose = false
    },
    get attempt() {
      return attempt
    },
  }
}

export type WsReconnectController = ReturnType<typeof createWsReconnectController>
