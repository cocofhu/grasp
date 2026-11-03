import { req, rootWsUrl } from '@/lib/api/httpCore'
import { GATE_SHARE_REQUEST_HEADER, GATE_SHARE_TOKEN_HEADER } from '@/lib/inbox/gateShareLink'
import type { EmbedTicket } from '@/lib/inbox/embedChat'

export type PreviewChatMode = 'isolated' | 'main'
export type PreviewShareScope = { runId: string; nodeId: string; soloSessionId?: string; port: number }
export type PreviewShareLink = { url: string; expiresAt: string }
export type PreviewShareSession = { ticket: string; chatMode: PreviewChatMode }
export type PreviewChatRow = { id: string; role: string; text: string }

function managementPath(scope: PreviewShareScope) {
  const enc = encodeURIComponent
  return scope.soloSessionId
    ? `/solo/sessions/${enc(scope.soloSessionId)}/preview-share`
    : `/runs/${enc(scope.runId)}/nodes/${enc(scope.nodeId)}/preview-share`
}

export async function createPreviewShare(scope: PreviewShareScope, chatMode: PreviewChatMode) {
  const link = await req<PreviewShareLink>(managementPath(scope), {
    method: 'POST', body: JSON.stringify({ port: scope.port, chatMode }),
  })
  const url = new URL(link.url, window.location.origin)
  // The API may be behind Vite, whose upstream Host differs from the browser.
  return { ...link, url: window.location.origin + url.pathname + url.hash }
}

export function revokePreviewShare(scope: PreviewShareScope, chatMode: PreviewChatMode) {
  return req<{ ok: boolean }>(managementPath(scope), {
    method: 'DELETE', body: JSON.stringify({ port: scope.port, chatMode }),
  })
}

const publicPath = '/public/approvals/app'

export function publicAppPreviewClient(token: string) {
  async function request<T>(path: string, signal: AbortSignal, body?: object): Promise<T> {
    const response = await fetch(publicPath + path, {
      method: body ? 'POST' : 'GET',
      credentials: 'omit',
      cache: 'no-store',
      signal,
      headers: { [GATE_SHARE_REQUEST_HEADER]: '1', [GATE_SHARE_TOKEN_HEADER]: token, 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!response.ok) throw Object.assign(new Error(String(response.status)), { status: response.status })
    return response.json()
  }
  return {
    open: (signal: AbortSignal) => request<EmbedTicket & { directUrl: string }>('/open', signal, {}),
    session: (ticket: string, signal: AbortSignal) => request<PreviewShareSession>('/session', signal, { ticket }),
    history: (signal: AbortSignal) => request<{ rows: PreviewChatRow[]; busy: boolean }>('/main-chat', signal),
    send: (text: string, signal: AbortSignal) => request<{ ok: boolean }>('/main-chat', signal, { text }),
    connectGuest: (ticket: string) => new WebSocket(rootWsUrl(publicPath + '/guest-chat'), ['grasp-preview-guest', ticket]),
  }
}
