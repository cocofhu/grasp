// @vitest-environment happy-dom
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({ getWorkspace: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))

async function setup() {
  const { useAuth } = await import('./useAuth')
  const { useWorkspaceContext } = await import('./useWorkspaceContext')
  return { auth: useAuth(), context: useWorkspaceContext() }
}
const workspace = (name = 'Studio') => ({ name, description: '', variables: [], createdAt: '', updatedAt: '' })
const tick = async () => { await nextTick(); await Promise.resolve(); await Promise.resolve(); await nextTick() }
beforeEach(() => { vi.resetModules(); mocks.getWorkspace.mockReset() })

describe('workspace context lifecycle', () => {
  it('waits for authentication and deduplicates concurrent consumers without guessing a default workspace', async () => {
    let resolve!: (value: ReturnType<typeof workspace>) => void
    mocks.getWorkspace.mockReturnValue(new Promise((done) => { resolve = done }))
    const { auth, context } = await setup()
    await context.ensureLoaded()
    expect(mocks.getWorkspace).not.toHaveBeenCalled()
    auth.setUser({ username: 'alice', expiresAt: '' })
    await nextTick()
    const first = context.ensureLoaded()
    const second = context.ensureLoaded()
    expect(first).toBe(second)
    expect(mocks.getWorkspace).toHaveBeenCalledTimes(1)
    resolve(workspace())
    await expect(first).resolves.toEqual(workspace())
    expect(context.workspace.value?.name).toBe('Studio')
    auth.setUser({ username: 'alice', expiresAt: 'renewed' })
    await nextTick()
    expect(mocks.getWorkspace).toHaveBeenCalledTimes(1)
  })
  it('cancels and ignores an old identity response after logout and another login', async () => {
    let oldResult!: (value: ReturnType<typeof workspace>) => void
    mocks.getWorkspace.mockReturnValueOnce(new Promise((done) => { oldResult = done })).mockResolvedValueOnce(workspace('Bob Studio'))
    const { auth, context } = await setup()
    auth.setUser({ username: 'alice', expiresAt: '' })
    await nextTick()
    const oldSignal = mocks.getWorkspace.mock.calls[0][0].signal as AbortSignal
    auth.clearUser()
    await nextTick()
    expect(oldSignal.aborted).toBe(true)
    expect(context.workspace.value).toBeNull()
    auth.setUser({ username: 'bob', expiresAt: '' })
    await tick()
    oldResult(workspace('Alice Private Studio'))
    await tick()
    expect(context.workspace.value?.name).toBe('Bob Studio')
    expect(context.error.value).toBe('')
  })
  it('reports failure, retries, and does not invent a workspace when the response is invalid', async () => {
    mocks.getWorkspace.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(null).mockResolvedValueOnce({}).mockResolvedValueOnce(workspace())
    const { auth, context } = await setup()
    auth.setUser({ username: 'alice', expiresAt: '' })
    await tick()
    expect(context.error.value).toBe('offline')
    expect(context.workspace.value).toBeNull()
    await context.reload()
    expect(context.error.value).not.toBe('')
    expect(context.workspace.value).toBeNull()
    await context.reload()
    expect(context.error.value).not.toBe('')
    expect(context.workspace.value).toBeNull()
    await context.reload()
    expect(context.workspace.value?.name).toBe('Studio')
    expect(context.error.value).toBe('')
    expect(context.loading.value).toBe(false)
  })
})
