import type { AgentPart, ClarifyTurn } from '@/lib/shared/types'

/**
 * The reply ended with nothing to show, or failed. The server decides this
 * (row.outcome); the text is never inspected here.
 */
export function isRetryableFailedAgent(t: ClarifyTurn | null | undefined): boolean {
  if (!t || t.role !== 'agent' || t.streaming) return false
  return t.outcome === 'empty' || t.outcome === 'failed'
}

const FAILURE_BANNER_RE = /^\((澄清回复失败|澄清会话已失效|复审修改失败):([\s\S]*)\)$/

/** Drop the persisted "(澄清回复失败:…)" wrapper and the bridge's stderr prefix. */
export function failureDetailText(text: string): string {
  const raw = text.trim()
  const banner = raw.match(FAILURE_BANNER_RE)
  let body = (banner ? banner[2] : raw).trim()
  body = body.replace(/^exit status \d+;\s*stderr:\s*/i, '')
  body = body.replace(/^NonRetriableError:\s*/i, '')
  body = body.replace(/^Provider Error\s*/i, '')
  return body.trim()
}

/**
 * Steps to draw for this turn. A failed reply keeps its own thought and tool
 * steps; its failure text is shown by the failure card, not as prose.
 */
export function turnProcessParts(t: ClarifyTurn): AgentPart[] {
  const parts = t.parts || []
  if (t.outcome !== 'failed') return parts
  const banner = (t.text || '').trim()
  return parts.filter((p) => p.kind !== 'message' || (p.text || '').trim() !== banner)
}

/** Headline: empty turns say so; a failure does not claim there was no output. */
export function emptyFailHeadline(t: ClarifyTurn, emptyTitle: string, failTitle: string): string {
  return t.outcome === 'failed' ? failTitle : emptyTitle
}

/** Copy under the headline: the cleaned failure detail, else the empty-output hint. */
export function emptyFailDisplayText(t: ClarifyTurn, emptyHint: string): string {
  if (t.outcome !== 'failed') return emptyHint
  return failureDetailText(t.error || t.text || '') || emptyHint
}
