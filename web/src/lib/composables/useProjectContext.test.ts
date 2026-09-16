// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'

const replace = vi.fn()
const route = { query: {} as Record<string, string> }

vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ replace }),
}))

import {
  PROJECT_CONTEXT_STORAGE_KEY,
  readStoredProjectId,
  useProjectContext,
  writeStoredProjectId,
} from './useProjectContext'

describe('useProjectContext', () => {
  beforeEach(() => {
    localStorage.clear()
    route.query = {}
    replace.mockReset()
  })

  it('reads/writes storage and hydrates URL', () => {
    expect(readStoredProjectId()).toBe('')
    writeStoredProjectId('p1')
    expect(localStorage.getItem(PROJECT_CONTEXT_STORAGE_KEY)).toBe('p1')
    writeStoredProjectId('')
    expect(localStorage.getItem(PROJECT_CONTEXT_STORAGE_KEY)).toBeNull()

    writeStoredProjectId('p2')
    const ctx = useProjectContext()
    expect(ctx.selected.value).toBe('')
    ctx.ensureHydrated()
    expect(replace).toHaveBeenCalledWith({ query: { projectId: 'p2' } })

    route.query = { projectId: 'p3' }
    const ctx2 = useProjectContext()
    ctx2.ensureHydrated()
    expect(localStorage.getItem(PROJECT_CONTEXT_STORAGE_KEY)).toBe('p3')
    expect(ctx2.selected.value).toBe('p3')

    ctx2.setProject('p4')
    expect(replace).toHaveBeenCalledWith({ query: { projectId: 'p4' } })
    ctx2.setProject('')
    expect(replace).toHaveBeenCalledWith({ query: {} })
  })

  it('writes URL projectId into storage when already present (plan g1.2)', () => {
    route.query = { projectId: 'proj-b', run: 'run-1' }
    writeStoredProjectId('proj-a')
    const ctx = useProjectContext()
    ctx.ensureHydrated()
    expect(localStorage.getItem(PROJECT_CONTEXT_STORAGE_KEY)).toBe('proj-b')
    expect(replace).not.toHaveBeenCalled()
  })

  it('does not hydrate stored project over a deep-link wait (plan g1.2)', () => {
    writeStoredProjectId('proj-a')
    route.query = { run: 'run-1', node: 'n1' }
    const ctx = useProjectContext()
    ctx.ensureHydrated()
    expect(replace).not.toHaveBeenCalled()
    expect(ctx.selected.value).toBe('')
    expect(localStorage.getItem(PROJECT_CONTEXT_STORAGE_KEY)).toBe('proj-a')
  })

  it('still hydrates stored project when there is no ?run= deep link', () => {
    writeStoredProjectId('proj-a')
    route.query = { wf: 'wf-1' }
    const ctx = useProjectContext()
    ctx.ensureHydrated()
    expect(replace).toHaveBeenCalledWith({ query: { wf: 'wf-1', projectId: 'proj-a' } })
  })
})
