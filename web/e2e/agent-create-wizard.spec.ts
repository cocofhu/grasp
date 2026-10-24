import { test, expect, type Page } from '@playwright/test'
import path from 'node:path'
import fs from 'node:fs'

const OUT = path.join('/tmp', 'wizard-e2e-shots')

async function customizeInheritance(page: Page, section: 'model' | 'git') {
  const toggle = page.getByTestId(`agent-inherit-${section}`)
  if (await toggle.getAttribute('aria-checked') === 'true') await toggle.click()
  await expect(toggle).toHaveAttribute('aria-checked', 'false')
}

async function selectBindingValue(page: Page, testId: string, value: string) {
  if (testId === 'model-binding-configuration-select') await customizeInheritance(page, 'model')
  if (testId === 'git-credential-add') await customizeInheritance(page, 'git')
  await page.getByTestId(testId).locator('[data-test="app-select-trigger"]').click()
  const panel = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
  await panel.locator('[data-test="app-select-search"]').fill(value)
  await panel.locator(`[data-test="app-select-option-${value}"]`).click()
}

async function expectSavedModelPickerOnly(page: Page) {
  const editor = page.getByTestId('model-binding-editor')
  await expect(editor.locator('[data-test="app-select-trigger"]')).toHaveCount(1)
  await expect(editor.getByTestId('model-binding-advanced')).toHaveCount(0)
  await expect(editor.getByTestId('model-binding-inherited')).toHaveCount(0)
  await expect(editor.getByTestId('model-binding-configuration-summary')).toHaveCount(0)
  await expect(editor.getByTestId('model-binding-problem')).toHaveCount(0)
  await expect(editor.locator('a')).toHaveCount(0)
}

test.beforeAll(() => {
  fs.mkdirSync(OUT, { recursive: true })
})

test('新建 Agent 四步向导浏览器验收', async ({ page }) => {
  let created: Record<string, unknown> | undefined
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    if (url.pathname === '/api/agents' && route.request().method() === 'POST') {
      created = route.request().postDataJSON()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ name: 'e2e-wizard-agent' }),
      })
      return
    }
    if (url.pathname === '/api/workspace/credentials') return route.fulfill({ json: { items: [{ id: 'cursor-key', name: 'Cursor team', purpose: 'tool_auth', toolId: 'cursor', configured: true }, { id: 'gitlab-access', name: 'GitLab team', purpose:'git', provider: 'gitlab', host: 'https://git.example.test', envKey: 'GITLAB_TOKEN', configured: true }, { id: 'github-access', name: 'GitHub shared', purpose:'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true }] } })
    if (url.pathname === '/api/workspace/model-connections') return route.fulfill({ json: { items: [] } })
    if (url.pathname === '/api/workspace/model-configurations') return route.fulfill({ json: { items: [{ id: 'cursor-profile', name: 'Cursor 日常模型', backend: 'cursor', binding: { kind: 'platform', credentialId: 'cursor-key', modelId: 'composer-1' }, state: 'active', revision: 1 }] } })
    if (url.pathname === '/api/workspace/tool-capabilities') return route.fulfill({ json: { items: [{ toolId: 'cursor', modes: ['platform'], protocols: [], adapterState: 'existing' }] } })
    if (url.pathname === '/api/workspace') return route.fulfill({ json: { name: 'Workspace' } })
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
  })

  await page.goto('/agent-create-wizard.html', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('agent-create-wizard-root')).toBeVisible()

  const rail = page.locator('.rail-item .lbl strong')
  await expect(rail).toHaveText(['基础信息', '工具与模型', 'Git', '确认创建'])
  const railText = await rail.allTextContents()
  expect(railText.join(' ')).not.toContain('ACP')
  expect(railText.join(' ')).not.toContain('ENV')
  expect(railText.join(' ')).not.toMatch(/MCP|Rules|Skills|Commands|Prompts/)

  await page.screenshot({ path: path.join(OUT, '01-basics.png'), fullPage: true })

  await page.locator('#wiz-name-input').fill('e2e-wizard-agent')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('工具与模型')
  await page.screenshot({ path: path.join(OUT, '02-agent.png'), fullPage: true })

  await expect(page.getByTestId('coding-tool-picker')).toHaveCount(0)
  await expect(page.locator('.sec-head h3')).toHaveText('工具与模型')
  await expect(page.getByTestId('model-binding-editor')).toBeVisible()
  await customizeInheritance(page, 'model')
  await expectSavedModelPickerOnly(page)
  await expect(page.getByRole('button', { name: /^下一步/ })).toBeDisabled()
  await selectBindingValue(page, 'model-binding-configuration-select', 'cursor-profile')
  await expect(page.getByRole('button', { name: /^下一步/ })).toBeEnabled()
  await expect(page.getByText('如何获取', { exact: true })).toHaveCount(0)
  await expect(page.getByText('GRASP_CURSOR_API_KEY')).toHaveCount(0)
  await expect(page.locator('a[href*="cursor.com/dashboard"]')).toHaveCount(0)
  await expect(page.locator('[data-test="api-key-input"]')).toHaveCount(0)
  await page.screenshot({ path: path.join(OUT, '03-api-key.png'), fullPage: true })

  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('Git')
  await customizeInheritance(page, 'git')
  const gitBody = await page.locator('.step-pane').innerText()
  expect(gitBody).not.toContain('未逐仓解析')
  expect(gitBody).not.toContain('无法在此页面逐仓解析')
  expect(gitBody).not.toContain('不会验证变量引用的实际值')
  expect(gitBody).not.toContain('远端 clone / push 权限')
  await expect(page.getByTestId('git-credential-add')).toBeVisible()
  await expect(page.locator('[data-test="git-help-link"]')).toBeVisible()
  await page.screenshot({ path: path.join(OUT, '04-git.png'), fullPage: true })

  await page.locator('[data-test="git-help-link"]').click()
  await expect(page.getByText('环境变量与凭据')).toBeVisible()
  await expect(page.getByText(/GITHUB_TOKEN|凭据可通过环境变量/)).toBeVisible()
  await expect(page.getByText(/不会验证变量引用的实际值|远端 clone/)).toBeVisible()
  await expect(page.locator('.wiz-rail')).toBeVisible()
  await expect(page.locator('.sec-head h3')).toHaveText('Git')
  await page.locator('[data-test="env-help-got-it"]').click()
  await expect(page.getByText('环境变量与凭据')).toHaveCount(0)
  await expect(page.locator('.wiz-rail')).toBeVisible()
  await expect(page.locator('.sec-head h3')).toHaveText('Git')

  await expect(page.getByRole('button', { name: /调整类型/ })).toHaveCount(0)
  await expect(page.locator('[data-test="git-apply-recommended"]')).toHaveCount(0)
  await selectBindingValue(page, 'git-credential-add', 'gitlab-access')
  await selectBindingValue(page, 'git-credential-add', 'github-access')
  await expect(page.getByTestId('git-preferred-gitlab-access')).toHaveAttribute('aria-pressed', 'true')
  await page.getByTestId('git-preferred-github-access').click()
  await expect(page.getByTestId('git-preferred-github-access')).toHaveAttribute('aria-pressed', 'true')
  await page.getByTestId('git-options-gitlab-access').locator('summary').click()
  await page.getByTestId('git-scope-gitlab-access').fill('git.example.test')
  await page.getByRole('button', { name: /^上一步/ }).click()
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.getByTestId('git-scope-gitlab-access')).toHaveValue('git.example.test')
  await expect(page.getByTestId('git-preferred-github-access')).toHaveAttribute('aria-pressed', 'true')
  await page.screenshot({ path: path.join(OUT, '06-git-multiple.png'), fullPage: true })

  await page.getByRole('button', { name: /^下一步|^跳过/ }).last().click()
  await expect(page.locator('.sec-head h3')).toHaveText('确认创建')
  await expect(page.getByText('鉴权提醒', { exact: true })).toHaveCount(0)
  await expect(page.locator('.step-pane')).toContainText('Cursor 日常模型')
  const reviewText = await page.locator('.step-pane').innerText()
  expect(reviewText).not.toMatch(/(^|\s)ENV(\s|$)/)
  expect(reviewText).not.toMatch(/(^|\s)ACP(\s|$)/)
  await page.screenshot({ path: path.join(OUT, '05-review.png'), fullPage: true })

  await page.getByRole('button', { name: /创建并进入 Studio/ }).click()
  await expect(page.getByTestId('created-name')).toHaveText('e2e-wizard-agent', { timeout: 10_000 })
  expect(created).toMatchObject({ acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cursor-profile' } })
  for (const key of ['GITHUB_TOKEN', 'GITLAB_TOKEN', 'GIT_SSH_PRIVATE_KEY', 'GIT_SSH_KNOWN_HOSTS']) expect(created?.env || {}).not.toHaveProperty(key)
  expect(created).not.toHaveProperty('repoIds')
  expect(created?.gitBindings).toEqual({ items: [{ credentialId: 'gitlab-access', scope: 'git.example.test' }, { credentialId: 'github-access' }], preferredCredentialId: 'github-access' })
  await expect(page.locator('.wiz-rail')).toHaveCount(0)
})

test('English template dropdown shows localized names without Chinese or internal names', async ({ page }) => {
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    if (!url.pathname.startsWith('/api/')) return route.continue()
    if (url.pathname === '/api/workspace') return route.fulfill({ json: { name: 'Workspace' } })
    if (url.pathname === '/api/agent-teams/templates') {
      return route.fulfill({
        json: {
          items: [
            { id: 'clarify', embedName: 'ClarifyAgent', roleLabelZh: '需求澄清', summary: '多轮对话澄清需求、写出计划' },
            { id: 'implement', embedName: 'ImplementAgent', roleLabelZh: '实现', summary: '按计划实现' },
            { id: 'test_review', embedName: 'TestReviewAgent', roleLabelZh: '测试评审', summary: '执行测试并做代码评审' },
          ],
        },
      })
    }
    return route.fulfill({ json: [] })
  })
  await page.goto('/agent-create-wizard.html?lang=en', { waitUntil: 'networkidle' })
  await page.getByTestId('agent-template-select-trigger').click()
  const list = page.getByTestId('agent-template-select-list')
  await expect(list.getByTestId('agent-template-select-option-clarify')).toContainText('Clarify')
  await expect(list.getByTestId('agent-template-select-option-test_review')).toContainText('Test & review')
  const text = await list.innerText()
  expect(text).not.toMatch(/[\u3400-\u9fff]/)
  expect(text).not.toMatch(/ClarifyAgent|ImplementAgent|TestReviewAgent/)
  await page.screenshot({ path: path.join(OUT, 'en-template-select.png') })
})

for (const lang of ['zh-CN', 'en']) {
  test(`DeepSeek Harness selects a saved model configuration (${lang})`, async ({ page }) => {
    const nextLabel = lang === 'en' ? /^Next/ : /^下一步/
    const skipLabel = lang === 'en' ? /^Skip/ : /^跳过/
    const createLabel = lang === 'en' ? /Create.*Studio/ : /创建并进入 Studio/
    let created: Record<string, unknown> | undefined
    await page.route('**/api/**', async (route) => {
      const url = new URL(route.request().url())
      if (!url.pathname.startsWith('/api/')) return route.continue()
      if (url.pathname === '/api/agents' && route.request().method() === 'POST') {
        created = route.request().postDataJSON()
        return route.fulfill({ json: { name: 'deepseek-agent' } })
      }
      if (url.pathname === '/api/workspace/credentials') return route.fulfill({ json: { items: [
        { id: 'dsh-key', name: 'DeepSeek team', purpose: 'model_api', issuer: 'deepseek', configured: true },
      ] } })
      if (url.pathname === '/api/workspace/model-connections') return route.fulfill({ json: { items: [
        { id: 'deepseek-connection', name: 'DeepSeek service', providerId: 'deepseek', protocol: 'anthropic-messages', baseUrl: 'https://api.deepseek.com/anthropic', credentialId: 'dsh-key', auth: { scheme: 'x-api-key' }, models: [{ id: 'deepseek-flash', source: 'manual' }], state: 'active', revision: 1 },
      ] } })
      if (url.pathname === '/api/workspace/tool-capabilities') return route.fulfill({ json: { items: [
        { toolId: 'deepseek_harness', modes: ['connection'], protocols: ['anthropic-messages'], routeProfiles: ['deepseek-native'], adapterState: 'existing' },
      ] } })
      if (url.pathname === '/api/workspace/model-configurations') return route.fulfill({ json: { items: [{ id: 'deepseek-profile', name: 'DeepSeek native', backend: 'deepseek_harness', binding: { kind: 'connection', connectionId: 'deepseek-connection', modelId: 'deepseek-flash', toolSettings: { routeProfile: 'deepseek-native' } }, state: 'active', revision: 1 }] } })
      if (url.pathname === '/api/workspace') return route.fulfill({ json: { name: 'Workspace' } })
      return route.fulfill({ json: [] })
    })
    await page.setViewportSize({ width: 1179, height: 900 })
    await page.goto(`/agent-create-wizard.html?lang=${lang}`, { waitUntil: 'networkidle' })
    await page.locator('#wiz-name-input').fill('deepseek-agent')
    await page.getByRole('button', { name: nextLabel }).click()
    await expect(page.locator('.sec-head h3')).toHaveText(lang === 'en' ? 'Tool & model' : '工具与模型')
    await expect(page.locator('.rail-item .lbl strong')).toHaveText(lang === 'en' ? ['Basics', 'Tool & model', 'Git', 'Confirm'] : ['基础信息', '工具与模型', 'Git', '确认创建'])
    await expect(page.getByTestId('coding-tool-picker')).toHaveCount(0)
    await expect(page.locator('.step-pane')).not.toContainText(/configRoot|\/root\//)
    await page.setViewportSize({ width: 760, height: 900 })
    const horizontalOverflow = await page.locator('.step-pane').evaluate((el) => el.scrollWidth - el.clientWidth)
    expect(horizontalOverflow).toBeLessThanOrEqual(1)
    if (lang === 'en') expect(await page.locator('.step-pane').innerText()).not.toMatch(/[\u3400-\u9fff]/)
    await customizeInheritance(page, 'model')
  await expectSavedModelPickerOnly(page)
    await expect(page.getByRole('button', { name: nextLabel })).toBeDisabled()
    await selectBindingValue(page, 'model-binding-configuration-select', 'deepseek-profile')
    await page.getByRole('button', { name: nextLabel }).click()
    await page.getByRole('button', { name: skipLabel }).click()
    await page.getByRole('button', { name: createLabel }).click()
    await expect(page.getByTestId('created-name')).toHaveText('deepseek-agent')
    expect(created).toMatchObject({ acpBackend: 'deepseek_harness', layout: { configRoot: '/root/.dsh' }, modelBinding: { kind: 'configuration', configurationId: 'deepseek-profile' } })
    expect(created?.modelBinding).not.toHaveProperty('toolSettings')
    expect(created).not.toHaveProperty('aiCredentialId')
    expect(created).not.toHaveProperty('openCodeCredentialId')
    expect(created?.env || {}).not.toHaveProperty('ACP_BRIDGE_MODEL')
  })
}

test('model-derived coding tools, sites and searchable models work at 320px', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  let created: Record<string, unknown> | undefined
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    if (!url.pathname.startsWith('/api/')) return route.continue()
    if (url.pathname === '/api/agents' && route.request().method() === 'POST') {
      created = route.request().postDataJSON()
      return route.fulfill({ json: { name: 'compact-tools-agent' } })
    }
    if (url.pathname === '/api/workspace/credentials') return route.fulfill({ json: { items: [{ id: 'team-key', name: '团队账号', purpose: 'tool_auth', toolId: 'codebuddy', configured: true }, { id: 'git-key', name: '团队 GitHub', purpose:'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true }] } })
    if (url.pathname === '/api/workspace/model-configurations') return route.fulfill({ json: { items: [{ id: 'production-profile', name: '团队生产模型', backend: 'codebuddy', binding: { kind: 'platform', credentialId: 'team-key', modelId: 'vendor/main' }, state: 'active', revision: 1 }] } })
    if (url.pathname === '/api/workspace/tool-capabilities') return route.fulfill({ json: { items: [{ toolId: 'codebuddy', modes: ['platform'], protocols: [], adapterState: 'existing' }] } })
    if (url.pathname === '/api/workspace/model-connections') return route.fulfill({ json: { items: [] } })
    if (url.pathname === '/api/workspace') return route.fulfill({ json: { name: 'Workspace' } })
    return route.fulfill({ json: [] })
  })
  await page.goto('/agent-create-wizard.html')
  await page.locator('#wiz-name-input').fill('compact-tools-agent')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('工具与模型')
  await expect(page.getByTestId('coding-tool-picker')).toHaveCount(0)
  await customizeInheritance(page, 'model')
  await page.getByTestId('model-binding-configuration-select').locator('[data-test="app-select-trigger"]').click()
  const panel = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
  const search = panel.locator('[data-test="app-select-search"]')
  await search.fill('生产')
  const box = await panel.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(320)
  await search.press('Enter')
  await expect(page.getByTestId('model-binding-configuration-select').locator('[data-test="app-select-trigger"]')).toContainText('团队生产模型')
  await customizeInheritance(page, 'model')
  await expectSavedModelPickerOnly(page)
  const site = page.getByTestId('agent-model-region').locator('[data-test="app-select-trigger"]')
  await expect(site).toContainText('国际站')
  await selectBindingValue(page, 'agent-model-region', 'internal')
  await expect(site).toContainText('国内站')
  expect(await page.locator('.step-pane').evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('Git')
  await page.getByRole('button', { name: /^上一步/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('工具与模型')
  await expect(site).toContainText('国内站')
  await expect(page.getByTestId('model-binding-configuration-select').locator('[data-test="app-select-trigger"]')).toContainText('团队生产模型')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await selectBindingValue(page, 'git-credential-add', 'git-key')
  await expect(page.getByTestId('git-preferred-git-key')).toHaveAttribute('aria-pressed', 'true')
  expect(await page.getByTestId('agent-git-credential-picker').evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  await page.getByRole('button', { name: /^跳过/ }).click()
  await page.getByRole('button', { name: /创建并进入 Studio/ }).click()
  await expect(page.getByTestId('created-name')).toHaveText('compact-tools-agent')
  expect(created).toMatchObject({ acpBackend: 'codebuddy', modelBinding: { kind: 'configuration', configurationId: 'production-profile' }, env: { GRASP_CODEBUDDY_REGION: 'internal' } })
  expect(created?.gitBindings).toEqual({ items: [] })
  expect(created).not.toHaveProperty('repoIds')
  expect(created?.env || {}).not.toHaveProperty('GITHUB_TOKEN')
  expect(created?.env || {}).not.toHaveProperty('GRASP_TRAE_REGION')
})
