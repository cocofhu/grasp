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
  getPmLeader: (_workspaceId: string) => req<PmLeaderBinding>(`/workspace/pm-leader`),
  updatePmLeader: (projectId: string, body: {
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
  listProjectCronJobs: (_workspaceId: string) =>
    req<{ items: AgentCronJob[] }>(`/workspace/cron-jobs`),
  patchProjectCronJob: (projectId: string, jobId: string, body: { deliverToChannel: boolean }) =>
    req<AgentCronJob>(`/workspace/cron-jobs/${jobId}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  deleteProjectCronJob: (projectId: string, jobId: string) =>
    req<{ status: string }>(`/workspace/cron-jobs/${jobId}`, { method: 'DELETE' }),
  listPmThreads: (_workspaceId: string) =>
    req<{ items: PmChatThread[] }>(`/workspace/pm/threads`),
  createPmThread: (projectId: string, body?: { title?: string }) =>
    req<PmChatThread>(`/workspace/pm/threads`, {
      method: 'POST',
      body: JSON.stringify(body || {}),
    }),
  getPmThread: (projectId: string, tid: string) =>
    req<PmChatThread>(`/workspace/pm/threads/${tid}`),
  deletePmThread: (projectId: string, tid: string) =>
    req<{ status: string }>(`/workspace/pm/threads/${tid}`, { method: 'DELETE' }),
  listPmMessages: (
    projectId: string,
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
    projectId: string,
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
    projectId: string,
    tid: string,
    mid: string,
    body: { status: 'ok' | 'failed' | string; failKind?: string },
  ) =>
    req<ChatMessage>(`/workspace/pm/threads/${tid}/messages/${mid}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  ensurePmSandbox: (
    projectId: string,
    tid: string,
    body?: { attachedContext?: AttachedContext; injectHistory?: boolean },
  ) =>
    req<{ sandbox: SandboxView; preamble?: string; thread: PmChatThread }>(
      `/workspace/pm/threads/${tid}/sandbox`,
      { method: 'POST', body: JSON.stringify(body || {}) },
    ),
  /** Queue a PM turn: persists the user message (or reuses retryOf); the server readies the sandbox. */
  startPmTurn: (
    projectId: string,
    tid: string,
    body: { content?: string; images?: ClarifyImage[]; retryOf?: string; attachedContext?: AttachedContext },
  ) =>
    req<{ message: ChatMessage; waiting: number }>(`/workspace/pm/threads/${tid}/turns`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  cancelPmTurn: (projectId: string, tid: string) =>
    req<{ ok: boolean }>(`/workspace/pm/threads/${tid}/turns/cancel`, { method: 'POST' }),
  /** PM thread WebSocket: subscribe-only (snapshot, active-turn replay, live frames). */
  pmThreadChatWsUrl: (projectId: string, tid: string) =>
    wsUrl(`/workspace/pm/threads/${tid}/chat`),
}
