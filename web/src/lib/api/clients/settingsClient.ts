import type { InboxItem } from '../../shared/types'
import { req, rootWsUrl } from '../httpCore'
import type {
  BrandSettings,
  ChannelConfig,
  ChannelConfigInput,
  ChannelDeleteOpts,
  DashboardStats,
  HealthResponse,
  NotifyDeliveryReceipt,
  PaginatedResponse,
  PlatformStatusMetrics,
  SettingItem,
} from '../apiTypes'

export type SettingsResponse = { items: SettingItem[]; brand: BrandSettings }

export const settingsClient = {
  health: () => req<HealthResponse>(`/health`),
  previewVncWsUrl: (runId: string, nodeId: string, port: number) =>
    rootWsUrl(`/preview-vnc/${runId}/${nodeId}/${port}/ws`),
  /** Console noVNC: sandbox-scoped WS (not preview runId/nodeId/port). */
  sandboxVncWsUrl: (sandboxId: number) =>
    rootWsUrl(`/sandbox-vnc/${sandboxId}/ws`),
  listGates: (params?: {
    page?: number
    pageSize?: number
    wf?: string
    projectId?: string
    tag?: string
    signal?: AbortSignal
  }) => {
    const qs = new URLSearchParams()
    if (params?.page != null) qs.set('page', String(params.page))
    if (params?.pageSize != null) qs.set('pageSize', String(params.pageSize))
    if (params?.wf) qs.set('wf', params.wf)
    if (params?.projectId) qs.set('projectId', params.projectId)
    if (params?.tag) qs.set('tag', params.tag)
    const q = qs.toString()
    const path = q ? `/gates?${q}` : '/gates'
    const init = params?.signal ? { signal: params.signal } : undefined
    if (params?.page != null || params?.pageSize != null) {
      return req<PaginatedResponse<InboxItem>>(path, init)
    }
    return req<InboxItem[]>(path, init)
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
  // multi-channel QQ APIs (primary + secondary)
  listProjectNotifyReceipts: (projectId: string) =>
    req<{ items: NotifyDeliveryReceipt[] }>(
      `/projects/${encodeURIComponent(projectId)}/notify-receipts`,
    ),
  listProjectChannels: (projectId: string) =>
    req<{ items: ChannelConfig[]; secretsKeyConfigured?: boolean; freeAgents?: string[] }>(
      `/projects/${encodeURIComponent(projectId)}/channels`,
    ),
  createProjectChannel: (projectId: string, body: ChannelConfigInput) =>
    req<ChannelConfig>(`/projects/${encodeURIComponent(projectId)}/channels`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  updateProjectChannel: (projectId: string, channelId: string, body: ChannelConfigInput) =>
    req<ChannelConfig>(
      `/projects/${encodeURIComponent(projectId)}/channels/${encodeURIComponent(channelId)}`,
      { method: 'PUT', body: JSON.stringify(body) },
    ),
  deleteProjectChannelById: (projectId: string, channelId: string, body?: ChannelDeleteOpts) =>
    req<{ status: string }>(
      `/projects/${encodeURIComponent(projectId)}/channels/${encodeURIComponent(channelId)}`,
      {
        method: 'DELETE',
        body: body ? JSON.stringify(body) : undefined,
      },
    ),
  // legacy singular aliases → primary channel
  getProjectChannel: (projectId: string) =>
    req<{ channel: ChannelConfig | null; secretsKeyConfigured?: boolean }>(
      `/projects/${encodeURIComponent(projectId)}/channel`,
    ),
  putProjectChannel: (projectId: string, body: ChannelConfigInput) =>
    req<ChannelConfig>(`/projects/${encodeURIComponent(projectId)}/channel`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteProjectChannel: (projectId: string) =>
    req<{ status: string }>(`/projects/${encodeURIComponent(projectId)}/channel`, { method: 'DELETE' }),
}
