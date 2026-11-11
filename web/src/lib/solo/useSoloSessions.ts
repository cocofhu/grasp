import { computed, ref } from 'vue'
import { api } from '@/lib/api/api'
import type { SoloSession } from '@/lib/api/apiTypes'

const sessions = ref<SoloSession[]>([])
const maxSandboxes = ref(5)
const usedSandboxes = ref(0)
const loaded = ref(false)
const loading = ref(false)
const error = ref('')
const closing = ref(new Set<string>())
const closeRequests = new Map<string, Promise<SoloSession>>()
/** In-flight stop/resume. The open chat reads this; it is not private to the sidebar. */
const environmentChanges = ref(new Map<string, 'stopping' | 'resuming'>())
let inflight: Promise<void> | null = null
let closeGeneration = 0
let environmentGeneration = 0

function beginEnvironmentChange(id: string, phase: 'stopping' | 'resuming') {
  const next = new Map(environmentChanges.value)
  next.set(id, phase)
  environmentChanges.value = next
}

function endEnvironmentChange(id: string) {
  if (!environmentChanges.value.has(id)) return
  const next = new Map(environmentChanges.value)
  next.delete(id)
  environmentChanges.value = next
}

export function isSoloActive(s: Pick<SoloSession, 'status'>): boolean {
  return s.status === 'starting' || s.status === 'running' || s.status === 'stopped'
}

/** Closed (cancelled) or failed sessions; the sidebar lists them under「历史执行」. */
export function isSoloEnded(s: Pick<SoloSession, 'status'>): boolean {
  return s.status === 'closed' || s.status === 'error'
}

/** Workspace-wide Solo session list shared by the sidebar, home and /solo. */
export function useSoloSessions() {
  async function refresh() {
    if (inflight) return inflight
    loading.value = true
    const generation = closeGeneration
    const environment = environmentGeneration
    inflight = (async () => {
      try {
        const r = await api.listSoloSessions()
        // A list read started before close or stop settled must not put its old row back.
        if (generation !== closeGeneration || environment !== environmentGeneration) return
        sessions.value = r.sessions ?? []
        if (r.maxSandboxes > 0) maxSandboxes.value = r.maxSandboxes
        usedSandboxes.value = r.usedSandboxes ?? 0
        error.value = ''
      } catch (e: any) {
        error.value = String(e?.message || e)
      } finally {
        loaded.value = true
        loading.value = false
        inflight = null
      }
    })()
    return inflight
  }

  /** Writes one session back into the list (create / rename / status). */
  function upsert(s: SoloSession) {
    const i = sessions.value.findIndex((x) => x.id === s.id)
    if (i >= 0) sessions.value.splice(i, 1, s)
    else sessions.value.unshift(s)
  }

  /**
   * One environment stop or resume shared by the sidebar, the limit dialog and
   * the open chat. The phase is visible before the request returns, and a failed
   * stop re-reads the session before the phase is cleared.
   */
  function changeEnvironment(id: string, phase: 'stopping' | 'resuming', run: () => Promise<SoloSession>): Promise<SoloSession> {
    beginEnvironmentChange(id, phase)
    return run().then(async (saved) => {
      environmentGeneration++
      upsert(saved)
      return saved
    }, async (cause) => {
      environmentGeneration++
      try { upsert(await api.getSoloSession(id)) } catch { /* the caller's error is the one to show */ }
      throw cause
    }).finally(() => endEnvironmentChange(id))
  }

  function remove(id: string) {
    sessions.value = sessions.value.filter((x) => x.id !== id)
  }

  /** Shared by desktop and drawer navigation; stopping compute can take seconds. */
  function close(id: string): Promise<SoloSession> {
    const pending = closeRequests.get(id)
    if (pending) return pending
    closing.value.add(id)
    const request = (async () => {
      try {
        const saved = await api.closeSoloSession(id)
        closeGeneration++
        upsert(saved)
        return saved
      } catch (cause) {
        // A failed response may still mean the chat closed, with stop pending.
        // Re-read after any older list request; never replay the mutation.
        closeGeneration++
        void Promise.resolve(inflight).then(() => refresh())
        throw cause
      } finally {
        closing.value.delete(id)
        closeRequests.delete(id)
      }
    })()
    closeRequests.set(id, request)
    return request
  }

  const active = computed(() => sessions.value.filter(isSoloActive))
  const ended = computed(() => sessions.value.filter(isSoloEnded))

  return { sessions, active, ended, maxSandboxes, usedSandboxes, loaded, loading, error, closing, close, environmentChanges, beginEnvironmentChange, endEnvironmentChange, changeEnvironment, refresh, upsert, remove }
}
