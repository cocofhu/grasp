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

export function pmActiveThreadStorageKey(_workspaceId?: string): string {
  return 'pm-leader:active-thread'
}

/** One-time conversion of a local selection; the server-provided visible IDs authorize it. */
export function migratePmActiveThreadStorage(visibleIds: readonly string[], workspaceId: string): void {
  try {
    const marker = 'pm-leader:workspace-selection-v1'
    if (localStorage.getItem(marker)) return
    if (!visibleIds.length) return
    const target = pmActiveThreadStorageKey()
    const current = localStorage.getItem(target)
    if (!current || !visibleIds.includes(current)) {
      let lastWorkspaceId = ''
      const selection = localStorage.getItem('grasp-project-context')
      if (selection) {
        try {
          const parsed = JSON.parse(selection)
          lastWorkspaceId = typeof parsed === 'string' ? parsed : typeof parsed?.projectId === 'string' ? parsed.projectId : ''
        } catch { lastWorkspaceId = selection }
      }
      const candidates = [...new Set([lastWorkspaceId, workspaceId].filter(Boolean))]
      for (const id of candidates) {
        const preferred = localStorage.getItem(`pm-leader:active-thread:${id}`)
        if (preferred && visibleIds.includes(preferred)) { localStorage.setItem(target, preferred); break }
      }
    }
    localStorage.removeItem('grasp-project-context')
    localStorage.setItem(marker, '1')
  } catch { /* Local preferences are optional when storage is unavailable. */ }
}
