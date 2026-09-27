import type { ClarifyTurn } from '@/lib/shared/types'

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

/** Copy for the failure card: prefer error text, else empty-output hint. */
export function emptyFailDisplayText(
  t: ClarifyTurn,
  emptyHint: string,
): string {
  const raw = (t.text || '').trim()
  if (raw) return raw
  return emptyHint
}
