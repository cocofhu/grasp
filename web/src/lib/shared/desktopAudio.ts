/**
 * Plays a sandbox desktop's sound from /sandbox-audio/:id/ws: binary frames of
 * signed 16-bit little-endian interleaved stereo PCM at 48 kHz (sandbox
 * grasp-desktop vnc-ws). Plain AudioBufferSourceNode scheduling keeps it
 * working on HTTP deployments, where AudioWorklet and WebCodecs are missing.
 */

export const DESKTOP_AUDIO_RATE = 48000
export const DESKTOP_AUDIO_CHANNELS = 2

/** Scheduling headroom after an underrun; absorbs network jitter. */
const LEAD_SECONDS = 0.08
/** Queued audio beyond this is dropped so sound never drifts behind the picture. */
const MAX_QUEUE_SECONDS = 0.4

export type DesktopAudioState = 'off' | 'connecting' | 'on' | 'error'

export interface DesktopAudioDeps {
  createContext?: () => AudioContext
  createSocket?: (url: string) => WebSocket
}

export class DesktopAudioPlayer {
  private ctx: AudioContext | null = null
  private ws: WebSocket | null = null
  private nextTime = 0
  private _state: DesktopAudioState = 'off'

  constructor(
    private readonly url: string,
    private readonly onState: (s: DesktopAudioState) => void,
    private readonly deps: DesktopAudioDeps = {},
  ) {}

  get state(): DesktopAudioState {
    return this._state
  }

  /** Call from a user gesture: browsers only let a clicked page start sound. */
  start(): void {
    if (this.ws) return
    let ctx: AudioContext
    try {
      ctx = this.deps.createContext ? this.deps.createContext() : new AudioContext()
    } catch {
      this.setState('error')
      return
    }
    this.ctx = ctx
    void ctx.resume?.()?.catch(() => {})
    this.nextTime = 0
    let ws: WebSocket
    try {
      ws = this.deps.createSocket ? this.deps.createSocket(this.url) : new WebSocket(this.url)
    } catch {
      this.teardown('error')
      return
    }
    ws.binaryType = 'arraybuffer'
    this.ws = ws
    this.setState('connecting')
    ws.onopen = () => {
      if (this.ws === ws) this.setState('on')
    }
    ws.onmessage = (ev: MessageEvent) => {
      if (this.ws === ws && ev.data instanceof ArrayBuffer) this.play(ev.data)
    }
    ws.onerror = () => {
      if (this.ws === ws) this.teardown('error')
    }
    ws.onclose = () => {
      if (this.ws === ws) this.teardown(this._state === 'on' ? 'off' : 'error')
    }
  }

  stop(): void {
    this.teardown('off')
  }

  private play(data: ArrayBuffer): void {
    const ctx = this.ctx
    if (!ctx) return
    const pcm = new Int16Array(data, 0, Math.floor(data.byteLength / 2))
    const frames = Math.floor(pcm.length / DESKTOP_AUDIO_CHANNELS)
    if (frames === 0) return
    const now = ctx.currentTime
    if (this.nextTime < now) this.nextTime = now + LEAD_SECONDS
    if (this.nextTime - now > MAX_QUEUE_SECONDS + LEAD_SECONDS) return
    const buf = ctx.createBuffer(DESKTOP_AUDIO_CHANNELS, frames, DESKTOP_AUDIO_RATE)
    for (let ch = 0; ch < DESKTOP_AUDIO_CHANNELS; ch++) {
      const out = buf.getChannelData(ch)
      for (let i = 0; i < frames; i++) out[i] = (pcm[i * DESKTOP_AUDIO_CHANNELS + ch] ?? 0) / 32768
    }
    const src = ctx.createBufferSource()
    src.buffer = buf
    src.connect(ctx.destination)
    src.start(this.nextTime)
    this.nextTime += frames / DESKTOP_AUDIO_RATE
  }

  private teardown(next: DesktopAudioState): void {
    const ws = this.ws
    this.ws = null
    if (ws) {
      ws.onopen = ws.onmessage = ws.onerror = ws.onclose = null
      try {
        ws.close()
      } catch {
        /* already closed */
      }
    }
    const ctx = this.ctx
    this.ctx = null
    void ctx?.close?.()?.catch(() => {})
    this.setState(next)
  }

  private setState(s: DesktopAudioState): void {
    if (this._state === s) return
    this._state = s
    this.onState(s)
  }
}
