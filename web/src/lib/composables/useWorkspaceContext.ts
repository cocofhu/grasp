import { effectScope, ref, watch } from 'vue'
import { api } from '@/lib/api/api'
import type { Workspace } from '@/lib/shared/types'
import { useAuth } from './useAuth'

const workspace = ref<Workspace | null>(null)
const loading = ref(false)
const error = ref('')
let generation = 0
let controller: AbortController | null = null
let pending: Promise<Workspace | null> | null = null
let watching = false

function reset() {
  generation++
  controller?.abort()
  controller = null
  pending = null
  workspace.value = null
  loading.value = false
  error.value = ''
}

function ensureLoaded(): Promise<Workspace | null> {
  const { ready, user } = useAuth()
  if (!ready.value || !user.value) return Promise.resolve(null)
  if (workspace.value) return Promise.resolve(workspace.value)
  if (pending) return pending
  const current = ++generation
  controller = new AbortController()
  loading.value = true
  error.value = ''
  pending = api.getWorkspace({ signal: controller.signal }).then((result) => {
    if (current !== generation) return null
    if (typeof result?.name !== 'string') throw new Error('The workspace could not be loaded')
    workspace.value = result
    return result
  }).catch((cause: unknown) => {
    if (current === generation) error.value = cause instanceof Error ? cause.message : String(cause)
    return null
  }).finally(() => {
    if (current === generation) {
      loading.value = false
      controller = null
      pending = null
    }
  })
  return pending
}

/** Authenticated singleton metadata. Public pages do not need to call this composable. */
export function useWorkspaceContext() {
  const { ready, user } = useAuth()
  if (!watching) {
    watching = true
    // Detached from component lifetimes: one shared context follows the signed-in identity.
    effectScope(true).run(() => watch(() => ready.value ? user.value?.username || '' : undefined, () => {
      reset()
      if (ready.value && user.value) void ensureLoaded()
    }, { immediate: true, flush: 'sync' }))
  }
  return {
    workspace,
    loading,
    error,
    ensureLoaded,
    reload: () => { reset(); return ensureLoaded() },
    reset,
  }
}
