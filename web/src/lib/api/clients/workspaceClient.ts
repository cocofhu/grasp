import type {
  Project,
  ProjectAuditEvent,
  ProjectAuditFacets,
  ProjectAuditStats,
  RequirementDraft,
  RequirementDraftCreateBody,
  RequirementDraftSchedulePatch,
} from '../../shared/types'
import { origin, req } from '../httpCore'
import type {
  CreateProjectSharedAgentTestPayload,
  PaginatedResponse,
  ProjectCredentialItem,
  ProjectCredentialPutBody,
  ProjectCredentialsResponse,
  ProjectSharedAgentConfig,
  SandboxView,
} from '../apiTypes'

export const workspaceClient = {
  updateWorkspace: (body: Partial<Project>) =>
    req<Project>('/workspace', { method: 'PATCH', body: JSON.stringify(body) }),
  getWorkspace: (opts?: { signal?: AbortSignal }) =>
    req<Project>('/workspace', opts?.signal ? { signal: opts.signal } : undefined),
  /** Project shared Agent baseline (extend layer before Agent overlay). */
  getProjectSharedAgentConfig: (_workspaceId: string) =>
    req<ProjectSharedAgentConfig>(`/workspace/shared-agent-config`),
  putProjectSharedAgentConfig: (
    id: string,
    body: Omit<ProjectSharedAgentConfig, 'projectId'> & { projectId?: string },
  ) =>
    req<ProjectSharedAgentConfig>(`/workspace/shared-agent-config`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  /** Project-context chat test: extend shared config then overlay chosen Agent. */
  createProjectSharedAgentTest: (id: string, body: CreateProjectSharedAgentTestPayload) =>
    req<SandboxView>(`/workspace/shared-agent-config/test`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  /** Project-owned credentials. GET never returns plaintext; PUT accepts a new value. */
  getProjectCredentials: (_workspaceId: string) =>
    req<ProjectCredentialsResponse>(`/workspace/credentials`),
  createProjectCredential: (projectId: string, body: ProjectCredentialPutBody) =>
    req<ProjectCredentialItem>(
      `/workspace/credentials`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  putProjectCredential: (projectId: string, credentialId: string, body: ProjectCredentialPutBody) =>
    req<ProjectCredentialItem>(
      `/workspace/credentials/${encodeURIComponent(credentialId)}`,
      { method: 'PUT', body: JSON.stringify(body) },
    ),
  deleteProjectCredential: (projectId: string, credentialId: string) =>
    req<{ status: string }>(
      `/workspace/credentials/${encodeURIComponent(credentialId)}`,
      { method: 'DELETE' },
    ),

  listRequirementDrafts: (
    projectId: string,
    params?: { status?: 'open' | 'done' | 'all'; q?: string },
  ) => {
    const q = new URLSearchParams()
    if (params?.status) q.set('status', params.status)
    if (params?.q) q.set('q', params.q)
    const qs = q.toString()
    return req<{ items: RequirementDraft[] }>(
      `/workspace/requirement-drafts${qs ? `?${qs}` : ''}`,
    )
  },
  getRequirementDraft: (projectId: string, draftId: string) =>
    req<RequirementDraft>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}`,
    ),
  createRequirementDraft: (projectId: string, body?: RequirementDraftCreateBody) =>
    req<RequirementDraft>(
      `/workspace/requirement-drafts`,
      {
        method: 'POST',
        body: body ? JSON.stringify(body) : undefined,
      },
    ),
  updateRequirementDraft: (
    projectId: string,
    draftId: string,
    body: { title: string; bodyMarkdown?: string },
  ) =>
    req<RequirementDraft>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}`,
      { method: 'PUT', body: JSON.stringify(body) },
    ),
  patchRequirementDraftStatus: (
    projectId: string,
    draftId: string,
    status: 'open' | 'done',
  ) =>
    req<RequirementDraft>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}/status`,
      { method: 'PATCH', body: JSON.stringify({ status }) },
    ),
  patchRequirementDraftSchedule: (
    projectId: string,
    draftId: string,
    body: RequirementDraftSchedulePatch,
  ) => {
    const payload: Record<string, unknown> = {}
    if (body.kind !== undefined) payload.kind = body.kind
    if (body.startAt !== undefined) payload.startAt = body.startAt
    if (body.dueAt !== undefined) payload.dueAt = body.dueAt
    if (body.progress !== undefined) payload.progress = body.progress
    if (body.parentId !== undefined) {
      payload.parentId = body.parentId == null ? '' : body.parentId
    }
    return req<RequirementDraft>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}/schedule`,
      { method: 'PATCH', body: JSON.stringify(payload) },
    )
  },
  deleteRequirementDraft: (projectId: string, draftId: string) =>
    req<{ status: string }>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}`,
      { method: 'DELETE' },
    ),

  getProjectExternalMcp: (_workspaceId: string) =>
    req<{ enabled: boolean; enabledPacks: string[]; mcpBaseUrl: string; updatedAt?: string }>(
      `/workspace/external-mcp`,
    ),
  updateProjectExternalMcp: (
    projectId: string,
    body: { enabled: boolean; enabledPacks: string[] },
  ) =>
    req<{ enabled: boolean; enabledPacks: string[]; mcpBaseUrl: string; updatedAt?: string }>(
      `/workspace/external-mcp`,
      { method: 'PUT', body: JSON.stringify(body) },
    ),
  listProjectMcpKeys: (_workspaceId: string) =>
    req<{ id: string; name: string; key_prefix: string; created_at: string }[]>(
      `/workspace/external-mcp/keys`,
    ),
  createProjectMcpKey: (projectId: string, name: string) =>
    req<{ id: string; name: string; key: string; key_prefix: string; created_at: string }>(
      `/workspace/external-mcp/keys`,
      { method: 'POST', body: JSON.stringify({ name }) },
    ),
  revokeProjectMcpKey: (projectId: string, keyId: string) =>
    req<{ status: string }>(
      `/workspace/external-mcp/keys/${encodeURIComponent(keyId)}`,
      { method: 'DELETE' },
    ),

  /** Project audit timeline (paginated). Default time window: 24h. */
  listProjectAudit: (
    id: string,
    params?: {
      time?: string
      actor?: string
      callerKind?: string
      action?: string
      resource?: string
      runId?: string
      nodeId?: string
      search?: string
      from?: string
      to?: string
      page?: number
      pageSize?: number
    },
  ) => {
    const q = new URLSearchParams()
    q.set('time', params?.time || '24h')
    if (params?.actor) q.set('actor', params.actor)
    if (params?.callerKind) q.set('callerKind', params.callerKind)
    if (params?.action) q.set('action', params.action)
    if (params?.resource) q.set('resource', params.resource)
    if (params?.runId) q.set('runId', params.runId)
    if (params?.nodeId) q.set('nodeId', params.nodeId)
    if (params?.search) q.set('search', params.search)
    if (params?.from) q.set('from', params.from)
    if (params?.to) q.set('to', params.to)
    q.set('page', String(params?.page ?? 1))
    q.set('pageSize', String(params?.pageSize ?? 20))
    return req<PaginatedResponse<ProjectAuditEvent> & { stats?: ProjectAuditStats }>(
      `/workspace/audit?${q}`,
    )
  },

  /** Run / node / resource options for dual-mode audit filters. */
  listProjectAuditFacets: (
    id: string,
    params?: {
      time?: string
      runId?: string
      from?: string
      to?: string
    },
  ) => {
    const q = new URLSearchParams()
    q.set('time', params?.time || '24h')
    if (params?.runId) q.set('runId', params.runId)
    if (params?.from) q.set('from', params.from)
    if (params?.to) q.set('to', params.to)
    return req<ProjectAuditFacets>(`/workspace/audit/facets?${q}`)
  },

  /** Download URL for audit export (JSON); triggers meta-audit on server. */
  exportProjectAuditUrl: (
    id: string,
    params?: {
      format?: 'json' | 'text'
      time?: string
      actor?: string
      callerKind?: string
      action?: string
      resource?: string
      runId?: string
      nodeId?: string
      search?: string
    },
  ) => {
    const q = new URLSearchParams()
    q.set('format', params?.format || 'json')
    q.set('time', params?.time || '24h')
    if (params?.actor) q.set('actor', params.actor)
    if (params?.callerKind) q.set('callerKind', params.callerKind)
    if (params?.action) q.set('action', params.action)
    if (params?.resource) q.set('resource', params.resource)
    if (params?.runId) q.set('runId', params.runId)
    if (params?.nodeId) q.set('nodeId', params.nodeId)
    if (params?.search) q.set('search', params.search)
    return `${origin()}/api/workspace/audit/export?${q}`
  },

}
