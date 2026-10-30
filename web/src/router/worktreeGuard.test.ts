// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { resetBrandSettingsForTest, useExecutionMode } from '@/lib/composables/useBrandSettings'
import { bindingProblem } from '@/lib/agent/modelRouting'

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

  it('treats local mode as worktree for sandbox routes and exposes credential-free local state', async () => {
    vi.mocked(api.getSettings).mockResolvedValue({
      items: [],
      brand: { product_name: '', home_subtitle: '' },
      execution_mode: 'local',
      installed_tools: ['deepseek_harness', 'claude_code'],
      host_git_user: { name: 'local-dev', email: 'dev@localhost' },
    })
    const router = createAppRouter(createMemoryHistory())
    await router.push('/sandboxes')
    expect(router.currentRoute.value.path).toBe('/dashboard')
    const { mode, worktreeMode, localMode, installedTools, hostGitUser } = useExecutionMode()
    expect(mode.value).toBe('local')
    expect(worktreeMode.value).toBe(true)
    expect(localMode.value).toBe(true)
    expect(installedTools.value).toEqual(['deepseek_harness', 'claude_code'])
    expect(hostGitUser.value).toEqual({ name: 'local-dev', email: 'dev@localhost' })
    expect(bindingProblem({ kind: 'local', modelId: 'deepseek-flash' }, 'deepseek_harness', [], [], [])).toBe('')
  })
})
