import type { AgentPart, ClarifyTurn } from '@/lib/shared/types'

/** Known agent failure banners (chat err / rehydrate / review revise). */
const FAILURE_TEXT_RE =
  /澄清回复失败|澄清会话已失效|复审修改失败|^\([^)]*失败[^)]*\)$/

/** True when agent text is an explicit failure banner, not a normal reply. */
export function isFailureAssistantText(text?: string | null): boolean {
  const t = (text || '').trim()
  if (!t) return false
  return FAILURE_TEXT_RE.test(t)
}

/** Agent turn ended with no body, thought, or questions (idle empty slot). */
export function isEmptyFailedAgent(t: ClarifyTurn | null | undefined): boolean {
  if (!t || t.role !== 'agent') return false
  if (t.streaming || t.interrupted || t.handoff) return false
  if (t.questions?.length) return false
  return !(t.text || '').trim() && !(t.thought || '').trim()
}

/**
 * Latest empty / failure agent turn is retryable (not Cancel「已中断」, not
 * success body or ask_question).
 */
export function isRetryableFailedAgent(t: ClarifyTurn | null | undefined): boolean {
  if (!t || t.role !== 'agent') return false
  if (t.streaming || t.interrupted || t.handoff) return false
  if (t.questions?.length) return false
  if (isEmptyFailedAgent(t)) return true
  return isFailureAssistantText(t.text)
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
 * steps, and drops the failure banner so it is not also rendered as prose.
 * The capped `tools` list is not used: it is the first 50 calls of the
 * session log, which on a later failure is the previous turn.
 */
export function turnProcessParts(t: ClarifyTurn): AgentPart[] {
  const parts = t.parts || []
  if (!isRetryableFailedAgent(t)) return parts
  return parts.filter((p) => p.kind !== 'message' || !isFailureAssistantText(p.text))
}

/** Headline: empty turns say so; a provider failure does not claim there was no output. */
export function emptyFailHeadline(t: ClarifyTurn, emptyTitle: string, failTitle: string): string {
  if ((t.text || '').trim() && isFailureAssistantText(t.text)) return failTitle
  return emptyTitle
}

/** Copy under the headline: cleaned failure text, else the empty-output hint. */
export function emptyFailDisplayText(
  t: ClarifyTurn,
  emptyHint: string,
): string {
  const raw = (t.text || '').trim()
  if (!raw) return emptyHint
  return failureDetailText(raw)
}
