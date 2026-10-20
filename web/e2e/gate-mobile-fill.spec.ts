import { test, expect, type Page } from '@playwright/test'

const attachment = {
  name: 'review-draft.png',
  mimeType: 'image/png',
  buffer: Buffer.from(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
    'base64',
  ),
}

async function mockApi(page: Page, initialIssues: Array<Record<string, unknown>> = []) {
  const issues = [...initialIssues]
  const revisions: Array<Record<string, unknown>> = []
  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (!url.pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    if (url.pathname.includes('/preview-issues')) {
      if (request.method() === 'POST') {
        const issue = { ...request.postDataJSON(), id: `issue-${issues.length + 1}`, status: 'open', createdAt: '2026-10-20T00:00:00Z' }
        issues.push(issue)
        await route.fulfill({ json: issue })
      } else {
        await route.fulfill({ json: { issues } })
      }
      return
    }
    if (url.pathname.endsWith('/react-revise') && request.method() === 'POST') {
      revisions.push(request.postDataJSON())
      await route.fulfill({ json: { status: 'queued', waiting: 1, producerNodeId: 'visual' } })
      return
    }
    if (url.pathname.includes('/primary-artifacts')) {
      await route.fulfill({ json: { items: [{ name: 'page.html', kind: 'html', readonly: false, nodeId: 'visual', outputKey: 'page' }] } })
      return
    }
    if (url.pathname.includes('/gate/')) {
      await route.fulfill({ status: 400, json: { error: 'offline' } })
      return
    }
    if (url.pathname.includes('/artifacts/')) {
      await route.fulfill({ json: { content: JSON.stringify({ summary: '上游需求', goals: ['g1'] }), etag: 'e1', updatedAt: '2026-07-18T00:00:00Z', sizeBytes: 32 } })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
  return { revisions }
}

async function openFixture(page: Page) {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/gate-mobile-fill.html')
  await expect(page.getByTestId('gate-mobile-fill-root')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByTestId('mobile-fill-preview-issues-loading')).toHaveCount(0)
  await expect(page.getByTestId('review-composer-pass')).toBeVisible()
}

async function openArtifacts(page: Page) {
  await page.getByTestId('conversation-panel-toggle').click()
  const panel = page.getByTestId('conversation-artifact-panel')
  await expect(panel).toBeVisible()
  await expect(panel).toHaveAttribute('role', 'dialog')
  await expect(panel).toHaveAttribute('aria-modal', 'true')
  await expect(page.getByTestId('conversation-main')).toHaveAttribute('inert', '')
  await expect(page.getByTestId('mobile-fill-preview')).toBeVisible()
  return panel
}

test.describe('移动审批聊天与按需产物预览', () => {
  test('390×844：n_open=0 默认完整聊天，打开产物可取点，返回可确认并流转', async ({ page }) => {
    await mockApi(page)
    await openFixture(page)
    const panel = page.getByTestId('conversation-artifact-panel')
    const chat = page.getByTestId('conversation-main')
    await expect(page.getByTestId('mobile-fill-remaining')).toBeVisible()
    await expect(page.getByTestId('review-shell')).toBeVisible()
    await expect(page.getByTestId('review-shell-drawer-handle')).toHaveCount(0)
    await expect(panel).toBeHidden()
    await expect(page.getByTestId('mobile-fill-preview')).toBeHidden()
    await expect(page.getByTestId('mobile-fill-feedback')).toBeVisible()
    const [chatBox, shellBox] = await Promise.all([chat.boundingBox(), page.getByTestId('review-shell').boundingBox()])
    expect(chatBox && shellBox).toBeTruthy()
    expect(chatBox!.width).toBeGreaterThanOrEqual(shellBox!.width - 2)
    expect(chatBox!.height).toBeGreaterThan(shellBox!.height - 60)

    const pass = page.getByTestId('review-composer-pass')
    const send = page.getByTestId('review-composer-send')
    await expect(pass).toContainText('确认并流转')
    await expect(pass).toBeEnabled()
    await expect(send).toHaveAttribute('aria-label', '发送')
    await expect(send).toBeDisabled()
    await expect(page.getByRole('button', { name: '通过并流转' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '打回修改' })).toHaveCount(0)
    await expect(page.getByTestId('review-record-issue')).toBeVisible()
    await expect(chat.getByTestId('paragraph-input')).toHaveCount(1)
    await expect(chat.getByTestId('paragraph-input-attach')).toBeVisible()

    await openArtifacts(page)
    const inspect = page.getByTestId('html-preview-inspect-toggle')
    await expect(page.getByTestId('html-preview-inspect-bar')).toBeVisible()
    await expect(inspect).toContainText('取点标注')
    await inspect.click()
    await expect(inspect).toHaveAttribute('aria-pressed', 'true')
    await page.getByTestId('conversation-panel-close').click()
    await expect(panel).toBeHidden()
    await expect(chat).not.toHaveAttribute('inert')
    await expect(pass).toBeEnabled()
    await pass.click()
    await expect(page.getByTestId('gate-mobile-fill-root')).toHaveAttribute('data-resolved', 'approve')
  })

  test('390×844：产物覆盖层反复打开和返回保留草稿、附件及审批状态', async ({ page }) => {
    await mockApi(page)
    await openFixture(page)
    const input = page.getByTestId('paragraph-input')
    await input.fill('保留这份尚未发送的视觉评审意见')
    await page.getByTestId('review-composer-gate').locator('input[type="file"]').setInputFiles(attachment)
    await expect(page.getByTestId('paragraph-draft-image-thumb')).toBeVisible()
    await expect(page.getByTestId('review-composer-send')).toBeEnabled()
    await expect(page.getByTestId('review-composer-pass')).toBeEnabled()

    for (let i = 0; i < 2; i++) {
      const panel = await openArtifacts(page)
      const [panelBox, chatBox] = await Promise.all([panel.boundingBox(), page.getByTestId('conversation-main').boundingBox()])
      expect(panelBox && chatBox).toBeTruthy()
      expect(Math.abs(panelBox!.x - chatBox!.x)).toBeLessThanOrEqual(1)
      expect(Math.abs(panelBox!.width - chatBox!.width)).toBeLessThanOrEqual(1)
      expect(panelBox!.y + panelBox!.height).toBeLessThanOrEqual(844)
      if (i === 0) await page.getByTestId('conversation-panel-close').click()
      else await page.getByTestId('conversation-panel-close').press('Escape')
      await expect(panel).toBeHidden()
      await expect(page.getByTestId('conversation-panel-toggle')).toBeFocused()
      await expect(input).toHaveValue('保留这份尚未发送的视觉评审意见')
      await expect(page.getByTestId('paragraph-draft-image-thumb')).toBeVisible()
      await expect(page.getByTestId('review-composer-send')).toBeEnabled()
      await expect(page.getByTestId('review-composer-pass')).toBeEnabled()
      await expect(page.getByRole('button', { name: '打回修改' })).toHaveCount(0)
    }
    const pageWidth = await page.evaluate(() => document.documentElement.scrollWidth)
    expect(pageWidth).toBeLessThanOrEqual(390)
  })

  test('390×844：n_open≥1 返回聊天后可继续发送，确认并流转保持禁用', async ({ page }) => {
    const { revisions } = await mockApi(page, [{ id: 'iss-open', runId: 'run-gate-fill', nodeId: 'hg-visual', body: '布局需调整', status: 'open', createdAt: '2026-07-18T00:01:00Z' }])
    await openFixture(page)
    const send = page.getByTestId('review-composer-send')
    const pass = page.getByTestId('review-composer-pass')
    await expect(send).toHaveAttribute('aria-label', '发送')
    await expect(send).not.toContainText('发送')
    await expect(pass).toContainText('确认并流转')
    await expect(pass).toBeDisabled()
    await expect(send).toBeEnabled()
    await openArtifacts(page)
    await page.getByTestId('conversation-panel-close').click()
    await expect(send).toBeEnabled()
    await expect(pass).toBeDisabled()
    await expect(page.getByRole('button', { name: /打回/ })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '通过并流转' })).toHaveCount(0)
    await send.click()
    await expect.poll(() => revisions.length).toBe(1)
    expect(revisions[0]).toEqual({ text: '', images: [], annotations: [] })
    await expect(page.getByTestId('gate-mobile-fill-root')).toHaveAttribute('data-revised', '1')
    await expect(page.getByTestId('gate-mobile-fill-root')).toHaveAttribute('data-resolved', '')
    await expect(pass).toBeDisabled()
  })
})
