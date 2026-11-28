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
  private disconnected = false
  private canvas = document.createElement('canvas')

  constructor(host: HTMLElement, private readonly ws: WebSocket) {
    this.canvas.tabIndex = -1
    this.canvas.addEventListener('keydown', event => {
      if (!this.viewOnly) event.preventDefault()
    })
    host.appendChild(this.canvas)
    const delayMs = Number(new URLSearchParams(window.location.search).get('connectDelay') || 0)
    const onConnect = () => {
      if (this.disconnected) return
      this.emit('connect')
      // Simulate ~12 FPS framebuffer updates for e2e FPS assertions.
      this.flipTimer = setInterval(() => this._display.flip(), 1000 / 12)
    }
    // Real noVNC only reports connect after the RFB handshake, so never before the socket is open.
    const afterOpen = () => {
      if (delayMs > 0) setTimeout(onConnect, delayMs)
      else queueMicrotask(onConnect)
    }
    if (ws.readyState === WebSocket.OPEN) afterOpen()
    else ws.onopen = afterOpen
  }

  addEventListener(type: string, fn: Listener) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set())
    this.listeners.get(type)!.add(fn)
  }

  removeEventListener(type: string, fn: Listener) {
    this.listeners.get(type)?.delete(fn)
  }

  focus() {
    this.canvas.focus()
  }

  sendKey(keysym: number, code: string, down?: boolean) {
    if (this.viewOnly || this.disconnected) return
    if (down === undefined) {
      this.sendKey(keysym, code, true)
      this.sendKey(keysym, code, false)
      return
    }
    const event = new DataView(new ArrayBuffer(8))
    event.setUint8(0, 4)
    event.setUint8(1, Number(down))
    event.setUint32(4, keysym)
    this.ws.send(event.buffer)
  }

  disconnect() {
    this.disconnected = true
    this.canvas.remove()
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
