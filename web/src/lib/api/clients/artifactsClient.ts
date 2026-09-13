import type { Artifact, InboxItem } from '../../shared/types'
import { i18n } from '../../shared/i18n'
import {
  isDraining,
  mutationsBlocked,
  showDrainToast,
  shutdownState,
} from '../../composables/useShutdownState'
import { apiState, BASE, blobContentUrl, origin, redirectToLogin, req } from '../httpCore'
import type { PaginatedResponse } from '../apiTypes'

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

export const artifactsClient = {
  listArtifacts: (params?: {
    page?: number
    pageSize?: number
    wf?: string
    projectId?: string
    q?: string
    /** Opt-in: page by Run (total/pageSize = Run count; items = whole-Run flat list). */
    groupBy?: 'run'
  }) => {
    const qs = new URLSearchParams()
    if (params?.page != null) qs.set('page', String(params.page))
    if (params?.pageSize != null) qs.set('pageSize', String(params.pageSize))
    if (params?.wf) qs.set('wf', params.wf)
    if (params?.projectId) qs.set('projectId', params.projectId)
    if (params?.q) qs.set('q', params.q)
    if (params?.groupBy) qs.set('groupBy', params.groupBy)
    const q = qs.toString()
    const path = q ? `/artifacts?${q}` : '/artifacts'
    if (params?.page != null || params?.pageSize != null) {
      return req<PaginatedResponse<Artifact>>(path)
    }
    return req<Artifact[]>(path)
  },
  artifactContent: (id: string, opts?: { signal?: AbortSignal }) =>
    req<Artifact>(`/artifacts/${id}/content`, opts?.signal ? { signal: opts.signal } : undefined),
  artifactDownloadUrl: (id: string) => `${origin()}/api/artifacts/${id}/download`,
  /** Session pack download for one Run's artifacts (platform Artifacts page). */
  packRunArtifactsUrl: (runId: string) => `${origin()}/api/runs/${encodeURIComponent(runId)}/artifacts/pack`,
  packRunArtifacts: async (runId: string): Promise<{ blob: Blob; filename: string }> => {
    const res = await fetch(`${BASE}/runs/${encodeURIComponent(runId)}/artifacts/pack`, {
      credentials: 'include',
    })
    if (res.status === 401) {
      redirectToLogin()
      throw Object.assign(new Error('unauthorized'), { status: 401 })
    }
    if (!res.ok) {
      let msg = `${res.status} pack failed`
      try {
        const body = await res.json()
        if (body?.error) msg = body.error
      } catch {
        // non-JSON
      }
      throw Object.assign(new Error(msg), { status: res.status })
    }
    const blob = await res.blob()
    const filename = filenameFromContentDisposition(
      res.headers.get('Content-Disposition'),
      `${runId}-artifacts.zip`,
    )
    return { blob, filename }
  },
  blobContentUrl,
  // DELETE returns 204 No Content — must not go through req()'s unconditional res.json().
  deleteArtifact: async (id: string): Promise<void> => {
    const path = `/artifacts/${id}`
    if (mutationsBlocked()) {
      const msg = shutdownState.message || i18n.global.t('common.shutdown.notAcceptingRequests')
      showDrainToast(msg)
      throw new Error(msg)
    }
    const res = await fetch(BASE + path, {
      method: 'DELETE',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
    })
    apiState.checked = true
    if (res.status === 204) {
      apiState.online = true
      return
    }
    if (res.status === 401) {
      redirectToLogin()
      throw Object.assign(new Error('unauthorized'), { status: 401 })
    }
    if (res.status === 503) {
      let body: { status?: string; message?: string; error?: string } | null = null
      try {
        body = await res.json()
      } catch {
        // non-JSON 503 body
      }
      if (body?.status === 'shutting_down') {
        const msg = body.message || body.error || i18n.global.t('common.shutdown.notAcceptingRequests')
        showDrainToast(msg)
        throw Object.assign(new Error(msg), { status: 503 })
      }
    }
    if (!isDraining()) apiState.online = false
    let msg = `${res.status} ${path}`
    try {
      const body = await res.json()
      if (body?.error) msg = body.error
    } catch {
      // non-JSON error body; keep the status line
    }
    throw Object.assign(new Error(msg), { status: res.status })
  },
}
