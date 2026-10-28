// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { resetBrandSettingsForTest } from '@/lib/composables/useBrandSettings'

vi.mock('@/lib/shared/authGuard', () => ({ installAuthGuard: () => {} }))
vi.mock('@/lib/shared/routePending', () => ({ installRoutePendingGuards: () => {} }))
vi.mock('@/lib/api/api', () => ({
  api: { getSettings: vi.fn() },
}))
vi.mock('@/views/DashboardView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/SandboxListView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/SandboxConsoleView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/LoginView.vue', () => ({ default: { render: () => null } }))

const { api } = await import('@/lib/api/api')
const { createAppRouter } = await import('./index')

describe('worktree sandbox routes', () => {
  beforeEach(() => {
    resetBrandSettingsForTest()
    vi.mocked(api.getSettings).mockReset()
  })

  afterEach(() => {
    resetBrandSettingsForTest()
  })

  it('sends sandbox pages back to the dashboard', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({
      items: [],
      brand: { product_name: '', home_subtitle: '' },
      execution_mode: 'worktree',
    })
    const router = createAppRouter(createMemoryHistory())
    await router.push('/sandboxes')
    expect(router.currentRoute.value.path).toBe('/dashboard')
    await router.push('/sandboxes/7/console')
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it('leaves sandbox pages in place for the sandbox image', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({
      items: [],
      brand: { product_name: '', home_subtitle: '' },
      execution_mode: 'sandbox',
    })
    const router = createAppRouter(createMemoryHistory())
    await router.push('/sandboxes')
    expect(router.currentRoute.value.name).toBe('sandboxes')
    await router.push('/sandboxes/7/console')
    expect(router.currentRoute.value.name).toBe('sandbox-console')
  })
})
