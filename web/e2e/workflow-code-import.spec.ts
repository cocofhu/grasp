import { expect, test, type Page } from '@playwright/test'

const importedDirectory = `.grasp-code-imports/${'f'.repeat(64)}`

async function setup(page: Page, loseConfirmation = false) {
  const requests: { path: string; method: string; body?: Record<string, unknown> }[] = []
  const source = { nodeId: 'implement', iteration: 2, attempt: 1, shared: false, available: true, state: 'running', createdAt: '2026-11-09T09:00:00Z' }
  const repository = { path: 'app', commit: 'a'.repeat(40), head: 'a'.repeat(40), supported: true, dirty: true, untracked: true, files: 4, bytes: 2048 }
  let state = 'prepared'
  let saved = false
  const receipt = () => ({ id: 'private-receipt-token', source: { runId: 'completed-task', nodeId: 'implement', iteration: 2, attempt: 1 }, state, repositories: [repository], destination: importedDirectory, directory: state === 'applied' ? importedDirectory : undefined, paths: state === 'applied' ? [`${importedDirectory}/app`] : [], createdAt: '2026-11-09T09:00:00Z' })
  const base = '/api/solo/sessions/solo-1/workspace-imports'
  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (!path.startsWith('/api/')) { await route.continue(); return }
    let body: unknown = {}
    if (path.startsWith(base)) {
      requests.push({ path, method: request.method(), body: request.method() === 'POST' && request.postData() ? request.postDataJSON() : undefined })
      if (path === `${base}/sources`) body = [source]
      else if (path === `${base}/inventory`) body = { version: 1, repositories: [repository], workingChanges: false, nonGit: false, ignoredFiles: false }
      else if (path === base && request.method() === 'GET') body = saved ? [receipt()] : []
      else if (path === base && request.method() === 'POST') { saved = true; body = receipt() }
      else if (path.endsWith('/confirm')) {
        state = loseConfirmation ? 'unknown' : 'applied'
        if (loseConfirmation) { await route.fulfill({ status: 409, json: { code: 'workspace_import_unknown', error: '导入结果尚未确认，请先核对' } }); return }
        body = receipt()
      } else if (path.endsWith('/reconcile')) { state = 'applied'; body = receipt() }
      else body = receipt()
    } else if (path.endsWith('/solo/sessions/solo-1/workflows')) {
      body = { items: [
        { id: 'completed-task', title: '实现功能', workflowName: '代码工作流', status: 'completed', controlVersion: 1, nodes: [{ id: 'implement', label: '实现', type: 'agent', status: 'completed', iteration: 2 }] },
        { id: 'running-task', title: '继续分析', workflowName: '代码工作流', status: 'running', controlVersion: 0, nodes: [] },
      ], workflows: { enabled: true, locked: false, source: 'db' } }
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
  })
  await page.goto('/solo-workflows.html')
  return { requests, base }
}

async function preview(page: Page) {
  await page.getByTestId('solo-workflow-completed-task').getByTestId('workflow-import-code').click()
  await page.getByTestId('code-import-source').selectOption(JSON.stringify(['implement', 2, 1]))
  await page.getByTestId('code-import-inventory').click()
  await expect(page.getByLabel('app 完整 Git 提交 SHA')).toHaveValue('a'.repeat(40))
  await page.getByTestId('code-import-preview').click()
  await expect(page.getByTestId('code-import-confirm')).toBeVisible()
}

test('terminal workflow card imports explicitly into a new directory while Solo remains available', async ({ page }, testInfo) => {
  const { requests } = await setup(page)
  await page.getByTestId('resident-solo-draft').fill('继续我的 Solo 工作，不覆盖未提交代码')
  await expect(page.getByTestId('solo-workflow-running-task').getByTestId('workflow-import-code')).toHaveCount(0)
  await preview(page)
  expect(requests.filter(request => request.path.endsWith('/confirm'))).toHaveLength(0)
  await expect(page.getByTestId('workflow-code-import')).toContainText('未提交改动')
  await expect(page.getByTestId('workflow-code-import')).toContainText('包括未推送提交')
  await expect(page.getByTestId('workflow-code-import')).not.toContainText('private-receipt-token')
  const limits = page.getByTestId('code-import-limits')
  await expect(limits).not.toHaveAttribute('open', '')
  await expect(limits.locator('p').first()).not.toBeVisible()
  await limits.locator('summary').click()
  await expect(limits.locator('p').first()).toBeVisible()
  await expect(limits).toContainText('64 MiB')
  await limits.locator('summary').click()
  await page.getByTestId('code-import-confirm').click()
  await expect(page.getByTestId('code-import-result-path')).toHaveText(`${importedDirectory}/app`)
  const confirmations = requests.filter(request => request.path.endsWith('/confirm'))
  expect(confirmations).toHaveLength(1)
  expect(Object.keys(confirmations[0]!.body!).sort()).toEqual(['clientRequestId', 'confirmed'])
  expect(confirmations[0]!.body!.confirmed).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('workflow-code-import.png'), fullPage: true })
  await page.screenshot({ path: '/tmp/grasp-workspace-import-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByTestId('code-import-result-path')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: '/tmp/grasp-workspace-import-mobile.png', fullPage: true })
  await page.locator('.fixed.inset-0.z-40').getByRole('button').first().click()
  await expect(page.getByTestId('resident-solo-draft')).toHaveValue('继续我的 Solo 工作，不覆盖未提交代码')
  expect(page.url()).toContain('solo-workflows.html')
})

test('lost import confirmation survives drawer close and page reload without replaying the write', async ({ page }) => {
  const { requests } = await setup(page, true)
  await preview(page)
  await page.getByTestId('code-import-confirm').click()
  await expect(page.getByRole('alert')).toContainText('导入结果尚未确认')
  await page.locator('.fixed.inset-0.z-40').getByRole('button').first().click()
  await page.getByTestId('solo-workflow-completed-task').getByTestId('workflow-import-code').click()
  await expect(page.getByTestId('code-import-reconcile')).toBeVisible()
  expect(requests.filter(request => request.path.endsWith('/confirm'))).toHaveLength(1)
  expect(requests.filter(request => request.path.endsWith('/reconcile'))).toHaveLength(0)
  await page.reload()
  await page.getByTestId('solo-workflow-completed-task').getByTestId('workflow-import-code').click()
  await page.getByTestId('code-import-previous').click()
  await expect(page.getByTestId('code-import-reconcile')).toBeVisible()
  expect(requests.filter(request => request.path.endsWith('/confirm'))).toHaveLength(1)
  expect(requests.filter(request => request.path.endsWith('/reconcile'))).toHaveLength(0)
  await page.getByTestId('code-import-reconcile').click()
  await expect(page.getByTestId('code-import-result-path')).toHaveText(`${importedDirectory}/app`)
  expect(requests.filter(request => request.path.endsWith('/confirm'))).toHaveLength(1)
  expect(requests.filter(request => request.path.endsWith('/reconcile'))).toHaveLength(1)
})
