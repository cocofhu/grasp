// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'

vi.mock('@/lib/shared/authGuard', () => ({ installAuthGuard: () => {} }))
vi.mock('@/lib/shared/routePending', () => ({ installRoutePendingGuards: () => {} }))

const { createAppRouter } = await import('./index')

describe('Agent 默认配置 old link', () => {
  it('redirects /settings/shared-agent to the pinned defaults item in Agent Studio', async () => {
    const router = createAppRouter(createMemoryHistory())
    // Capture the resolved target and stop there, so the lazy Studio chunk never loads.
    let target: { path: string; query: Record<string, unknown>; from?: string } | null = null
    router.beforeEach((to) => {
      target = { path: to.path, query: { ...to.query }, from: to.redirectedFrom?.path }
      return false
    })
    await router.push('/settings/shared-agent')
    expect(target).toEqual({ path: '/agents', query: { view: 'defaults' }, from: '/settings/shared-agent' })
  })
})
