/** Detect a stalled workflow socket even when the browser never emits close. */
export function createWsHeartbeat(opts: { onTimeout: () => void; timeoutMs?: number }) {
  const timeoutMs = opts.timeoutMs ?? 60_000
  let timer: ReturnType<typeof setTimeout> | undefined
  let lastActivity = 0
  let active = false

  function stop() {
    active = false
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    window.removeEventListener('online', expire)
    document.removeEventListener('visibilitychange', check)
  }

  function expire() {
    if (!active) return
    stop()
    opts.onTimeout()
  }

  function check() {
    if (!active) return
    if (Date.now() - lastActivity >= timeoutMs) expire()
  }

  function touch() {
    if (!active) return
    lastActivity = Date.now()
    if (timer !== undefined) clearTimeout(timer)
    timer = setTimeout(check, timeoutMs)
  }

  return {
    start() {
      stop()
      active = true
      window.addEventListener('online', expire)
      document.addEventListener('visibilitychange', check)
      touch()
    },
    touch,
    stop,
  }
}
