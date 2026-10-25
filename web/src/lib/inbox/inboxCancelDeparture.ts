/**
 * The sidebar is cancelling the inbox conversation that is open right now
 * and, on success, leaves for the new-request page. Until that leave finishes,
 * the inbox must keep the open conversation: an empty list must not paint the
 * pending-empty state, and a remaining row must not become the selection.
 */
let departingRunId: string | null = null

export function beginInboxCancelDeparture(runId: string) {
  const id = runId.trim()
  if (!id) return
  departingRunId = id
}

export function endInboxCancelDeparture(runId?: string) {
  if (runId !== undefined && departingRunId !== runId) return
  departingRunId = null
}

export function inboxCancelDepartureRunId(): string | null {
  return departingRunId
}

export function isInboxCancelDeparture(runId: string | null | undefined): boolean {
  return !!runId && departingRunId === runId
}
