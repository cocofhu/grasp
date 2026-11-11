import { computed, ref } from 'vue'
import { api } from '@/lib/api/api'
import type { SoloSession } from '@/lib/api/apiTypes'

const sessions = ref<SoloSession[]>([])
const maxSandboxes = ref(5)
const usedSandboxes = ref(0)
const loaded = ref(false)
const loading = ref(false)
const error = ref('')
let inflight: Promise<void> | null = null

/** Open sessions, including stopped ones that can resume. */
export function isSoloActive(s: Pick<SoloSession, 'status'>): boolean {
  return s.status === 'starting' || s.status === 'running' || s.status === 'stopping' || s.status === 'stopped'
}

/** Sessions holding a sandbox; stopped ones do not count against the quota. */
export function isSoloComputing(s: Pick<SoloSession, 'status'>): boolean {
  return s.status === 'starting' || s.status === 'running' || s.status === 'stopping'
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
    inflight = (async () => {
      try {
        const r = await api.listSoloSessions()
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

  function remove(id: string) {
    sessions.value = sessions.value.filter((x) => x.id !== id)
  }

  const active = computed(() => sessions.value.filter(isSoloActive))
  const computing = computed(() => sessions.value.filter(isSoloComputing))
  const ended = computed(() => sessions.value.filter(isSoloEnded))

  return { sessions, active, computing, ended, maxSandboxes, usedSandboxes, loaded, loading, error, refresh, upsert, remove }
}
