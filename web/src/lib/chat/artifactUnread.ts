import { formatUnreadBadge } from '@/lib/run/useRunTerminalNotifications'

/** A progress citation the project-manager thread may carry. */
export type ArtifactCitationLike = {
  type?: string
  targetId?: string
}

export type ArtifactCitationMessage = {
  id: string
  citations?: ArtifactCitationLike[]
}

export type ArtifactButtonLabels = {
  open: string
  close: string
  /** Receives the visible badge text, such as "2". */
  unread: (count: string) => string
  /** Full accessible name once the badge is capped at 99+. */
  cap: string
}

export type CitationUnreadState = {
  threadId: string
  baselined: boolean
  seen: string[]
  count: number
  messageIds: string[]
}

export type CitationUnreadEvent = {
  threadId: string
  panelOpen: boolean
  /** True while the current thread page is being replaced. Existing rows must not count. */
  loading: boolean
  messages: ArtifactCitationMessage[]
}

const listeners = new Set<() => void>()

/** Run sessions publish one event per artifact write or same-name overwrite. */
export function notifyRunArtifactEdit(): void {
  for (const listener of [...listeners]) listener()
}

/** Subscribe to run-session artifact writes. Returns an unsubscribe function. */
export function onRunArtifactEdit(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/**
 * Count one run-session change.
 * Writes while the panel is open, and every non-artifact event, leave the count unchanged.
 */
export function nextRunArtifactUnread(count: number, panelOpen: boolean, kind: 'artifact' | 'other'): number {
  if (panelOpen || kind !== 'artifact') return count
  return count + 1
}

/** Opening the artifact panel clears the count. Closing leaves it at zero. */
export function runArtifactUnreadOnPanel(count: number, panelOpen: boolean): number {
  return panelOpen ? 0 : count
}

export function artifactCitationKey(messageId: string, index: number, citation: ArtifactCitationLike): string | null {
  if (citation.type !== 'artifact') return null
  return `${messageId}:${index}:${citation.targetId ?? ''}`
}

export function collectArtifactCitationKeys(messages: ArtifactCitationMessage[]): string[] {
  const keys: string[] = []
  for (const message of messages) {
    message.citations?.forEach((citation, index) => {
      const key = artifactCitationKey(message.id, index, citation)
      if (key) keys.push(key)
    })
  }
  return keys
}

export function createCitationUnreadState(): CitationUnreadState {
  return { threadId: '', baselined: false, seen: [], count: 0, messageIds: [] }
}

/**
 * Citations on messages that were prepended ahead of the previous window are history.
 * Everything else that was not seen yet is a new artifact reference.
 */
export function diffArtifactCitationKeys(
  previousMessageIds: string[],
  messages: ArtifactCitationMessage[],
  seen: ReadonlySet<string>,
): { seen: string[]; liveAdded: number } {
  const firstPrevious = previousMessageIds[0]
  const anchor = firstPrevious ? messages.findIndex((message) => message.id === firstPrevious) : -1
  const previousIds = new Set(previousMessageIds)
  const nextSeen = new Set(seen)
  let liveAdded = 0
  messages.forEach((message, index) => {
    const prepended = anchor >= 0 && index < anchor && !previousIds.has(message.id)
    message.citations?.forEach((citation, citationIndex) => {
      const key = artifactCitationKey(message.id, citationIndex, citation)
      if (!key || nextSeen.has(key)) return
      nextSeen.add(key)
      if (!prepended) liveAdded += 1
    })
  })
  return { seen: [...nextSeen], liveAdded }
}

/**
 * Project-manager threads count only new artifact citations on the current thread.
 * The first settled page, a thread switch, and older pages loaded upward do not count.
 */
export function reduceArtifactCitationUnread(state: CitationUnreadState, event: CitationUnreadEvent): CitationUnreadState {
  let next = state
  if (event.threadId !== state.threadId) {
    next = { threadId: event.threadId, baselined: false, seen: [], count: 0, messageIds: [] }
  }
  if (event.panelOpen && next.count !== 0) next = { ...next, count: 0 }
  if (event.loading || !event.threadId) {
    return { ...next, baselined: false, seen: [], count: event.panelOpen ? 0 : next.count, messageIds: [] }
  }
  if (!next.baselined) {
    return {
      ...next,
      baselined: true,
      seen: collectArtifactCitationKeys(event.messages),
      messageIds: event.messages.map((message) => message.id),
      count: 0,
    }
  }
  const diff = diffArtifactCitationKeys(next.messageIds, event.messages, new Set(next.seen))
  return {
    ...next,
    seen: diff.seen,
    messageIds: event.messages.map((message) => message.id),
    count: event.panelOpen ? 0 : next.count + diff.liveAdded,
  }
}

/** Accessible name for an artifact button. The badge itself is not named. */
export function artifactButtonAriaLabel(panelOpen: boolean, count: number, labels: ArtifactButtonLabels): string {
  if (panelOpen) return labels.close
  const text = formatUnreadBadge(count)
  if (!text) return labels.open
  if (text === '99+') return labels.cap
  return labels.unread(text)
}
