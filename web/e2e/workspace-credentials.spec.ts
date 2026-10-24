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

test('credential lists group coding, model, repository and SSH credentials into full-width desktop rows', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await openCredentialList(page)

  const coding = page.getByTestId('workspace-credential-section-coding')
  const models = page.getByTestId('workspace-credential-section-models')
  const git = page.getByTestId('workspace-credential-section-git')
  const ssh = page.getByTestId('workspace-credential-section-ssh')
  const other = page.getByTestId('workspace-credential-section-other')
  for (const section of [coding, models, git, ssh, other]) {
    await expect(section).toHaveJSProperty('tagName', 'SECTION')
    await expect(section.getByRole('heading').first()).not.toHaveText('')
  }
  await expect(coding.getByTestId('workspace-credential-row')).toHaveCount(5)
  await expect(models.getByTestId('workspace-credential-row')).toHaveCount(2)
  await expect(models.getByTestId('workspace-credential-row').filter({ hasText: 'DeepSeek model' })).toBeVisible()
  await expect(models).toContainText('model-…5678')
  await expect(git).toContainText('GitHub team')
  await expect(ssh).toContainText('Repository deploy key')
  await expect(other).toContainText('Vault managed token')
  await expect(other.getByTestId('workspace-credential-edit-vault-token')).toHaveCount(0)
  await expect(other.getByTestId('workspace-credential-clear-vault-token')).toHaveCount(0)

  const rows = coding.getByTestId('workspace-credential-row')
  const rowBoxes = await Promise.all((await rows.all()).map((row) => row.boundingBox()))
  expect(rowBoxes.every(Boolean)).toBe(true)
  for (let index = 1; index < rowBoxes.length; index++) {
    expect(Math.abs(rowBoxes[index]!.x - rowBoxes[0]!.x)).toBeLessThanOrEqual(1)
    expect(Math.abs(rowBoxes[index]!.width - rowBoxes[0]!.width)).toBeLessThanOrEqual(1)
    expect(rowBoxes[index]!.y).toBeGreaterThanOrEqual(rowBoxes[index - 1]!.y + rowBoxes[index - 1]!.height)
  }
  for (const row of await rows.all()) {
    for (const testId of ['workspace-credential-alias', 'workspace-credential-status', 'workspace-credential-actions']) {
      await expect(row.getByTestId(testId)).toBeVisible()
      await expectWithinViewport(row.getByTestId(testId), page)
    }
  }
  await expect(page.getByTestId('workspace-credential-create')).toBeEnabled()
  await expect(coding.getByTestId('workspace-credential-masked')).toHaveText('cursor-…1234')
  await expect(coding.getByTestId('workspace-credential-edit-cursor-api')).toBeEnabled()
  await expect(coding.getByTestId('workspace-credential-clear-cursor-api')).toBeEnabled()
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
  const models = page.getByTestId('workspace-credential-section-models')
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

test('existing provider rows stay in their groups and GitLab URL is not a separate credential', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await installWorkspaceApiDefaults(page)
  const rows = [
    { id: 'old-cursor', type: 'ai', provider: 'cursor', name: 'Existing Cursor', configured: true, masked: '••••' },
    { id: 'old-github', type: 'git', provider: 'github', name: 'Existing GitHub', configured: true, masked: '••••' },
    { id: 'old-gitlab', type: 'git', provider: 'gitlab', name: 'Existing GitLab', configured: true, masked: '••••' },
    { id: 'old-gitlab-url', type: 'git', provider: 'gitlab', envKey: 'GITLAB_URL', name: 'GitLab URL', configured: true },
  ]
  const deleted: string[] = []
  await routeApi(page, '**/api/workspace/credentials', async route => route.fulfill({ json: { items: rows } }))
  await routeApi(page, '**/api/workspace/credentials/old-gitlab', async route => {
    expect(route.request().method()).toBe('DELETE')
    deleted.push('old-gitlab')
    await route.fulfill({ json: { status: 'ok' } })
  })
  await page.goto('/workspace-config.html?view=/settings/credentials')
  await expect(page.getByTestId('workspace-credential-section-coding')).toContainText('Existing Cursor')
  await expect(page.getByTestId('workspace-credential-group-github')).toContainText('Existing GitHub')
  await expect(page.getByTestId('workspace-credential-group-gitlab')).toContainText('Existing GitLab')
  await expect(page.getByTestId('workspace-credential-section-other')).toHaveCount(0)
  await expect(page.getByTestId('workspace-credential-row')).toHaveCount(3)
  await expect(page.getByTestId('workspace-credentials-panel')).not.toContainText('GitLab URL')
  await expect(page.getByTestId('workspace-credential-edit-old-gitlab')).toHaveCount(0)
  await expect(page.getByTestId('workspace-credential-group-gitlab')).toContainText('请重新添加此凭据后编辑')
  await page.getByTestId('workspace-credential-clear-old-gitlab').click()
  await expect.poll(() => deleted).toEqual(['old-gitlab'])
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
