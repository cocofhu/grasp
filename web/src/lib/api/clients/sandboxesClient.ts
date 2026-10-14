import { origin, req, wsUrl } from '../httpCore'
import type { SandboxLeaseExtend, SandboxView } from '../apiTypes'

export const sandboxesClient = {
  // sandboxes (interactive Agent chat-test containers)
  listSandboxes: () => req<SandboxView[]>('/sandboxes'),
  getSandbox: (id: number) => req<SandboxView>(`/sandboxes/${id}`),
  stopSandbox: (id: number) => req<{ status: string }>(`/sandboxes/${id}/stop`, { method: 'POST' }),
  extendSandboxLease: (id: number, body: SandboxLeaseExtend) =>
    req<SandboxView>(`/sandboxes/${id}/leases`, { method: 'POST', body: JSON.stringify(body) }),
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
  sandboxLog: (id: number, opts?: { signal?: AbortSignal }) =>
    req<{ content: string; live: boolean; found: boolean }>(
      `/sandboxes/${id}/log`,
      opts?.signal ? { signal: opts.signal } : undefined,
    ),
}
