import type { Artifact } from '@/lib/shared/types'
import type { SoloMessage, SoloSession } from '@/lib/api/apiTypes'
import { rootWsUrl } from '@/lib/api/httpCore'

export function soloEmbedClient(runId: string, token: string, onExpired?: () => void) {
  const base = `/embed-api/solo/${encodeURIComponent(runId)}`
  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(base + path, {
      ...init,
      credentials: 'omit',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    })
    if (res.status === 401) onExpired?.()
    if (!res.ok) {
      const body = await res.json().catch(() => null)
      throw Object.assign(new Error(body?.error || String(res.status)), { status: res.status })
    }
    return res.json()
  }
  return {
    getSoloSession: () => request<SoloSession>(''),
    listSoloMessages: () => request<SoloMessage[]>('/messages'),
    listArtifacts: (signal?: AbortSignal) => request<{ artifacts: Artifact[] | null }>('/artifacts', { signal }),
    sendSoloTurn: (_id: string, body: { text: string }) =>
      request<{ waiting: number }>('/turns', { method: 'POST', body: JSON.stringify(body) }),
    cancelSoloTurn: () => request<{ ok: boolean }>('/turns/cancel', { method: 'POST' }),
    soloChatWsUrl: (_id: string, after?: number) => rootWsUrl(base + '/chat' + (after === undefined ? '' : `?after=${after}`)),
    soloChatProtocols: ['grasp-solo-embed', token],
  }
}
