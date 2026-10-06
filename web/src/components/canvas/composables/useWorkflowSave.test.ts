// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import {
  BACKUP_DELAY_MS,
  BACKUP_PREFIX,
  clearWorkflowBackup,
  pendingBackup,
  readWorkflowBackup,
  useWorkflowSave,
  writeWorkflowBackup,
} from './useWorkflowSave'

function deferred() {
  let resolve!: () => void
  let reject!: (e: unknown) => void
  const promise = new Promise<void>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function setup(over: { enabled?: () => boolean; id?: string } = {}) {
  const state = ref('a')
  const save = vi.fn(async () => {})
  const s = useWorkflowSave({ source: () => state.value, save, backupId: () => over.id ?? 'wf-1', enabled: over.enabled })
  return { state, save, s }
}

describe('useWorkflowSave', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('tracks dirty state against the last saved snapshot and never saves on its own', async () => {
    const { state, save, s } = setup()
    expect(s.status.value).toBe('saved')
    state.value = 'b'
    await nextTick()
    expect(s.dirty.value).toBe(true)
    expect(s.status.value).toBe('dirty')
    await vi.advanceTimersByTimeAsync(10_000)
    expect(save).not.toHaveBeenCalled()
    state.value = 'a'
    await nextTick()
    expect(s.status.value).toBe('saved')
  })

  it('saves on demand, shows saving and becomes clean', async () => {
    const { state, save, s } = setup()
    const d = deferred()
    save.mockImplementationOnce(() => d.promise)
    state.value = 'b'
    const p = s.save()
    await nextTick()
    expect(s.status.value).toBe('saving')
    d.resolve()
    expect(await p).toBe(true)
    expect(s.status.value).toBe('saved')
    expect(await s.save()).toBe(true)
    expect(save).toHaveBeenCalledTimes(1)
  })

  it('stays dirty when edits land during a save', async () => {
    const { state, save, s } = setup()
    const d = deferred()
    save.mockImplementationOnce(() => d.promise)
    state.value = 'b'
    const p = s.save()
    state.value = 'c'
    d.resolve()
    await p
    expect(s.status.value).toBe('dirty')
  })

  it('reports failures and recovers on retry', async () => {
    const { state, save, s } = setup()
    save.mockRejectedValueOnce(new Error('offline'))
    state.value = 'b'
    expect(await s.save()).toBe(false)
    expect(s.status.value).toBe('error')
    expect(s.error.value).toBe('offline')
    expect(await s.save()).toBe(true)
    expect(s.status.value).toBe('saved')
  })

  it('does nothing while disabled', async () => {
    const on = ref(false)
    const { state, save, s } = setup({ enabled: () => on.value })
    state.value = 'b'
    expect(await s.save()).toBe(false)
    expect(save).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(BACKUP_DELAY_MS)
    expect(readWorkflowBackup('wf-1')).toBeNull()
  })

  it('backs up unsaved changes after a pause and clears the backup once saved', async () => {
    const { state, s } = setup()
    state.value = 'b'
    await nextTick()
    await vi.advanceTimersByTimeAsync(BACKUP_DELAY_MS - 1)
    expect(readWorkflowBackup('wf-1')).toBeNull()
    await vi.advanceTimersByTimeAsync(1)
    expect(readWorkflowBackup('wf-1')).toMatchObject({ snapshot: 'b' })
    await s.save()
    expect(localStorage.getItem(BACKUP_PREFIX + 'wf-1')).toBeNull()
  })

  it('skips backups for a workflow without an id and drops them on discard', async () => {
    const { state } = setup({ id: '' })
    state.value = 'b'
    await nextTick()
    await vi.advanceTimersByTimeAsync(BACKUP_DELAY_MS)
    expect(localStorage.length).toBe(0)

    const other = setup()
    other.state.value = 'z'
    await nextTick()
    other.s.flushBackup()
    expect(readWorkflowBackup('wf-1')).toMatchObject({ snapshot: 'z' })
    other.s.discardBackup()
    expect(readWorkflowBackup('wf-1')).toBeNull()
  })
})

describe('pendingBackup', () => {
  beforeEach(() => localStorage.clear())

  it('offers only a backup that is newer than the server copy and differs from it', () => {
    const at = Date.parse('2026-01-01T00:00:00Z')
    writeWorkflowBackup('w', { snapshot: 'local', savedAt: at + 1000 })
    expect(pendingBackup('w', 'server', '2026-01-01T00:00:00Z')).toMatchObject({ snapshot: 'local' })
    expect(pendingBackup('w', 'local', '2026-01-01T00:00:00Z')).toBeNull()
    expect(pendingBackup('w', 'server', '2026-01-01T00:00:05Z')).toBeNull()
    expect(pendingBackup('w', 'server', '')).toMatchObject({ snapshot: 'local' })
    clearWorkflowBackup('w')
    expect(pendingBackup('w', 'server', '')).toBeNull()
  })

  it('ignores corrupt entries', () => {
    localStorage.setItem(BACKUP_PREFIX + 'w', '{nope')
    expect(readWorkflowBackup('w')).toBeNull()
  })
})
