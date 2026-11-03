import type { Artifact } from '@/lib/shared/types'
import { createSoloChatClient } from '@/lib/api/clients/soloClient'
import type { ScopedSessionModelCatalog } from './useSessionModelCatalog'
import { rootWsUrl } from '@/lib/api/httpCore'
import { previewSessionRequest } from '@/lib/inbox/embedChat'

export function soloEmbedClient(runId: string, token: string, onExpired?: () => void) {
  const base = `/embed-api/solo/${encodeURIComponent(runId)}`
  const request = previewSessionRequest(base, token, onExpired)
  return {
    ...createSoloChatClient(request, rootWsUrl, () => ''),
    listArtifacts: (signal?: AbortSignal) => request<{ artifacts: Artifact[] | null }>('/artifacts', { signal }),
    attachment: (messageId: string, id: string, signal: AbortSignal) =>
      request<Blob>(`/messages/${encodeURIComponent(messageId)}/blobs/${encodeURIComponent(id)}`, { signal }, res => res.blob()),
    modelCatalog: () => request<ScopedSessionModelCatalog>('/model-catalog'),
    soloChatProtocols: ['grasp-solo-embed', token],
  }
}
