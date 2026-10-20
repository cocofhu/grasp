// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'

vi.mock('@/lib/shared/authGuard', () => ({ installAuthGuard: () => {} }))
vi.mock('@/lib/shared/routePending', () => ({ installRoutePendingGuards: () => {} }))

const { createAppRouter } = await import('./index')

describe('legacy and unknown routes', () => {
  it.each(['/projects', '/projects/proj-796910b5', '/projects/proj-1/board?tab=x'])('redirects %s to the workflow list', async (path) => {
    const router = createAppRouter(createMemoryHistory())
    await router.push(path)
    expect(router.currentRoute.value.path).toBe('/workflows')
  })

  it('redirects unknown paths to the dashboard instead of a blank page', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/no-such-page')
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it('keeps existing routes', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/settings/workspace')
    expect(router.currentRoute.value.name).toBe('workspaceInfo')
  })
})
