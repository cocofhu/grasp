import { req, rootWsUrl } from '@/lib/api/httpCore'
import { GATE_SHARE_REQUEST_HEADER, GATE_SHARE_TOKEN_HEADER } from '@/lib/inbox/gateShareLink'
import type { ScopedSessionModelCatalog } from '@/lib/solo/useSessionModelCatalog'
import type { VisitModelChoice } from '@/lib/api/clients/runsClient'
import { referenceQuery, type ReferenceSearchParams } from '@/lib/api/clients/soloClient'
import type { MessageReference, ReferenceInput, ReferenceSearchResult } from '@/lib/api/apiTypes'
import type { AgentPart, AgentTool, ChatMessageMetadata, ClarifyImage, ReactAnnotation } from '@/lib/shared/types'
import type { EmbedTicket } from '@/lib/inbox/embedChat'
import { locale, type AppLocale } from '@/lib/shared/locale'

export type PreviewChatMode = 'isolated' | 'main'
export type PreviewShareScope = { runId: string; nodeId: string; soloSessionId?: string; port: number }
export type PreviewShareLink = { url: string; expiresAt: string }
export type PreviewShareSession = { ticket: string; chatMode: PreviewChatMode; agentName?: string }
export type PreviewChatRow = ChatMessageMetadata & {
  id: string
  role: string
  text: string
  at?: string
  streaming?: boolean
  thought?: string
  tools?: AgentTool[]
  parts?: AgentPart[]
  startedAt?: string
  endedAt?: string
  interrupted?: boolean
  annotations?: ReactAnnotation[]
  images?: ClarifyImage[]
  references?: MessageReference[]
}

export type PreviewChatQueueItem = Pick<PreviewChatRow, 'id' | 'text' | 'images' | 'annotations'>
export type PreviewChatHistory = { rows: PreviewChatRow[]; queue?: PreviewChatQueueItem[]; busy: boolean; model?: VisitModelChoice }

export function previewShareURL(raw: string, lang: AppLocale = locale.value) {
  const url = new URL(raw, window.location.origin)
  const fragment = new URLSearchParams(url.hash.slice(1))
  fragment.set('lang', lang)
  // The API may be behind Vite, whose upstream Host differs from the browser.
  return window.location.origin + url.pathname + url.search + '#' + fragment.toString()
}

export type PreviewChatMessage = {
  text: string
  annotations: ReactAnnotation[]
  images: ClarifyImage[]
  references: ReferenceInput[]
}

function managementPath(scope: PreviewShareScope) {
  const enc = encodeURIComponent
  return scope.soloSessionId
    ? `/solo/sessions/${enc(scope.soloSessionId)}/preview-share`
    : `/runs/${enc(scope.runId)}/nodes/${enc(scope.nodeId)}/preview-share`
}

export async function createPreviewShare(scope: PreviewShareScope, chatMode: PreviewChatMode) {
  const lang = locale.value
  const link = await req<PreviewShareLink>(managementPath(scope), {
    method: 'POST', body: JSON.stringify({ port: scope.port, chatMode }),
  })
  return { ...link, url: previewShareURL(link.url, lang) }
}

export function revokePreviewShare(scope: PreviewShareScope, chatMode: PreviewChatMode) {
  return req<{ ok: boolean }>(managementPath(scope), {
    method: 'DELETE', body: JSON.stringify({ port: scope.port, chatMode }),
  })
}

const publicPath = '/public/approvals/app'

export function publicAppPreviewClient(token: string) {
  async function request<T>(path: string, signal: AbortSignal, body?: object, decode: (response: Response) => Promise<T> = res => res.json()): Promise<T> {
    const response = await fetch(publicPath + path, {
      method: body ? 'POST' : 'GET',
      credentials: 'omit',
      cache: 'no-store',
      signal,
      headers: { [GATE_SHARE_REQUEST_HEADER]: '1', [GATE_SHARE_TOKEN_HEADER]: token, 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!response.ok) {
      const error = await response.json().catch(() => null)
      throw Object.assign(new Error(error?.error || String(response.status)), { status: response.status, code: error?.code })
    }
    return decode(response)
  }
  return {
    open: (signal: AbortSignal) => request<EmbedTicket & { directUrl: string }>('/open', signal, {}),
    session: (ticket: string, signal: AbortSignal) => request<PreviewShareSession>('/session', signal, { ticket }),
    modelCatalog: (signal: AbortSignal) => request<ScopedSessionModelCatalog>('/model-catalog', signal),
    getModel: (signal: AbortSignal) => request<VisitModelChoice>('/model', signal),
    setModel: (signal: AbortSignal, body: object) => request<VisitModelChoice>('/model', signal, body),
    history: (signal: AbortSignal) => request<PreviewChatHistory>('/main-chat', signal),
    send: (message: PreviewChatMessage, signal: AbortSignal) =>
      request<{ ok: boolean }>('/main-chat', signal, message),
    searchReferences: <T>(params: ReferenceSearchParams, signal: AbortSignal) =>
      request<ReferenceSearchResult<T>>(`/references/search?${referenceQuery(params)}`, signal),
    image: (messageId: string, index: number, signal: AbortSignal) =>
      request<Blob>(`/images/${encodeURIComponent(messageId)}/${index}`, signal, undefined, res => res.blob()),
    connectGuest: (ticket: string) => new WebSocket(rootWsUrl(publicPath + '/guest-chat'), ['grasp-preview-guest', ticket]),
    connectMain: (ticket: string) => new WebSocket(rootWsUrl(publicPath + '/main-events'), ['grasp-preview-main', ticket]),
  }
}
