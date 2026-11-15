import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const refreshRunLive = vi.hoisted(() => vi.fn(async () => {}))
vi.mock('@/lib/api/api', () => ({ api: { runEventsWsUrl: (id: string) => `ws://test/api/runs/${id}/events` } }))
vi.mock('@/lib/run/runLiveCards', () => ({ refreshRunLive }))

import { openRunEventsSocket, type RunEventsSocketOptions } from './runEventsSocket'

class FakeSocket {
  static last: FakeSocket | null = null
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  constructor(public url: string) {
    FakeSocket.last = this
  }
}

function harness(over: Partial<RunEventsSocketOptions<{ type?: string }>> = {}) {
  const calls: string[] = []
  let held: WebSocket | null = null
  const frames: unknown[] = []
  const opts: RunEventsSocketOptions<{ type?: string }> = {
    runId: 'r1',
    heartbeat: {
      start: () => calls.push('hb.start'),
      touch: () => calls.push('hb.touch'),
      stop: () => calls.push('hb.stop'),
    },
    reconnect: {
      markConnecting: () => calls.push('rc.connecting'),
      markOpened: () => calls.push('rc.opened'),
      onClose: () => calls.push('rc.close'),
    },
    isCurrent: (s) => held === s,
    adopt: (s) => {
      calls.push('adopt')
      held = s
    },
    catchUp: false,
    onOpen: () => calls.push('open'),
    onFrame: (f, s) => {
      expect(s).toBe(held)
      frames.push(f)
    },
    onClosed: () => {
      calls.push('closed')
      held = null
    },
    ...over,
  }
  const socket = openRunEventsSocket(opts) as unknown as FakeSocket | null
  return { calls, frames, socket, drop: () => (held = null) }
}

describe('openRunEventsSocket', () => {
  beforeEach(() => {
    refreshRunLive.mockClear()
    vi.stubGlobal('WebSocket', FakeSocket)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('adopts the socket before its heartbeat starts and routes the lifecycle', () => {
    const { calls, frames, socket } = harness()
    expect(socket?.url).toBe('ws://test/api/runs/r1/events')
    expect(calls).toEqual(['rc.connecting', 'adopt', 'hb.start'])
    socket!.onopen!()
    socket!.onmessage!({ data: '{"type":"live"}' })
    socket!.onmessage!({ data: 'not json' })
    socket!.onclose!()
    expect(frames).toEqual([{ type: 'live' }])
    expect(calls.slice(3)).toEqual(['open', 'rc.opened', 'hb.touch', 'hb.touch', 'hb.touch', 'hb.stop', 'closed', 'rc.close'])
    expect(refreshRunLive).not.toHaveBeenCalled()
  })

  it('catches Live up only for a replacement socket', () => {
    const { socket } = harness({ catchUp: true })
    socket!.onopen!()
    expect(refreshRunLive).toHaveBeenCalledWith('r1')
  })

  it('ignores every event from a superseded socket', () => {
    const { calls, frames, socket, drop } = harness({ catchUp: true })
    drop()
    socket!.onopen!()
    socket!.onmessage!({ data: '{"type":"live"}' })
    socket!.onclose!()
    expect(calls).toEqual(['rc.connecting', 'adopt', 'hb.start'])
    expect(frames).toEqual([])
    expect(refreshRunLive).not.toHaveBeenCalled()
  })

  it('schedules a retry when the socket cannot be created', () => {
    vi.stubGlobal('WebSocket', class {
      constructor() {
        throw new Error('blocked')
      }
    })
    const failed = vi.fn()
    const { calls, socket } = harness({ onCreateFailed: failed })
    expect(socket).toBeNull()
    expect(failed).toHaveBeenCalledOnce()
    expect(calls).toEqual(['rc.connecting', 'rc.close'])
  })
})
