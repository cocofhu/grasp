import { describe, it, expect, vi } from 'vitest'
import { PreviewVncChannel } from './previewVncChannel'

function mockWs() {
  const handlers: Record<string, ((ev: any) => void) | null> = {
    onmessage: null,
    onopen: null,
    onclose: null,
    onerror: null,
  }
  const ws = {
    binaryType: 'blob' as BinaryType,
    readyState: 1,
    protocol: '',
    send: vi.fn(),
    close: vi.fn(),
    set onmessage(fn: ((ev: any) => void) | null) {
      handlers.onmessage = fn
    },
    get onmessage() {
      return handlers.onmessage
    },
    set onopen(fn: ((ev: any) => void) | null) {
      handlers.onopen = fn
    },
    get onopen() {
      return handlers.onopen
    },
    set onclose(fn: ((ev: any) => void) | null) {
      handlers.onclose = fn
    },
    get onclose() {
      return handlers.onclose
    },
    set onerror(fn: ((ev: any) => void) | null) {
      handlers.onerror = fn
    },
    get onerror() {
      return handlers.onerror
    },
    _emit(type: string, ev: any) {
      handlers[type]?.(ev)
    },
  }
  return ws
}

describe('PreviewVncChannel', () => {
  it('exposes properties required by noVNC attach()', () => {
    const channel = new PreviewVncChannel(mockWs() as unknown as WebSocket)
    const props = [
      ...Object.keys(channel),
      ...Object.getOwnPropertyNames(Object.getPrototypeOf(channel)),
    ]
    for (const p of ['send', 'close', 'binaryType', 'onerror', 'onmessage', 'onopen', 'protocol', 'readyState']) {
      expect(props).toContain(p)
    }
  })

  it('routes text frames to ctrl handler and binary to RFB onmessage', () => {
    const ws = mockWs()
    const channel = new PreviewVncChannel(ws as unknown as WebSocket)
    const ctrl = vi.fn()
    const rfbMsg = vi.fn()
    channel.setCtrlHandler(ctrl)
    channel.onmessage = rfbMsg

    ws._emit('onmessage', { data: '{"type":"ready"}' })
    expect(ctrl).toHaveBeenCalledWith('{"type":"ready"}')
    expect(rfbMsg).not.toHaveBeenCalled()

    const bin = new ArrayBuffer(4)
    ws._emit('onmessage', { data: bin })
    expect(rfbMsg).toHaveBeenCalledTimes(1)
    expect(rfbMsg.mock.calls[0][0].data).toBe(bin)
    expect(ctrl).toHaveBeenCalledTimes(1)
  })

  it('forwards close/error to both app and RFB handlers', () => {
    const ws = mockWs()
    const channel = new PreviewVncChannel(ws as unknown as WebSocket)
    const appClose = vi.fn()
    const rfbClose = vi.fn()
    const appErr = vi.fn()
    const rfbErr = vi.fn()
    channel.setAppCloseHandler(appClose)
    channel.setAppErrorHandler(appErr)
    channel.onclose = rfbClose
    channel.onerror = rfbErr

    const closeEv = { code: 1000 }
    ws._emit('onclose', closeEv)
    expect(appClose).toHaveBeenCalledWith(closeEv)
    expect(rfbClose).toHaveBeenCalledWith(closeEv)

    const errEv = {}
    ws._emit('onerror', errEv)
    expect(appErr).toHaveBeenCalledWith(errEv)
    expect(rfbErr).toHaveBeenCalledWith(errEv)
  })

  it('proxies send/close to the underlying socket', () => {
    const ws = mockWs()
    const channel = new PreviewVncChannel(ws as unknown as WebSocket)
    channel.send('{"type":"inspect","on":true}')
    expect(ws.send).toHaveBeenCalledWith('{"type":"inspect","on":true}')
    channel.close(1000, 'bye')
    expect(ws.close).toHaveBeenCalledWith(1000, 'bye')
  })

  it('sends the chosen paste ahead of queued input and copies the reusable RFB buffer', () => {
    const ws = mockWs()
    const channel = new PreviewVncChannel(ws as unknown as WebSocket)
    channel.send('clipboard probe')
    const release = channel.holdOutgoing()
    const frame = new Uint8Array([4, 1])
    channel.send(frame)
    frame[1] = 0
    channel.send(frame)
    channel.afterOutgoing(() => channel.send('next action'))
    expect(ws.send.mock.calls).toEqual([['clipboard probe']])

    release(() => channel.send('chosen paste'))
    expect(ws.send.mock.calls).toEqual([
      ['clipboard probe'], ['chosen paste'], [new Uint8Array([4, 1])], [new Uint8Array([4, 0])], ['next action'],
    ])
  })
})
