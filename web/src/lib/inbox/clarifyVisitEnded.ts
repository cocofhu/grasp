import { isClarifyVisitEndedError } from '@/lib/api/httpCore'

type Translate = (key: string) => string

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
