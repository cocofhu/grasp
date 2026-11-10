import { expect, test } from '@playwright/test'

test('the home Solo mode starts a Solo without a workflow launch', async ({ page }) => {
  let soloCreates = 0
  let workflowCreates = 0
  await page.addInitScript(() => { localStorage.setItem('grasp.home.mode', 'workflow'); localStorage.setItem('grasp.home.lastSoloAgent', 'Solo') })
  await page.route('**/api/**', async route => {
    const request = route.request(); const path = new URL(request.url()).pathname
    if (!path.startsWith('/api/')) { await route.continue(); return }
    let response: unknown = { items: [] }
    if (path === '/api/agents') response = [{ name: 'Solo', acpBackend: 'cursor' }]
    else if (path === '/api/projects') response = []
    else if (path === '/api/solo/creation-requests' && request.method() === 'POST') { soloCreates++; response = { id: 'solo-new', userId: 'home-preview', clientRequestId: request.postDataJSON().clientRequestId, initialTurnId: 'first-new', status: 'starting', agentName: 'Solo' } }
    else if (path.includes('/workflows/') && request.method() === 'POST') workflowCreates++
    else if (path === '/api/solo/sessions') response = { items: [], maxSandboxes: 5 }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(response) })
  })
  await page.goto('/dashboard-home-chat.html')
  await expect(page.getByTestId('home-mode-workflow')).toHaveAttribute('aria-selected', 'true')
  await page.getByTestId('home-mode-solo').click()
  await expect(page.getByTestId('home-workflow-cards')).toHaveCount(0)
  await page.getByTestId('home-composer-input').fill('评估这段代码')
  expect(soloCreates).toBe(0)
  expect(workflowCreates).toBe(0)
  await page.getByTestId('home-composer-send').click()
  await expect(page.getByTestId('solo-chat-page')).toHaveText('solo-new')
  expect(soloCreates).toBe(1)
  expect(workflowCreates).toBe(0)
})

for (const entry of ['home', 'modal'] as const) {
  test(`${entry} restores the first creation request after a real reload and retries its frozen nonce only on click`, async ({ page }, testInfo) => {
    const requests: Record<string, unknown>[] = []
    let legacyCreates = 0
    let receipt: object | undefined
    await page.addInitScript(() => { localStorage.setItem('grasp.home.mode', 'solo'); localStorage.setItem('grasp.home.lastSoloAgent', 'Solo') })
    await page.route('**/api/**', async route => {
      const request = route.request()
      const path = new URL(request.url()).pathname
      if (!path.startsWith('/api/')) { await route.continue(); return }
      let response: unknown = { items: [] }
      if (path === '/api/agents') response = [{ name: 'Solo', acpBackend: 'cursor' }]
      else if (path === '/api/projects') response = []
      else if (path === '/api/solo/sessions' && request.method() === 'POST') legacyCreates++
      else if (path === '/api/solo/sessions') response = { items: [], maxSandboxes: 5 }
      else if (path.startsWith('/api/solo/creation-requests/') && request.method() === 'GET') {
        if (!receipt) return route.fulfill({ status: 404, json: { code: 'creation_not_found', error: 'not accepted' } })
        response = receipt
      } else if (path === '/api/solo/creation-requests' && request.method() === 'POST') {
        const body = request.postDataJSON()
        requests.push(body)
        const saved = await page.evaluate(async () => {
          return new Promise<unknown[]>((resolve, reject) => {
            const opening = indexedDB.open('grasp-solo-outbox-v1')
            opening.onerror = () => reject(opening.error)
            opening.onsuccess = () => {
              const db = opening.result
              const read = db.transaction('requests').objectStore('requests').getAll()
              read.onsuccess = () => { db.close(); resolve(read.result) }
              read.onerror = () => { db.close(); reject(read.error) }
            }
          })
        })
        expect(saved).toHaveLength(1)
        expect(saved[0]).toMatchObject({ id: body.clientRequestId, kind: 'creation', request: body })
        if (requests.length === 1) return route.fulfill({ status: 503, json: { error: 'acknowledgement unavailable' } })
        receipt = { id: 'once-created', userId: 'home-preview', clientRequestId: body.clientRequestId, initialTurnId: 'first-once', status: 'starting', agentName: 'Solo' }
        response = receipt
      }
      await route.fulfill({ status: 200, json: response })
    })
    await page.goto(`/dashboard-home-chat.html${entry === 'modal' ? '?create=1' : ''}`)
    const input = page.getByTestId(entry === 'modal' ? 'solo-create-text' : 'home-composer-input')
    const submit = page.getByTestId(entry === 'modal' ? 'solo-create-submit' : 'home-composer-send')
    await input.fill('Preserve this original first message')
    await submit.click()
    await expect(page.getByTestId('solo-creation-recovery')).toBeVisible()
    await expect(page.getByRole('alert')).toContainText('acknowledgement unavailable')
    expect(requests).toHaveLength(1)
    await page.reload()
    await expect(page.getByTestId('solo-creation-retry')).toBeEnabled()
    await expect(submit).toBeDisabled()
    expect(requests).toHaveLength(1)
    await page.screenshot({ path: testInfo.outputPath('solo-creation-recovery.png'), fullPage: true })
    await input.fill('A different draft must not replace the saved request')
    await page.getByTestId('solo-creation-retry').click()
    await expect(page.getByTestId('solo-chat-page')).toHaveText('once-created')
    expect(requests).toHaveLength(2)
    expect(requests[1]).toEqual(requests[0])
    expect(legacyCreates).toBe(0)
  })
}

test('a lost first-create acknowledgement opens the accepted chat after reload without another POST', async ({ page }) => {
  let creates = 0
  let receipt: object | undefined
  await page.addInitScript(() => { localStorage.setItem('grasp.home.mode', 'solo'); localStorage.setItem('grasp.home.lastSoloAgent', 'Solo') })
  await page.route('**/api/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (!path.startsWith('/api/')) { await route.continue(); return }
    let response: unknown = { items: [] }
    if (path === '/api/agents') response = [{ name: 'Solo', acpBackend: 'cursor' }]
    else if (path === '/api/projects') response = []
    else if (path === '/api/solo/sessions') response = { items: [], maxSandboxes: 5 }
    else if (path === '/api/solo/creation-requests' && request.method() === 'POST') {
      creates++
      receipt = { id: 'already-accepted', userId: 'home-preview', clientRequestId: request.postDataJSON().clientRequestId, initialTurnId: 'first-accepted', status: 'starting', agentName: 'Solo' }
      return route.fulfill({ status: 503, json: { error: 'response was lost' } })
    } else if (path.startsWith('/api/solo/creation-requests/')) response = receipt
    await route.fulfill({ status: 200, json: response })
  })
  await page.goto('/dashboard-home-chat.html')
  await page.getByTestId('home-composer-input').fill('Run exactly this request once')
  await page.getByTestId('home-composer-send').click()
  await expect(page.getByRole('alert')).toContainText('response was lost')
  await page.reload()
  await expect(page.getByTestId('solo-creation-retry')).toHaveText('打开已创建的会话')
  expect(creates).toBe(1)
  await page.getByTestId('solo-creation-retry').click()
  await expect(page.getByTestId('solo-chat-page')).toHaveText('already-accepted')
  expect(creates).toBe(1)
})
