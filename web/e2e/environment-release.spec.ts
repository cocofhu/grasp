import { expect, test, type Page } from '@playwright/test'

async function setup(page: Page, loseAcknowledgement = false) {
  let state = 'prepared'
  let saved = false
  const requests: { path: string; method: string; body?: Record<string, unknown> }[] = []
  const base = '/api/solo/sessions/solo-1/environment-releases'
  const receipt = () => ({ id: 'private-release-receipt', state, wholeEnvironment: true, chatsRetained: true, snapshotsRetained: 2, returnPointsRetained: 1, workspacesAffected: 3, lossConfirmationRequired: true, requiresReconciliation: ['unknown', 'dispatching'].includes(state), createdAt: '2026-11-09T12:00:00Z' })
  await page.route('**/api/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (!path.startsWith('/api/')) { await route.continue(); return }
    let body: unknown = {}
    if (path.startsWith(base)) {
      requests.push({ path, method: request.method(), body: request.postData() ? request.postDataJSON() : undefined })
      if (path === base && request.method() === 'GET') body = { items: saved ? [receipt()] : [] }
      else if (path === base) { saved = true; body = receipt() }
      else if (path.endsWith('/confirm')) {
        state = loseAcknowledgement ? 'unknown' : 'confirmed'
        if (loseAcknowledgement) { await route.fulfill({ status: 503, json: { code: 'workspace_release_unavailable' } }); return }
        body = receipt()
      } else if (path.endsWith('/reconcile')) { state = 'released'; body = receipt() }
      else { if (state === 'confirmed') state = 'dispatching'; body = receipt() }
    }
    await route.fulfill({ status: 200, json: body })
  })
  await page.goto('/environment-release.html')
  return requests
}
async function preview(page: Page) {
  await page.getByTestId('release-open').click()
  await page.getByTestId('environment-release-preview').click()
  await expect(page.getByTestId('environment-release-receipt')).toContainText('待确认释放')
}
async function close(page: Page) { await page.locator('.fixed.inset-0.z-40').getByRole('button').first().click() }

test('whole-environment release requires separate permanent-loss confirmation and retains chat', async ({ page }) => {
  const requests = await setup(page)
  await page.getByTestId('resident-draft').fill('继续保留我的对话草稿')
  await preview(page)
  expect(requests.filter(item => item.path.endsWith('/confirm'))).toHaveLength(0)
  await expect(page.getByTestId('environment-release-confirm')).toBeDisabled()
  await expect(page.getByTestId('environment-release-receipt')).toContainText('3 个工作区')
  await expect(page.getByTestId('environment-release-receipt')).toContainText('2 份已保存快照和 1 个返回点')
  await expect(page.getByTestId('environment-release')).not.toContainText('private-release-receipt')
  const details = page.getByTestId('environment-release-details')
  await expect(details).not.toHaveAttribute('open', '')
  await details.locator('summary').click()
  await expect(details).toContainText('不能视为整个磁盘的备份')
  await details.locator('summary').click()
  await page.screenshot({ path: '/tmp/grasp-environment-release-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByTestId('environment-release-confirm')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: '/tmp/grasp-environment-release-mobile.png', fullPage: true })
  await page.getByTestId('environment-release-loss').check()
  await page.getByTestId('environment-release-confirm').click()
  await expect(page.getByTestId('environment-release-check')).toBeVisible()
  await page.getByTestId('environment-release-check').click()
  await expect(page.getByTestId('released-status')).toBeVisible()
  const confirms = requests.filter(item => item.path.endsWith('/confirm'))
  expect(confirms).toHaveLength(1)
  expect(confirms[0]!.body).toEqual({ clientRequestId: expect.any(String), confirmed: true, lossAccepted: true })
  await close(page)
  await expect(page.getByTestId('resident-draft')).toHaveValue('继续保留我的对话草稿')
})

test('uncertain release survives drawer close and reload with only explicit receipt reconciliation', async ({ page }) => {
  const requests = await setup(page, true)
  await preview(page)
  await page.getByTestId('environment-release-loss').check()
  await page.getByTestId('environment-release-confirm').click()
  await expect(page.getByRole('alert')).toBeVisible()
  await close(page)
  await page.getByTestId('release-open').click()
  await expect(page.getByTestId('environment-release-check')).toBeVisible()
  expect(requests.filter(item => item.path.endsWith('/confirm'))).toHaveLength(1)
  expect(requests.filter(item => item.path.endsWith('/reconcile'))).toHaveLength(0)
  await page.reload()
  await page.getByTestId('release-open').click()
  await expect(page.getByTestId('environment-release-check')).toBeVisible()
  expect(requests.filter(item => item.path.endsWith('/confirm'))).toHaveLength(1)
  expect(requests.filter(item => item.path.endsWith('/reconcile'))).toHaveLength(0)
  await page.getByTestId('environment-release-check').click()
  await expect(page.getByTestId('released-status')).toBeVisible()
  expect(requests.filter(item => item.path.endsWith('/confirm'))).toHaveLength(1)
  expect(requests.filter(item => item.path.endsWith('/reconcile'))).toHaveLength(1)
})
