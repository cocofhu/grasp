export type SoloChatDiagnostic = {
  viewId: string
  event: 'history_slow' | 'history_done' | 'history_failed' | 'ws_connecting' | 'ws_waiting' | 'ws_snapshot' | 'ws_closed' | 'replay_drained' | 'replay_caught_up' | 'sequence_gap' | 'frame_failed'
  connection: number
  request?: number
  elapsedMs?: number
  received: number
  processed: number
  buffered: number
  pendingHistory: number
  lastReceived: number
  acknowledged: number
  highWatermark: number
  historyReady: boolean
  busy: boolean
  serverBusy: boolean
  turns: number
  agentTurns: number
  liveParts: number
  liveTextChars: number
  queueMs?: number
  responseMs?: number
  httpStatus?: number
  closeCode?: number
}

/** Timing metadata distinguishes waiting in the browser from server latency. */
export function soloHistoryTiming(sessionId: string, started: number): { queueMs?: number; responseMs?: number } {
  try {
    const entries = performance.getEntriesByType('resource') as PerformanceResourceTiming[]
    const match = entries.slice().reverse().find(entry => entry.startTime >= started && new URL(entry.name).pathname.endsWith(`/solo/sessions/${encodeURIComponent(sessionId)}/messages`))
    if (match && match.requestStart >= match.fetchStart && match.responseStart >= match.requestStart) {
      return { queueMs: Math.round(match.requestStart - match.fetchStart), responseMs: Math.round(match.responseStart - match.requestStart) }
    }
  } catch { /* resource timing is optional */ }
  return {}
}
