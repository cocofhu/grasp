import type { Page } from '@playwright/test'
import { routeApi } from './apiRoute'
import { seedOnboardingDismissed } from './onboarding'

/** Shell defaults; tests register their behavior-specific API handlers afterwards. */
export async function installWorkspaceApiDefaults(page: Page) {
  await seedOnboardingDismissed(page)
  await routeApi(page, '**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const pageResult = { items: [], total: 0, page: 1, pageSize: 20, hasMore: false }
    let body: unknown = pageResult
    if (path === '/api/workspace') body = { name: '工作区', description: '', variables: [] }
    else if (path === '/api/auth/me') body = { username: 'workspace-config-test', expires_at: '2099-01-01T00:00:00Z', is_admin: true }
    else if (path === '/api/health') body = { status: 'ok', accepting: true }
    else if (path === '/api/settings/brand') body = { productName: 'Grasp' }
    else if (path === '/api/workflows' || path === '/api/workflows/catalog' || path === '/api/agents') body = []
    else if (path === '/api/workspace/pm-leader') body = { enabled: false, agentAvailable: false }
    else if (path === '/api/workspace/shared-agent-config') body = { env: {}, mcp: [], files: [], layout: {} }
    else if (path === '/api/workspace/channels') body = { items: [], freeAgents: [], secretsKeyConfigured: true }
    else if (path === '/api/workspace/run-tags') body = { tags: [] }
    else if (path === '/api/stats/dashboard') body = { totalRuns: 0, runningRuns: 0, pendingGates: 0, totalWorkflows: 0 }
    await route.fulfill({ json: body })
  })
}
