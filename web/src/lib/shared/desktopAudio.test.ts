import { describe, it, expect, vi } from 'vitest'
import { DesktopAudioPlayer, DESKTOP_AUDIO_RATE, type DesktopAudioState } from './desktopAudio'

class FakeSocket {
  binaryType = ''
  closed = false
  onopen: (() => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  constructor(public url: string) {}
  close() {
    this.closed = true
  }
  emit(data: unknown) {
    this.onmessage?.({ data } as MessageEvent)
  }
}

function fakeContext() {
  const starts: number[] = []
  const channels: Float32Array[][] = []
  const ctx = {
    currentTime: 1,
    destination: {},
    resume: vi.fn(() => Promise.resolve()),
    close: vi.fn(() => Promise.resolve()),
    createBuffer: vi.fn((n: number, frames: number, rate: number) => {
      const data = Array.from({ length: n }, () => new Float32Array(frames))
      channels.push(data)
      return { rate, getChannelData: (ch: number) => data[ch] }
    }),
    createBufferSource: vi.fn(() => {
      const src = { buffer: null as unknown, connect: vi.fn(), start: (t: number) => starts.push(t) }
      return src
    }),
  }
  return { ctx, starts, channels }
}

function pcm(frames: number, l: number, r: number): ArrayBuffer {
  const a = new Int16Array(frames * 2)
  for (let i = 0; i < frames; i++) {
    a[i * 2] = l
    a[i * 2 + 1] = r
  }
  return a.buffer
}

function setup() {
  const { ctx, starts, channels } = fakeContext()
  const sockets: FakeSocket[] = []
  const states: DesktopAudioState[] = []
  const player = new DesktopAudioPlayer('ws://x/sandbox-audio/1/ws', (s) => states.push(s), {
    createContext: () => ctx as unknown as AudioContext,
    createSocket: (url) => {
      const s = new FakeSocket(url)
      sockets.push(s)
      return s as unknown as WebSocket
    },
  })
  return { player, ctx, starts, channels, sockets, states }
}

describe('DesktopAudioPlayer', () => {
  it('decodes stereo PCM and schedules frames back to back', () => {
    const { player, ctx, starts, channels, sockets, states } = setup()
    player.start()
    expect(states).toEqual(['connecting'])
    expect(sockets[0]!.binaryType).toBe('arraybuffer')
    expect(ctx.resume).toHaveBeenCalled()
    sockets[0]!.onopen?.()
    expect(player.state).toBe('on')

    sockets[0]!.emit(pcm(960, 16384, -32768))
    sockets[0]!.emit(pcm(960, 0, 0))
    sockets[0]!.emit('text frame ignored')
    expect(ctx.createBuffer).toHaveBeenCalledWith(2, 960, DESKTOP_AUDIO_RATE)
    expect(channels[0]![0]![0]).toBe(0.5)
    expect(channels[0]![1]![0]).toBe(-1)
    expect(starts[0]).toBeCloseTo(1.08)
    expect(starts[1]).toBeCloseTo(1.1)

    player.start() // already running
    expect(sockets).toHaveLength(1)
  })

  it('resyncs after an underrun and drops audio queued too far ahead', () => {
    const { player, ctx, starts, sockets } = setup()
    player.start()
    sockets[0]!.onopen?.()
    sockets[0]!.emit(pcm(960, 1, 1))
    ctx.currentTime = 5
    sockets[0]!.emit(pcm(960, 1, 1))
    expect(starts[1]).toBeCloseTo(5.08)
    // 0.5 s in one frame pushes the queue past its cap; the next one is dropped.
    sockets[0]!.emit(pcm(24000, 1, 1))
    sockets[0]!.emit(pcm(960, 1, 1))
    expect(starts).toHaveLength(3)
    sockets[0]!.emit(new ArrayBuffer(2)) // under one frame
    expect(starts).toHaveLength(3)
  })

  it('stops cleanly and reports failures', () => {
    const { player, ctx, sockets, states } = setup()
    player.start()
    sockets[0]!.onopen?.()
    player.stop()
    expect(sockets[0]!.closed).toBe(true)
    expect(ctx.close).toHaveBeenCalled()
    expect(player.state).toBe('off')

    player.start()
    sockets[1]!.onclose?.() // closed before open: image without sound
    expect(player.state).toBe('error')

    player.start()
    sockets[2]!.onopen?.()
    sockets[2]!.onclose?.() // stream ended after playing
    expect(player.state).toBe('off')

    player.start()
    sockets[3]!.onerror?.()
    expect(player.state).toBe('error')
    expect(states).toContain('connecting')
  })

  it('reports an AudioContext that cannot be created', () => {
    const states: DesktopAudioState[] = []
    const player = new DesktopAudioPlayer('ws://x', (s) => states.push(s), {
      createContext: () => {
        throw new Error('no audio')
      },
    })
    player.start()
    expect(states).toEqual(['error'])
  })

  it('releases the AudioContext when the socket cannot be created', () => {
    const { ctx } = fakeContext()
    const player = new DesktopAudioPlayer('bad url', () => {}, {
      createContext: () => ctx as unknown as AudioContext,
      createSocket: () => {
        throw new SyntaxError('bad url')
      },
    })
    player.start()
    expect(player.state).toBe('error')
    expect(ctx.close).toHaveBeenCalled()
  })
})
