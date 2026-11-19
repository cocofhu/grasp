import type { InboxItem } from '../../shared/types'
import { req, rootWsUrl } from '../httpCore'
import type {
  BrandSettings,
  DashboardStats,
  HealthResponse,
  PaginatedResponse,
  PlatformStatusMetrics,
  SettingItem,
} from '../apiTypes'

type SettingsResponse = { items: SettingItem[]; brand: BrandSettings }

export const settingsClient = {
  health: () => req<HealthResponse>(`/health`),
  /** The one noVNC address of a sandbox desktop; app ports are pages on it. */
  sandboxVncWsUrl: (sandboxId: number) =>
    rootWsUrl(`/sandbox-vnc/${sandboxId}/ws`),
  /** Sound of that desktop: raw PCM frames (see lib/shared/desktopAudio). */
  sandboxAudioWsUrl: (sandboxId: number) =>
    rootWsUrl(`/sandbox-audio/${sandboxId}/ws`),
  listGates: (params?: {
    page?: number
    pageSize?: number
    wf?: string
    tag?: string
    signal?: AbortSignal
  }) => {
    const qs = new URLSearchParams()
    if (params?.page != null) qs.set('page', String(params.page))
    if (params?.pageSize != null) qs.set('pageSize', String(params.pageSize))
    if (params?.wf) qs.set('wf', params.wf)
    if (params?.tag) qs.set('tag', params.tag)
    const q = qs.toString()
    const path = q ? `/pending-nodes?${q}` : '/pending-nodes'
    const init = params?.signal ? { signal: params.signal } : undefined
    return req<PaginatedResponse<InboxItem>>(path, init)
  },
  dashboard: () => req<DashboardStats>('/stats/dashboard'),
  platformStatus: (params?: { timezone?: string; utcOffsetMinutes?: number }) => {
    const q = new URLSearchParams()
    if (params?.timezone) q.set('timezone', params.timezone)
    if (params?.utcOffsetMinutes != null && Number.isFinite(params.utcOffsetMinutes)) {
      q.set('utcOffsetMinutes', String(Math.round(params.utcOffsetMinutes)))
    }
    const qs = q.toString()
    return req<PlatformStatusMetrics>(qs ? `/stats/platform-status?${qs}` : '/stats/platform-status')
  },
  // platform settings (scheduling params)
  getSettings: () => req<SettingsResponse>('/settings'),
  updateSettings: (patch: Record<string, number | string>) =>
    req<SettingsResponse>('/settings', {
      method: 'PUT',
      body: JSON.stringify(patch),
    }),
}
