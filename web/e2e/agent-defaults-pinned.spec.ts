import { test, expect, type Page } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { dismissOnboardingIfOpen } from './helpers/onboarding'

/**
 * Agent 默认配置 lives in the Agent Studio list (pinned first item):
 * switching to it stays on the same page, switching back keeps the Agent's tab,
 * and the old /settings/shared-agent link lands on it.
 */
const AGENTS = ['alpha', 'beta'].map((name) => ({
  name,
  acpBackend: 'opencode',
  modelBinding: { kind: 'configuration', configurationId: 'opencode-team-model' },
  gitBindings: { items: [] },
  files: [{ path: 'AGENTS.md', content: `# ${name}\n` }],
  mcp: [{ name: `${name}-fs`, type: 'http', url: 'https://example.com/mcp' }],
  env: {},
  layout: { configRoot: '/root/.config/opencode', workspaceDir: '/root/workspace' },
}))

const SHARED = {
  files: [{ path: 'AGENTS.md', content: '# 团队默认\n' }],
  mcp: [{ name: 'github', type: 'http', url: 'https://example.com/github' }],
  env: { TZ: 'Asia/Shanghai' },
  layout: { configRoot: '/root/.config/opencode', workspaceDir: '/root/workspace' },
}

const MODEL_KEY = { id: 'team-model-key', name: 'Team model key', purpose: 'model_api', issuer: 'deepseek', secretKind: 'api_key', type: 'ai', provider: 'deepseek', configured: true, enabled: true }
const MODEL_CONNECTION = {
  id: 'team-model-service', name: 'DeepSeek service', providerId: 'deepseek', protocol: 'openai-chat-completions',
  baseUrl: 'https://api.deepseek.com/v1', credentialId: MODEL_KEY.id, auth: { scheme: 'bearer' },
  models: [{ id: 'deepseek-chat', source: 'manual' }], state: 'active', revision: 1,
}
const MODEL_CONFIGURATION = {
  id: 'opencode-team-model', name: 'OpenCode team model', backend: 'opencode',
  binding: { kind: 'connection', connectionId: MODEL_CONNECTION.id, modelId: 'deepseek-chat' }, state: 'active', revision: 1,
}

async function openStudio(page: Page, view: string) {
  await page.setViewportSize({ width: 1440, height: 900 })
  await installWorkspaceApiDefaults(page)
  await routeApi(page, '**/api/workspace/credentials', async (route) => route.fulfill({ json: { items: [MODEL_KEY] } }))
  await routeApi(page, '**/api/workspace/model-connections', async (route) => route.fulfill({ json: { items: [MODEL_CONNECTION] } }))
  await routeApi(page, '**/api/workspace/model-configurations', async (route) => route.fulfill({ json: { items: [MODEL_CONFIGURATION] } }))
  await routeApi(page, '**/api/agents', async (route) => {
    if (route.request().method() === 'GET') await route.fulfill({ json: AGENTS })
    else await route.continue()
  })
  await routeApi(page, '**/api/workspace/shared-agent-config', async (route) => { await route.fulfill({ json: SHARED }) })
  await page.goto(`/workspace-config.html?view=${encodeURIComponent(view)}`)
  await dismissOnboardingIfOpen(page)
}

const pinned = (page: Page) => page.getByTestId('resource-list-pinned').getByRole('button', { name: 'Agent 默认配置' })
const agentRow = (page: Page, name: string) => page.getByTestId('agent-resource-sidebar').locator(`[data-resource-key="${name}"]`)
const studioTab = (page: Page, label: RegExp) => page.locator('[data-test="studio-tab-strip"] button').filter({ hasText: label })

test('Agent 默认配置 is the first item of the Agent list and opens in the same page', async ({ page }) => {
  await openStudio(page, '/agents?agent=alpha')
  await expect(agentRow(page, 'alpha')).toHaveAttribute('aria-current', 'true')
  await pinned(page).click()
  await expect(page.getByTestId('studio-defaults-pane')).toBeVisible()
  await expect(pinned(page)).toHaveAttribute('aria-current', 'true')
  await expect(page.getByTestId('workspace-shared-agent-panel')).toBeVisible()
  await expect(page.getByTestId('shared-agent-subtab-mcp')).toContainText('MCP')
  await expect(page.locator('[data-testid^="shared-agent-subtab-"]')).toHaveCount(4)
  await expect(page.getByTestId('shared-agent-subtab-git')).toHaveCount(0)
  await expect(page.getByTestId('shared-agent-subtab-model')).toHaveCount(0)
  // Still Agent Studio: the Agent list stays on the left.
  await expect(agentRow(page, 'beta')).toBeVisible()
})

test('switching back from Agent 默认配置 keeps the Agent tab', async ({ page }) => {
  await openStudio(page, '/agents?agent=alpha')
  await studioTab(page, /^MCP/).click()
  await expect(studioTab(page, /^MCP/)).toHaveClass(/border-accent/)
  await pinned(page).click()
  await expect(page.getByTestId('studio-defaults-pane')).toBeVisible()
  await agentRow(page, 'alpha').click()
  await expect(page.getByTestId('studio-defaults-pane')).toHaveCount(0)
  await expect(studioTab(page, /^MCP/)).toHaveClass(/border-accent/)
  await expect(page.locator('[data-test="mcp-custom-name"]').first()).toHaveValue('alpha-fs')
})

test('old /settings/shared-agent link opens Agent 默认配置 in Agent Studio', async ({ page }) => {
  await openStudio(page, '/settings/shared-agent')
  await expect(page.getByTestId('studio-defaults-pane')).toBeVisible()
  await expect(pinned(page)).toHaveAttribute('aria-current', 'true')
  await expect(page.getByTestId('agent-resource-sidebar')).toBeVisible()
})

test('with no Agents the empty state still opens Agent 默认配置, and the old link lands there', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await installWorkspaceApiDefaults(page)
  await routeApi(page, '**/api/workspace/shared-agent-config', async (route) => { await route.fulfill({ json: SHARED }) })
  await page.goto(`/workspace-config.html?view=${encodeURIComponent('/agents')}`)
  await dismissOnboardingIfOpen(page)
  await expect(page.getByTestId('agent-studio-empty-team')).toBeVisible()
  await page.getByTestId('agent-studio-empty-defaults').click()
  await expect(page.getByTestId('studio-defaults-pane')).toBeVisible()
  // Only the pinned item in the list; no "暂无工作区" copy under it.
  await expect(page.getByTestId('resource-list-pinned')).toBeVisible()
  await expect(page.getByTestId('agent-resource-sidebar').getByTestId('resource-list-empty')).toHaveCount(0)
  await expect(page.getByTestId('workspace-shared-agent-panel')).toBeVisible()

  await page.goto(`/workspace-config.html?view=${encodeURIComponent('/settings/shared-agent')}`)
  await dismissOnboardingIfOpen(page)
  await expect(page.getByTestId('studio-defaults-pane')).toBeVisible()
  await expect(page.getByTestId('agent-studio-empty-team')).toHaveCount(0)
})
