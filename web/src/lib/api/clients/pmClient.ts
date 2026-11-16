import type {
  PmLeaderBinding,
  PmChatThread,
  ChatMessage,
  AttachedContext,
  ClarifyImage,
} from '../../shared/types'
import { req, wsUrl } from '../httpCore'

export const pmClient = {
  // PM Leader
  getPmLeader: () => req<PmLeaderBinding>(`/workspace/pm-leader`),
  updatePmLeader: (body: {
    enabled?: boolean
    agentConfigRef?: string
    enabledMcps?: string[]
    gateAutoVar?: string
    gateAutoPrompt?: string
  }) =>
    req<PmLeaderBinding>(`/workspace/pm-leader`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  listPmThreads: () =>
    req<{ items: PmChatThread[] }>(`/workspace/pm/threads`),
  createPmThread: (body?: { title?: string }) =>
    req<PmChatThread>(`/workspace/pm/threads`, {
      method: 'POST',
      body: JSON.stringify(body || {}),
    }),
  deletePmThread: (tid: string) =>
    req<{ status: string }>(`/workspace/pm/threads/${tid}`, { method: 'DELETE' }),
  listPmMessages: (
    tid: string,
    params?: { limit?: number; before?: string },
  ) => {
    const qs = new URLSearchParams()
    if (params?.limit != null) qs.set('limit', String(params.limit))
    if (params?.before) qs.set('before', params.before)
    const q = qs.toString()
    return req<{ items: ChatMessage[]; hasMore: boolean }>(
      `/workspace/pm/threads/${tid}/messages${q ? `?${q}` : ''}`,
    )
  },
  /** Queue a PM turn: persists the user message (or reuses retryOf); the server readies the sandbox. */
  startPmTurn: (
    tid: string,
    body: { content?: string; images?: ClarifyImage[]; retryOf?: string; attachedContext?: AttachedContext },
  ) =>
    req<{ message: ChatMessage; waiting: number }>(`/workspace/pm/threads/${tid}/turns`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  cancelPmTurn: (tid: string) =>
    req<{ ok: boolean }>(`/workspace/pm/threads/${tid}/turns/cancel`, { method: 'POST' }),
  /** PM thread WebSocket: subscribe-only (snapshot, active-turn replay, live frames). */
  pmThreadChatWsUrl: (tid: string) =>
    wsUrl(`/workspace/pm/threads/${tid}/chat`),
}
