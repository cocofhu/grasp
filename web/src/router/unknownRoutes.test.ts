// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'

vi.mock('@/lib/shared/authGuard', () => ({ installAuthGuard: () => {} }))
vi.mock('@/lib/shared/routePending', () => ({ installRoutePendingGuards: () => {} }))

const { createAppRouter } = await import('./index')

describe('unknown routes', () => {
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
