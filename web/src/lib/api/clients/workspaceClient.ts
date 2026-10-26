import type {
  Workspace,
  AuditEvent,
  AuditFacets,
  AuditStats,
  RequirementDraft,
  RequirementDraftCreateBody,
  RequirementDraftSchedulePatch,
} from '../../shared/types'
import { origin, req } from '../httpCore'
import type {
  CreateSharedAgentTestPayload,
  PaginatedResponse,
  CredentialItem,
  CredentialPutBody,
  CredentialsResponse,
  SharedAgentConfig,
  SandboxView,
  ModelConnection,
  ModelConnectionBody,
  ToolCapability,
  ModelConfiguration,
  ModelConfigurationBody,
} from '../apiTypes'

export const workspaceClient = {
  listModelConfigurations: () => req<{ items: ModelConfiguration[] }>('/workspace/model-configurations'),
  getModelConfiguration: (id: string) => req<ModelConfiguration>(`/workspace/model-configurations/${encodeURIComponent(id)}`),
  createModelConfiguration: (body: ModelConfigurationBody) => req<ModelConfiguration>('/workspace/model-configurations', { method: 'POST', body: JSON.stringify(body) }),
  updateModelConfiguration: (id: string, body: Partial<ModelConfigurationBody>) => req<ModelConfiguration>(`/workspace/model-configurations/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteModelConfiguration: (id: string) => req<{ status: string }>(`/workspace/model-configurations/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  listModelConnections: () => req<{ items: ModelConnection[] }>('/workspace/model-connections'),
  listModelConnectionModels: (id: string) => req<{ models: ModelConnection['models']; error?: string }>(`/workspace/model-connections/${encodeURIComponent(id)}/models`),
  createModelConnection: (body: ModelConnectionBody) => req<ModelConnection>('/workspace/model-connections', { method: 'POST', body: JSON.stringify(body) }),
  updateModelConnection: (id: string, body: Partial<ModelConnectionBody>) => req<ModelConnection>(`/workspace/model-connections/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteModelConnection: (id: string) => req<{ status: string }>(`/workspace/model-connections/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  listToolCapabilities: () => req<{ items: ToolCapability[] }>('/workspace/tool-capabilities'),
  updateWorkspace: (body: Partial<Workspace>) =>
    req<Workspace>('/workspace', { method: 'PATCH', body: JSON.stringify(body) }),
  getWorkspace: (opts?: { signal?: AbortSignal }) =>
    req<Workspace>('/workspace', opts?.signal ? { signal: opts.signal } : undefined),
  /** Workspace shared Agent baseline (extend layer before Agent overlay). */
  getSharedAgentConfig: () =>
    req<SharedAgentConfig>(`/workspace/shared-agent-config`),
  putSharedAgentConfig: (
    body: SharedAgentConfig,
  ) =>
    req<SharedAgentConfig>(`/workspace/shared-agent-config`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  /** Workspace-context chat test: extend shared config then overlay chosen Agent. */
  createSharedAgentTest: (body: CreateSharedAgentTestPayload) =>
    req<SandboxView>(`/workspace/shared-agent-config/test`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  /** Workspace-owned credentials. GET never returns plaintext; PUT accepts a new value. */
  listCredentials: () =>
    req<CredentialsResponse>(`/workspace/credentials`),
  createCredential: (body: CredentialPutBody) =>
    req<CredentialItem>(
      `/workspace/credentials`,
      { method: 'POST', body: JSON.stringify(body) },
    ),
  putCredential: (credentialId: string, body: CredentialPutBody) =>
    req<CredentialItem>(
      `/workspace/credentials/${encodeURIComponent(credentialId)}`,
      { method: 'PUT', body: JSON.stringify(body) },
    ),
  deleteCredential: (credentialId: string) =>
    req<{ status: string }>(
      `/workspace/credentials/${encodeURIComponent(credentialId)}`,
      { method: 'DELETE' },
    ),
  /** Removes a workspace-owned API account. DELETE without this suffix only clears the secret. */
  removeCredential: (credentialId: string) =>
    req<{ status: string }>(
      `/workspace/credentials/${encodeURIComponent(credentialId)}/account`,
      { method: 'DELETE' },
    ),

  listRequirementDrafts: (
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
  getRequirementDraft: (draftId: string) =>
    req<RequirementDraft>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}`,
    ),
  createRequirementDraft: (body?: RequirementDraftCreateBody) =>
    req<RequirementDraft>(
      `/workspace/requirement-drafts`,
      {
        method: 'POST',
        body: body ? JSON.stringify(body) : undefined,
      },
    ),
  updateRequirementDraft: (
    draftId: string,
    body: { title: string; bodyMarkdown?: string },
  ) =>
    req<RequirementDraft>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}`,
      { method: 'PUT', body: JSON.stringify(body) },
    ),
  patchRequirementDraftStatus: (
    draftId: string,
    status: 'open' | 'done',
  ) =>
    req<RequirementDraft>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}/status`,
      { method: 'PATCH', body: JSON.stringify({ status }) },
    ),
  patchRequirementDraftSchedule: (
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
  deleteRequirementDraft: (draftId: string) =>
    req<{ status: string }>(
      `/workspace/requirement-drafts/${encodeURIComponent(draftId)}`,
      { method: 'DELETE' },
    ),

  getExternalMcp: () =>
    req<{ enabled: boolean; enabledPacks: string[]; mcpBaseUrl: string; updatedAt?: string }>(
      `/workspace/external-mcp`,
    ),
  updateExternalMcp: (
    body: { enabled: boolean; enabledPacks: string[] },
  ) =>
    req<{ enabled: boolean; enabledPacks: string[]; mcpBaseUrl: string; updatedAt?: string }>(
      `/workspace/external-mcp`,
      { method: 'PUT', body: JSON.stringify(body) },
    ),
  listMcpKeys: () =>
    req<{ id: string; name: string; key_prefix: string; created_at: string }[]>(
      `/workspace/external-mcp/keys`,
    ),
  createMcpKey: (name: string) =>
    req<{ id: string; name: string; key: string; key_prefix: string; created_at: string }>(
      `/workspace/external-mcp/keys`,
      { method: 'POST', body: JSON.stringify({ name }) },
    ),
  revokeMcpKey: (keyId: string) =>
    req<{ status: string }>(
      `/workspace/external-mcp/keys/${encodeURIComponent(keyId)}`,
      { method: 'DELETE' },
    ),

  /** Workspace audit timeline (paginated). Default time window: 24h. */
  listAudit: (
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
    return req<PaginatedResponse<AuditEvent> & { stats?: AuditStats }>(
      `/workspace/audit?${q}`,
    )
  },

  /** Run / node / resource options for dual-mode audit filters. */
  listAuditFacets: (
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
    return req<AuditFacets>(`/workspace/audit/facets?${q}`)
  },

  /** Download URL for audit export (JSON); triggers meta-audit on server. */
  exportAuditUrl: (
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
