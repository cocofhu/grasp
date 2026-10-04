/**
 * Reconnect policy for the noVNC preview after the server or the network drops
 * the viewer. Reasons come from the server `closed` frame (browser.Service close
 * reasons) or are synthesized client-side for socket drops.
 */

export type VncCloseReason = 'idle' | 'desktop-closed' | 'superseded' | 'evicted' | 'disconnect' | string

export const VNC_RECONNECT_MAX_ATTEMPTS = 6
const BASE_DELAY_MS = 1_000
const MAX_DELAY_MS = 30_000

/** Ping cadence while live and visible; must stay well under the server TabIdleTTL. */
export const VNC_HEARTBEAT_MS = 60_000

/**
 * `superseded` / `evicted` mean another viewer or the global cap took the slot;
 * reconnecting automatically would make two windows fight over it.
 */
export function shouldAutoReconnect(reason: VncCloseReason): boolean {
  return reason !== 'superseded' && reason !== 'evicted'
}

/** Delay before retry n (0-based): 1s, 2s, 4s… capped at 30s. */
export function reconnectDelayMs(attempt: number): number {
  const n = Math.max(0, Math.floor(attempt))
  return Math.min(MAX_DELAY_MS, BASE_DELAY_MS * 2 ** n)
}

const KNOWN_REASONS = new Set(['idle', 'desktop-closed', 'superseded', 'evicted', 'disconnect'])

/** i18n key under pages.appPreview.novnc.closedReason for a close reason. */
export function closeReasonKey(reason: VncCloseReason): string {
  return KNOWN_REASONS.has(reason) ? reason : 'disconnect'
}
