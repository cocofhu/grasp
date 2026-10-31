import { test, expect, type Locator, type Page } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'

const codexAlias = 'Codex-Workspace-Team-Shared-Login-Credential-With-A-Long-Alias'
const credentialItems = [
  { id: 'cursor-api', purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', issuer: 'cursor', enabled: true, type: 'ai', name: 'Cursor team', provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'cursor-…1234' },
  { id: 'claude-api', purpose: 'tool_auth', toolId: 'claude_code', secretKind: 'api_key', issuer: 'claude_code', enabled: true, type: 'ai', name: 'Claude Code', provider: 'claude_code', envKey: 'GRASP_CLAUDE_API_KEY', configured: false },
  { id: 'codebuddy-api', purpose: 'tool_auth', toolId: 'codebuddy', secretKind: 'api_key', issuer: 'codebuddy', enabled: true, type: 'ai', name: 'CodeBuddy', provider: 'codebuddy', envKey: 'GRASP_CODEBUDDY_API_KEY', configured: false },
  { id: 'trae-api', purpose: 'tool_auth', toolId: 'trae', secretKind: 'token', issuer: 'trae', enabled: true, type: 'ai', name: 'Trae', provider: 'trae', envKey: 'GRASP_TRAE_API_KEY', configured: false },
  { id: 'codex-login', purpose: 'tool_auth', toolId: 'codex', secretKind: 'login_file', issuer: 'codex', enabled: true, type: 'ai', name: codexAlias, provider: 'codex', envKey: 'GRASP_CODEX_AUTH_JSON', configured: false },
  { id: 'opencode-official', purpose: 'tool_auth', toolId: 'opencode', secretKind: 'api_key', issuer: 'opencode', enabled: true, type: 'ai', name: 'OpenCode official account', provider: 'opencode', envKey: 'GRASP_OPENCODE_API_KEY', configured: true, masked: 'oc-…4321' },
  { id: 'deepseek-official', purpose: 'tool_auth', toolId: 'deepseek_harness', secretKind: 'api_key', issuer: 'deepseek_harness', enabled: true, type: 'ai', name: 'DeepSeek official account', provider: 'deepseek_harness', envKey: 'GRASP_DEEPSEEK_API_KEY', configured: true, masked: 'ds-…8765' },
  { id: 'deepseek-api', purpose: 'model_api', issuer: 'deepseek', secretKind: 'api_key', type: 'ai', name: 'DeepSeek API', provider: 'deepseek', configured: false, enabled: true },
  { id: 'model-api', purpose: 'model_api', issuer: 'deepseek', secretKind: 'api_key', type: 'ai', name: 'DeepSeek model', provider: 'deepseek', configured: true, enabled: true, masked: 'model-…5678' },
  { id: 'github-token', purpose: 'git', issuer: 'github', secretKind: 'token', enabled: true, type: 'git', name: 'GitHub team', provider: 'github', envKey: 'GITHUB_TOKEN', configured: false },
  { id: 'ssh-key', purpose: 'ssh', issuer: 'ssh', secretKind: 'ssh_private_key', enabled: true, type: 'ssh', name: 'Repository deploy key', provider: 'ssh', envKey: 'GIT_SSH_PRIVATE_KEY', configured: false },
  { id: 'vault-token', purpose: 'custom', issuer: 'custom', secretKind: 'token', enabled: true, type: 'custom', name: 'Vault managed token', provider: 'custom', source: 'vault', configured: true, masked: 'vault-…9012' },
]

const modelConnection = {
  id: 'deepseek-service', name: 'DeepSeek service', providerId: 'deepseek',
  protocol: 'openai-chat-completions', baseUrl: 'https://api.deepseek.com/v1', credentialId: 'model-api',
  auth: { scheme: 'bearer' }, models: [{ id: 'deepseek-chat', source: 'manual' }], state: 'active', revision: 1,
}

async function mockModelConnections(page: Page) {
  await routeApi(page, '**/api/workspace/model-connections', async (route) => route.fulfill({ json: { items: [modelConnection] } }))
}

async function openCredentialList(page: Page) {
  await installWorkspaceApiDefaults(page)
  await routeApi(page, '**/api/workspace/credentials', async (route) => route.fulfill({ json: { items: credentialItems } }))
  await mockModelConnections(page)
  await page.goto('/workspace-config.html?view=/settings/credentials')
  await expect(page.getByTestId('workspace-credential-section-coding')).toBeVisible()
}

async function openAddCredential(page: Page) {
  await page.getByTestId('workspace-credential-create').click()
  await page.getByTestId('workspace-add-credential').click()
}

async function expectWithinViewport(locator: Locator, page: Page) {
  const box = await locator.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width + 1)
}

async function expectNoHorizontalOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  for (const row of await page.getByTestId('workspace-credential-row').all()) {
    await expectWithinViewport(row, page)
    expect(await row.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  }
}

test('credential editing uses workspace settings, sends an explicit value and keeps secrets masked', async ({ page }) => {
  await installWorkspaceApiDefaults(page)
  const item = { id: 'cursor-api', purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', issuer: 'cursor', enabled: true, type: 'ai', name: 'Cursor API key', provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'test-…1234' }
  const puts: unknown[] = []
  await routeApi(page, '**/api/workspace/credentials', async (route) => route.fulfill({ json: { items: [item] } }))
  await routeApi(page, '**/api/workspace/credentials/cursor-api', async (route) => {
    puts.push(route.request().postDataJSON())
    await route.fulfill({ json: { ...item, masked: 'test-…5678' } })
  })
  await page.goto('/workspace-config.html?view=/settings/credentials')
  await expect(page.getByTestId('workspace-credentials-panel')).toBeVisible()
  await expect(page.getByTestId('workspace-credential-masked')).toHaveText('test-…1234')
  await page.getByTestId('workspace-credential-edit-cursor-api').click()
  const input = page.getByTestId('workspace-credential-input-cursor-api')
  await expect(input).toHaveValue('')
  await input.fill('test-replacement-5678')
  await page.getByTestId('workspace-credential-save-cursor-api').click()
  await expect.poll(() => puts).toEqual([expect.objectContaining({ purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', value: 'test-replacement-5678', envKey: 'GRASP_CURSOR_API_KEY' })])
  await expect(page.getByTestId('workspace-credential-masked')).toHaveText('test-…5678')
  await expect(input).toHaveCount(0)
  await expect(page.getByTestId('workspace-credentials-panel')).not.toContainText('test-replacement-5678')
})

test('one header add menu opens either wizard on both tabs and stays inside a narrow viewport', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 780 })
  await openCredentialList(page)
  const trigger = page.getByTestId('workspace-credential-create')
  await expect(trigger).toHaveText('新增')
  await trigger.focus()
  await trigger.press('ArrowDown')
  const menu = page.getByTestId('workspace-add-menu')
  await expect(menu).toBeVisible()
  await expectWithinViewport(menu, page)
  expect(await menu.evaluate((element) => element.parentElement === document.body)).toBe(true)
  await expect(page.getByTestId('workspace-add-credential')).toBeFocused()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByTestId('workspace-add-model')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await trigger.press('ArrowUp')
  await expect(page.getByTestId('workspace-add-model')).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.getByTestId('credentials-tab-models')).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('model-configuration-add')).toHaveCount(0)
  await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '1')
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(trigger).toBeVisible()
  await openAddCredential(page)
  await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '1')
  await expect(page.getByTestId('credentials-tab-models')).toHaveAttribute('aria-selected', 'true')
})

test('credential lists separate Agent CLI and API accounts with provider icons and Git and SSH groups', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await openCredentialList(page)

  const coding = page.getByTestId('workspace-credential-section-coding')
  const accounts = page.getByTestId('workspace-credential-section-api')
  const git = page.getByTestId('workspace-credential-section-git')
  const ssh = page.getByTestId('workspace-credential-section-ssh')
  const other = page.getByTestId('workspace-credential-section-other')
  for (const section of [coding, accounts, git, ssh, other]) {
    await expect(section).toHaveJSProperty('tagName', 'SECTION')
    await expect(section.getByRole('heading').first()).not.toHaveText('')
  }
  await expect(accounts.getByRole('heading', { level: 3 })).toHaveText('API 账号')
  await expect(coding.getByRole('heading', { level: 3 })).toHaveText('Agent CLI')
  await expect(coding.getByTestId('workspace-credential-row')).toHaveCount(7)
  await expect(coding.locator('.credentials-section-count')).toHaveText('7')
  await expect(coding).toContainText('OpenCode official account')
  await expect(coding).toContainText('DeepSeek official account')
  await expect(accounts).not.toContainText('OpenCode official account')
  await expect(accounts).not.toContainText('DeepSeek official account')
  await expect(accounts.getByTestId('workspace-credential-row')).toHaveCount(2)
  await expect(accounts.locator('.credentials-section-count')).toHaveText('2')
  await expect(accounts.getByTestId('workspace-credential-row').filter({ hasText: 'DeepSeek model' })).toBeVisible()
  await expect(accounts).toContainText('model-…5678')
  await expect(page.getByTestId('model-connections-manager')).toHaveCount(0)
  await expect(page.getByTestId('model-connection-add')).toHaveCount(0)
  await expect(page.getByTestId('connection-model-catalog')).toHaveCount(0)
  await expect(git).toContainText('GitHub team')
  await expect(ssh).toContainText('Repository deploy key')
  await expect(other).toContainText('Vault managed token')
  await expect(other.getByTestId('workspace-credential-edit-vault-token')).toHaveCount(0)
  await expect(other.getByTestId('workspace-credential-delete-vault-token')).toHaveCount(0)

  const rows = coding.getByTestId('workspace-credential-row').or(accounts.getByTestId('workspace-credential-row'))
  const rowBoxes = await Promise.all((await rows.all()).map((row) => row.boundingBox()))
  expect(rowBoxes.every(Boolean)).toBe(true)
  for (let index = 1; index < rowBoxes.length; index++) {
    expect(Math.abs(rowBoxes[index]!.x - rowBoxes[0]!.x)).toBeLessThanOrEqual(1)
    expect(Math.abs(rowBoxes[index]!.width - rowBoxes[0]!.width)).toBeLessThanOrEqual(1)
    expect(rowBoxes[index]!.y).toBeGreaterThanOrEqual(rowBoxes[index - 1]!.y + rowBoxes[index - 1]!.height)
  }
  for (const row of await rows.all()) {
    await expect(row.locator('[data-provider-logo]')).toHaveCount(1)
    await expect(row.locator('[data-logo-source="bundled-brand-asset"]')).toHaveCount(1)
    for (const testId of ['workspace-credential-alias', 'workspace-credential-status', 'workspace-credential-actions']) {
      await expect(row.getByTestId(testId)).toBeVisible()
      await expectWithinViewport(row.getByTestId(testId), page)
    }
  }
  await expect(page.getByTestId('workspace-credential-create')).toBeEnabled()
  await expect(coding.getByTestId('workspace-credential-row').filter({ hasText: 'Cursor team' }).getByTestId('workspace-credential-masked')).toHaveText('cursor-…1234')
  await expect(coding.getByTestId('workspace-credential-edit-cursor-api')).toBeEnabled()
  await expect(coding.getByTestId('workspace-credential-delete-cursor-api')).toBeEnabled()
  await coding.getByTestId('workspace-credential-fill-codebuddy-api').click()
  await expect(coding.getByTestId('workspace-credential-input-codebuddy-api')).toHaveAttribute('type', 'password')
  await expect(coding.getByTestId('workspace-credential-save-codebuddy-api')).toBeDisabled()
  await coding.getByTestId('workspace-credential-cancel-codebuddy-api').click()
  await expect(coding.getByTestId('workspace-credential-input-codebuddy-api')).toHaveCount(0)
})

test('the add-credential wizard selects a kind, moves back and clears cancelled secrets', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  await openCredentialList(page)
  await openAddCredential(page)
  const dialog = page.getByRole('dialog')
  const wizard = dialog.getByTestId('workspace-credential-create-form')
  await expect(wizard).toHaveAttribute('data-credential-step', '1')
  await expect(dialog.getByTestId('workspace-credential-create-next')).toBeDisabled()
  await expect(dialog.getByTestId('workspace-credential-create-value')).toHaveCount(0)
  await dialog.getByTestId('credential-kind-cursor').click()
  await expect(dialog.getByTestId('credential-kind-cursor')).toHaveAttribute('aria-pressed', 'true')
  await dialog.getByTestId('workspace-credential-create-next').click()
  await expect(wizard).toHaveAttribute('data-credential-step', '2')
  const alias = dialog.getByTestId('workspace-credential-create-alias')
  const secret = dialog.getByTestId('workspace-credential-create-value')
  await expect(alias).toBeVisible()
  await expect(secret).toHaveAttribute('type', 'password')
  await alias.fill('Cancelled team credential')
  await secret.fill('cancelled-secret-must-not-return')
  const back = dialog.getByTestId('workspace-credential-create-back')
  await expect(back).toBeEnabled()
  await back.click()
  await expect(wizard).toHaveAttribute('data-credential-step', '1')
  await expect(dialog.getByTestId('credential-kind-cursor')).toHaveAttribute('aria-pressed', 'true')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).toHaveCount(0)

  await openAddCredential(page)
  await expect(wizard).toHaveAttribute('data-credential-step', '1')
  await expect(dialog.getByTestId('workspace-credential-create-next')).toBeDisabled()
  await dialog.getByTestId('credential-kind-cursor').click()
  await dialog.getByTestId('workspace-credential-create-next').click()
  await expect(wizard).toHaveAttribute('data-credential-step', '2')
  await expect(alias).toHaveValue('')
  await expect(secret).toHaveValue('')
  await expect(secret).toHaveAttribute('type', 'password')
  await expect(dialog.getByTestId('workspace-credential-review-next')).toBeDisabled()
})

for (const width of [320, 390]) {
  test(`credential lists and multiline login editing fit a ${width}px viewport`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 })
    await openCredentialList(page)
    const coding = page.getByTestId('workspace-credential-section-coding')
    const rows = coding.getByTestId('workspace-credential-row')
    const first = await rows.nth(0).boundingBox()
    const second = await rows.nth(1).boundingBox()
    expect(first).not.toBeNull()
    expect(second).not.toBeNull()
    expect(Math.abs(first!.x - second!.x)).toBeLessThanOrEqual(1)
    expect(second!.y).toBeGreaterThanOrEqual(first!.y + first!.height)
    await expectNoHorizontalOverflow(page)

    const row = coding.getByTestId('workspace-credential-row').filter({ hasText: codexAlias })
    await row.getByTestId('workspace-credential-fill-codex-login').click()
    const input = row.getByTestId('workspace-credential-input-codex-login')
    await expect(input).toHaveJSProperty('tagName', 'TEXTAREA')
    await expect(input).toHaveValue('')
    await input.fill('{"tokens":{"access_token":"local-draft-login-token"}}')
    const save = row.getByTestId('workspace-credential-save-codex-login')
    await expect(save).toBeEnabled()
    await save.scrollIntoViewIfNeeded()
    await expect(save).toBeInViewport()
    await expectWithinViewport(save, page)
    await expectWithinViewport(input, page)
    await expectNoHorizontalOverflow(page)
    await row.getByTestId('workspace-credential-cancel-codex-login').click()
    await expect(input).toHaveCount(0)
    await expect(row).not.toContainText('local-draft-login-token')
    await expect(row.getByTestId('workspace-credential-fill-codex-login')).toBeEnabled()
  })
}

test('a model API key can be replaced on a narrow screen while its value stays masked', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await installWorkspaceApiDefaults(page)
  await mockModelConnections(page)
  let masked = 'model-…5678'
  const puts: unknown[] = []
  await routeApi(page, '**/api/workspace/credentials', async (route) => route.fulfill({
    json: { items: credentialItems.map((item) => item.id === 'model-api' ? { ...item, masked } : item) },
  }))
  await routeApi(page, '**/api/workspace/credentials/model-api', async (route) => {
    puts.push(route.request().postDataJSON())
    masked = 'model-…9999'
    await route.fulfill({ json: { ...credentialItems.find((item) => item.id === 'model-api'), masked } })
  })
  await page.goto('/workspace-config.html?view=/settings/credentials')
  const models = page.getByTestId('workspace-credential-section-api')
  await models.getByTestId('workspace-credential-edit-model-api').click()
  const input = models.getByTestId('workspace-credential-input-model-api')
  await expect(input).toHaveAttribute('type', 'password')
  await expect(input).toHaveValue('')
  await input.fill('model-replacement-secret-9999')
  const save = models.getByTestId('workspace-credential-save-model-api')
  await save.scrollIntoViewIfNeeded()
  await expect(save).toBeInViewport()
  await expectWithinViewport(save, page)
  await expectNoHorizontalOverflow(page)
  await save.click()
  await expect.poll(() => puts).toEqual([expect.objectContaining({ purpose: 'model_api', issuer: 'deepseek', secretKind: 'api_key', value: 'model-replacement-secret-9999' })])
  expect(puts[0]).not.toHaveProperty('metadata')
  expect(puts[0]).not.toHaveProperty('envKey')
  await expect(models).toContainText('model-…9999')
  await expect(input).toHaveCount(0)
  await expect(page.getByTestId('workspace-credentials-panel')).not.toContainText('model-replacement-secret-9999')
})

test('the trash button deletes unfilled Agent CLI credentials and API accounts after confirmation', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  const empty = { id: 'empty-api', purpose: 'model_api', issuer: 'deepseek', secretKind: 'api_key', type: 'ai', name: '1', provider: 'deepseek', configured: false, enabled: true, source: 'workspace' }
  const kept = { id: 'model-api', purpose: 'model_api', issuer: 'openai', secretKind: 'api_key', type: 'ai', name: 'Shared model key', provider: 'openai', configured: true, enabled: true, masked: 'sk-…9999', source: 'workspace' }
  const cli = { id: 'cursor-api', purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', issuer: 'cursor', enabled: true, type: 'ai', name: 'Cursor team', provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'cursor-…1234' }
  const unfilled = { id: 'cursor-empty', purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', issuer: 'cursor', enabled: true, type: 'ai', name: '123123', provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY', configured: false, source: 'workspace' }
  const deletes: string[] = []
  await installWorkspaceApiDefaults(page)
  await routeApi(page, '**/api/workspace/credentials', async (route) => {
    if (route.request().method() === 'GET') await route.fulfill({ json: { items: [cli, unfilled, empty, kept] } })
    else await route.fallback()
  })
  for (const id of ['empty-api', 'cursor-empty']) {
    await routeApi(page, `**/api/workspace/credentials/${id}/account`, async (route) => {
      deletes.push(`${route.request().method()} ${new URL(route.request().url()).pathname}`)
      await route.fulfill({ json: { status: 'removed' } })
    })
  }
  await routeApi(page, '**/api/workspace/model-connections', async (route) => route.fulfill({ json: { items: [] } }))
  await page.goto('/workspace-config.html?view=/settings/credentials')
  const coding = page.getByTestId('workspace-credential-section-coding')
  const accounts = page.getByTestId('workspace-credential-section-api')
  await expect(accounts).toBeVisible()
  await expect(page.locator('[data-testid^="api-account-more-"]')).toHaveCount(0)

  const unfilledAlias = coding.getByTestId('workspace-credential-alias').filter({ hasText: /^123123$/ })
  await expect(coding.getByTestId('workspace-credential-fill-cursor-empty')).toBeVisible()
  const removeUnfilled = coding.getByTestId('workspace-credential-delete-cursor-empty')
  await expect(removeUnfilled).toHaveAttribute('aria-label', '删除')
  const dismissed = new Promise<string>((resolve) => {
    page.once('dialog', (dialog) => {
      const message = dialog.message()
      void dialog.dismiss().then(() => resolve(message))
    })
  })
  await removeUnfilled.click()
  expect(await dismissed).toBe('删除凭据「123123」后，引用它的模型配置或 Git 绑定会失效，相关 Agent 将无法使用此凭据。确认删除？')
  expect(deletes).toEqual([])
  await expect(unfilledAlias).toBeVisible()

  page.once('dialog', (dialog) => { void dialog.accept() })
  await removeUnfilled.click()
  await expect.poll(() => deletes).toEqual(['DELETE /api/workspace/credentials/cursor-empty/account'])
  await expect(unfilledAlias).toHaveCount(0)
  await expect(page.getByTestId('toast-host')).toContainText('凭据已删除')
  await expect(coding.locator('.credentials-section-count')).toHaveText('1')

  const alias = accounts.getByTestId('workspace-credential-alias').filter({ hasText: /^1$/ })
  const confirmed = new Promise<string>((resolve) => {
    page.once('dialog', (dialog) => {
      const message = dialog.message()
      void dialog.accept().then(() => resolve(message))
    })
  })
  await accounts.getByTestId('workspace-credential-delete-empty-api').click()
  expect(await confirmed).toBe('删除 API 账号「1」后，使用此 API 账号的模型配置会失效，绑定这些配置的 Agent 将无法运行。确认删除？')
  await expect.poll(() => deletes).toEqual(['DELETE /api/workspace/credentials/cursor-empty/account', 'DELETE /api/workspace/credentials/empty-api/account'])
  await expect(alias).toHaveCount(0)
  await expect(page.getByTestId('toast-host')).toContainText('API 账号已删除')
  await expect(accounts.locator('.credentials-section-count')).toHaveText('1')
  await expect(accounts.getByTestId('workspace-credential-delete-model-api')).toBeVisible()
})

test('an empty Cursor slot does not duplicate the current CursorDefault credential', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await installWorkspaceApiDefaults(page)
  const rows = [
    { id: 'Cursor', type: 'ai', provider: 'cursor', name: 'Cursor', source: 'workspace', configured: false },
    { id: 'opaque-old-empty', type: 'git', provider: 'gitlab', name: 'Old empty GitLab', configured: false },
    { id: 'old-url', type: 'git', provider: 'gitlab', envKey: 'GITLAB_URL', name: 'GitLab URL', configured: true },
    { id: 'cursor-default', purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', type: 'ai', provider: 'cursor', name: 'CursorDefault', configured: true, masked: 'cursor-…1234' },
  ]
  const writes: string[] = []
  page.on('request', request => { if (request.url().includes('/api/workspace/credentials') && request.method() !== 'GET') writes.push(request.method()) })
  await routeApi(page, '**/api/workspace/credentials', async route => route.fulfill({ json: { items: rows } }))
  await page.goto('/workspace-config.html?view=/settings/credentials')
  const coding = page.getByTestId('workspace-credential-section-coding')
  await expect(page.getByTestId('workspace-credential-alias')).toHaveText(['CursorDefault'])
  await expect(page.getByTestId('workspace-credential-row')).toHaveCount(1)
  await expect(coding.locator('.credentials-section-count')).toHaveText('1')
  await expect(page.getByTestId('workspace-credential-masked')).toHaveText('cursor-…1234')
  await expect(page.getByTestId('workspace-credential-section-other')).toHaveCount(0)
  await expect(page.getByTestId('workspace-credentials-panel')).not.toContainText('GitLab URL')
  expect(writes).toEqual([])
  await expectNoHorizontalOverflow(page)
})

test('GitLab Host is configured with its token and can be changed without entering that token again', async ({ page }) => {
  await installWorkspaceApiDefaults(page)
  const rows: Record<string, unknown>[] = []
  const creates: Record<string, unknown>[] = []
  const updates: Record<string, unknown>[] = []
  await routeApi(page, '**/api/workspace/credentials', async route => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      creates.push(body)
      const { value: _value, ...metadata } = body
      const row = { ...metadata, id: 'gitlab-team', host: new URL(body.host).origin, configured: true, masked: 'gl-…1234' }
      rows.push(row)
      await route.fulfill({ json: row })
    } else await route.fulfill({ json: { items: rows } })
  })
  await routeApi(page, '**/api/workspace/credentials/gitlab-team', async route => {
    const body = route.request().postDataJSON()
    updates.push(body)
    rows[0] = { ...rows[0], host: new URL(body.host).origin }
    await route.fulfill({ json: rows[0] })
  })
  await page.goto('/workspace-config.html?view=/settings/credentials')
  await openAddCredential(page)
  await page.getByTestId('credential-kind-gitlab').click()
  await page.getByTestId('workspace-credential-create-next').click()
  await expect(page.getByTestId('workspace-credential-create-host')).toHaveValue('gitlab.com')
  await page.getByTestId('workspace-credential-create-alias').fill('GitLab team')
  await page.getByTestId('workspace-credential-create-value').fill('private-example-token')
  await page.getByTestId('workspace-credential-create-host').fill('https://user:secret@git.example.test/repository')
  await expect(page.getByTestId('workspace-credential-review-next')).toBeDisabled()
  await page.getByTestId('workspace-credential-create-host').fill('http://git.example.test:8080')
  await page.getByTestId('workspace-credential-review-next').click()
  await expect(page.getByTestId('workspace-credential-review-host')).toHaveText('http://git.example.test:8080')
  await page.getByTestId('workspace-credential-create-submit').click()
  await expect.poll(() => creates).toEqual([expect.objectContaining({ purpose: 'git', provider: 'gitlab', host: 'http://git.example.test:8080' })])
  await expect(page.getByTestId('workspace-credential-host-label-gitlab-team')).toHaveText('http://git.example.test:8080')
  await page.getByTestId('workspace-credential-edit-gitlab-team').click()
  await expect(page.getByTestId('workspace-credential-input-gitlab-team')).toHaveValue('')
  await expect(page.getByTestId('workspace-credential-save-gitlab-team')).toBeDisabled()
  await page.getByTestId('workspace-credential-host-gitlab-team').fill('https://git.company.test:8443')
  await page.getByTestId('workspace-credential-save-gitlab-team').click()
  await expect.poll(() => updates.length).toBe(1)
  expect(updates[0]).toMatchObject({ host: 'https://git.company.test:8443' })
  expect(updates[0]).not.toHaveProperty('value')
  await expect(page.getByTestId('workspace-credential-host-label-gitlab-team')).toHaveText('git.company.test:8443')
  await expect(page.getByTestId('workspace-credential-masked')).toHaveText('gl-…1234')
  await expect(page.getByTestId('workspace-credentials-panel')).not.toContainText('private-example-token')
})
