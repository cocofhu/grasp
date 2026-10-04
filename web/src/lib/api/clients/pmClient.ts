import type {
  PmLeaderBinding,
  AgentCronJob,
  ChatThread,
  ChatMessage,
  ProgressCitation,
  AttachedContext,
  ClarifyImage,
} from '../../shared/types'
import { req, wsUrl } from '../httpCore'
import type { SandboxView } from '../apiTypes'

export const pmClient = {
  // PM Leader
  getPmLeader: (projectId: string) => req<PmLeaderBinding>(`/projects/${projectId}/pm-leader`),
  updatePmLeader: (projectId: string, body: {
    enabled?: boolean
    agentConfigRef?: string
    enabledMcps?: string[]
    gateAutoVar?: string
    gateAutoPrompt?: string
  }) =>
    req<PmLeaderBinding>(`/projects/${projectId}/pm-leader`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  listProjectCronJobs: (projectId: string) =>
    req<{ items: AgentCronJob[] }>(`/projects/${projectId}/cron-jobs`),
  patchProjectCronJob: (projectId: string, jobId: string, body: { deliverToChannel: boolean }) =>
    req<AgentCronJob>(`/projects/${projectId}/cron-jobs/${jobId}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  deleteProjectCronJob: (projectId: string, jobId: string) =>
    req<{ status: string }>(`/projects/${projectId}/cron-jobs/${jobId}`, { method: 'DELETE' }),
  listPmThreads: (projectId: string) =>
    req<{ items: ChatThread[] }>(`/projects/${projectId}/pm/threads`),
  createPmThread: (projectId: string, body?: { title?: string }) =>
    req<ChatThread>(`/projects/${projectId}/pm/threads`, {
      method: 'POST',
      body: JSON.stringify(body || {}),
    }),
  getPmThread: (projectId: string, tid: string) =>
    req<ChatThread>(`/projects/${projectId}/pm/threads/${tid}`),
  deletePmThread: (projectId: string, tid: string) =>
    req<{ status: string }>(`/projects/${projectId}/pm/threads/${tid}`, { method: 'DELETE' }),
  listPmMessages: (
    projectId: string,
    tid: string,
    params?: { limit?: number; before?: string },
  ) => {
    const qs = new URLSearchParams()
    if (params?.limit != null) qs.set('limit', String(params.limit))
    if (params?.before) qs.set('before', params.before)
    const q = qs.toString()
    return req<{ items: ChatMessage[]; hasMore?: boolean }>(
      `/projects/${projectId}/pm/threads/${tid}/messages${q ? `?${q}` : ''}`,
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
    req<ChatMessage>(`/projects/${projectId}/pm/threads/${tid}/messages`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  patchPmMessage: (
    projectId: string,
    tid: string,
    mid: string,
    body: { status: 'ok' | 'failed' | string; failKind?: string },
  ) =>
    req<ChatMessage>(`/projects/${projectId}/pm/threads/${tid}/messages/${mid}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  ensurePmSandbox: (
    projectId: string,
    tid: string,
    body?: { attachedContext?: AttachedContext; injectHistory?: boolean },
  ) =>
    req<{ sandbox: SandboxView; preamble?: string; thread: ChatThread }>(
      `/projects/${projectId}/pm/threads/${tid}/sandbox`,
      { method: 'POST', body: JSON.stringify(body || {}) },
    ),
  /** Queue a PM turn: persists the user message (or reuses retryOf); the server readies the sandbox. */
  startPmTurn: (
    projectId: string,
    tid: string,
    body: { content?: string; images?: ClarifyImage[]; retryOf?: string; attachedContext?: AttachedContext },
  ) =>
    req<{ message: ChatMessage; waiting: number }>(`/projects/${projectId}/pm/threads/${tid}/turns`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  cancelPmTurn: (projectId: string, tid: string) =>
    req<{ ok: boolean }>(`/projects/${projectId}/pm/threads/${tid}/turns/cancel`, { method: 'POST' }),
  /** PM thread WebSocket: subscribe-only (snapshot, active-turn replay, live frames). */
  pmThreadChatWsUrl: (projectId: string, tid: string) =>
    wsUrl(`/projects/${projectId}/pm/threads/${tid}/chat`),
}
