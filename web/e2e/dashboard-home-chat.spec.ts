import { expect, test } from '@playwright/test'

test('home always starts a Solo and has no separate workflow launch entry', async ({ page }) => {
  let soloCreates = 0
  let workflowCreates = 0
  await page.addInitScript(() => { localStorage.setItem('grasp.home.mode', 'workflow'); localStorage.setItem('grasp.home.lastSoloAgent', 'Solo') })
  await page.route('**/api/**', async route => {
    const request = route.request(); const path = new URL(request.url()).pathname
    if (!path.startsWith('/api/')) { await route.continue(); return }
    let response: unknown = { items: [] }
    if (path === '/api/agents') response = [{ name: 'Solo', acpBackend: 'cursor' }]
    else if (path === '/api/projects') response = []
    else if (path === '/api/solo/sessions' && request.method() === 'POST') { soloCreates++; response = { id: 'solo-new', status: 'starting', agentName: 'Solo' } }
    else if (path.includes('/workflows/') && request.method() === 'POST') workflowCreates++
    else if (path === '/api/solo/sessions') response = { items: [], maxSandboxes: 5 }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(response) })
  })
  await page.goto('/dashboard-home-chat.html')
  await expect(page.getByTestId('home-mode-switch')).toHaveCount(0)
  await expect(page.getByTestId('home-workflow-cards')).toHaveCount(0)
  await page.getByTestId('home-composer-input').fill('评估这段代码')
  expect(soloCreates).toBe(0)
  expect(workflowCreates).toBe(0)
  await page.getByTestId('home-composer-send').click()
  await expect(page.getByTestId('solo-chat-page')).toHaveText('solo-new')
  expect(soloCreates).toBe(1)
  expect(workflowCreates).toBe(0)
})
