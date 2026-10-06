import type {
  AgentCronJob,
  ChatThread,
  ChatMessage,
  ProjectMemoryItem,
} from '../../shared/types'
import type { BackendId } from '../../shared/regionPolicy'
import { BASE, req } from '../httpCore'
import type {
  Agent,
  AgentTemplate,
  OpenCodeModelsResponse,
  OpenCodeProvidersResponse,
  ProjectAgentsImportResult,
  TeamBootstrapRequest,
  TeamBootstrapSession,
} from '../apiTypes'

function filenameFromContentDisposition(header: string | null, fallback: string): string {
  if (!header) return fallback
  const star = /filename\*=(?:UTF-8''|utf-8'')([^;]+)/i.exec(header)
  if (star?.[1]) {
    try {
      return decodeURIComponent(star[1])
    } catch {
      /* ignore */
    }
  }
  const quoted = /filename="([^"]+)"/i.exec(header)
  if (quoted?.[1]) return quoted[1]
  const plain = /filename=([^;]+)/i.exec(header)
  if (plain?.[1]) return plain[1].trim()
  return fallback
}

export const agentsClient = {
  // agents (reusable, user-defined Agent identities referenced by agent_profile:
  // skill/rules + MCP servers + environment variables)
  listAgents: () => req<Agent[]>('/agents'),
  getAgent: (name: string) => req<Agent>(`/agents/${encodeURIComponent(name)}`),
  bootstrapProjectOnboarding: (
    projectId: string,
    body: {
      acpBackend: BackendId
      apiKey: string
      language?: string
      region?: string
      gitCredentialType?: string
      githubToken?: string
      gitlabToken?: string
      gitlabUrl?: string
      sshPrivateKey?: string
      sshKnownHosts?: string
      repoUrl?: string
      repoBranch?: string
      gitUserName?: string
      gitUserEmail?: string
      vncPreview?: boolean
      browserMcp?: boolean
      openCodeProvider?: string
      openCodeBaseURL?: string
      openCodeModel?: string
      openCodeModelVision?: boolean
      agents?: { templateId: string; name?: string; model?: string }[]
    },
  ) =>
    req<{
      agentIds: string[]
      workflowId: string
      published: boolean
    }>(`/projects/${encodeURIComponent(projectId)}/bootstrap-onboarding`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  bootstrapAgentTeam: (body: TeamBootstrapRequest) =>
    req<TeamBootstrapSession>('/agent-teams/bootstrap', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  getAgentTeamBootstrap: (id: string) =>
    req<TeamBootstrapSession>(`/agent-teams/bootstrap/${encodeURIComponent(id)}`),
  retryAgentTeamBootstrap: (id: string) =>
    req<TeamBootstrapSession>(`/agent-teams/bootstrap/${encodeURIComponent(id)}/retry`, {
      method: 'POST',
    }),
  listAgentTeamTemplates: () =>
    req<{ items: AgentTemplate[] }>('/agent-teams/templates'),
  createAgent: (agent: Agent) =>
    req<Agent>('/agents', { method: 'POST', body: JSON.stringify(agent) }),
  saveAgent: (agent: Agent, opts?: { reason?: string }) =>
    req<{ status: string }>(`/agents/${encodeURIComponent(agent.name)}`, {
      method: 'PUT',
      body: JSON.stringify({ ...agent, reason: opts?.reason }),
    }),
  renameAgent: (name: string, newName: string) =>
    req<Agent & { updatedWorkflowCount?: number }>(`/agents/${encodeURIComponent(name)}/rename`, {
      method: 'POST',
      body: JSON.stringify({ name: newName }),
    }),
  deleteAgent: (name: string) =>
    req<{ status: string }>(`/agents/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  listAgentWorkspaceRevisions: (name: string) =>
    req<{ revisions: import('../apiTypes').WorkspaceRevision[] }>(
      `/agents/${encodeURIComponent(name)}/workspace/revisions`,
    ),
  getAgentWorkspaceRevisionDiff: (name: string, sha: string) =>
    req<{ sha: string; diff: string }>(
      `/agents/${encodeURIComponent(name)}/workspace/revisions/${encodeURIComponent(sha)}/diff`,
    ),
  restoreAgentWorkspaceRevision: (name: string, sha: string, reason?: string) =>
    req<{ status: string; sha: string; agent: Agent }>(
      `/agents/${encodeURIComponent(name)}/workspace/revisions/${encodeURIComponent(sha)}/restore`,
      { method: 'POST', body: JSON.stringify({ reason: reason || '' }) },
    ),

  // Agent-scoped data (Studio). Project resolved server-side from agent.projectId.
  listAgentMemories: (name: string) =>
    req<{ items: ProjectMemoryItem[] }>(`/agents/${encodeURIComponent(name)}/memories`),
  upsertAgentMemory: (name: string, body: { title: string; content: string }) =>
    req<ProjectMemoryItem>(`/agents/${encodeURIComponent(name)}/memories`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  updateAgentMemory: (name: string, mid: string, body: { title?: string; content?: string }) =>
    req<ProjectMemoryItem>(`/agents/${encodeURIComponent(name)}/memories/${mid}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteAgentMemory: (name: string, mid: string) =>
    req<{ status: string }>(`/agents/${encodeURIComponent(name)}/memories/${mid}`, { method: 'DELETE' }),
  clearAgentMemories: (name: string) =>
    req<{ status: string; count: number }>(`/agents/${encodeURIComponent(name)}/memories`, { method: 'DELETE' }),
  listAgentThreads: (name: string) =>
    req<{ items: ChatThread[]; messageCounts: Record<string, number> }>(
      `/agents/${encodeURIComponent(name)}/threads`,
    ),
  listAgentThreadMessages: (name: string, tid: string) =>
    req<{ items: ChatMessage[]; total: number }>(
      `/agents/${encodeURIComponent(name)}/threads/${tid}/messages`,
    ),
  deleteAgentThread: (name: string, tid: string) =>
    req<{ status: string }>(`/agents/${encodeURIComponent(name)}/threads/${tid}`, { method: 'DELETE' }),
  listAgentCronJobs: (name: string) =>
    req<{ items: AgentCronJob[] }>(`/agents/${encodeURIComponent(name)}/cron-jobs`),
  patchAgentCronJob: (name: string, jobId: string, body: { enabled?: boolean; deliverToChannel?: boolean }) =>
    req<AgentCronJob>(`/agents/${encodeURIComponent(name)}/cron-jobs/${jobId}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  deleteAgentCronJob: (name: string, jobId: string) =>
    req<{ status: string }>(`/agents/${encodeURIComponent(name)}/cron-jobs/${jobId}`, { method: 'DELETE' }),

  exportAgent: async (name: string): Promise<Blob> => {
    const res = await fetch(`${BASE}/agents/${encodeURIComponent(name)}/export`, {
      credentials: 'include',
    })
    if (!res.ok) {
      let msg = `${res.status} export failed`
      try {
        const body = await res.json()
        if (body?.error) msg = body.error
      } catch {
        // non-JSON
      }
      throw new Error(msg)
    }
    return res.blob()
  },
  exportProjectAgents: async (projectId: string): Promise<{ blob: Blob; filename: string }> => {
    const res = await fetch(`${BASE}/projects/${encodeURIComponent(projectId)}/agents/export`, {
      credentials: 'include',
    })
    if (!res.ok) {
      let msg = `${res.status} export failed`
      try {
        const body = await res.json()
        if (body?.error) msg = body.error
      } catch {
        // non-JSON
      }
      throw new Error(msg)
    }
    const blob = await res.blob()
    const filename = filenameFromContentDisposition(
      res.headers.get('Content-Disposition'),
      'project.zip',
    )
    return { blob, filename }
  },
  importProjectAgents: async (
    projectId: string,
    zipFile: File,
    opts: { mode: 'rename' | 'overwrite' },
  ): Promise<ProjectAgentsImportResult> => {
    const fd = new FormData()
    fd.append('file', zipFile)
    fd.append('mode', opts.mode)
    const res = await fetch(`${BASE}/projects/${encodeURIComponent(projectId)}/agents/import`, {
      method: 'POST',
      credentials: 'include',
      body: fd,
    })
    if (!res.ok) {
      let msg = `${res.status} import failed`
      try {
        const body = await res.json()
        if (body?.error) msg = body.error
      } catch {
        // non-JSON
      }
      throw new Error(msg)
    }
    return (await res.json()) as ProjectAgentsImportResult
  },
  /** Vendors OpenCode can resolve models for (models.dev, cached server-side). */
  openCodeProviders: () => req<OpenCodeProvidersResponse>(`/opencode/providers`),
  /** One vendor's models; empty for a gateway the catalog does not know. */
  openCodeModels: (provider: string) =>
    req<OpenCodeModelsResponse>(`/opencode/providers/${encodeURIComponent(provider)}/models`),
  importAgent: async (
    zipFile: File,
    opts: { projectId: string; targetName: string; mode: 'create' | 'overwrite' },
  ): Promise<Agent> => {
    const fd = new FormData()
    fd.append('file', zipFile)
    fd.append('projectId', opts.projectId)
    fd.append('targetName', opts.targetName)
    fd.append('mode', opts.mode)
    const res = await fetch(`${BASE}/agents/import`, {
      method: 'POST',
      credentials: 'include',
      body: fd,
    })
    if (!res.ok) {
      let msg = `${res.status} import failed`
      try {
        const body = await res.json()
        if (body?.error) msg = body.error
      } catch {
        // non-JSON
      }
      throw new Error(msg)
    }
    return (await res.json()) as Agent
  },
}
