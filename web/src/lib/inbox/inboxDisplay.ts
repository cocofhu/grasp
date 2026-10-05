import { truncateText } from '@/lib/shared/format'
import type { InboxItem } from '@/lib/shared/types'

/** Secondary-line run label: non-empty runTitle (truncated) or `Run #id`. */
export function inboxRunLabel(it: Pick<InboxItem, 'runId' | 'runTitle'>): string {
  const title = it.runTitle?.trim()
  if (title) return truncateText(title, 60)
  return `Run #${it.runId.replace(/^run-/, '')}`
}

export function inboxSecondaryLine(it: Pick<InboxItem, 'workflowName' | 'runId' | 'runTitle'>): string {
  return `${it.workflowName} · ${inboxRunLabel(it)}`
}

/** i18n key for the list badge: gate / clarify / review / starting / replying. */
export type InboxBadgeLabelKey =
  | 'pages.gatesInbox.gateType'
  | 'pages.gatesInbox.clarifyType'
  | 'pages.gatesInbox.reviewType'
  | 'pages.gatesInbox.startingType'
  | 'pages.gatesInbox.replyingType'

/** Visual tone for icon/badge chips (Demo: warn / review-green / clarify-cyan / amber). */
export type InboxBadgeTone = 'gate' | 'review' | 'clarify' | 'replying'

export type InboxStateItem = Pick<InboxItem, 'type'> & { kind?: string; state?: string }

/** True while the item's sandbox is still booting (no transcript, no reply yet). */
export function isStartingInboxItem(
  it: InboxStateItem | null | undefined,
): boolean {
  return !!it && it.type === 'clarify' && it.state === 'starting'
}

/** True while a parked clarify/review item's session is busy (not starting). */
export function isReplyingInboxItem(
  it: InboxStateItem | null | undefined,
): boolean {
  return !!it && it.type === 'clarify' && it.state === 'replying' && !isStartingInboxItem(it)
}

/** Spinner/in-progress icon: starting or replying. */
export function isInboxProgressItem(
  it: InboxStateItem | null | undefined,
): boolean {
  return isStartingInboxItem(it) || isReplyingInboxItem(it)
}

/**
 * List badge copy key by inbox semantics.
 * Priority: starting > replying > type (gate / review / clarify).
 */
export function inboxBadgeLabelKey(
  it: InboxStateItem,
): InboxBadgeLabelKey {
  if (isStartingInboxItem(it)) return 'pages.gatesInbox.startingType'
  if (isReplyingInboxItem(it)) return 'pages.gatesInbox.replyingType'
  if (it.type === 'gate') return 'pages.gatesInbox.gateType'
  if (it.kind === 'review') return 'pages.gatesInbox.reviewType'
  return 'pages.gatesInbox.clarifyType'
}

/** Badge/icon color tone; replying uses amber so it is distinct from static type chips. */
export function inboxBadgeTone(it: InboxStateItem): InboxBadgeTone {
  if (isReplyingInboxItem(it)) return 'replying'
  if (it.type === 'gate') return 'gate'
  if (it.kind === 'review') return 'review'
  return 'clarify'
}

/** Merge sessionBusy onto a parked card without changing membership. starting wins. */
export function applyInboxReplyingState<T extends InboxStateItem>(item: T, busy: boolean): T {
  if (item.type !== 'clarify' || isStartingInboxItem(item)) return item
  const next = busy ? 'replying' : undefined
  if (item.state === next) return item
  return { ...item, state: next }
}

/** Merge remote state onto displayed rows when keys match; never add/remove rows. */
export function mergeInboxReplyingFromRemote<T extends InboxStateItem & { runId: string; nodeId: string }>(
  displayed: T[],
  remote: T[],
  keyOf: (it: T) => string = (it) => `${it.runId}:${it.nodeId}`,
): T[] {
  const remoteByKey = new Map(remote.map((it) => [keyOf(it), it]))
  let changed = false
  const next = displayed.map((it) => {
    const r = remoteByKey.get(keyOf(it))
    if (!r || r.type !== 'clarify' || it.type !== 'clarify') return it
    if (isStartingInboxItem(it) && !isStartingInboxItem(r)) {
      // Parking transition is a real list-row change handled by loadList, not peek merge.
      return it
    }
    if (isStartingInboxItem(r) || isStartingInboxItem(it)) return it
    const merged = applyInboxReplyingState(it, r.state === 'replying')
    if (merged !== it) changed = true
    return merged
  })
  return changed ? next : displayed
}

/** Icon tile classes by tone. */
export function inboxIconToneClass(tone: InboxBadgeTone): string {
  switch (tone) {
    case 'gate':
      return 'bg-warn/15 text-warn'
    case 'review':
      return 'bg-n-review/15 text-n-review'
    case 'replying':
      return 'bg-n-artifact/15 text-n-artifact'
    default:
      return 'bg-n-clarify/15 text-n-clarify'
  }
}

/** Badge chip classes by tone. */
export function inboxBadgeToneClass(tone: InboxBadgeTone): string {
  switch (tone) {
    case 'gate':
      return 'border-warn/30 bg-warn/10 text-warn'
    case 'review':
      return 'border-n-review/30 bg-n-review/10 text-n-review'
    case 'replying':
      return 'border-n-artifact/35 bg-n-artifact/10 text-n-artifact'
    default:
      return 'border-n-clarify/30 bg-n-clarify/10 text-n-clarify'
  }
}
