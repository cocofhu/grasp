import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { AUTOSAVE_DELAY_MS, useAutosave } from './useAutosave'

function deferred() {
  let resolve!: () => void
  let reject!: (e: unknown) => void
  const promise = new Promise<void>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('useAutosave', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('saves once, 1s after the last change', async () => {
    const state = ref('a')
    const save = vi.fn(async () => {})
    const a = useAutosave({ source: () => state.value, save })
    expect(a.status.value).toBe('saved')
    state.value = 'b'
    await nextTick()
    expect(a.status.value).toBe('unsaved')
    vi.advanceTimersByTime(AUTOSAVE_DELAY_MS - 1)
    state.value = 'c'
    await nextTick()
    vi.advanceTimersByTime(AUTOSAVE_DELAY_MS - 1)
    expect(save).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(save).toHaveBeenCalledTimes(1)
    expect(save).toHaveBeenCalledWith('c')
    expect(a.status.value).toBe('saved')
    a.stop()
  })

  it('returns to saved without a request when the change is reverted', async () => {
    const state = ref('a')
    const save = vi.fn(async () => {})
    const a = useAutosave({ source: () => state.value, save })
    state.value = 'b'
    await nextTick()
    state.value = 'a'
    await nextTick()
    await vi.advanceTimersByTimeAsync(AUTOSAVE_DELAY_MS * 2)
    expect(save).not.toHaveBeenCalled()
    expect(a.status.value).toBe('saved')
    a.stop()
  })

  it('never overlaps saves and saves again for edits made during a save', async () => {
    const state = ref('a')
    const pending: ReturnType<typeof deferred>[] = []
    const save = vi.fn(() => {
      const d = deferred()
      pending.push(d)
      return d.promise
    })
    const a = useAutosave({ source: () => state.value, save })
    state.value = 'b'
    await nextTick()
    const first = a.flush()
    expect(a.status.value).toBe('saving')
    state.value = 'c'
    await nextTick()
    const second = a.flush()
    expect(save).toHaveBeenCalledTimes(1)
    pending[0]!.resolve()
    await first
    await vi.waitFor(() => expect(save).toHaveBeenCalledTimes(2))
    expect(save).toHaveBeenLastCalledWith('c')
    pending[1]!.resolve()
    await second
    expect(a.status.value).toBe('saved')
    a.stop()
  })

  it('reports errors and rejects an explicit flush', async () => {
    const state = ref('a')
    const a = useAutosave({ source: () => state.value, save: async () => Promise.reject(new Error('boom')) })
    state.value = 'b'
    await nextTick()
    await expect(a.flush()).rejects.toThrow('boom')
    expect(a.status.value).toBe('error')
    expect(a.error.value).toBe('boom')
    expect(a.isDirty()).toBe(true)
    a.stop()
  })

  it('stays idle while disabled and markSaved clears dirtiness', async () => {
    const state = ref('a')
    const enabled = ref(false)
    const save = vi.fn(async () => {})
    const a = useAutosave({ source: () => state.value, save, enabled: () => enabled.value })
    state.value = 'b'
    await nextTick()
    await vi.advanceTimersByTimeAsync(AUTOSAVE_DELAY_MS * 2)
    await a.flush()
    expect(save).not.toHaveBeenCalled()
    a.markSaved()
    expect(a.isDirty()).toBe(false)
    expect(a.status.value).toBe('saved')
    a.stop()
  })
})
