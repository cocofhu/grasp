import { api } from '@/lib/api/api'
import { refreshRunLive } from '@/lib/run/runLiveCards'

type Heartbeat = { start(): void; touch(): void; stop(): void }
type Reconnect = { markConnecting(): void; markOpened(): void; onClose(): unknown }

export type RunEventsSocketOptions<F> = {
  runId: string
  heartbeat: Heartbeat
  reconnect: Reconnect
  /** The owner still holds this socket (and, where it has one, the same visit). */
  isCurrent: (socket: WebSocket) => boolean
  /** Store the socket before its heartbeat starts. */
  adopt: (socket: WebSocket) => void
  /** This socket replaces a dropped one: re-read the Live snapshot once it opens. */
  catchUp: boolean
  onCreateFailed?: () => void
  onOpen?: () => void
  /** Parsed frame from the current socket; malformed frames are dropped. */
  onFrame: (frame: F, socket: WebSocket) => void
  /** Clear the owner's socket state; runs after the heartbeat stops and before a reconnect is scheduled. */
  onClosed?: () => void
}

/**
 * Open WS /runs/:id/events with the lifecycle every run page shares: reconnect
 * bookkeeping, heartbeat, superseded-socket guards and the Live catch-up after
 * a reconnect. Frame handling stays with the page.
 */
export function openRunEventsSocket<F>(o: RunEventsSocketOptions<F>): WebSocket | null {
  o.reconnect.markConnecting()
  let socket: WebSocket
  try {
    socket = new WebSocket(api.runEventsWsUrl(o.runId))
  } catch {
    o.onCreateFailed?.()
    o.reconnect.onClose()
    return null
  }
  o.adopt(socket)
  o.heartbeat.start()
  socket.onopen = () => {
    if (!o.isCurrent(socket)) return
    o.onOpen?.()
    o.reconnect.markOpened()
    o.heartbeat.touch()
    if (o.catchUp) void refreshRunLive(o.runId)
  }
  socket.onmessage = (ev) => {
    if (!o.isCurrent(socket)) return
    o.heartbeat.touch()
    let frame: F
    try {
      frame = JSON.parse(String(ev.data))
    } catch {
      return
    }
    o.onFrame(frame, socket)
  }
  socket.onclose = () => {
    if (!o.isCurrent(socket)) return
    o.heartbeat.stop()
    o.onClosed?.()
    o.reconnect.onClose()
  }
  return socket
}
