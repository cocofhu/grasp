import type { ClarifyImage, ReactAnnotation } from '../../shared/types'
import { req, wsUrl } from '../httpCore'
import type {
  Project,
  ProjectInput,
  ReferenceInput,
  ReferenceSearchResult,
  SoloCreateBody,
  SoloEnvEntry,
  SoloMessage,
  SoloResources,
  SoloSession,
  SoloSessionList,
} from '../apiTypes'
import type { EmbedTicket } from '../../inbox/embedChat'

const enc = encodeURIComponent

type ReferenceSearchParams = { lane: 'solo' | 'github'; q: string; kind?: string; state?: string; page?: number }

function referenceQuery(params: ReferenceSearchParams) {
  const qs = new URLSearchParams({ lane: params.lane, q: params.q })
  if (params.kind) qs.set('kind', params.kind)
  if (params.state) qs.set('state', params.state)
  if (params.page && params.page > 1) qs.set('page', String(params.page))
  return qs
}

/** The Solo page and the bearer-scoped drawer use the same chat contract. */
export function createSoloChatClient(req: <T>(path: string, init?: RequestInit) => Promise<T>, wsUrl: (path: string) => string, path: (id: string) => string) {
  return {
    getSoloSession: (id: string) => req<SoloSession>(path(id)),
    /** Empty modelId selects the primary model. The sandbox changes on the next turn. */
    setSoloSessionModel: (id: string, modelId: string, reasoningEffort?: string, configurationId?: string) =>
      req<SoloSession>(`${path(id)}/model`, { method: 'POST', body: JSON.stringify({ modelId, ...(reasoningEffort === undefined ? {} : { reasoningEffort }), ...(configurationId === undefined ? {} : { configurationId }) }) }),
    /** Omit modelId so changing effort preserves the selected model. */
    setSoloSessionReasoningEffort: (id: string, reasoningEffort: string) =>
      req<SoloSession>(`${path(id)}/model`, { method: 'POST', body: JSON.stringify({ reasoningEffort }) }),
    listSoloMessages: (id: string) => req<SoloMessage[]>(`${path(id)}/messages`),
    sendSoloTurn: (id: string, body: { text: string; images?: ClarifyImage[]; annotations?: ReactAnnotation[]; references?: ReferenceInput[] }) =>
      req<{ waiting: number }>(`${path(id)}/turns`, { method: 'POST', body: JSON.stringify(body) }),
    searchSoloReferences: <T>(id: string, params: ReferenceSearchParams, signal?: AbortSignal) =>
      req<ReferenceSearchResult<T>>(`${path(id)}/references/search?${referenceQuery(params)}`, { signal }),
    cancelSoloTurn: (id: string) =>
      req<{ ok: boolean }>(`${path(id)}/turns/cancel`, { method: 'POST' }),
    removeSoloQueued: (id: string, turnId: string) =>
      req<{ ok: boolean }>(`${path(id)}/queue/${enc(turnId)}`, { method: 'DELETE' }),
    reorderSoloQueue: (id: string, ids: string[]) =>
      req<{ ok: boolean }>(`${path(id)}/queue`, { method: 'PUT', body: JSON.stringify({ ids }) }),
    soloChatWsUrl: (id: string, afterSeq?: number) =>
      wsUrl(`${path(id)}/chat${afterSeq != null && afterSeq >= 0 ? `?after=${afterSeq}` : ''}`),
  }
}

export const soloClient = {
  ...createSoloChatClient(req, wsUrl, id => `/solo/sessions/${enc(id)}`),
  listProjects: () => req<Project[]>('/projects'),
  getProject: (id: string) =>
    req<{ project: Project; activeSessions: number }>(`/projects/${enc(id)}`),
  createProject: (body: ProjectInput) =>
    req<Project>('/projects', { method: 'POST', body: JSON.stringify(body) }),
  updateProject: (id: string, body: ProjectInput) =>
    req<Project>(`/projects/${enc(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteProject: (id: string) =>
    req<{ ok: boolean }>(`/projects/${enc(id)}`, { method: 'DELETE' }),

  listSoloSessions: (params?: { status?: 'active' }) =>
    req<SoloSessionList>(`/solo/sessions${params?.status ? `?status=${params.status}` : ''}`),
  createSoloSession: (body: SoloCreateBody) =>
    req<SoloSession>('/solo/sessions', { method: 'POST', body: JSON.stringify(body) }),
  getSoloResources: (id: string, signal?: AbortSignal) =>
    req<SoloResources>(`/solo/sessions/${enc(id)}/resources`, { signal }),
  soloEmbedTicket: (id: string) =>
    req<EmbedTicket>(`/solo/sessions/${enc(id)}/embed-ticket`, { method: 'POST' }),
  renameSoloSession: (id: string, title: string) =>
    req<SoloSession>(`/solo/sessions/${enc(id)}`, { method: 'PATCH', body: JSON.stringify({ title }) }),
  deleteSoloSession: (id: string) =>
    req<{ ok: boolean }>(`/solo/sessions/${enc(id)}`, { method: 'DELETE' }),
  closeSoloSession: (id: string) =>
    req<SoloSession>(`/solo/sessions/${enc(id)}/close`, { method: 'POST' }),
  restartSoloSession: (id: string) =>
    req<SoloSession>(`/solo/sessions/${enc(id)}/restart`, { method: 'POST' }),
  getSoloEnv: (id: string) => req<SoloEnvEntry[]>(`/solo/sessions/${enc(id)}/env`),
  /** Home workflow composer: the workflow's default repos and first clarify Agent. */
  searchWorkflowReferences: <T>(workflowId: string, params: ReferenceSearchParams, signal?: AbortSignal) =>
    req<ReferenceSearchResult<T>>(`/workflows/${enc(workflowId)}/references/search?${referenceQuery(params)}`, { signal }),
  /** Run-node chats: the run's repos and the node's Agent. */
  searchRunNodeReferences: <T>(runId: string, nodeId: string, params: ReferenceSearchParams, signal?: AbortSignal) =>
    req<ReferenceSearchResult<T>>(`/runs/${enc(runId)}/nodes/${enc(nodeId)}/references/search?${referenceQuery(params)}`, { signal }),
  /** Before a session exists: agent and picked projects stand in for it. */
  searchLaunchReferences: <T>(params: { lane: 'solo' | 'github'; q: string; kind?: string; state?: string; page?: number; agent: string; projects: string[] }, signal?: AbortSignal) => {
    const qs = new URLSearchParams({ lane: params.lane, q: params.q, agent: params.agent })
    if (params.projects.length) qs.set('projects', params.projects.join(','))
    if (params.kind) qs.set('kind', params.kind)
    if (params.state) qs.set('state', params.state)
    if (params.page && params.page > 1) qs.set('page', String(params.page))
    return req<ReferenceSearchResult<T>>(`/solo/references/search?${qs}`, { signal })
  },
}
