import { test, expect } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'

test('credential editing uses workspace settings, sends an explicit value and keeps secrets masked', async ({ page }) => {
  await installWorkspaceApiDefaults(page)
  const item = { id: 'cursor-api', type: 'ai', kind: 'ai', name: 'Cursor API key', provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'test-…1234' }
  const puts: unknown[] = []
  await routeApi(page, '**/api/workspace/credentials', async (route) => route.fulfill({ json: { items: [item] } }))
  await routeApi(page, '**/api/workspace/credentials/cursor-api', async (route) => {
    puts.push(route.request().postDataJSON())
    await route.fulfill({ json: { ...item, masked: 'test-…5678' } })
  })
  await page.goto('/workspace-config.html?view=/settings/credentials')
  await expect(page.getByTestId('project-credentials-panel')).toBeVisible()
  await expect(page.getByTestId('project-credential-masked')).toHaveText('test-…1234')
  await page.getByTestId('project-credential-edit-cursor-api').click()
  const input = page.getByTestId('project-credential-input-cursor-api')
  await expect(input).toHaveValue('')
  await input.fill('test-replacement-5678')
  await page.getByTestId('project-credential-save-cursor-api').click()
  await expect.poll(() => puts).toEqual([expect.objectContaining({ value: 'test-replacement-5678', envKey: 'GRASP_CURSOR_API_KEY' })])
  await expect(page.getByTestId('project-credential-masked')).toHaveText('test-…5678')
  await expect(input).toHaveCount(0)
  await expect(page.getByTestId('project-credentials-panel')).not.toContainText('test-replacement-5678')
})
