import type {
  Run,
  Artifact,
  GateShareInboxStatus,
  AcpEvent,
  ClarifyImage,
  ReactAnnotation,
} from '../../shared/types'
import type { InboxContextResponse } from '../../inbox/inboxContext'
import type { EmbedTicket } from '../../inbox/embedChat'
import { apiState, BASE, origin, req, wsUrl } from '../httpCore'
import type {
  EventPaginatedResponse,
  NodeEventsResponse,
  PaginatedResponse,
  PreviewIssue,
  PreviewPort,
  SandboxView,
} from '../apiTypes'

/** Body fragment for startRun's opening message; blank messages are omitted. */
function startRunFirstMessage(
  msg?: { text?: string; images?: ClarifyImage[] } | null,
): { firstMessage: { text: string; images: ClarifyImage[] } } | null {
  const text = String(msg?.text ?? '').trim()
  const images = msg?.images ?? []
  if (!text && !images.length) return null
  return { firstMessage: { text, images } }
}

export const runsClient = {
  // runs
  listRuns: (params?: {
    status?: string
    tag?: string
    wf?: string
    projectId?: string
    page?: number
    pageSize?: number
    /** Whitelist: started_at | priority. Only sent when paired with a valid order. */
    sort?: string
    /** Whitelist: asc | desc. Only sent when paired with a valid sort. */
    order?: 'asc' | 'desc' | string
    signal?: AbortSignal
  }) => {
    const qs = new URLSearchParams()
    if (params?.status) qs.set('status', params.status)
    if (params?.tag) qs.set('tag', params.tag)
    if (params?.wf) qs.set('wf', params.wf)
    if (params?.projectId) qs.set('projectId', params.projectId)
    if (params?.page != null) qs.set('page', String(params.page))
    if (params?.pageSize != null) qs.set('pageSize', String(params.pageSize))
    const sort = params?.sort
    const order = params?.order
    if (
      (sort === 'started_at' || sort === 'priority') &&
      (order === 'asc' || order === 'desc')
    ) {
      qs.set('sort', sort)
      qs.set('order', order)
    }
    const q = qs.toString()
    const path = q ? `/runs?${q}` : '/runs'
    const init = params?.signal ? { signal: params.signal } : undefined
    if (params?.page != null || params?.pageSize != null) {
      return req<PaginatedResponse<Run>>(path, init)
    }
    return req<Run[]>(path, init)
  },
  getRun: (id: string) => req<Run>(`/runs/${id}`),
  runArtifacts: (id: string) => req<Artifact[]>(`/runs/${id}/artifacts`),
  inboxContext: (
    runId: string,
    nodeId: string,
    iteration: number,
    opts?: { signal?: AbortSignal },
  ) =>
    req<InboxContextResponse>(
      `/runs/${runId}/inbox-context?nodeId=${encodeURIComponent(nodeId)}&iteration=${iteration}`,
      opts?.signal ? { signal: opts.signal } : undefined,
    ),
  listProjectRunTags: (projectId: string) =>
    req<{ tags: string[] }>(`/projects/${encodeURIComponent(projectId)}/run-tags`),
  startRun: (
    workflowId: string,
    inputs: Record<string, any>,
    trigger = 'manual',
    priority = 'normal',
    tags: string[] = [],
    opts?: {
      signal?: AbortSignal
      env?: { key: string; value: string; secret?: boolean }[]
      title?: string
      /** Opening chat message; the engine delivers it once the approve node parks. */
      firstMessage?: { text: string; images?: ClarifyImage[] }
    },
  ) =>
    req<{ id: string; status: string; priority?: string }>(`/workflows/${workflowId}/runs`, {
      method: 'POST',
      body: JSON.stringify({
        inputs,
        trigger,
        priority,
        tags,
        ...(opts?.env && opts.env.length ? { env: opts.env } : {}),
        ...(opts?.title && opts.title.trim() ? { title: opts.title.trim() } : {}),
        ...(startRunFirstMessage(opts?.firstMessage) ?? {}),
      }),
      ...(opts?.signal ? { signal: opts.signal } : {}),
    }),
  updateRunPriority: (id: string, priority: string) =>
    req<{ id: string; status: string; priority: string }>(`/runs/${id}/priority`, {
      method: 'PATCH',
      body: JSON.stringify({ priority }),
    }),
  cancelRun: (id: string) => req<{ status: string }>(`/runs/${id}/cancel`, { method: 'POST' }),
  // Hard-delete a completed/failed run. Missing → 404; non-deletable status → 409.
  deleteRun: (id: string) => req<{ status: string }>(`/runs/${id}`, { method: 'DELETE' }),
  resumeRun: (id: string, nodeId = '') =>
    req<{ status: string }>(`/runs/${id}/resume`, {
      method: 'POST',
      body: JSON.stringify({ nodeId }),
    }),
  runEventsWsUrl: (id: string) => wsUrl(`/runs/${id}/events`),
  resumeGate: (runId: string, nodeId: string, action: string, form: Record<string, any> = {}) =>
    req<{ status: string }>(`/runs/${runId}/gates/${nodeId}/resume`, {
      method: 'POST',
      body: JSON.stringify({ action, form }),
    }),
  createGateShareLink: (runId: string, nodeId: string, ttlTier = '24h', permissionPreset = 'full') =>
    req<{ id: string; url: string; ttlTier: string; permissionPreset: string; expiresAt: string; state: string }>(
      `/runs/${runId}/gates/${nodeId}/share-link`,
      { method: 'POST', body: JSON.stringify({ ttlTier, permissionPreset }) },
    ),
  getGateShareLink: (runId: string, nodeId: string) =>
    req<GateShareInboxStatus>(`/runs/${runId}/gates/${nodeId}/share-link`),
  regenGateShareLink: (runId: string, nodeId: string) =>
    req<{ id: string; url: string; ttlTier: string; permissionPreset: string; expiresAt: string; state: string }>(
      `/runs/${runId}/gates/${nodeId}/share-link/regen`,
      { method: 'POST' },
    ),
  revokeGateShareLink: (runId: string, nodeId: string) =>
    req<{ status: string }>(`/runs/${runId}/gates/${nodeId}/share-link/revoke`, { method: 'POST' }),
  createReviewShareLink: (runId: string, nodeId: string, ttlTier = '24h', permissionPreset = 'full') =>
    req<{ id: string; url: string; ttlTier: string; permissionPreset: string; expiresAt: string; state: string }>(
      `/runs/${runId}/reviews/${nodeId}/share-link`,
      { method: 'POST', body: JSON.stringify({ ttlTier, permissionPreset }) },
    ),
  getReviewShareLink: (runId: string, nodeId: string) =>
    req<GateShareInboxStatus>(`/runs/${runId}/reviews/${nodeId}/share-link`),
  regenReviewShareLink: (runId: string, nodeId: string) =>
    req<{ id: string; url: string; ttlTier: string; permissionPreset: string; expiresAt: string; state: string }>(
      `/runs/${runId}/reviews/${nodeId}/share-link/regen`,
      { method: 'POST' },
    ),
  revokeReviewShareLink: (runId: string, nodeId: string) =>
    req<{ status: string }>(`/runs/${runId}/reviews/${nodeId}/share-link/revoke`, { method: 'POST' }),
  listGatePrimaryArtifacts: (runId: string, nodeId: string) =>
    req<{
      items: {
        name: string
        kind: string
        readonly?: boolean
        nodeId?: string
        outputKey?: string
      }[]
    }>(`/runs/${runId}/gates/${nodeId}/primary-artifacts`),
  saveGateArtifact: (
    runId: string,
    nodeId: string,
    name: string,
    content: string,
    ifMatch?: string,
  ) => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    if (ifMatch) headers['If-Match'] = ifMatch
    return req<{
      id: string
      name: string
      kind: string
      sizeBytes: number
      updatedAt: string
      etag: string
      nodeId: string
      content: string
    }>(`/runs/${runId}/gates/${nodeId}/artifacts/${encodeURIComponent(name)}`, {
      method: 'PUT',
      headers,
      body: JSON.stringify({ content }),
    })
  },
  /** Explicit CommentPin package write (preview_annotations.json); not primary whitelist. */
  saveAnnotationArtifact: (
    runId: string,
    nodeId: string,
    body: {
      kind?: string
      consumer?: string
      route?: string
      hardScope?: string
      count?: number
      annotations: Array<{
        seq: number
        selector: string
        comment: string
        currentText?: string
        screenshot: 'present' | 'MISSING'
        imageDataUrl?: string
        markKind?: string
        bounds?: { left: number; top: number; width: number; height: number }
      }>
    },
  ) =>
    req<{
      id: string
      name: string
      kind: string
      sizeBytes: number
      updatedAt: string
      etag: string
      nodeId: string
      content: string
      cleared?: boolean
    }>(`/runs/${runId}/gates/${nodeId}/annotation-artifact`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  reactReply: (
    runId: string,
    nodeId: string,
    text: string,
    images: ClarifyImage[] = [],
    force = false,
    annotations: ReactAnnotation[] = [],
    retryLast = false,
  ) =>
    req<{ status: string; waiting?: number }>(`/runs/${runId}/react/${nodeId}/reply`, {
      method: 'POST',
      body: JSON.stringify({ text, images, force, annotations, retryLast }),
    }),
  /** 轮级 Cancel for node-inline review (clears FIFO + aborts active ACP turn). */
  reactCancel: (runId: string, nodeId: string) =>
    req<{ status: string }>(`/runs/${runId}/react/${nodeId}/cancel`, { method: 'POST' }),
  /** Remove one waiting queue item by server id. */
  reactQueueRemove: (runId: string, nodeId: string, itemId: string) =>
    req<{ status: string }>(`/runs/${runId}/react/${nodeId}/queue/remove`, {
      method: 'POST',
      body: JSON.stringify({ itemId }),
    }),
  /** Reorder waiting queue items (authoritative ids). */
  reactQueueReorder: (runId: string, nodeId: string, itemIds: string[]) =>
    req<{ status: string }>(`/runs/${runId}/react/${nodeId}/queue/reorder`, {
      method: 'POST',
      body: JSON.stringify({ itemIds }),
    }),
  // Approval-gate ReAct reject: send annotations/text/images to the gate's
  // upstream producer's still-alive session for an in-place edit; the gate stays
  // pending. Requires gate.reactSessionAlive.
  gateReactRevise: (
    runId: string,
    nodeId: string,
    text: string,
    images: ClarifyImage[] = [],
    annotations: ReactAnnotation[] = [],
  ) =>
    req<{ status: string; waiting?: number; producerNodeId?: string }>(
      `/runs/${runId}/gates/${nodeId}/react-revise`,
      {
        method: 'POST',
        body: JSON.stringify({ text, images, annotations }),
      },
    ),
  /** 轮级 Cancel for gate hot-revise (upstream producer session). */
  gateReactCancel: (runId: string, nodeId: string) =>
    req<{ status: string; producerNodeId?: string }>(
      `/runs/${runId}/gates/${nodeId}/react-cancel`,
      { method: 'POST' },
    ),
  gateReactQueueRemove: (runId: string, nodeId: string, itemId: string) =>
    req<{ status: string; producerNodeId?: string }>(
      `/runs/${runId}/gates/${nodeId}/react-queue/remove`,
      { method: 'POST', body: JSON.stringify({ itemId }) },
    ),
  gateReactQueueReorder: (runId: string, nodeId: string, itemIds: string[]) =>
    req<{ status: string; producerNodeId?: string }>(
      `/runs/${runId}/gates/${nodeId}/react-queue/reorder`,
      { method: 'POST', body: JSON.stringify({ itemIds }) },
    ),

  exportRunLogsUrl: (id: string) => `${origin()}/api/runs/${id}/logs/export`,
  // Agent event log, read straight from the node's live sandbox (falls back to
  // the persisted snapshot once the sandbox is gone).
  nodeEvents: (
    runId: string,
    nodeId: string,
    params?: { cursor?: string; limit?: number; signal?: AbortSignal },
  ) => {
    const qs = new URLSearchParams()
    if (params?.cursor) qs.set('cursor', params.cursor)
    if (params?.limit != null) qs.set('limit', String(params.limit))
    const q = qs.toString()
    const path = `/runs/${runId}/nodes/${nodeId}/events` + (q ? `?${q}` : '')
    const init = params?.signal ? { signal: params.signal } : undefined
    if (params?.cursor || params?.limit != null) {
      return req<EventPaginatedResponse>(path, init)
    }
    return req<NodeEventsResponse>(path, init)
  },
  // Raw sandbox container logs (docker logs): live if running, else the archived
  // snapshot captured at teardown. Used for post-mortem troubleshooting.
  // `error` is set when the control plane should have logs but the read failed
  // (distinct from found=false = no log source).
  nodeSandboxLog: (runId: string, nodeId: string, opts?: { signal?: AbortSignal }) =>
    req<{ content: string; live: boolean; found: boolean; error?: string }>(
      `/runs/${runId}/nodes/${nodeId}/sandbox-log`,
      opts?.signal ? { signal: opts.signal } : undefined,
    ),
  getRunNodeSandbox: async (runId: string, nodeId: string): Promise<SandboxView | null> => {
    const res = await fetch(BASE + `/runs/${runId}/nodes/${nodeId}/sandbox`, {
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
    })
    if (res.status === 404) return null
    if (!res.ok) {
      let msg = `${res.status} /runs/${runId}/nodes/${nodeId}/sandbox`
      try {
        const body = await res.json()
        if (body?.error) msg = body.error
      } catch {
        // non-JSON error body
      }
      throw new Error(msg)
    }
    apiState.checked = true
    apiState.online = true
    return (await res.json()) as SandboxView
  },
  nodePreviews: (runId: string, nodeId: string, opts?: { signal?: AbortSignal }) =>
    req<{ ports: PreviewPort[] }>(
      `/runs/${runId}/nodes/${nodeId}/previews`,
      opts?.signal ? { signal: opts.signal } : undefined,
    ),
  listPreviewIssues: (runId: string, nodeId: string) =>
    req<{ issues: PreviewIssue[] }>(`/runs/${runId}/nodes/${nodeId}/preview-issues`),
  createPreviewIssue: (
    runId: string,
    nodeId: string,
    body: string,
    selector = '',
    port = 0,
    images: ClarifyImage[] = [],
  ) =>
    req<PreviewIssue>(`/runs/${runId}/nodes/${nodeId}/preview-issues`, {
      method: 'POST',
      body: JSON.stringify({ body, selector, port, images }),
    }),
  deletePreviewIssue: (runId: string, nodeId: string, issueId: string) =>
    req<{ status: string }>(`/runs/${runId}/nodes/${nodeId}/preview-issues/${issueId}`, {
      method: 'DELETE',
    }),
  /** One-shot ticket for the direct-preview chat drawer (see lib/inbox/embedChat). */
  embedTicket: (runId: string, nodeId: string) =>
    req<EmbedTicket>(`/runs/${runId}/nodes/${nodeId}/embed-ticket`, { method: 'POST' }),
}
