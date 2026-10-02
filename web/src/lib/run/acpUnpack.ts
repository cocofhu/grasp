/**
 * Shared ACP frame unpack helpers aligned with AgentChatTester.
 * Peels {op:"event", data:{type:"session_update", update:...}} and extracts
 * agent_message_chunk text across string / array / parts content shapes.
 */

/** Peel a persisted event frame ({op:"event",data:{…}}) down to the bare event. */
export function unwrapFrame(f: unknown): any {
  const frame = f as { op?: string; data?: unknown } | null
  return frame && typeof frame === 'object' && frame.op === 'event' && frame.data ? frame.data : f
}

export function flattenUpdate(u: any): any {
  if (!u || typeof u !== 'object') return u
  const out: any = { ...u }
  const su = out.sessionUpdate ?? out.session_update
  if (su && typeof su === 'object') {
    for (const k of Object.keys(su)) if (!(k in out)) out[k] = su[k]
    delete out.sessionUpdate
    delete out.session_update
  }
  return out
}

export function normalizeKind(s: unknown): string {
  return String(s || '')
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .replace(/-/g, '_')
    .toLowerCase()
}

/** Extract visible text from ACP content (string | array | {text} | {parts}). */
export function contentText(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'string') return v
  if (Array.isArray(v)) return v.map(contentText).join('')
  if (typeof v === 'object') {
    const o = v as { text?: unknown; parts?: unknown }
    if (typeof o.text === 'string') return o.text
    if (Array.isArray(o.parts)) return o.parts.map(contentText).join('')
  }
  return ''
}

export type AcpMessageDelta = {
  kind: string
  text: string
}

/**
 * Parse one ACP envelope (nested op:event or bare session_update) and return
 * the agent_message_chunk text delta when present. Non-session_update frames
 * and other kinds yield null (caller should ignore, not mis-accumulate).
 * Aligns with AgentChatTester.applyAcp: `envelope?.data ?? envelope`.
 */
export function extractAgentMessageDelta(raw: unknown): AcpMessageDelta | null {
  let envelope: any = raw
  if (typeof raw === 'string') {
    try {
      envelope = JSON.parse(raw)
    } catch {
      return null
    }
  }
  if (!envelope || typeof envelope !== 'object') return null

  // Peel {op:"event",data} then fall back to envelope.data ?? envelope (Chat test).
  const peeled = unwrapFrame(envelope)
  const ev = peeled?.type ? peeled : (peeled?.data ?? peeled ?? envelope?.data ?? envelope)
  if (!ev || typeof ev !== 'object' || ev.type !== 'session_update' || !ev.update) return null

  const u = flattenUpdate(ev.update)
  const kind = normalizeKind(u.sessionUpdate || u.session_update || u.type || u.kind || '')
  if (kind !== 'agent_message_chunk') return null
  const text = contentText(u.content)
  if (!text) return null
  return { kind, text }
}
