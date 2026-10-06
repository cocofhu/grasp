import { computed, getCurrentScope, onScopeDispose, ref, watch } from 'vue'

export const BACKUP_DELAY_MS = 800
export const BACKUP_PREFIX = 'grasp.workflowDraft.'

export type SaveStatus = 'saved' | 'dirty' | 'saving' | 'error'

/** Unsaved editor state kept in localStorage so a crash or closed tab loses nothing. */
export interface WorkflowBackup {
  /** Same serialization as `source()`. */
  snapshot: string
  /** Epoch ms of the last backed-up edit. */
  savedAt: number
}

function storage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null
  }
}

export function readWorkflowBackup(id: string): WorkflowBackup | null {
  if (!id) return null
  try {
    const raw = storage()?.getItem(BACKUP_PREFIX + id)
    if (!raw) return null
    const v = JSON.parse(raw) as WorkflowBackup
    return typeof v?.snapshot === 'string' && typeof v.savedAt === 'number' ? v : null
  } catch {
    return null
  }
}

export function writeWorkflowBackup(id: string, backup: WorkflowBackup) {
  try {
    storage()?.setItem(BACKUP_PREFIX + id, JSON.stringify(backup))
  } catch {
    /* quota or storage unavailable: the backup is best effort */
  }
}

export function clearWorkflowBackup(id: string) {
  try {
    storage()?.removeItem(BACKUP_PREFIX + id)
  } catch {
    /* storage unavailable */
  }
}

/**
 * The backup worth offering on open: newer than the server copy and different from it.
 * `serverUpdatedAt` is the workflow's updatedAt; `serverSnapshot` its serialization.
 */
export function pendingBackup(id: string, serverSnapshot: string, serverUpdatedAt?: string): WorkflowBackup | null {
  const b = readWorkflowBackup(id)
  if (!b || b.snapshot === serverSnapshot) return null
  const server = serverUpdatedAt ? Date.parse(serverUpdatedAt) : NaN
  if (Number.isFinite(server) && b.savedAt <= server) return null
  return b
}

export interface WorkflowSaveOptions {
  /** Serialized editable state; differs from the last saved value ⇒ dirty. */
  source: () => string
  /** Persists the current state. */
  save: () => Promise<void>
  /** Workflow id the local backup is stored under; '' (unsaved new workflow) disables backups. */
  backupId: () => string
  /** Saving and backups are suspended while this returns false (loading, load failed, previewing). */
  enabled?: () => boolean
  backupDelay?: number
}

/**
 * Manual save for the workflow editor: dirty tracking against the last saved snapshot,
 * one save at a time, and a debounced localStorage backup of unsaved changes.
 */
export function useWorkflowSave(opts: WorkflowSaveOptions) {
  const delay = opts.backupDelay ?? BACKUP_DELAY_MS
  const enabled = () => (opts.enabled ? opts.enabled() : true)
  const current = computed(opts.source)
  const saved = ref(current.value)
  const saving = ref(false)
  const error = ref('')
  let timer: ReturnType<typeof setTimeout> | null = null
  let inflight: Promise<boolean> | null = null

  const dirty = computed(() => current.value !== saved.value)
  const status = computed<SaveStatus>(() => {
    if (saving.value) return 'saving'
    if (error.value && dirty.value) return 'error'
    return dirty.value ? 'dirty' : 'saved'
  })

  function clearTimer() {
    if (timer) clearTimeout(timer)
    timer = null
  }

  function backupNow() {
    clearTimer()
    const id = opts.backupId()
    if (!id || !enabled()) return
    if (dirty.value) writeWorkflowBackup(id, { snapshot: current.value, savedAt: Date.now() })
    else clearWorkflowBackup(id)
  }

  async function run(): Promise<boolean> {
    saving.value = true
    error.value = ''
    try {
      const snap = current.value
      await opts.save()
      saved.value = snap
      clearTimer()
      const id = opts.backupId()
      if (id) {
        if (dirty.value) writeWorkflowBackup(id, { snapshot: current.value, savedAt: Date.now() })
        else clearWorkflowBackup(id)
      }
      return true
    } catch (e: any) {
      error.value = String(e?.message || e)
      return false
    } finally {
      saving.value = false
    }
  }

  /** Saves when dirty (or always with `force`). Resolves false when the save failed (see `error`). */
  async function save(force = false): Promise<boolean> {
    if (!enabled()) return false
    while (inflight) await inflight
    if (!dirty.value && !force) return true
    inflight = run().finally(() => {
      inflight = null
    })
    return inflight
  }

  /** Treats the current state as saved (after load, restore or publish). */
  function markSaved(snapshot = current.value) {
    saved.value = snapshot
    error.value = ''
    clearTimer()
  }

  /** Drops the local backup without touching the dirty state. */
  function discardBackup() {
    clearTimer()
    const id = opts.backupId()
    if (id) clearWorkflowBackup(id)
  }

  const stop = watch(current, () => {
    if (!enabled() || !opts.backupId()) return
    clearTimer()
    timer = setTimeout(backupNow, delay)
  })

  function dispose() {
    stop()
    clearTimer()
  }
  if (getCurrentScope()) onScopeDispose(dispose)

  return {
    status,
    dirty,
    saving,
    error,
    save,
    markSaved,
    discardBackup,
    /** Writes the pending backup right away (before the page unloads). */
    flushBackup: () => {
      if (timer) backupNow()
    },
    stop: dispose,
  }
}
