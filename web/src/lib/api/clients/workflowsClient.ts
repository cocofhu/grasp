import type { WorkflowCatalogItem } from '../apiTypes'
import type { Workflow, WorkflowVersion, WorkflowNotifyPolicy, WorkflowGraph } from '../../shared/types'
import type { RepoRow } from '../../../components/ui/ReposEditor.vue'
import { req } from '../httpCore'

export const workflowsClient = {
  listWorkflowCatalog: (opts?: { signal?: AbortSignal }) =>
    req<WorkflowCatalogItem[]>('/workflows/catalog', opts?.signal ? { signal: opts.signal } : undefined),
  listWorkflows: (params?: { projectId?: string; signal?: AbortSignal }) => {
    return req<Workflow[]>('/workflows', params?.signal ? { signal: params.signal } : undefined)
  },
  getWorkflow: (id: string, opts?: { signal?: AbortSignal }) =>
    req<Workflow>(`/workflows/${id}`, opts?.signal ? { signal: opts.signal } : undefined),
  saveWorkflow: (wf: Partial<Workflow>) =>
    req<Workflow>(wf.id ? `/workflows/${wf.id}` : '/workflows', {
      method: wf.id ? 'PUT' : 'POST',
      body: JSON.stringify(wf),
    }),
  createWorkflowFromBaseline: (projectId: string, name: string, repos: RepoRow[]) =>
    req<Workflow>('/workflows/from-baseline', {
      method: 'POST',
      body: JSON.stringify({ name, repos }),
    }),
  /** Notify-only: never sends nodes/edges (avoids stale list-cache graph rollback). */
  patchWorkflowNotifyPolicy: (id: string, notifyPolicy: WorkflowNotifyPolicy) =>
    req<Workflow>(`/workflows/${id}/notify-policy`, {
      method: 'PATCH',
      body: JSON.stringify({ notifyPolicy }),
    }),
  /** Home-visibility only: never sends nodes/edges (plan g1.2). */
  patchWorkflowHomeVisibility: (id: string, showOnHome: boolean) =>
    req<Workflow>(`/workflows/${id}/home-visibility`, {
      method: 'PATCH',
      body: JSON.stringify({ showOnHome }),
    }),
  publishWorkflow: (id: string) => req<Workflow>(`/workflows/${id}/publish`, { method: 'POST' }),
  listAPIKeys: (workflowId: string) =>
    req<{ id: string; name: string; key_prefix: string; created_at: string }[]>(`/workflows/${workflowId}/api-keys`),
  createAPIKey: (workflowId: string, name: string) =>
    req<{ id: string; name: string; key: string; key_prefix: string; created_at: string }>(`/workflows/${workflowId}/api-keys`, {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),
  revokeAPIKey: (workflowId: string, keyId: string) =>
    req<{ status: string }>(`/workflows/${workflowId}/api-keys/${keyId}`, { method: 'DELETE' }),
  deleteWorkflow: (id: string) => req<{ status: string }>(`/workflows/${id}`, { method: 'DELETE' }),
  listWorkflowVersions: (id: string, opts?: { signal?: AbortSignal }) =>
    req<WorkflowVersion[]>(`/workflows/${id}/versions`, opts?.signal ? { signal: opts.signal } : undefined),
  restoreWorkflowVersion: (id: string, version: number) =>
    req<Workflow>(`/workflows/${id}/versions/${version}/restore`, { method: 'POST' }),
  copyPreviewWorkflow: (id: string) =>
    req<{ suggestedName: string; sourceName: string; sourceId: string }>(`/workflows/${id}/copy-preview`),
  copyWorkflow: (id: string, name: string, opts?: { signal?: AbortSignal }) =>
    req<Workflow>(`/workflows/${id}/copy`, {
      method: 'POST',
      body: JSON.stringify({ name }),
      ...(opts?.signal ? { signal: opts.signal } : {}),
    }),
  getWorkflowVersionGraph: (id: string, version: number, opts?: { signal?: AbortSignal }) =>
    req<WorkflowGraph>(
      `/workflows/${id}/versions/${version}/graph`,
      opts?.signal ? { signal: opts.signal } : undefined,
    ),
  importWorkflow: (json: string, _workspaceId?: string) => {
    return req<Workflow>('/workflows/import', { method: 'POST', body: json })
  },
}
