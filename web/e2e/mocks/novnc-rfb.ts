/** Test double for @novnc/novnc — avoids RFB handshake against the mock sandbox-vnc socket. */
type Listener = (ev?: { detail?: { status?: number; reason?: string } }) => void

export default class MockRFB {
  scaleViewport = false
  resizeSession = false
  viewOnly = false
  focusOnClick = true
  showDotCursor = false
  background = ''

  _display = {
    flip: () => {
      /* no-op; e2e may call manually or via interval */
    },
  }

  private listeners = new Map<string, Set<Listener>>()
  private flipTimer: ReturnType<typeof setInterval> | null = null

  constructor(_host: HTMLElement, _ws: WebSocket) {
    const delayMs = Number(new URLSearchParams(window.location.search).get('connectDelay') || 0)
    const onConnect = () => {
      this.emit('connect')
      // Simulate ~12 FPS framebuffer updates for e2e FPS assertions.
      this.flipTimer = setInterval(() => this._display.flip(), 1000 / 12)
    }
    if (delayMs > 0) {
      setTimeout(onConnect, delayMs)
    } else {
      queueMicrotask(onConnect)
    }
  }

  addEventListener(type: string, fn: Listener) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set())
    this.listeners.get(type)!.add(fn)
  }

  removeEventListener(type: string, fn: Listener) {
    this.listeners.get(type)?.delete(fn)
  }

  disconnect() {
    if (this.flipTimer) {
      clearInterval(this.flipTimer)
      this.flipTimer = null
    }
    this.emit('disconnect', { detail: { clean: true } })
  }

  private emit(type: string, ev?: { detail?: { clean?: boolean } }) {
    for (const fn of this.listeners.get(type) ?? []) fn(ev)
  }
}
