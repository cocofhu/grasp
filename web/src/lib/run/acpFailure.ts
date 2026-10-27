/** A provider failure from a bridge frame; empty text means no detail was sent. */
export function acpFailureText(envelope: any): string | null {
  const ev = envelope?.data ?? envelope
  if (ev?.type === 'error_text') {
    const text = typeof ev.text === 'string' ? ev.text.trim() : ''
    return text || null
  }
  if (ev?.type === 'prompt_done') {
    const stop = String(ev.stopReason ?? '').trim().toLowerCase()
    if (stop === 'failed' || stop === 'refusal') return ''
  }
  return null
}

/** Keep detailed diagnostics when a later completion only says the turn failed. */
export function appendAcpError(current: string | undefined, text: string): string | undefined {
  if (!text || current === text) return current
  return current ? `${current}\n${text}` : text
}
