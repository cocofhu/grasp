import type { AcpEvent } from '../shared/types'

/** One WS ACP frame buffered while the dialogue surface is unmounted. */
type PendingAcpFrame = {
  nodeId?: string
  events: AcpEvent[]
  busy?: boolean
}

/**
 * Extract cumulative thought/message rails from an ACP event list.
 * Later same-kind events overwrite (platform pushes cumulative snapshots).
 */
export function pickAcpRails(events: readonly AcpEvent[] | undefined | null): {
  thought: string
  message: string
} {
  let thought = ''
  let message = ''
  if (!events?.length) return { thought, message }
  for (const ev of events) {
    if (ev.kind === 'thought' && ev.text) thought = ev.text
    if (ev.kind === 'message' && ev.text) message = ev.text
  }
  return { thought, message }
}

/**
 * Buffer latest cumulative ACP per nodeId while ClarifyChat/GateApproval is gone
 * (Inbox hard load / RunDetail remount). Empty event frames still update busy
 * metadata but do not wipe a prior non-empty buffer for that node.
 */
export function createPendingAcpBuffer() {
  const byNode = new Map<string, PendingAcpFrame>()
  let anonymous: PendingAcpFrame | null = null

  function keyOf(nodeId?: string): string {
    return nodeId && nodeId.length > 0 ? nodeId : ''
  }

  return {
    push(frame: PendingAcpFrame) {
      const key = keyOf(frame.nodeId)
      const prev = key ? byNode.get(key) : anonymous
      const next: PendingAcpFrame = {
        nodeId: frame.nodeId,
        events: frame.events?.length ? frame.events : prev?.events ?? [],
        busy: typeof frame.busy === 'boolean' ? frame.busy : prev?.busy,
      }
      if (key) byNode.set(key, next)
      else anonymous = next
    },
    takeAll(): PendingAcpFrame[] {
      const out: PendingAcpFrame[] = []
      if (anonymous) out.push(anonymous)
      for (const f of byNode.values()) out.push(f)
      anonymous = null
      byNode.clear()
      return out
    },
    peekAll(): PendingAcpFrame[] {
      const out: PendingAcpFrame[] = []
      if (anonymous) out.push(anonymous)
      for (const f of byNode.values()) out.push(f)
      return out
    },
    clear() {
      anonymous = null
      byNode.clear()
    },
    get size() {
      return byNode.size + (anonymous ? 1 : 0)
    },
  }
}
