/**
 * Browser acceptance: PM Leader gate-auto var + prompt text fields (Demo-aligned).
 */
import { expect, test } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', 'test-results', 'gate-auto-pm-settings')

test.describe('PM Leader gate-auto settings (browser)', () => {
  test('text var + optional prompt load/save; empty var allowed', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })

    let lastSaveBody: Record<string, unknown> | null = null
    await page.route('**/api/**', async (route) => {
      // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts.
      if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
        await route.continue()
        return
      }
      const url = new URL(route.request().url())
      const method = route.request().method()
      if (url.pathname === '/api/workspace' && method === 'GET') {
        await route.fulfill({
          json: {
            name: '测试工作区',
            description: '',
            variables: [],
            notifyPolicy: { enabled: true, defaultEvents: ['waiting_human', 'failed'], channelIds: [] },
          },
        })
        return
      }
      if (url.pathname === '/api/workspace/pm-leader' && method === 'GET') {
        await route.fulfill({
          status: 200,
          json: {
            enabled: true,
            agentAvailable: true,
            agentConfigRef: 'agent-1',
            enabledMcps: ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs'],
            gateAutoVar: 'pm_auto_gate',
            gateAutoPrompt: '优先批准低风险',
            aclNote: 'note',
          },
        })
        return
      }
      if (url.pathname === '/api/workspace/pm-leader' && method === 'PUT') {
        lastSaveBody = route.request().postDataJSON() as Record<string, unknown>
        await route.fulfill({
          status: 200,
          json: {
            enabled: true,
            agentAvailable: true,
            agentConfigRef: 'agent-1',
            enabledMcps: ['pm-progress', 'pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs'],
            gateAutoVar: lastSaveBody.gateAutoVar ?? '',
            gateAutoPrompt: lastSaveBody.gateAutoPrompt ?? '',
            aclNote: 'note',
          },
        })
        return
      }
      if (url.pathname.startsWith('/api/agents')) {
        await route.fulfill({ status: 200, json: [{ name: 'agent-1' }] })
        return
      }
      if (url.pathname === '/api/workspace/channels' && method === 'GET') {
        await route.fulfill({
          status: 200,
          json: { items: [], freeAgents: ['agent-1'], secretsKeyConfigured: true },
        })
        return
      }
      if (['/api/workspace/pm/threads', '/api/workspace/notify-receipts'].includes(url.pathname) && method === 'GET') {
        await route.fulfill({ status: 200, json: { items: [] } })
        return
      }
      await route.fulfill({ status: 404, json: { error: 'not mocked' } })
    })

    await page.goto('/gate-auto-pm-settings.html')
    await expect(page.getByTestId('gate-auto-settings-root')).toBeVisible({ timeout: 15_000 })
    const varInput = page.getByTestId('pm-gate-auto-var')
    const promptInput = page.getByTestId('pm-gate-auto-prompt')
    await expect(varInput).toBeVisible()
    await expect(promptInput).toBeVisible()
    await expect(varInput).toHaveValue('pm_auto_gate')
    await expect(promptInput).toHaveValue('优先批准低风险')
    // Must be text input (type omitted ⇒ HTML default text), not select/dropdown
    await expect(varInput).toHaveJSProperty('tagName', 'INPUT')
    await expect(varInput).toHaveJSProperty('type', 'text')
    await expect(page.locator('select#pm-gate-auto-var')).toHaveCount(0)
    await expect(page.getByText('遇门禁自动唤起 · 开关变量')).toBeVisible()
    await expect(page.getByText(/留空则能力关闭/)).toBeVisible()

    await page.screenshot({ path: path.join(shotDir, '01-settings-loaded.png'), fullPage: true })

    await varInput.fill('does_not_exist_yet')
    await promptInput.fill('')
    await page.getByTestId('pm-leader-save').click()
    await expect.poll(() => lastSaveBody?.gateAutoVar).toBe('does_not_exist_yet')
    await expect.poll(() => lastSaveBody?.gateAutoPrompt).toBe('')

    await varInput.fill('')
    await page.getByTestId('pm-leader-save').click()
    await expect.poll(() => lastSaveBody?.gateAutoVar).toBe('')

    await page.screenshot({ path: path.join(shotDir, '02-settings-empty-var.png'), fullPage: true })
  })
})
