import { computed, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import type { DesktopStatus } from '@/lib/api/apiTypes'

/** Status poll while the desktop panel is open. */
export const DESKTOP_POLL_MS = 3_000
/** Control renewal; the sandbox returns control to the Agent 30s after the last one. */
export const DESKTOP_HEARTBEAT_MS = 10_000
/** Consecutive status failures before polling stops. */
export const DESKTOP_POLL_MAX_FAILURES = 3

function newClientId(): string {
  const c = globalThis.crypto
  if (c && typeof c.randomUUID === 'function') return c.randomUUID()
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

/**
 * Soft interlock for the desktop image: while `control` is true this viewer
 * holds the desktop (holder=user, renewed every 10s) and the desktop MCP
 * refuses mouse and keyboard tools. Control goes back to the Agent when the
 * viewer returns it, leaves the page, hides the tab or unmounts the panel.
 * On the browser image nothing is reported.
 */
export function useDesktopControl(sandboxId: () => number | undefined, control: Ref<boolean>) {
  const status = ref<DesktopStatus | null>(null)
  const isDesktop = computed(() => status.value?.mode === 'desktop')
  const client = newClientId()
  let pollTimer: ReturnType<typeof setInterval> | null = null
  let beatTimer: ReturnType<typeof setInterval> | null = null
  let holding = false
  let failures = 0

  async function refresh() {
    const id = sandboxId()
    if (!id) return
    try {
      status.value = await api.sandboxDesktop(id)
    } catch {
      // Older runtimes have no desktop status endpoint; stop asking.
      if (++failures >= DESKTOP_POLL_MAX_FAILURES) stopPolling()
      return
    }
    failures = 0
    if (status.value.mode !== 'desktop') stopPolling()
  }

  async function report(holder: 'user' | 'agent', keepalive = false) {
    const id = sandboxId()
    if (!id) return
    try {
      status.value = await api.setSandboxDesktopControl(id, holder, client, keepalive ? { keepalive } : undefined)
    } catch {
      /* the next poll shows the real holder */
    }
  }

  function stopBeat() {
    if (beatTimer != null) clearInterval(beatTimer)
    beatTimer = null
  }

  function take() {
    holding = true
    void report('user')
    stopBeat()
    beatTimer = setInterval(() => void report('user'), DESKTOP_HEARTBEAT_MS)
  }

  function release(keepalive = false) {
    stopBeat()
    if (!holding) return
    holding = false
    void report('agent', keepalive)
  }

  function stopPolling() {
    if (pollTimer != null) clearInterval(pollTimer)
    pollTimer = null
  }

  watch(control, (on) => {
    if (on && isDesktop.value) take()
    else if (!on) release()
  })

  watch(isDesktop, (desktop) => {
    if (desktop && control.value && !holding) take()
  })

  function onPageHide() {
    release(true)
  }

  function onVisibility() {
    if (document.visibilityState === 'hidden' && control.value && isDesktop.value) control.value = false
  }

  onMounted(() => {
    if (!sandboxId()) return
    void refresh()
    pollTimer = setInterval(() => void refresh(), DESKTOP_POLL_MS)
    window.addEventListener('pagehide', onPageHide)
    document.addEventListener('visibilitychange', onVisibility)
  })

  onBeforeUnmount(() => {
    stopPolling()
    window.removeEventListener('pagehide', onPageHide)
    document.removeEventListener('visibilitychange', onVisibility)
    release(true)
  })

  /** The Agent is mid-turn: taking over makes its mouse/keyboard tools fail. */
  const agentBusy = computed(() => !!status.value?.agentActive)
  const userHolds = computed(() => status.value?.holder === 'user')

  return { status, isDesktop, agentBusy, userHolds, refresh }
}
