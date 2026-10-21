import { isClarifyVisitEndedError } from '@/lib/api/httpCore'
import {
  dropClarifyOutbox,
  newClientMessageId,
  putClarifyOutbox,
  stashClarifyDraft,
  takeClarifyOutbox,
} from '@/lib/inbox/clarifyDraftStash'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

type Translate = (key: string, named?: Record<string, unknown>) => string

type Sent = {
  text?: string
  images?: ClarifyImage[]
  annotations?: ReactAnnotation[]
  clientId?: string
  at?: number
  imagesDropped?: number
}

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
  ctx: {
    chat: VisitEndedChat
    runId: string
    nodeId: string
    sent: Sent
    retryLast?: boolean
    /** The id this send carried; its outbox entry is consumed here. */
    clientMessageId?: string
  },
): { notice: string; shownInChat: boolean } | null {
  const notice = clarifyVisitEndedNotice(e, t)
  if (!notice || !isClarifyVisitEndedError(e)) return null
  const auto = !!e.autoRetryOnResume
  const dropped = readDropped(e.dropped)
  // The 409 answers this very request: local copy (the caller's arguments,
  // then the persisted outbox) first; `dropped` only fills gaps. A ref-only
  // image in `dropped` is used only when no local copy (with preview data) exists.
  const outbox = takeClarifyOutbox(ctx.runId, ctx.nodeId, ctx.clientMessageId)
  const local: Sent = {
    text: ctx.sent.text || outbox?.text,
    images: ctx.sent.images?.length ? ctx.sent.images : outbox?.images,
    annotations: ctx.sent.annotations?.length ? ctx.sent.annotations : outbox?.annotations,
  }
  const usedLocalImages = !!ctx.sent.images?.length
  const sent: Sent = ctx.retryLast
    ? {}
    : {
        at: outbox?.at,
        imagesDropped: usedLocalImages ? 0 : outbox?.imagesDropped,
        clientId: dropped.clientId,
        text: local.text || dropped.text,
        images: local.images?.length ? local.images : (dropped.images ?? local.images),
        annotations: local.annotations?.length ? local.annotations : (dropped.annotations ?? local.annotations),
      }
  if (ctx.chat?.restoreUnsentQueued) {
    ctx.chat.restoreUnsentQueued(sent, notice, { autoRetryOnResume: auto })
    return { notice, shownInChat: true }
  }
  if (!auto && !ctx.retryLast) {
    const r = stashClarifyDraft(
      ctx.runId,
      ctx.nodeId,
      {
        text: sent.text ?? '',
        images: sent.images ?? [],
        annotations: sent.annotations ?? [],
      },
      { at: sent.at ?? Date.now() },
    )
    const n = r.imagesDropped + (sent.imagesDropped ?? 0)
    // No chat to show it in: one toast carries both lines.
    if (n > 0) return { notice: `${notice}\n${t('pages.clarify.stashImagesDropped', { n })}`, shownInChat: false }
  }
  return { notice, shownInChat: false }
}

/**
 * Start one clarify send from a host: a fresh clientMessageId plus its outbox
 * entry (per tab), so a later clarify_visit_ended frame for it can be matched.
 * retryLast / force sends carry no user message and get no id.
 */
export function beginClarifySend(
  runId: string,
  nodeId: string,
  sent: { text: string; images: ClarifyImage[]; annotations: ReactAnnotation[] },
  opts: { force?: boolean; retryLast?: boolean },
): string | undefined {
  if (opts.force || opts.retryLast) return undefined
  const id = newClientMessageId()
  putClarifyOutbox(runId, nodeId, id, sent)
  return id
}

/** A send failed for a reason other than clarify_visit_ended: forget its outbox entry. */
export function failClarifySend(runId: string, nodeId: string, id: string | undefined): void {
  dropClarifyOutbox(runId, nodeId, id)
}
