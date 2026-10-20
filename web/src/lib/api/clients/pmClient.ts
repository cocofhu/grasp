import type {
  PmLeaderBinding,
  AgentCronJob,
  PmChatThread,
  ChatMessage,
  ProgressCitation,
  AttachedContext,
  ClarifyImage,
} from '../../shared/types'
import { req, wsUrl } from '../httpCore'
import type { SandboxView } from '../apiTypes'

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
  listCronJobs: () =>
    req<{ items: AgentCronJob[] }>(`/workspace/cron-jobs`),
  patchCronJob: (jobId: string, body: { deliverToChannel: boolean }) =>
    req<AgentCronJob>(`/workspace/cron-jobs/${jobId}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  deleteCronJob: (jobId: string) =>
    req<{ status: string }>(`/workspace/cron-jobs/${jobId}`, { method: 'DELETE' }),
  listPmThreads: () =>
    req<{ items: PmChatThread[] }>(`/workspace/pm/threads`),
  createPmThread: (body?: { title?: string }) =>
    req<PmChatThread>(`/workspace/pm/threads`, {
      method: 'POST',
      body: JSON.stringify(body || {}),
    }),
  getPmThread: (tid: string) =>
    req<PmChatThread>(`/workspace/pm/threads/${tid}`),
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
  appendPmMessage: (
    tid: string,
    body: {
      role?: string
      content: string
      images?: ClarifyImage[]
      citations?: ProgressCitation[]
      attachedContext?: AttachedContext
    },
  ) =>
    req<ChatMessage>(`/workspace/pm/threads/${tid}/messages`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  patchPmMessage: (
    tid: string,
    mid: string,
    body: { status: 'ok' | 'failed' | string; failKind?: string },
  ) =>
    req<ChatMessage>(`/workspace/pm/threads/${tid}/messages/${mid}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  ensurePmSandbox: (
    tid: string,
    body?: { attachedContext?: AttachedContext; injectHistory?: boolean },
  ) =>
    req<{ sandbox: SandboxView; preamble?: string; thread: PmChatThread }>(
      `/workspace/pm/threads/${tid}/sandbox`,
      { method: 'POST', body: JSON.stringify(body || {}) },
    ),
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
