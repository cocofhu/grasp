import { reactive } from 'vue'
import { i18n } from '../shared/i18n'
import { applyHealthCommit } from './useServiceCommit'

const BASE = ((import.meta as any).env?.VITE_API_BASE ?? '/api').replace(/\/$/, '')

type ShutdownMode = 'normal' | 'draining' | 'offline'

interface ShutdownHealth {
  status: string
  ready?: boolean
  message?: string
  grace_remaining_seconds?: number
}

export const shutdownState = reactive({
  mode: 'normal' as ShutdownMode,
  graceRemainingSeconds: 0,
  message: '',
  checked: false,
})

let pollTimer: ReturnType<typeof setInterval> | null = null
let toastTimer: ReturnType<typeof setTimeout> | null = null
export const drainToast = reactive({ visible: false, text: '' })

const MUTATING = new Set(['POST', 'PUT', 'DELETE', 'PATCH'])

export function isDraining(): boolean {
  return shutdownState.mode === 'draining'
}

export function isOffline(): boolean {
  return shutdownState.mode === 'offline'
}

export function isMutationMethod(method?: string): boolean {
  return MUTATING.has((method ?? 'GET').toUpperCase())
}

export function mutationsBlocked(): boolean {
  return isDraining()
}

export function showDrainToast(text = i18n.global.t('common.shutdown.mutationsBlocked')) {
  drainToast.text = text
  drainToast.visible = true
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => {
    drainToast.visible = false
  }, 4000)
}

export async function pollShutdownHealth(): Promise<void> {
  try {
    const res = await fetch(BASE + '/health', { cache: 'no-store' })
    shutdownState.checked = true
    if (res.status === 503) {
      let body: ShutdownHealth = { status: 'shutting_down' }
      try {
        body = (await res.json()) as ShutdownHealth
      } catch {
        // keep default shutting_down body
      }
      if (body.status === 'shutting_down') {
        shutdownState.mode = 'draining'
        shutdownState.graceRemainingSeconds = body.grace_remaining_seconds ?? 0
        shutdownState.message = body.message ?? i18n.global.t('common.shutdown.notAcceptingRequests')
        return
      }
    }
    if (res.ok) {
      shutdownState.mode = 'normal'
      shutdownState.graceRemainingSeconds = 0
      shutdownState.message = ''
      try {
        const body = (await res.json()) as { commit?: unknown }
        applyHealthCommit(body.commit)
      } catch {
        applyHealthCommit('')
      }
      return
    }
    shutdownState.mode = 'offline'
  } catch {
    shutdownState.checked = true
    shutdownState.mode = 'offline'
  }
}

export function startShutdownPolling(intervalMs = 4000): void {
  if (pollTimer) return
  void pollShutdownHealth()
  pollTimer = setInterval(() => void pollShutdownHealth(), intervalMs)
}

export function stopShutdownPolling(): void {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

export function formatGrace(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}
