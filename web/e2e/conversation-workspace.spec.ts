import { test, expect, type Page } from '@playwright/test'
import { routeApi } from './helpers/apiRoute'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'

async function openConversation(page: Page, width = 1440, theme = 'light') {
  await page.setViewportSize({ width, height: 960 })
  await page.goto(`/workspace.html?view=/assistant&assistant=conversation&theme=${theme}`)
  await page.getByTestId('pm-threads-aside').getByRole('button', { name: /本周研发进展/ }).click()
  await expect(page.getByTestId('chat-message-rail-mark')).toHaveCount(10)
}

test('Solo opens a registered app and the pinned HTML from its thumbnail', async ({ page }) => {
  await installWorkspaceApiDefaults(page)
  const session = { id: 'preview-check', title: 'Solo preview', agentName: 'Demo', status: 'running', artifactRunId: 'solo-preview-check', sandboxId: 42 }
  const runId = session.artifactRunId
  const content = '<!doctype html><html><body><h1>Solo preview smoke</h1></body></html>'
  const artifact = { id: 'art-demo', runId, nodeId: 'solo', name: 'demo.html', kind: 'html', sizeBytes: content.length, revision: 1, createdAt: '2026-11-03T00:00:00Z' }
  const port = { runId, nodeId: 'solo', kind: 'port', port: 8899, label: 'Demo app', proxyUrl: '/preview/solo-preview-check/solo/8899/', healthy: true }
  await routeApi(page, '**/api/solo/sessions**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/solo/sessions') return route.fulfill({ json: { sessions: [session], maxSandboxes: 8 } })
    if (path.endsWith('/messages')) return route.fulfill({ json: [] })
    if (path.endsWith('/resources')) return route.fulfill({
      json: { runId, previewArtifact: artifact.name, artifacts: [artifact], ports: [port, { ...port, port: 5173, label: 'Second app' }] },
    })
    await route.fulfill({ json: session })
  })
  await routeApi(page, '**/api/artifacts/art-demo/content', (route) => route.fulfill({ json: { ...artifact, content } }))
  await routeApi(page, '**/api/artifacts/art-demo/versions', (route) => route.fulfill({ json: [] }))
  await page.routeWebSocket('**/api/solo/sessions/*/chat*', (ws) => ws.send(JSON.stringify({ type: 'session', event: 'queue_state', busy: false, items: [] })))
  await page.routeWebSocket('**/sandbox-vnc/42/ws', (ws) => ws.send(JSON.stringify({ type: 'ready', url: 'http://127.0.0.1:8899/' })))

  await page.setViewportSize({ width: 1440, height: 950 })
  await page.goto(`/workspace-config.html?view=/solo/${session.id}`)
  await page.getByTestId('conversation-resource-toggle').click()
  await page.getByTestId('react-artifact-card-app').click()
  await expect(page.getByTestId('app-preview').getByRole('button', { name: 'Demo app' })).toBeVisible()

  await page.getByTestId('conversation-panel-toggle').click()
  const card = page.getByTestId('react-artifact-card-demo.html')
  await expect(card.locator('iframe')).toBeVisible()
  await card.locator('.artifact-card-cover').click()
  await expect(page.getByTestId('react-artifact-tab-demo.html')).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('react-artifact-preview-demo.html').frameLocator('iframe:visible').getByText('Solo preview smoke')).toBeVisible()
})

for (const theme of ['light', 'dark']) {
  test(`desktop ${theme}: artifact references open beside chat, with deliberate file provenance and preserved draft`, async ({ page }) => {
    await openConversation(page, 1440, theme)
    const input = page.getByTestId('pm-chat-section').locator('textarea')
    await input.fill('保留这条未发送的修改意见')
    await input.evaluate(el => { el.dataset.retained = 'yes' })
    await page.getByTestId('citation-open-source').first().click()
    const panel = page.getByTestId('conversation-artifact-panel')
    await expect(panel).toBeVisible()
    await expect(page.locator('[data-artifact-id]')).toHaveCount(2)
    await expect(page.getByTestId('artifact-preview')).toHaveCount(0)
    await page.locator('[data-artifact-id="art-a1b2c3d4"]').click()
    await expect(panel).toContainText('发布说明')
    const [chatBox, panelBox] = await Promise.all([page.getByTestId('conversation-main').boundingBox(), panel.boundingBox()])
    expect(panelBox!.x).toBeGreaterThanOrEqual(chatBox!.x + chatBox!.width)
    expect(chatBox!.width).toBeGreaterThan(400)
    const sash = page.getByTestId('conversation-panel-sash')
    const previous = Number(await sash.getAttribute('aria-valuenow'))
    await sash.focus()
    await sash.press('ArrowLeft')
    expect(Number(await sash.getAttribute('aria-valuenow'))).toBe(previous + 24)
    await page.getByTestId('conversation-panel-close').click()
    await expect(panel).toBeHidden()
    await expect(input).toHaveValue('保留这条未发送的修改意见')
    await expect(input).toHaveAttribute('data-retained', 'yes')
    await page.getByTestId('citation-open-source').nth(1).click()
    await expect(panel).toContainText('评审结论')
    await expect(panel.getByTestId('conversation-artifact-sources')).toHaveCount(0)
    expect(Number(await sash.getAttribute('aria-valuenow'))).toBe(previous + 24)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

test('navigation previews question and answer, jumps and retains anchor identity after loading earlier history', async ({ page }) => {
  await openConversation(page)
  const mark = page.getByTestId('chat-message-rail-mark').nth(2)
  await expect(mark).toHaveAttribute('data-message-id', 'question-8')
  await mark.hover()
  await expect(page.getByTestId('chat-message-rail-card')).toContainText('第 9 轮')
  await expect(page.getByTestId('chat-message-rail-card')).toContainText('发布配置检查')
  await expect(page.getByTestId('chat-message-rail-card')).not.toContainText('##')
  await expect(page.getByTestId('chat-message-rail-card')).not.toContainText('- 研发')
  await mark.click()
  const target = page.locator('[data-chat-anchor="question-8"]')
  await expect(target).toHaveClass(/chat-message-located/)
  await expect.poll(async () => {
    const [a, b] = await Promise.all([target.boundingBox(), page.getByTestId('pm-message-scroller').boundingBox()])
    return a && b ? a.y >= b.y && a.y < b.y + 80 : false
  }).toBe(true)
  await page.getByTestId('chat-message-rail-earlier').click()
  await expect(page.getByTestId('chat-message-rail-mark')).toHaveCount(16)
  await expect(page.locator('[data-testid="chat-message-rail-mark"][data-message-id="question-8"]')).toHaveCount(1)
  await expect(target).toHaveCount(1)
  await page.locator('[data-testid="chat-message-rail-mark"][data-message-id="question-8"]').click()
  await expect(target).toHaveClass(/chat-message-located/)
})

for (const width of [390, 768]) {
  test(`${width}px: preview returns to mounted chat and draft, with modal focus containment`, async ({ page }) => {
    await openConversation(page, width)
    const input = page.getByTestId('pm-chat-section').locator('textarea')
    await input.fill('返回后继续编辑')
    await input.evaluate(el => { el.dataset.retained = 'yes' })
    await page.getByTestId('citation-open-source').nth(1).click()
    const panel = page.getByTestId('conversation-artifact-panel')
    await expect(panel).toHaveAttribute('role', 'dialog')
    await expect(panel).toHaveAttribute('aria-modal', 'true')
    await expect(page.getByTestId('conversation-main')).toHaveAttribute('inert', '')
    await expect(panel).toContainText('评审结论')
    await page.getByTestId('conversation-panel-close').focus()
    await page.keyboard.press('Shift+Tab')
    expect(await panel.evaluate(el => el.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(panel).toBeHidden()
    await expect(input).toHaveValue('返回后继续编辑')
    await expect(input).toHaveAttribute('data-retained', 'yes')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

for (const width of [1280, 1440]) test(`${width}px: opening, resizing and closing artifacts preserves the passage being read`, async ({ page }) => {
  await openConversation(page, width)
  await page.locator('[data-testid="chat-message-rail-mark"][data-message-id="question-10"]').click()
  const offset = () => page.locator('[data-chat-anchor="question-10"]').evaluate(el => {
    const scroll = el.closest('[data-conversation-scroll]')!
    return el.getBoundingClientRect().top - scroll.getBoundingClientRect().top
  })
  const initial = await offset()
  await page.getByTestId('pm-chat-artifacts').click()
  await expect(page.getByTestId('conversation-panel-sash')).toBeVisible()
  await expect.poll(async () => Math.abs(await offset() - initial)).toBeLessThan(4)
  await page.getByTestId('conversation-panel-sash').press('ArrowLeft')
  await expect.poll(async () => Math.abs(await offset() - initial)).toBeLessThan(4)
  await page.getByTestId('conversation-panel-close').click()
  await expect.poll(async () => Math.abs(await offset() - initial)).toBeLessThan(4)
})

test('Escape dismisses a version menu before the artifact pane and closing the pane leaves no floating menu', async ({ page }) => {
  await openConversation(page)
  await page.getByTestId('citation-open-source').nth(2).click()
  await page.locator('[data-artifact-id="artifact-3"]').click()
  const version = page.getByTestId('artifact-preview-version-chip-btn')
  const menu = page.getByTestId('artifact-preview-version-menu')
  await version.click()
  await expect(menu).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await expect(page.getByTestId('conversation-artifact-panel')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('conversation-artifact-panel')).toBeHidden()
  await page.getByTestId('pm-chat-artifacts').click()
  await version.click()
  await expect(menu).toBeVisible()
  await page.getByTestId('conversation-panel-close').click()
  await expect(menu).toHaveCount(0)
})
