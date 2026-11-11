/**
 * Demux wrapper so noVNC RFB and Vue control JSON can share one preview-vnc WebSocket.
 *
 * Server sends text JSON (ready/picked/error/closed) and binary RFB on the same socket.
 * noVNC attach() replaces onmessage and treats every frame as binary — without demux,
 * control JSON corrupts the RFB stream (black preview).
 *
 * Must expose the properties noVNC checks: send, close, binaryType, onerror, onmessage,
 * onopen, protocol, readyState.
 */

type CtrlTextHandler = (data: string) => void

type MsgHandler = ((ev: MessageEvent) => void) | null
type EvHandler = ((ev: Event) => void) | null
type CloseHandler = ((ev: CloseEvent) => void) | null

export class PreviewVncChannel {
  binaryType: BinaryType = 'arraybuffer'

  private readonly ws: WebSocket
  private ctrlHandler: CtrlTextHandler | null = null
  private appOnError: EvHandler = null
  private appOnClose: CloseHandler = null
  private rfbOnMessage: MsgHandler = null
  private rfbOnOpen: EvHandler = null
  private rfbOnClose: CloseHandler = null
  private rfbOnError: EvHandler = null

  constructor(ws: WebSocket) {
    this.ws = ws
    ws.binaryType = 'arraybuffer'
    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        this.ctrlHandler?.(ev.data)
        return
      }
      this.rfbOnMessage?.(ev)
    }
    ws.onopen = (ev) => {
      this.rfbOnOpen?.(ev)
    }
    ws.onclose = (ev) => {
      this.appOnClose?.(ev)
      this.rfbOnClose?.(ev)
    }
    ws.onerror = (ev) => {
      this.appOnError?.(ev)
      this.rfbOnError?.(ev)
    }
  }

  get readyState(): number {
    return this.ws.readyState
  }

  get protocol(): string {
    return this.ws.protocol
  }

  setCtrlHandler(handler: CtrlTextHandler | null) {
    this.ctrlHandler = handler
  }

  setAppErrorHandler(handler: EvHandler) {
    this.appOnError = handler
  }

  setAppCloseHandler(handler: CloseHandler) {
    this.appOnClose = handler
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView) {
    this.ws.send(data)
  }

  close(code?: number, reason?: string) {
    this.ws.close(code, reason)
  }

  get onmessage(): MsgHandler {
    return this.rfbOnMessage
  }
  set onmessage(fn: MsgHandler) {
    this.rfbOnMessage = fn
  }

  get onopen(): EvHandler {
    return this.rfbOnOpen
  }
  set onopen(fn: EvHandler) {
    this.rfbOnOpen = fn
  }

  get onclose(): CloseHandler {
    return this.rfbOnClose
  }
  set onclose(fn: CloseHandler) {
    this.rfbOnClose = fn
  }

  get onerror(): EvHandler {
    return this.rfbOnError
  }
  set onerror(fn: EvHandler) {
    this.rfbOnError = fn
  }

  /** Detach app handlers so teardown does not race RFB disconnect. */
  clearAppHandlers() {
    this.ctrlHandler = null
    this.appOnError = null
    this.appOnClose = null
  }
}
