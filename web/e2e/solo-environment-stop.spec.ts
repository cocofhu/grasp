import { expect, test, type Page } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'

async function setup(page: Page) {
  await installWorkspaceApiDefaults(page)
  const sessions = ['solo-1', 'solo-2'].map((id, index) => ({
    id, userId: 'workspace-config-test', title: index ? '另一个会话' : '停止环境', agentName: 'Demo', status: 'running', error: '',
    busy: false, waiting: 0, restarts: 0, createdAt: '2026-11-11T00:00:00Z', updatedAt: '2026-11-11T00:00:00Z',
    sandbox: { id: index + 7, name: `sbx-${id}`, desiredState: 'running', status: 'running', error: '' },
  }))
  let release!: () => void
  const stopped = new Promise<void>(resolve => { release = resolve })
  const calls: string[] = []
  await page.route('**/api/solo/sessions**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/solo/sessions') { await route.fulfill({ json: { sessions, maxSandboxes: 5 } }); return }
    const session = sessions.find(row => path.startsWith(`/api/solo/sessions/${row.id}`))
    if (!session) { await route.fallback(); return }
    if (path.endsWith('/environment/stop')) {
      calls.push(session.id)
      await stopped
      session.status = 'stopped'
      session.sandbox = { ...session.sandbox, desiredState: 'stopped', status: 'stopped' }
      await route.fulfill({ json: session })
    } else if (path.endsWith('/messages')) await route.fulfill({ json: [] })
    else if (path.endsWith('/resources')) await route.fulfill({ json: { runId: '', artifacts: [], ports: [] } })
    else await route.fulfill({ json: session })
  })
  await page.routeWebSocket('**/api/solo/sessions/*/chat*', ws => ws.send(JSON.stringify({ type: 'session', event: 'queue_state', busy: false, items: [] })))
  await page.goto('/workspace-config.html?view=/solo/solo-1')
  await expect(page.getByTestId('solo-session-pane')).toBeVisible()
  return { release, calls }
}

test('the Solo menu offers no cancel and a slow stop shows progress without duplicate requests', async ({ page }) => {
  const { release, calls } = await setup(page)
  const row = page.locator('[data-kind="solo"][data-key="solo-1"]')
  await row.click({ button: 'right' })
  await expect(page.getByTestId('nav-pending-menu-cancel')).toHaveCount(0)
  await page.getByTestId('nav-solo-menu-environment').click()
  await expect(row.getByTestId('nav-solo-environment-phase')).toHaveText('正在停止…')
  await expect(row).toHaveAttribute('data-motion', 'spin')
  await row.click({ button: 'right' })
  const pending = page.getByTestId('nav-solo-menu-environment')
  await expect(pending).toBeDisabled()
  await expect(pending).toContainText('正在停止…')
  await page.keyboard.press('Escape')
  await page.locator('[data-kind="solo"][data-key="solo-2"]').click()
  await expect(page.getByTestId('solo-input')).toBeEnabled()
  await page.screenshot({ path: '/tmp/grasp-solo-stop-pending.png' })
  expect(calls).toEqual(['solo-1'])
  release()
  await expect(row.getByTestId('nav-solo-environment-phase')).toHaveCount(0)
  await row.click({ button: 'right' })
  await expect(page.getByTestId('nav-solo-menu-environment')).toContainText('恢复环境')
  expect(calls).toEqual(['solo-1'])
})
