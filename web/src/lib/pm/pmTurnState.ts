/**
 * PM Leader consult turn failure kinds (product-level). `connection` only
 * survives on messages stored before the server owned turn failure.
 */
export type PmFailKind = 'connection' | 'sandbox' | 'empty' | 'unknown' | 'stopped' | 'interrupted'

const PM_FAIL_KINDS: readonly PmFailKind[] = [
  'connection',
  'sandbox',
  'empty',
  'unknown',
  'stopped',
  'interrupted',
] as const

export function isPmFailKind(kind: string): kind is PmFailKind {
  return (PM_FAIL_KINDS as readonly string[]).includes(kind)
}

/** Thread WebSocket reconnect backoff: 1s, 2s, 4s… capped at 15s. */
export function pmWsReconnectDelayMs(attempt: number): number {
  return Math.min(15_000, 1000 * 2 ** Math.max(0, Math.min(attempt, 4)))
}

export function pmActiveThreadStorageKey(projectId: string): string {
  return `pm-leader:active-thread:${projectId}`
}
