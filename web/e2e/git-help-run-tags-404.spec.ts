import { test, expect } from '@playwright/test'
import path from 'node:path'
import fs from 'node:fs'

const OUT = path.join('/tmp', 'git-help-404-shots')

test.beforeAll(() => {
  fs.mkdirSync(OUT, { recursive: true })
})

test('run-tags 404 时点 git-help-link 仍打开环境变量与凭据', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (err) => {
    pageErrors.push(String(err))
  })

  await page.route('**/api/**', async (route) => {
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    if (url.pathname.includes('/run-tags')) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'not found' }),
      })
      return
    }
    if (url.pathname === '/api/agents' && route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ name: 'e2e-help-404' }),
      })
      return
    }
    if (url.pathname === '/api/workspace/credentials') return route.fulfill({ json: { items: [{ id: 'cursor-key', name: 'Cursor team', purpose: 'tool_auth', toolId: 'cursor', configured: true }] } })
    if (url.pathname === '/api/workspace/model-configurations') return route.fulfill({ json: { items: [{ id: 'cursor-profile', name: 'Cursor 日常模型', backend: 'cursor', binding: { kind: 'platform', credentialId: 'cursor-key', modelId: 'composer-1' }, state: 'active', revision: 1 }] } })
    if (url.pathname === '/api/workspace/model-connections') return route.fulfill({ json: { items: [] } })
    if (url.pathname === '/api/workspace/tool-capabilities') return route.fulfill({ json: { items: [{ toolId: 'cursor', modes: ['platform'], protocols: [], adapterState: 'existing' }] } })
    if (url.pathname === '/api/workspace/model-defaults') return route.fulfill({ json: { bindingsByBackend: {} } })
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
  })

  await page.goto('/agent-create-wizard.html', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('agent-create-wizard-root')).toBeVisible()

  await page.locator('#wiz-name-input').fill('e2e-help-404')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await page.getByTestId('agent-wizard-backend-cursor').click()
  await page.getByTestId('model-binding-configuration-select').locator('[data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])').locator('[data-test="app-select-option-cursor-profile"]').click()
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('Git')
  await expect(page.locator('[data-test="git-help-link"]')).toBeVisible()
  await page.screenshot({ path: path.join(OUT, '01-git-step.png'), fullPage: true })

  // Concurrent noise: fire a run-tags request that 404s while opening help
  await page.evaluate(async () => {
    try {
      await fetch('/api/workspace/run-tags')
    } catch {
      /* ignore */
    }
  })

  await page.locator('[data-test="git-help-link"]').click()
  await expect(page.getByText('环境变量与凭据')).toBeVisible()
  await expect(page.getByText(/GITHUB_TOKEN|凭据可通过环境变量/)).toBeVisible()
  await page.screenshot({ path: path.join(OUT, '02-help-modal-under-404.png'), fullPage: true })

  expect(pageErrors.filter((e) => /is not iterable/i.test(e))).toEqual([])

  await page.locator('[data-test="env-help-got-it"]').click()
  await expect(page.getByText('环境变量与凭据')).toHaveCount(0)
  await expect(page.locator('.sec-head h3')).toHaveText('Git')
  await page.screenshot({ path: path.join(OUT, '03-after-close.png'), fullPage: true })
})
