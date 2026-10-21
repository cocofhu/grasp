import { isClarifyVisitEndedError } from '@/lib/api/httpCore'
import { stashClarifyDraft } from '@/lib/inbox/clarifyDraftStash'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

type Translate = (key: string) => string

type Sent = { text?: string; images?: ClarifyImage[]; annotations?: ReactAnnotation[]; clientId?: string }

export type RestoreUnsent = (sent: Sent, notice: string, opts?: { autoRetryOnResume?: boolean }) => void

/** The chat surface hosts reach through reviewChatRef. */
export type VisitEndedChat = { restoreUnsentQueued?: RestoreUnsent } | null | undefined

/**
 * Notice for a clarify reply refused with 409 clarify_visit_ended, or null for
 * any other error. The system first message (autoRetryOnResume) is redelivered
 * by resume, so it must not ask the user to send again.
 */
export function clarifyVisitEndedNotice(e: unknown, t: Translate): string | null {
  if (!isClarifyVisitEndedError(e)) return null
  return e.autoRetryOnResume
    ? t('pages.clarify.visitEndedAutoRetry')
    : t('pages.clarify.visitEndedResend')
}

/** `dropped` is optional and loosely typed on the wire; read it defensively. */
export function readDropped(raw: unknown): Sent {
  if (!raw || typeof raw !== 'object') return {}
  const d = raw as Record<string, unknown>
  const imgs = Array.isArray(d.attachments) ? d.attachments : Array.isArray(d.images) ? d.images : undefined
  return {
    clientId: typeof d.clientId === 'string' && d.clientId ? d.clientId : undefined,
    text: typeof d.text === 'string' ? d.text : undefined,
    images: imgs as ClarifyImage[] | undefined,
    annotations: Array.isArray(d.annotations) ? (d.annotations as ReactAnnotation[]) : undefined,
  }
}

/**
 * Host catch for a clarify reply. Returns null when `e` is not
 * clarify_visit_ended. Otherwise hands the message back — to the chat when it
 * is mounted (composer, or its run+node stash when closed), else straight into
 * the stash — and reports whether the chat showed the notice itself.
 */
export function handleClarifyVisitEnded(
  e: unknown,
  t: Translate,
  ctx: { chat: VisitEndedChat; runId: string; nodeId: string; sent: Sent; retryLast?: boolean },
): { notice: string; shownInChat: boolean } | null {
  const notice = clarifyVisitEndedNotice(e, t)
  if (!notice || !isClarifyVisitEndedError(e)) return null
  const auto = !!e.autoRetryOnResume
  const dropped = readDropped(e.dropped)
  // Local optimistic copy first; `dropped` fills gaps.
  const sent: Sent = ctx.retryLast
    ? {}
    : {
        clientId: dropped.clientId,
        text: ctx.sent.text || dropped.text,
        images: ctx.sent.images?.length ? ctx.sent.images : (dropped.images ?? ctx.sent.images),
        annotations: ctx.sent.annotations?.length
          ? ctx.sent.annotations
          : (dropped.annotations ?? ctx.sent.annotations),
      }
  if (ctx.chat?.restoreUnsentQueued) {
    ctx.chat.restoreUnsentQueued(sent, notice, { autoRetryOnResume: auto })
    return { notice, shownInChat: true }
  }
  if (!auto && !ctx.retryLast) {
    stashClarifyDraft(ctx.runId, ctx.nodeId, {
      text: sent.text ?? '',
      images: sent.images ?? [],
      annotations: sent.annotations ?? [],
    })
  }
  return { notice, shownInChat: false }
}
