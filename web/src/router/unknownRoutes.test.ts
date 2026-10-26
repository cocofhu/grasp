// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'

vi.mock('@/lib/shared/authGuard', () => ({ installAuthGuard: () => {} }))
vi.mock('@/lib/shared/routePending', () => ({ installRoutePendingGuards: () => {} }))

// These tests exercise the real route table, redirects and URL state. Stub view
// rendering so navigating lazy routes does not compile their unrelated UI trees.
vi.mock('@/views/LoginView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/DashboardView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/GatesInboxView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/RunDetailView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/RunLiveView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/PublicGateApprovalView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/SettingsView.vue', () => ({ default: { render: () => null } }))

const { createAppRouter } = await import('./index')

describe('unknown routes', () => {
  it('redirects unknown paths to the dashboard instead of a blank page', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/no-such-page')
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })

  it('sends a retired /gates bookmark to the dashboard without a node path', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/gates?run=run-1&node=clarify&wf=wf-1&tag=urgent&share=1')
    expect(router.currentRoute.value.path).toBe('/dashboard')
    expect(router.currentRoute.value.fullPath).not.toContain('/nodes/')
  })

  it('opens a node conversation on /runs/:runId/nodes/:nodeId and keeps run detail', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/runs/run-1/nodes/clarify?wf=wf-1&tag=urgent')
    expect(router.currentRoute.value.name).toBe('gates')
    expect(router.currentRoute.value.params).toMatchObject({ runId: 'run-1', nodeId: 'clarify' })
    expect(router.currentRoute.value.query).toMatchObject({ wf: 'wf-1', tag: 'urgent' })
    await router.push('/runs/run-1')
    expect(router.currentRoute.value.name).toBe('run-detail')
    await router.push('/runs/run-1/live')
    expect(router.currentRoute.value.name).toBe('run-live')
    await router.push('/public/approvals')
    expect(router.currentRoute.value.name).toBe('public-gate-approval')
  })

  it('sends the retired workspace info address to general settings', async () => {
    const router = createAppRouter(createMemoryHistory())
    await router.push('/settings/workspace?integrations=1')
    expect(router.currentRoute.value.name).toBe('settings')
    expect(router.currentRoute.value.path).toBe('/settings')
    expect(router.currentRoute.value.query).toEqual({})
  })
})
