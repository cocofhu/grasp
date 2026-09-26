import { reactive } from 'vue'
import { i18n } from '../shared/i18n'
import {
  isDraining,
  isMutationMethod,
  mutationsBlocked,
  showDrainToast,
  shutdownState,
} from '../composables/useShutdownState'

// API base: configurable via VITE_API_BASE, defaults to same-origin /api.
// The app is purely API-driven (no bundled mock data); on error views show
// an empty/error state.
export const BASE = ((import.meta as any).env?.VITE_API_BASE ?? '/api').replace(/\/$/, '')

/** Browser URL for a stored attachment (`blob:{id}` → `/api/blobs/{id}`). */
export function blobContentUrl(ref: string): string {
  const id = String(ref || '').trim().replace(/^blob:/, '')
  if (!id) return ''
  return `${BASE}/blobs/${encodeURIComponent(id)}`
}

const AUTH_WHITELIST = new Set(['/auth/login', '/auth/logout', '/auth/me', '/health', '/live'])

export function redirectToLogin() {
  if (typeof window === 'undefined') return
  const path = window.location.pathname + window.location.search
  // Cookieless pages (share links, the preview-page drawer) must never bounce
  // to login when the app shell's /api calls fire before the route resolves.
  if (path.startsWith('/login') || path.startsWith('/public/') || path.startsWith('/embed/')) return
  const redirect = encodeURIComponent(path)
  window.location.assign(`/login?redirect=${redirect}`)
}

export const apiState = reactive({ online: false, checked: false })

/** 409 sandbox_busy: confirm would queue behind an orphan sandbox turn. */
export function isSandboxBusyError(e: unknown): e is Error & { status?: number; code?: string; runningOpId?: string } {
  if (!e || typeof e !== 'object') return false
  return (e as { code?: string }).code === 'sandbox_busy'
}

export async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const method = (init?.method ?? 'GET').toUpperCase()
  if (mutationsBlocked() && isMutationMethod(method)) {
    const msg = shutdownState.message || i18n.global.t('common.shutdown.notAcceptingRequests')
    showDrainToast(msg)
    throw new Error(msg)
  }

  const res = await fetch(BASE + path, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) },
    ...init,
  })
  apiState.checked = true

  if (!res.ok) {
    if (res.status === 401 && !AUTH_WHITELIST.has(path)) {
      redirectToLogin()
      throw new Error('unauthorized')
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
        if (isMutationMethod(method)) showDrainToast(msg)
        throw new Error(msg)
      }
    }
    if (!isDraining()) apiState.online = false
    let msg = `${res.status} ${path}`
    let extra: { code?: string; runningOpId?: string } = {}
    try {
      const body = await res.json()
      if (body?.error) msg = body.error
      if (typeof body?.code === 'string') extra.code = body.code
      if (typeof body?.runningOpId === 'string') extra.runningOpId = body.runningOpId
    } catch {
      // non-JSON error body; keep the status line
    }
    throw Object.assign(new Error(msg), { status: res.status, ...extra })
  }
  apiState.online = true
  return (await res.json()) as T
}

// origin resolves the API origin (absolute VITE_API_BASE, else same-origin).
export function origin(): string {
  if (/^https?:\/\//.test(BASE)) return BASE.replace(/\/api$/, '')
  return window.location.origin
}

// wsUrl builds a ws(s):// URL for an API path under BASE.
export function wsUrl(path: string): string {
  let base = BASE
  if (!/^https?:\/\//.test(base)) base = window.location.origin + base
  return base.replace(/^http/, 'ws') + path
}

// rootWsUrl builds a ws(s):// URL for a site-root path (not under the /api base).
export function rootWsUrl(path: string): string {
  return window.location.origin.replace(/^http/, 'ws') + path
}
