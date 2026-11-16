import { origin, req, wsUrl } from '../httpCore'
import type { DesktopStatus, SandboxCapacity, SandboxView } from '../apiTypes'

export const sandboxesClient = {
  sandboxCapacity: () => req<SandboxCapacity>('/sandboxes/capacity'),
  // Sandboxes across all purposes and backends.
  listSandboxes: () => req<SandboxView[]>('/sandboxes'),
  getSandbox: (id: number) => req<SandboxView>(`/sandboxes/${id}`),
  stopSandbox: (id: number) => req<{ status: string }>(`/sandboxes/${id}/stop`, { method: 'POST' }),
  destroySandbox: (id: number) => req<{ status: string }>(`/sandboxes/${id}`, { method: 'DELETE' }),
  cleanupSandboxes: () => req<{ destroyed: number; skipped: number }>('/sandboxes/cleanup', { method: 'POST' }),
  sandboxChatWsUrl: (id: number) => wsUrl(`/sandboxes/${id}/chat`),
  sandboxTerminalWsUrl: (id: number) => wsUrl(`/sandboxes/${id}/terminal`),
  sandboxIdeUrl: (id: number) => `${origin()}/sandbox/${id}/?folder=/root/workspace`,
  // Reverse-proxy to the in-container acp-bridge native web UI (8765). The
  // trailing slash matters so the UI resolves its relative assets/WS against
  // this subpath (document.baseURI).
  sandboxBridgeUrl: (id: number) => `${origin()}/sandbox-bridge/${id}/`,

  // Raw agent event frames (unaggregated) — used to rebuild the chat transcript
  // when reopening a reused sandbox.
  sandboxEventLog: (id: number, params?: { cursor?: string; limit?: number }) => {
    const qs = new URLSearchParams()
    if (params?.cursor) qs.set('cursor', params.cursor)
    if (params?.limit != null) qs.set('limit', String(params.limit))
    const q = qs.toString()
    const path = `/sandboxes/${id}/eventlog` + (q ? `?${q}` : '')
    if (params?.cursor || params?.limit != null) {
      return req<{ events: any[]; nextCursor: string; hasMore: boolean }>(path)
    }
    return req<{ events: any[] }>(path)
  },
  sandboxDesktop: (id: number) => req<DesktopStatus>(`/sandboxes/${id}/desktop`),
  /** holder=user must be renewed (heartbeat); the sandbox hands control back to the Agent after 30s. */
  setSandboxDesktopControl: (id: number, holder: 'user' | 'agent', client: string, opts?: { keepalive?: boolean }) =>
    req<DesktopStatus>(`/sandboxes/${id}/desktop/control`, {
      method: 'PUT',
      body: JSON.stringify({ holder, client }),
      ...(opts?.keepalive ? { keepalive: true } : {}),
    }),
  sandboxLog: (id: number, opts?: { signal?: AbortSignal }) =>
    req<{ content: string; live: boolean; found: boolean }>(
      `/sandboxes/${id}/log`,
      opts?.signal ? { signal: opts.signal } : undefined,
    ),
}
