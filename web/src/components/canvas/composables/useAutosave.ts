import { getCurrentScope, onScopeDispose, ref, watch } from 'vue'

export const AUTOSAVE_DELAY_MS = 1000

export type SaveStatus = 'saved' | 'unsaved' | 'saving' | 'error'

export interface AutosaveOptions {
  /** Serialized state; a change from the last saved value schedules a save. */
  source: () => string
  /** Persists the state that was current when the save started. */
  save: (snapshot: string) => Promise<void>
  /** Autosave is suspended while this returns false (loading, load failed, no project). */
  enabled?: () => boolean
  delay?: number
}

/**
 * Debounced draft autosave: saves `delay` ms after the last change, never runs
 * two saves at once, and saves again if edits arrived while a save was in flight.
 */
export function useAutosave(opts: AutosaveOptions) {
  const delay = opts.delay ?? AUTOSAVE_DELAY_MS
  const status = ref<SaveStatus>('saved')
  const error = ref('')
  let saved = opts.source()
  let timer: ReturnType<typeof setTimeout> | null = null
  let inflight: Promise<void> | null = null

  const enabled = () => (opts.enabled ? opts.enabled() : true)

  function clearTimer() {
    if (timer) clearTimeout(timer)
    timer = null
  }

  function schedule() {
    clearTimer()
    timer = setTimeout(() => {
      timer = null
      flush().catch(() => {})
    }, delay)
  }

  async function run(): Promise<void> {
    const snap = opts.source()
    if (snap === saved) {
      if (status.value !== 'error') status.value = 'saved'
      return
    }
    status.value = 'saving'
    error.value = ''
    try {
      await opts.save(snap)
      saved = snap
      status.value = opts.source() === saved ? 'saved' : 'unsaved'
    } catch (e: any) {
      status.value = 'error'
      error.value = String(e?.message || e)
      throw e
    }
  }

  /** Saves now (Ctrl/Cmd+S, before run / publish / leaving). Rejects when the save fails. */
  async function flush(): Promise<void> {
    clearTimer()
    if (!enabled()) return
    while (inflight) await inflight.catch(() => {})
    inflight = run().finally(() => {
      inflight = null
    })
    await inflight
    if (opts.source() !== saved) schedule()
  }

  /** Treats the current state as saved (after load, restore or an explicit save elsewhere). */
  function markSaved(snapshot = opts.source()) {
    clearTimer()
    saved = snapshot
    status.value = 'saved'
    error.value = ''
  }

  const stop = watch(opts.source, (snap) => {
    if (snap === saved) {
      clearTimer()
      if (!inflight && status.value !== 'error') status.value = 'saved'
      return
    }
    if (!enabled()) return
    if (status.value !== 'saving') status.value = 'unsaved'
    schedule()
  })

  if (getCurrentScope()) {
    onScopeDispose(() => {
      stop()
      clearTimer()
    })
  }

  return {
    status,
    error,
    flush,
    markSaved,
    isDirty: () => opts.source() !== saved,
    stop: () => {
      stop()
      clearTimer()
    },
  }
}
