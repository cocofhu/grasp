/** Shared shape for GET /runs/:id/nodes/:nodeId/events (paginated or not). */
type NodeEventsPayload = {
  events?: unknown[]
  live?: boolean
  /** True when live sandbox is registered but bridge read failed transiently. */
  unavailable?: boolean
  error?: string
}

/** True when the control plane returned a soft-fail body (HTTP 200 + unavailable). */
export function isNodeEventsUnavailable(r: NodeEventsPayload | null | undefined): boolean {
  return !!r?.unavailable
}
