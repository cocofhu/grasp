import { test, expect } from '@playwright/test'
import path from 'node:path'
import fs from 'node:fs'

const OUT = path.join('/tmp', 'wizard-e2e-shots')

test.beforeAll(() => {
  fs.mkdirSync(OUT, { recursive: true })
})

test('新建 Agent 五步向导浏览器验收', async ({ page }) => {
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    if (url.pathname === '/api/agents' && route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ name: 'e2e-wizard-agent' }),
      })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
  })

  await page.goto('/agent-create-wizard.html', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('agent-create-wizard-root')).toBeVisible()

  const rail = page.locator('.rail-item .lbl strong')
  await expect(rail).toHaveText(['基础信息', 'Agent', 'API Key', 'Git', '确认创建'])
  const railText = await rail.allTextContents()
  expect(railText.join(' ')).not.toContain('ACP')
  expect(railText.join(' ')).not.toContain('ENV')
  expect(railText.join(' ')).not.toMatch(/MCP|Rules|Skills|Commands|Prompts/)

  await page.screenshot({ path: path.join(OUT, '01-basics.png'), fullPage: true })

  await page.locator('#wiz-name-input').fill('e2e-wizard-agent')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('Agent')
  await page.screenshot({ path: path.join(OUT, '02-agent.png'), fullPage: true })

  await page.getByTestId('agent-wizard-path-cli').click()
  await page.getByTestId('agent-wizard-backend-cursor').click()
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('API Key')
  await expect(page.getByText('如何获取', { exact: true })).toBeVisible()
  await expect(page.getByText('GRASP_CURSOR_API_KEY')).toHaveCount(0)
  await expect(page.locator('a[href*="cursor.com/dashboard"]')).toBeVisible()
  await page.screenshot({ path: path.join(OUT, '03-api-key.png'), fullPage: true })

  await page.getByRole('button', { name: /^跳过/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('Git')
  const gitBody = await page.locator('.step-pane').innerText()
  expect(gitBody).not.toContain('未逐仓解析')
  expect(gitBody).not.toContain('无法在此页面逐仓解析')
  expect(gitBody).not.toContain('不会验证变量引用的实际值')
  expect(gitBody).not.toContain('远端 clone / push 权限')
  await expect(page.getByText(/GitHub|GitLab|SSH/).first()).toBeVisible()
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

  // g1.1: pick GitLab on the flat first screen — no「调整类型」modal.
  await expect(page.getByRole('button', { name: /调整类型/ })).toHaveCount(0)
  await page.locator('[data-test="git-choice-gitlab_https"]').click()
  await expect(page.locator('[data-test="git-choice-gitlab_https"]')).toHaveAttribute('aria-pressed', 'true')
  const gitAfter = await page.locator('.step-pane').innerText()
  expect(gitAfter).not.toContain('未逐仓解析')
  expect(gitAfter).not.toContain('无法在此页面逐仓解析')
  expect(gitAfter).not.toContain('调整类型')
  expect(gitAfter).toContain('GitLab')
  await page.screenshot({ path: path.join(OUT, '06-git-typed.png'), fullPage: true })

  await page.getByRole('button', { name: /^下一步|^跳过/ }).last().click()
  await expect(page.locator('.sec-head h3')).toHaveText('确认创建')
  await expect(page.getByText('鉴权提醒', { exact: true })).toBeVisible()
  await expect(page.getByRole('status')).toContainText('Studio Env')
  const reviewText = await page.locator('.step-pane').innerText()
  expect(reviewText).not.toMatch(/(^|\s)ENV(\s|$)/)
  expect(reviewText).not.toMatch(/(^|\s)ACP(\s|$)/)
  await page.screenshot({ path: path.join(OUT, '05-review.png'), fullPage: true })

  await page.getByRole('button', { name: /创建并进入 Studio/ }).click()
  await expect(page.getByTestId('created-name')).toHaveText('e2e-wizard-agent', { timeout: 10_000 })
  await expect(page.locator('.wiz-rail')).toHaveCount(0)
})

test('向导能取到项目共享 Git Token 时 Git 步仍出现三选（plan g3.2）', async ({ page }) => {
  await page.route('**/api/**', async (route) => {
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    if (url.pathname.includes('/shared-agent-config') && route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          projectId: 'proj-shared',
          env: { GITLAB_TOKEN: '${vars.gitlab_pat}' },
          files: [],
          mcp: [],
          layout: {},
        }),
      })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
  })

  await page.goto('/agent-create-wizard.html?projectId=proj-shared', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('agent-create-wizard-root')).toBeVisible()
  await page.locator('#wiz-name-input').fill('e2e-inherited-git')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await page.getByRole('button', { name: /Cursor/ }).click()
  await page.getByRole('button', { name: /^下一步/ }).click()
  await page.getByRole('button', { name: /^跳过/ }).click()
  await expect(page.locator('.sec-head h3')).toHaveText('Git')
  await expect(page.locator('[data-test="git-guide"]')).toHaveCount(1)
  await expect(page.locator('[data-test="git-choice-github_https"]')).toBeVisible()
  await expect(page.locator('[data-test="git-choice-gitlab_https"]')).toBeVisible()
  await expect(page.locator('[data-test="git-choice-ssh"]')).toBeVisible()
  await expect(page.locator('[data-test="git-choice-gitlab_https"]')).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expect(page.getByText('调整类型')).toHaveCount(0)
  await expect(page.getByText(/预选类型/)).toBeVisible()
  await page.locator('[data-test="git-choice-ssh"]').click()
  await expect(page.locator('[data-test="git-choice-ssh"]')).toHaveAttribute('aria-pressed', 'true')
})
